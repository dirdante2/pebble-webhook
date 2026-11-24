package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/joplin"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

var backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Run verarbeitet Aufträge, bis ctx endet. Der Stand liegt je Schritt in der Datenbank,
// ein Neustart setzt also beim letzten fertigen Schritt fort.
func (a *App) Run(ctx context.Context) {
	a.initRing() // vor sortAudio: die Pfade der Einträge stimmen noch
	a.migrateMedia()
	a.sortAudio()
	a.fixItemYears()
	a.fixNoteFilms()
	a.Cleanup()
	lastCleanup := time.Now()
	for {
		for ctx.Err() == nil {
			j, err := a.DB.NextDue(store.Now())
			if err != nil {
				logf("worker: %v", err)
				break
			}
			if j == nil {
				break
			}
			a.process(ctx, j)
		}
		a.AutoClose()
		a.sortPlannings(ctx)
		a.describeIfDue(ctx)
		a.Purge()
		a.pushPending()
		a.remindDue()
		a.trainIfDue(ctx)
		a.learnObservations(ctx)
		// Suchindex nachziehen (beim ersten Mal ganz – bei großem Bestand einige Sekunden), damit die Suche selbst kaum warten muss.
		if err := a.DB.SearchSync(); err != nil {
			logf("Suchindex: %v", err)
		}
		if time.Since(lastCleanup) > 24*time.Hour {
			a.Cleanup()
			lastCleanup = time.Now()
		}
		wait := time.Minute
		if next, err := a.DB.NextWake(); err == nil && next > 0 {
			if d := time.Until(time.UnixMilli(next)); d < wait {
				wait = max(d, time.Second)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-time.After(wait):
		}
	}
}

// process führt die offenen Schritte eines Auftrags aus, bis er fertig ist oder ein Schritt scheitert.
// Wird der Auftrag währenddessen gelöscht, bricht halt die Verarbeitung ab; nichts wird mehr gespeichert.
func (a *App) process(ctx context.Context, j *store.Job) {
	jobCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	a.runMu.Lock()
	a.runID, a.runStop, a.runDone = j.ID, stop, done
	a.runMu.Unlock()
	defer func() {
		a.runMu.Lock()
		a.runID, a.runStop, a.runDone = "", nil, nil
		a.runMu.Unlock()
		stop()
		close(done)
	}()
	for j.State != store.StateDone {
		stepCtx, cancel := context.WithTimeout(jobCtx, 10*time.Minute)
		err := a.step(stepCtx, j)
		cancel()
		if jobCtx.Err() != nil {
			return // Dienst endet oder Auftrag gelöscht: Stand bleibt beim letzten gespeicherten Schritt
		}
		if err != nil {
			j.Attempts++
			j.Error = err.Error()
			var perm permanentError
			var empty emptyError
			if errors.As(err, &empty) {
				// Fehlauslösung ohne Sprache: still vermerken, nach kurzer Frist löschen.
				j.Failed, j.Empty, j.FailedAt = true, true, store.Now()
			} else if errors.As(err, &perm) || j.Attempts >= a.Cfg.MaxAttempts {
				j.Failed, j.FailedAt = true, store.Now()
			} else {
				j.NextTryAt = time.Now().Add(backoff[min(j.Attempts-1, len(backoff)-1)]).UnixMilli()
			}
			logf("Auftrag %s (%s, Schritt nach „%s“): %v", j.ID, j.Mode, j.State, err)
			if serr := a.DB.SaveJob(j); serr != nil {
				logf("worker: speichern: %v", serr)
			}
			a.changed(j.ID)
			// Endgültige Fehler (Kanal „Fehler“); leere Aufnahmen und Testläufe bleiben still.
			if j.Failed && !j.Empty && !j.IsTest {
				title := a.T("Notiz fehlgeschlagen")
				if j.Mode == "planung" {
					title = a.T("Planung fehlgeschlagen")
				}
				a.notify(config.NotifyErrors, title, store.Preview(j.Error, 120), "/eintrag/"+j.ID)
			}
			return
		}
		j.Attempts, j.Error, j.NextTryAt = 0, "", 0
		if err := a.DB.SaveJob(j); err != nil {
			logf("worker: speichern: %v", err)
			return
		}
		a.changed(j.ID)
	}
	a.notifyDone(j)
}

// halt bricht die Verarbeitung ab, wenn der Worker gerade an einem der Aufträge arbeitet, und wartet darauf.
func (a *App) halt(ids ...string) {
	a.runMu.Lock()
	id, stop, done := a.runID, a.runStop, a.runDone
	a.runMu.Unlock()
	if stop == nil || !slices.Contains(ids, id) {
		return
	}
	stop()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		logf("Löschen: Auftrag %s hält nicht an", id)
	}
}

