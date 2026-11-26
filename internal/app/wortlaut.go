package app

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"pebble-webhook/internal/store"
)

// wordKey vergleicht Wörter ohne Satzzeichen und Groß-/Kleinschreibung.
func wordKey(w string) string {
	return strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
}

func trimPunct(w string) string {
	return strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// wordFixes vergleicht alten und neuen Wortlaut wortweise (längste gemeinsame Folge) und liefert die ersetzten
// Stellen als Paare „falsch → richtig“, je Seite höchstens 4 Wörter. Reine Einfügungen und Streichungen,
// reine Satzzeichen- oder Schreibweisen-Änderungen zählen nicht.
func wordFixes(old, new string) [][2]string {
	a, b := strings.Fields(old), strings.Fields(new)
	if len(a) == 0 || len(b) == 0 || len(a)*len(b) > 4_000_000 {
		return nil
	}
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i, w := range a {
		ka[i] = wordKey(w)
	}
	for i, w := range b {
		kb[i] = wordKey(w)
	}
	// lcs[i][k] = Länge der gemeinsamen Folge von a[i:] und b[k:]
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for k := len(b) - 1; k >= 0; k-- {
			if ka[i] == kb[k] {
				lcs[i][k] = lcs[i+1][k+1] + 1
			} else {
				lcs[i][k] = max(lcs[i+1][k], lcs[i][k+1])
			}
		}
	}
	var out [][2]string
	seen := map[string]bool{}
	var delA, insB []string
	flush := func() {
		if len(delA) > 0 && len(insB) > 0 && len(delA) <= 4 && len(insB) <= 4 {
			from, to := strings.Join(delA, " "), strings.Join(insB, " ")
			if from != "" && to != "" && !strings.EqualFold(from, to) && !seen[from+"\x00"+to] {
				seen[from+"\x00"+to] = true
				out = append(out, [2]string{from, to})
			}
		}
		delA, insB = nil, nil
	}
	i, k := 0, 0
	for i < len(a) || k < len(b) {
		switch {
		case i < len(a) && k < len(b) && ka[i] == kb[k]:
			flush()
			i, k = i+1, k+1
		case k < len(b) && (i == len(a) || lcs[i][k+1] >= lcs[i+1][k]):
			if w := trimPunct(b[k]); w != "" {
				insB = append(insB, w)
			}
			k++
		default:
			if w := trimPunct(a[i]); w != "" {
				delA = append(delA, w)
			}
			i++
		}
	}
	flush()
	return out
}

// handleTranscriptEdit: PUT /api/eintraege/{id}/wortlaut mit {text}. Korrigiert den erkannten Wortlaut und lässt
// die KI neu auswerten (Notiz: Auswertung, Liste, Termin; Planung: nur die letzte Runde, Recherche ab dem Stand davor).
// Ersetzte Wörter werden als Vorschläge ins Gedächtnis gelegt.
func (a *App) handleTranscriptEdit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Text) == "" {
		writeErr(w, 400, "ungueltig", "Text fehlt")
		return
	}
	text := strings.TrimSpace(in.Text)
	if len(text) > 20000 {
		writeErr(w, 400, "ungueltig", "Text zu lang")
		return
	}
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
		return
	}
	if j.Mode != "diktat" && j.Mode != "planung" {
		writeErr(w, http.StatusConflict, "modus", "Nur Notizen und Planungen lassen sich korrigieren")
		return
	}
	if j.State != store.StateDone && !j.Failed {
		writeErr(w, http.StatusConflict, "laeuft", "Wird noch verarbeitet – danach korrigieren")
		return
	}
	if text == j.Transcript {
		writeJSON(w, 200, map[string]any{"ok": true, "geaendert": false, "gelernt": 0})
		return
	}
	planningID := j.PlanningID
	if planningID != "" {
		rounds, err := a.DB.Rounds(planningID)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if len(rounds) > 0 && rounds[len(rounds)-1].ID != j.ID {
			writeErr(w, http.StatusConflict, "nicht_letzte", "Nur die letzte Runde einer Planung lässt sich korrigieren")
			return
		}
	}
	fixes := wordFixes(j.Transcript, text)

	j.Transcript, j.TranscriptSource = text, "nutzer"
	j.Failed, j.Empty, j.FailedAt, j.Attempts, j.NextTryAt, j.Error = false, false, 0, 0, 0, ""
	if j.Mode == "diktat" {
		// Die Punkte der alten Auswertung fallen weg; die neue legt sie wieder an (Projekt-Themen bleiben, wie bei Rückfragen).
		items, err := a.DB.ItemsByJob(j.ID)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		// Der Erledigt-Stand liegt an der Notiz (erledigt_at); die neue Auswertung übernimmt ihn für neue Punkte.
		for _, it := range items {
			if l, err := a.DB.List(it.ListID); err == nil && l.Project != "" {
				continue
			}
			a.DB.DeleteListItem(it.ID)
			a.listChanged(it.ListID)
		}
		j.State = store.StateTranscribed
	} else if planningID != "" {
		// Recherche dieser Runde neu, ab dem Stand der Runde davor.
		addCost(&j.CostAux, j.CostUSD)
		j.Result, j.CostUSD, j.LimitHit, j.State = "", nil, false, store.StateAssigned
	} else {
		j.State = store.StateTranscribed
	}
	if err := a.DB.SaveJob(j); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if planningID != "" {
		if err := a.detach(planningID, 0); err != nil {
			logf("Wortlaut korrigiert: Planung %s: %v", planningID, err)
		}
		a.hub.send("planung:" + planningID)
	}
	lines := make([]string, 0, len(fixes))
	for _, f := range fixes[:min(len(fixes), 10)] {
		lines = append(lines, fmt.Sprintf("Wortlaut berichtigt: „%s“ → „%s“.", f[0], f[1]))
	}
	// Nur Beobachtungen: was davon ins Gedächtnis gehört, entscheidet die KI (learnObservations).
	a.observe("korrektur", lines...)
	learned := len(lines)
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"ok": true, "geaendert": true, "gelernt": learned, "begriffe_frage": a.termQuestions(fixes)})
}

// termQuestions: Wörter, nach denen die Web-App nach einer Korrektur fragt („in die Begriffsliste aufnehmen?“) –
// nur wo genau ein Wort durch ein anderes ersetzt wurde. Neu ist ein Wort, das noch nirgends steht; steht es schon
// als Begriff in der Liste (und wurde trotzdem wieder falsch erkannt), fragt die Seite, ob es Favorit werden soll.
// Abgelehnte, gelöschte und Favoriten bleiben ohne Frage.
func (a *App) termQuestions(fixes [][2]string) []map[string]any {
	out := []map[string]any{}
	for _, f := range fixes {
		t := cleanTerm(f[1])
		if strings.ContainsAny(f[0], " ") || strings.ContainsAny(f[1], " ") || len([]rune(t)) < 2 || len(out) == 5 {
			continue
		}
		if strings.IndexFunc(t, unicode.IsLetter) < 0 {
			continue // reine Zahl
		}
		known := false
		if e, err := a.DB.MemoryByText(t); err == nil {
			if e.Status != store.MemoryTerm {
				continue
			}
			t, known = e.Text, true
		}
		out = append(out, map[string]any{"wort": t, "vorhanden": known})
	}
	return out
}

// handleCounters: GET /api/zaehler – Zahlen für die Navigation (offene Gedächtnis-Vorschläge).
func (a *App) handleCounters(w http.ResponseWriter, r *http.Request) {
	n, err := a.DB.CountMemory(store.MemoryProposed)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	tj, _ := a.DB.TrashedJobs(0)
	tp, _ := a.DB.TrashedPlannings(0)
	out := map[string]any{"vorschlaege": n, "update": a.AvailableUpdate(), "papierkorb": len(tj) + len(tp),
		"version": strings.Fields(a.Version + " ")[0], "kanal": a.installedChannel(), "joplin": a.Cfg.JoplinEnabled, "audio_unbekannt": len(a.orphans()),
		"version_bestaetigt": a.confirmedVersion(), "ring": a.ringSummary(), "spenden": a.donateURL()}
	if v := a.System.fullestStorage(); v != nil {
		out["speicher_voll"] = v
	}
	// menue=1: Zahlen des Seitenmenüs (Verlauf-Filter, Neu, Listen-Gruppen), damit es ohne Seitenwechsel stimmt.
	if r.URL.Query().Get("menue") == "1" {
		m, err := a.menuCounts(r)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		out["menue"] = m
	}
	writeJSON(w, 200, out)
}

// versionConfirmedKey: zuletzt bestätigte Version „<version>|<kanal>“ – für alle Geräte gemeinsam (ein Nutzer).
const versionConfirmedKey = "version-bestaetigt"

// runningVersion: laufende Version mit Kanal, so wie sie bestätigt wird.
func (a *App) runningVersion() string {
	return strings.Fields(a.Version + " ")[0] + "|" + a.installedChannel()
}

// confirmedVersion: bestätigte Version; beim ersten Mal gilt die laufende als bestätigt (keine Karte nach der Einrichtung).
func (a *App) confirmedVersion() string {
	v, _ := a.DB.Get(versionConfirmedKey)
	if v == "" {
		v = a.runningVersion()
		a.DB.Set(versionConfirmedKey, v)
	}
	return v
}

// handleVersionConfirm: „Bestätigen“ auf der Karte nach einem Update – gilt für alle Geräte.
func (a *App) handleVersionConfirm(w http.ResponseWriter, r *http.Request) {
	if err := a.DB.Set(versionConfirmedKey, a.runningVersion()); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	a.hub.send("version")
	writeJSON(w, 200, map[string]any{"version_bestaetigt": a.runningVersion()})
}