// step führt genau einen Schritt aus und setzt den neuen Stand im Auftrag (gespeichert wird außen).
func (a *App) step(ctx context.Context, j *store.Job) error {
	switch j.State {
	case store.StateNew:
		return a.transcribe(ctx, j)
	case store.StateTranscribed:
		switch j.Mode {
		case "diktat":
			if err := a.analyseDictation(ctx, j); err != nil {
				return err
			}
			j.State = store.StateProcessed
			return nil
		case "planung":
			if err := a.assign(ctx, j); err != nil {
				return err
			}
			j.State = store.StateAssigned
			return nil
		}
		return permanentError{a.T("Kein Aktionsmodus für diesen Auftrag")}
	case store.StateAssigned:
		p, err := a.DB.Planning(j.PlanningID)
		if err != nil {
			return fmt.Errorf("Planung fehlt: %w", err)
		}
		if err := a.research(ctx, j, p); err != nil {
			// Kosten eines gescheiterten Versuchs bleiben am Auftrag stehen.
			return err
		}
		// Nur das Ergebnis schreiben: Schließen/Öffnen während der Recherche bleibt erhalten.
		if err := a.DB.SavePlanResult(p.ID, j.Result, j.Title); err != nil {
			return err
		}
		j.JoplinID = p.JoplinID
		j.State = store.StateProcessed
		return nil
	case store.StateProcessed:
		// Medien (Bilder, Trailer) erst nach dem gespeicherten Ergebnis: es ist schon sichtbar, ein Fehler hier kostet
		// keine neue Auswertung. Was scheitert, steht am Medium.
		if err := a.resolveJobMedia(ctx, j); err != nil {
			return err
		}
		if a.Notes != nil {
			if err := a.pushJob(j); err != nil {
				return err
			}
			j.JoplinPushedAt = store.Now()
		}
		j.State = store.StateDone
		return nil
	}
	return permanentError{a.T("Unbekannter Verarbeitungsstand") + " " + j.State}
}

func (a *App) transcribe(ctx context.Context, j *store.Job) error {
	if j.Mode != "diktat" && j.Mode != "planung" {
		return permanentError{j.Error}
	}
	switch {
	case j.Transcript != "": // Textantwort aus der Web-App
		if j.TranscriptSource == "" {
			j.TranscriptSource = "text"
		}
	case a.Cfg.Transcription == "pebble" && j.Source == "pebble":
		if j.PebbleText == "" {
			return emptyError{a.T("Pebble hat kein Transkript mitgeschickt (TRANSCRIPTION=pebble)")}
		}
		j.Transcript, j.TranscriptSource = j.PebbleText, "pebble"
	default:
		if j.AudioPath == "" || j.AudioDeletedAt != 0 {
			if j.PebbleText == "" {
				return permanentError{a.T("Kein Audio (mehr) vorhanden – Transkription nicht möglich")}
			}
			// Ohne Aufnahme bleibt das Transkript der Pebble-App.
			j.Transcript, j.TranscriptSource = j.PebbleText, "pebble"
			break
		}
		audio, err := a.readAudio(j.AudioPath)
		if err != nil {
			return a.Errorf("Audio nicht lesbar: %w", err)
		}
		name := strings.TrimSuffix(filepath.Base(j.AudioPath), schluessel.AudioExt)
		text, seconds, err := a.AI.Transcribe(ctx, a.Cfg.TranscribeModel, name, audio, j.AudioMime, a.Cfg.Language)
		if err != nil {
			return err
		}
		a.ringSeconds(j.ID, seconds)
		if seconds >= 0 && a.Cfg.PriceTranscribeMin != nil {
			c := seconds / 60 * *a.Cfg.PriceTranscribeMin
			addCost(&j.CostTranscribe, &c)
		}
		switch {
		case text != "":
			j.Transcript, j.TranscriptSource = text, "openai"
		case j.PebbleText != "":
			// OpenAI hört nichts, die Pebble-App hat aber Text erkannt: den nehmen.
			j.Transcript, j.TranscriptSource = j.PebbleText, "pebble"
		default:
			return emptyError{a.T("Die Transkription ist leer – keine Sprache erkannt")}
		}
	}
	j.State = store.StateTranscribed
	return nil
}

// emptyError: die Aufnahme enthält keine Sprache. Endet ohne weitere Versuche und ohne Benachrichtigung.
type emptyError struct{ msg string }

func (e emptyError) Error() string { return e.msg }

// Die Id des Notizbuchs für Notizen leitet sich weiter vom früheren Namen „Diktate“ ab: eine neue Id wäre ein zweites Notizbuch.
func (a *App) folderIDs() (root, dictations, plannings string) {
	return store.DerivedID("ordner:" + a.Cfg.JoplinRoot), store.DerivedID("ordner:" + a.Cfg.JoplinRoot + "/Diktate"),
		store.DerivedID("ordner:" + a.Cfg.JoplinRoot + "/Planungsaufgaben")
}

func (a *App) ensureFolders() error {
	root, dict, plan := a.folderIDs()
	if err := a.Notes.EnsureFolder(root, a.Cfg.JoplinRoot, ""); err != nil {
		return err
	}
	if err := a.Notes.EnsureFolder(dict, "Notizen", root); err != nil {
		return err
	}
	// Bis 15.11.2026 hieß das Notizbuch „Diktate“. Die Id bleibt (sie hängt an den Notizen darin),
	// der Titel wird einmal nachgezogen.
	if done, err := a.DB.Get("joplin_ordner_notizen"); err != nil {
		return err
	} else if done == "" {
		if err := a.Notes.RenameFolder(dict, "Notizen", root); err != nil {
			return err
		}
		if err := a.DB.Set("joplin_ordner_notizen", "1"); err != nil {
			return err
		}
	}
	if err := a.Notes.EnsureFolder(plan, "Planungsaufgaben", root); err != nil {
		return err
	}
	return a.Notes.EnsureFolder(a.listFolderID(), "Listen", root)
}

// pushJob überträgt das fertige Ergebnis eines Auftrags nach Joplin (nur diese Richtung).
func (a *App) pushJob(j *store.Job) (err error) {
	defer func() { a.joplinState(err) }()
	if j.Mode == "planung" {
		p, err := a.DB.Planning(j.PlanningID)
		if err != nil {
			return err
		}
		return a.pushPlanning(p)
	}
	if j.JoplinID == "" {
		// Listenpunkt: steht in der Notiz seiner Liste.
		return a.DB.ClearJobDirty(j.ID)
	}
	if err := a.ensureFolders(); err != nil {
		return err
	}
	_, dict, _ := a.folderIDs()
	if err := a.Notes.PutNote(joplin.Note{ID: j.JoplinID, ParentID: dict, Title: nrPrefix(j.Nr) + j.Title, Body: renderDictation(j),
		CreatedAt: j.RecordedAt, UpdatedAt: store.Now()}); err != nil {
		return err
	}
	if err := a.pushTags("job", j.ID, j.JoplinID); err != nil {
		return err
	}
	return a.DB.ClearJobDirty(j.ID)
}

func (a *App) pushPlanning(p *store.Planning) error {
	plan := parsePlan(p.Plan)
	if plan == nil {
		return permanentError{a.T("Planung ohne Ergebnis")}
	}
	rounds, err := a.DB.Rounds(p.ID)
	if err != nil {
		return err
	}
	if err := a.ensureFolders(); err != nil {
		return err
	}
	_, _, folder := a.folderIDs()
	n := joplin.Note{ID: p.JoplinID, ParentID: folder, Title: nrPrefix(p.Nr) + p.Title, Body: a.renderPlan(plan, rounds), IsTodo: true,
		CreatedAt: p.CreatedAt, UpdatedAt: store.Now()}
	if p.Status == "geschlossen" {
		n.TodoCompleted = p.ClosedAt
	}
	if err := a.Notes.PutNote(n); err != nil {
		return err
	}
	if err := a.pushTags("planung", p.ID, p.JoplinID); err != nil {
		return err
	}
	p.JoplinDirty, p.JoplinPushedAt, p.JoplinError = false, store.Now(), ""
	return a.DB.SavePlanning(p)
}

// pushPending überträgt Planungen mit offenem Joplin-Stand (Schließen, Umhängen) und Löschungen.
func (a *App) pushPending() {
	if a.Notes == nil {
		return
	}
	ps, err := a.DB.DirtyPlannings()
	if err != nil {
		logf("worker: %v", err)
		return
	}
	for _, p := range ps {
		err := a.pushPlanning(p)
		a.joplinState(err)
		if err != nil {
			p.JoplinError = err.Error()
			a.DB.SavePlanning(p)
			logf("Planung %s: Joplin: %v", p.ID, err)
		}
		a.hub.send("planung:" + p.ID)
	}
	// Diktate, deren Tags sich nach der Übertragung geändert haben.
	if dicts, err := a.DB.DirtyDictations(); err == nil {
		for _, j := range dicts {
			err := a.pushJob(j)
			if err != nil {
				logf("Diktat %s: Joplin: %v", j.ID, err)
			}
			a.changed(j.ID)
		}
	}
	if lists, err := a.DB.DirtyLists(); err == nil {
		for _, l := range lists {
			err := a.pushList(l)
			a.joplinState(err)
			if err != nil {
				logf("Liste %s: Joplin: %v", l.ID, err)
			}
		}
	}
	ids, _ := a.DB.JoplinDeletes()
	for _, id := range ids {
		if err := a.Notes.Delete(id); err != nil {
			a.joplinState(err)
			logf("Joplin: Löschen %s: %v", id, err)
			continue
		}
		a.DB.DoneJoplinDelete(id)
	}
}

func (a *App) joplinState(err error) {
	a.joplinMu.Lock()
	defer a.joplinMu.Unlock()
	wasOK := a.joplinErr == ""
	a.joplinAt = store.Now()
	a.joplinErr = ""
	if err != nil {
		a.joplinErr = err.Error()
		// Nur beim Wechsel auf Fehler melden; Wiederholungen bleiben still.
		if wasOK {
			a.notify(config.NotifyErrors, a.T("Joplin-Übertragung fehlgeschlagen"), store.Preview(a.joplinErr, 120), "/status/joplin")
		}
	}
}

func (a *App) notifyDone(j *store.Job) {
	if j.IsTest {
		return
	}
	// Notizen: Kanal „Gespeicherte Einträge“ (Vorgabe aus); Planungen: Kanal „Rückfragen“.
	if j.Mode != "planung" {
		a.notify(config.NotifySaved, a.T("Notiz gespeichert"), j.Title, "/eintrag/"+j.ID)
		return
	}
	title, body := a.T("Planung fertig"), j.Title
	if plan := parsePlan(j.Result); plan != nil && len(plan.OffeneFragen) == 1 {
		title = a.T("Planung fertig · 1 Frage")
	} else if plan != nil && len(plan.OffeneFragen) > 1 {
		title = a.Tf("Planung fertig · %d Fragen", len(plan.OffeneFragen))
	}
	if j.LimitHit {
		body += a.T(" (Kostenlimit erreicht)")
	}
	a.notify(config.NotifyQuestions, title, body, "/planung/"+j.PlanningID)
}

// AutoClose schließt offene Planungen, deren jüngste Runde länger als die Frist zurückliegt.
// Geschlossene Planungen werden nie fortgesetzt; die Joplin-Notiz wird beim nächsten Push abgehakt.
func (a *App) AutoClose() {
	if a.Cfg.AutoCloseDays <= 0 {
		return
	}
	stale, err := a.DB.StalePlannings(time.Now().AddDate(0, 0, -a.Cfg.AutoCloseDays).UnixMilli())
	if err != nil {
		logf("Automatisches Schließen: %v", err)
		return
	}
	for _, p := range stale {
		p.Status, p.ClosedAt, p.ClosedBy, p.JoplinDirty = "geschlossen", store.Now(), "automatik", true
		if err := a.DB.SavePlanning(p); err != nil {
			logf("Automatisches Schließen: %v", err)
			continue
		}
		logf("Planung %s („%s“) nach %d Tagen ohne neue Runde geschlossen", p.ID, store.Preview(p.Title, 60), a.Cfg.AutoCloseDays)
		a.hub.send("planung:" + p.ID)
	}
}

// Cleanup (täglich): abgelaufenen Papierkorb endgültig leeren – nur dort werden Aufnahmen gelöscht –,
// Aufnahmen ohne Eintrag suchen, leere Audio-Ordner entfernen (Backups bereinigt das System).
func (a *App) Cleanup() {
	if _, err := a.migrateLegacyAudio(false); err != nil {
		logf("Aufnahmen bis 2.x: %v", err)
	}
	a.trashExpired()
	a.scanAudio()
	a.removeEmptyDirs(a.audioRoot())
	a.cleanImages()
	a.removeEmptyDirs(a.imageRoot())
}
