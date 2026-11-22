package app

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "fehler": i18n.T(msg)})

}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// status fasst Stand und Fehlerzustand eines Auftrags für die Anzeige zusammen.
func jobStatus(j *store.Job) string {
	switch {
	case j.Empty:
		return "leer"
	case j.Failed:
		return "fehler"
	case j.State == store.StateDone:
		return "fertig"
	case j.Error != "":
		return "wiederholung"
	}
	return "in_arbeit"
}

var stepLabel = map[string]string{
	store.StateNew:         "Transkription",
	store.StateTranscribed: "Auswertung",
	store.StateAssigned:    "Recherche",
	store.StateProcessed:   "Übertragung nach Joplin",
}

func audioState(j *store.Job) string {
	switch {
	case j.AudioPath == "":
		return "keins"
	case j.AudioDeletedAt != 0:
		return "abgelaufen"
	}
	return "da"
}

func (a *App) jobSummary(j *store.Job) map[string]any {
	// Das Datum steht in der Web-App unter dem Titel, nicht darin.
	title := shortTitle(j.Title)
	if title == "" {
		title = store.Preview(j.Transcript, 60)
	}
	m := map[string]any{
		"gelesen": j.ReadAt != 0, "loeschbar": true,
		"typ": "eintrag", "id": j.ID, "modus": j.Mode, "quelle": j.Source, "test": j.IsTest, "aufgenommen": j.RecordedAt,
		"eingegangen": j.ReceivedAt, "titel": title, "vorschau": store.Preview(j.Transcript, 160), "status": jobStatus(j),
		"planung": j.PlanningID, "runde": j.RoundNo, "geaendert": j.UpdatedAt,
	}
	if j.PlanningID == "" {
		a.addTags(m, "job", j.ID)
	}
	if j.Nr > 0 {
		m["nr"] = j.Nr
	}
	if at := a.purgeAt(j); at != 0 {
		m["loeschung"] = at
	}
	if j.TerminAt > 0 {
		m["termin"] = appointmentSummary(j, store.Now())
	}
	if j.Mode == "diktat" && j.PlanningID == "" {
		m["erledigt"] = j.DoneAt
	}
	if j.Mode == "diktat" {
		if d := parseDictation(j.Result); d != nil && j.State == store.StateDone && !j.Failed && len(d.Fragen) > 0 {
			m["fragen"] = len(d.Fragen)
		}
		if l, items := a.jobList(j.ID); l != nil {
			// erledigt: Zeitpunkt des letzten Hakens, sobald alle Punkte der Notiz abgehakt sind; sonst 0.
			var done int64
			for _, it := range items {
				if it.DoneAt == 0 {
					done = 0
					break
				}
				done = max(done, it.DoneAt)
			}
			m["liste"] = map[string]any{"id": l.ID, "nr": l.Nr, "name": l.Name, "projekt": l.Project, "punkte": len(items), "erledigt": done}
		}
	}
	if j.PlanningID != "" {
		// Schließen oder Umhängen ändert die Planung, nicht den Auftrag: der Zwischenspeicher der Web-App soll es merken.
		if p, err := a.DB.Planning(j.PlanningID); err == nil {
			// Eine Runde läuft unter der Nummer ihrer Planung.
			if p.Nr > 0 {
				m["nr"] = p.Nr
			}
			if p.UpdatedAt > j.UpdatedAt {
				m["geaendert"] = p.UpdatedAt
			}
		}
	}
	if j.State != store.StateDone {
		m["schritt"] = i18n.T(stepLabel[j.State])
	}
	if j.Error != "" {
		m["fehler"] = j.Error
	}
	return m
}

// planningSummary fasst eine Planung für den Verlauf zusammen: ein Eintrag mit dem Stand der jüngsten Runde.
func (a *App) planningSummary(p *store.Planning, rounds []*store.Job) map[string]any {
	last := rounds[len(rounds)-1]
	title := p.Title
	if title == "" {
		title = store.Preview(rounds[0].Transcript, 60)
	}
	changed := p.UpdatedAt
	read := true
	for _, r := range rounds {
		changed = max(changed, r.UpdatedAt)
		read = read && r.ReadAt != 0
	}
	m := map[string]any{
		"gelesen": read, "loeschbar": true,
		"typ": "planung", "id": p.ID, "modus": "planung", "titel": title, "planung_status": p.Status, "runden": len(rounds),
		"aufgenommen": last.RecordedAt, "eingegangen": last.ReceivedAt, "vorschau": store.Preview(last.Transcript, 160),
		"quelle": last.Source, "status": jobStatus(last), "geaendert": changed,
	}
	if p.Nr > 0 {
		m["nr"] = p.Nr
	}
	if last.State != store.StateDone {
		m["schritt"] = i18n.T(stepLabel[last.State])
	}
	if last.Error != "" {
		m["fehler"] = last.Error
	}
	if at := a.purgeAt(last); at != 0 {
		m["loeschung"] = at
	}
	m["fragen"] = a.openQuestions(p, rounds)
	if items, err := a.DB.ItemsByPlanning(p.ID); err == nil && len(items) > 0 {
		if l, err := a.DB.List(items[0].ListID); err == nil {
			m["liste"] = map[string]any{"id": l.ID, "nr": l.Nr, "name": l.Name, "projekt": l.Project, "erledigt": items[0].DoneAt}
		}
	}
	a.addTags(m, "planung", p.ID)
	return m
}

// addTags hängt Tags und offene KI-Vorschläge an eine Zusammenfassung.
func (a *App) addTags(m map[string]any, kind, id string) {
	set, err := a.DB.Tags(kind, id)
	if err != nil {
		return
	}
	if set.Tags == nil {
		set.Tags = []string{}
	}
	if set.Suggestions == nil {
		set.Suggestions = []string{}
	}
	m["tags"], m["tag_vorschlaege"] = set.Tags, set.Suggestions
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("vor"), 10, 64)
	kind := r.URL.Query().Get("art")
	// Offene Rückfragen über alle offenen Planungen: Zahl für den Filterknopf, bei art=fragen auch die Liste.
	total := 0
	var asking []map[string]any
	if open, err := a.DB.OpenPlannings(); err == nil {
		for _, p := range open {
			rounds, err := a.DB.Rounds(p.ID)
			if err != nil {
				continue
			}
			if n := a.openQuestions(p, rounds); n > 0 {
				total += n
				if kind == "fragen" {
					asking = append(asking, a.planningSummary(p, rounds))
				}
			}
		}
	}
	// Dazu die Rückfragen an einzelnen Notizen.
	if jobs, err := a.DB.JobsAsking(); err == nil {
		for _, j := range jobs {
			if d := parseDictation(j.Result); d != nil && len(d.Fragen) > 0 {
				total += len(d.Fragen)
				if kind == "fragen" {
					asking = append(asking, a.jobSummary(j))
				}
			}
		}
	}
	// Tag-Filter: mehrere tag=… gelten zusammen (UND).
	var tags []string
	for _, t := range r.URL.Query()["tag"] {
		if t = cleanTag(t); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	next, missed, err := a.nextAppointment()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	upcoming, err := a.DB.Appointments(false)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	// Badge am nächsten Termin: offene Termine, die in den nächsten 24 Stunden beginnen.
	soon, now := 0, store.Now()
	for _, j := range upcoming {
		if j.TerminAt >= now && j.TerminAt < now+24*3600_000 {
			soon++
		}
	}
	termine := map[string]any{"naechster_termin": next, "termine_verpasst": missed, "termine_offen": len(upcoming), "termine_24h": soon}
	// Termine: der früheste zuerst, verpasste unbestätigte vorn; mit alle=1 auch bestätigte.
	if kind == "termin" {
		all, err := a.DB.Appointments(true)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		// aeltere = bestätigte Termine, die erst mit alle=1 kommen.
		list, done := upcoming, len(all)-len(upcoming)
		if r.URL.Query().Get("alle") == "1" {
			list, done = all, 0
		}
		out := []map[string]any{}
	termine:
		for _, j := range list {
			m := a.jobSummary(j)
			t, _ := m["tags"].([]string)
			for _, want := range tags {
				if !slices.Contains(t, want) {
					continue termine
				}
			}
			out = append(out, m)
		}
		resp := map[string]any{"eintraege": out, "mehr": false, "fragen_offen": total, "aeltere": done, "besuch_pause_minuten": a.Cfg.VisitGapMinutes}
		maps.Copy(resp, termine)
		writeJSON(w, 200, resp)
		return
	}
	if kind == "fragen" {
		if asking == nil {
			asking = []map[string]any{}
		}
		sort.Slice(asking, func(i, k int) bool { return asking[i]["eingegangen"].(int64) > asking[k]["eingegangen"].(int64) })
		resp := map[string]any{"eintraege": asking, "mehr": false, "fragen_offen": total, "aeltere": 0, "besuch_pause_minuten": a.Cfg.VisitGapMinutes}
		maps.Copy(resp, termine)
		writeJSON(w, 200, resp)
		return
	}
	if kind != "diktat" && kind != "planung" {
		kind = ""
	}
	// Ohne alle=1: die letzten 24 Stunden, mindestens aber die jüngsten HistoryMinEntries Einträge,
	// dazu alles seit zuletzt gesehen (seit=, vom Gerät: letzte Aktion des vorigen Besuchs). since geht als grenze an die Web-App.
	// Das Fenster hängt vom Filter ab (je Art die eigenen jüngsten Einträge); die Zahl jedes Filterknopfs
	// nutzt deshalb das Fenster seiner Art, sonst zeigt ein Knopf 0 und seine Ansicht doch Einträge.
	showAll := r.URL.Query().Get("alle") == "1"
	seit, _ := strconv.ParseInt(r.URL.Query().Get("seit"), 10, 64)
	window := func(kind string) (int64, error) {
		if showAll {
			return 0, nil
		}
		since := store.Now() - 24*3600_000
		if seit > 0 {
			since = min(since, seit)
		}
		latest, err := a.DB.HistoryRefs(kind, tags, 0, a.Cfg.HistoryMinEntries, 0)
		if err != nil {
			return 0, err
		}
		if len(latest) > 0 && len(latest) == a.Cfg.HistoryMinEntries {
			since = min(since, latest[len(latest)-1].At)
		} else if len(latest) > 0 {
			since = 0 // weniger Einträge als das Minimum: alle zeigen
		}
		return since, nil
	}
	since, err := window(kind)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	refs, err := a.DB.HistoryRefs(kind, tags, before, 51, since)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	// Zahlen der Filterknöpfe: offene Einträge im Fenster ihrer Art; aeltere = was die Standardansicht ausblendet.
	counts := map[string]any{"fragen": total, "termin": len(upcoming)}
	for name, k := range map[string]string{"alles": "", "diktat": "diktat", "planung": "planung"} {
		ks, err := window(k)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		n, err := a.DB.HistoryOpenCount(k, tags, ks)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		counts[name] = n
	}
	// neu: noch nicht aufgerufene Einträge seit zuletzt gesehen (seit=) je Filter – die Web-App zeigt sie gelb im Menü.
	newCounts := map[string]int{}
	if seit, err := strconv.ParseInt(r.URL.Query().Get("seit"), 10, 64); err == nil && seit > 0 {
		for name, k := range map[string]string{"alles": "", "diktat": "diktat", "termin": "termin", "planung": "planung"} {
			n, err := a.DB.HistoryUnreadCount(k, tags, seit)
			if err != nil {
				writeErr(w, 500, "speicher", "Datenbankfehler")
				return
			}
			newCounts[name] = n
		}
	}
	older := 0
	if since > 0 {
		all, err := a.DB.HistoryCount(kind, tags, 0)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		shown, _ := a.DB.HistoryCount(kind, tags, since)
		older = all - shown
	}
	more := len(refs) > 50
	if more {
		refs = refs[:50]
	}
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == "job" {
			if j, err := a.DB.Job(ref.ID); err == nil {
				out = append(out, a.jobSummary(j))
			}
			continue
		}
		p, err := a.DB.Planning(ref.ID)
		if err != nil {
			continue
		}
		if rounds, err := a.DB.Rounds(p.ID); err == nil && len(rounds) > 0 {
			out = append(out, a.planningSummary(p, rounds))
		}
	}
	resp := map[string]any{"eintraege": out, "mehr": more, "fragen_offen": total, "aeltere": older, "zahlen": counts, "neu": newCounts, "besuch_pause_minuten": a.Cfg.VisitGapMinutes, "grenze": since}
	maps.Copy(resp, termine)
	writeJSON(w, 200, resp)
}

// handleMarkRead merkt einen Eintrag oder eine ganze Planung als gelesen. Eigener Aufruf statt Nebenwirkung
// des Lesens: die Web-App lädt Details auch im Hintergrund vor.
func (a *App) handleMarkRead(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var changed bool
		var err error
		if kind == "planung" {
			changed, err = a.DB.MarkPlanningRead(id)
		} else {
			changed, err = a.DB.MarkJobRead(id)
		}
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if changed {
			if kind == "planung" {
				a.hub.send("planung:" + id)
			} else {
				a.changed(id)
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// handlePlanning liefert eine Planung als Ganzes: aktueller Stand und alle Runden, die jüngste zuerst.
func (a *App) handlePlanning(w http.ResponseWriter, r *http.Request) {
	p, err := a.DB.Planning(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Planung nicht gefunden")
		return
	}
	rounds, err := a.DB.Rounds(p.ID)
	if err != nil || len(rounds) == 0 {
		writeErr(w, 404, "fehlt", "Planung nicht gefunden")
		return
	}
	m := a.planningSummary(p, rounds)
	m["geschlossen"] = p.ClosedAt
	m["geschlossen_von"] = p.ClosedBy
	m["schliesst_nach_tagen"] = a.Cfg.AutoCloseDays
	m["limit_usd"] = a.Cfg.ResearchLimitUSD
	m["joplin"] = a.joplinInfo(p.JoplinID, p.JoplinPushedAt, p.JoplinDirty, p.JoplinError)
	total, known := 0.0, false
	current := -1 // jüngste Runde mit Ergebnis = aktueller Stand
	for i, rd := range rounds {
		if rd.Result != "" {
			current = i
		}
	}
	list := make([]map[string]any, 0, len(rounds))
	for i := len(rounds) - 1; i >= 0; i-- {
		rd := rounds[i]
		rm := map[string]any{"id": rd.ID, "nr": rd.RoundNo, "zeit": rd.RecordedAt, "quelle": rd.Source, "transkript": rd.Transcript,
			"transkript_quelle": rd.TranscriptSource, "pebble_text": rd.PebbleText, "status": jobStatus(rd), "versuche": rd.Attempts,
			"naechster_versuch": rd.NextTryAt, "limit_erreicht": rd.LimitHit, "zuordnung": rd.AssignHow, "audio": audioState(rd),
			"letzte": i == len(rounds)-1, "aktuell": i == current, "loeschbar": true}
		if rd.State != store.StateDone {
			rm["schritt"] = i18n.T(stepLabel[rd.State])
		}
		if rd.Error != "" {
			rm["fehler"] = rd.Error
		}
		if plan := parsePlan(rd.Result); plan != nil {
			rm["plan"] = plan
		}
		if rd.CostTranscribe != nil || rd.CostAux != nil || rd.CostUSD != nil {
			c := 0.0
			for _, v := range []*float64{rd.CostTranscribe, rd.CostAux, rd.CostUSD} {
				if v != nil {
					c += *v
				}
			}
			rm["kosten_usd"] = c
			total += c
			known = true
		}
		list = append(list, rm)
	}
	m["rundenliste"] = list
	if known {
		m["kosten_usd"] = total
	}
	if plan := parsePlan(p.Plan); plan != nil {
		m["plan"] = plan
		m["text"] = "# " + plan.Titel + "\n\n" + a.renderPlan(plan, rounds)
	}
	writeJSON(w, 200, m)
}

func (a *App) joplinInfo(id string, pushedAt int64, dirty bool, errMsg string) map[string]any {
	if a.Notes == nil {
		return map[string]any{"aktiv": false}
	}
	return map[string]any{"aktiv": true, "id": id, "uebertragen": pushedAt, "offen": pushedAt == 0 || dirty, "fehler": errMsg}
}

func (a *App) handleJob(w http.ResponseWriter, r *http.Request) {
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
		return
	}
	m := a.jobSummary(j)
	m["transkript"] = j.Transcript
	m["transkript_quelle"] = j.TranscriptSource
	m["pebble_text"] = j.PebbleText
	m["audio"] = audioState(j)
	m["zuordnung"] = j.AssignHow
	m["versuche"] = j.Attempts
	m["naechster_versuch"] = j.NextTryAt
	m["limit_erreicht"] = j.LimitHit
	m["limit_usd"] = a.Cfg.ResearchLimitUSD
	if j.CostUSD != nil {
		m["kosten_usd"] = *j.CostUSD
	}
	text := j.Transcript
	switch j.Mode {
	case "diktat":
		if d := parseDictation(j.Result); d != nil {
			m["analyse"] = d
		}
		if j.JoplinID != "" {
			m["joplin"] = a.joplinInfo(j.JoplinID, j.JoplinPushedAt, false, "")
			text = renderDictation(j)
		}
		if l, items := a.jobList(j.ID); l != nil {
			list := make([]map[string]any, 0, len(items))
			for _, it := range items {
				list = append(list, itemJSON(it))
			}
			m["liste"] = map[string]any{"id": l.ID, "nr": l.Nr, "name": l.Name, "projekt": l.Project, "punkte": list}
		}
	case "planung":
		if p, err := a.DB.Planning(j.PlanningID); err == nil {
			rounds, _ := a.DB.Rounds(p.ID)
			rs := []map[string]any{}
			last := ""
			for _, rd := range rounds {
				rs = append(rs, map[string]any{"id": rd.ID, "nr": rd.RoundNo, "zeit": rd.RecordedAt, "quelle": rd.Source,
					"transkript": rd.Transcript, "status": jobStatus(rd), "limit_erreicht": rd.LimitHit})
				last = rd.ID
			}
			pm := map[string]any{"id": p.ID, "eintrag_nr": p.Nr, "titel": p.Title, "status": p.Status, "geschlossen": p.ClosedAt, "runden": rs,
				"letzte_runde": last == j.ID, "aktuell": p.Plan == j.Result && j.Result != ""}
			// Gezeigt wird der Stand nach dieser Runde; der aktuelle Stand hängt an der letzten Runde.
			if plan := parsePlan(j.Result); plan != nil {
				pm["plan"] = plan
				text = "# " + plan.Titel + "\n\n" + a.renderPlan(plan, rounds)
			}
			m["planung_daten"] = pm
			m["joplin"] = a.joplinInfo(p.JoplinID, p.JoplinPushedAt, p.JoplinDirty, p.JoplinError)
		}
	}
	m["text"] = text
	writeJSON(w, 200, m)
}

func (a *App) handleAudio(w http.ResponseWriter, r *http.Request) {
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil || j.AudioPath == "" {
		writeErr(w, 404, "fehlt", "Keine Aufnahme")
		return
	}
	if j.AudioDeletedAt != 0 {
		writeErr(w, http.StatusGone, "abgelaufen", "Aufnahme abgelaufen")
		return
	}
	f, err := os.Open(j.AudioPath)
	if err != nil {
		writeErr(w, 404, "fehlt", "Aufnahme nicht lesbar")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", j.AudioMime)
	// Eine Aufnahme ändert sich nie: einmal geladen, kommt sie 30 Tage aus dem Browser-Speicher.
	w.Header().Set("Cache-Control", "private, max-age=2592000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (a *App) handleRestart(w http.ResponseWriter, r *http.Request) {
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
		return
	}
	if !j.Failed && j.Error == "" {
		writeErr(w, http.StatusConflict, "laeuft", "Der Auftrag ist nicht fehlgeschlagen")
		return
	}
	if j.Mode != "diktat" && j.Mode != "planung" {
		writeErr(w, http.StatusConflict, "modus", "Für diese Geste ist keine Aktion zugeordnet")
		return
	}
	// Erfolgreiche Zwischenstände bleiben; nur der gescheiterte Schritt läuft neu.
	j.Failed, j.Empty, j.FailedAt, j.Attempts, j.NextTryAt, j.Error = false, false, 0, 0, 0, ""
	if j.LimitHit && j.State == store.StateAssigned {
		// Bewusster Neustart: neues Kostenlimit für diesen Lauf; das Ausgegebene bleibt in der Kostenübersicht.
		addCost(&j.CostAux, j.CostUSD)
		j.CostUSD, j.LimitHit = nil, false
	}
	if err := a.DB.SaveJob(j); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleOpenPlannings(w http.ResponseWriter, r *http.Request) {
	ps, err := a.DB.OpenPlannings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, p := range ps {
		vorhaben := ""
		if plan := parsePlan(p.Plan); plan != nil {
			vorhaben = store.Preview(plan.Vorhaben, 160)
		}
		out = append(out, map[string]any{"id": p.ID, "nr": p.Nr, "titel": p.Title, "vorhaben": vorhaben, "geaendert": p.UpdatedAt})
	}
	writeJSON(w, 200, map[string]any{"planungen": out})
}

// handleReassign hängt die letzte Runde einer Planung an eine andere offene Planung oder macht sie zur neuen.
func (a *App) handleReassign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ziel string `json:"ziel"`
	}
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
		return
	}
	if readJSON(r, &in) != nil || in.Ziel == "" || in.Ziel == j.PlanningID {
		writeErr(w, 400, "ungueltig", "Ziel fehlt")
		return
	}
	if j.Mode != "planung" || (j.State != store.StateDone && !j.Failed) || j.Transcript == "" {
		writeErr(w, http.StatusConflict, "laeuft", "Der Auftrag wird noch verarbeitet")
		return
	}
	if in.Ziel != "neu" {
		if p, err := a.DB.Planning(in.Ziel); err != nil || p.Status != "offen" {
			writeErr(w, http.StatusConflict, "geschlossen", "Die Ziel-Planung ist nicht mehr offen")
			return
		}
	}
	old := j.PlanningID
	if old != "" {
		rounds, err := a.DB.Rounds(old)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if len(rounds) > 0 && rounds[len(rounds)-1].ID != j.ID {
			writeErr(w, http.StatusConflict, "nicht_letzte", "Nur die letzte Runde einer Planung lässt sich umhängen")
			return
		}
	}
	j.PlanningID, j.RoundNo, j.ForcePlanning, j.AssignHow = "", 0, in.Ziel, "umgehaengt"
	j.State, j.Failed, j.FailedAt, j.Attempts, j.NextTryAt, j.Error = store.StateTranscribed, false, 0, 0, 0, ""
	addCost(&j.CostAux, j.CostUSD) // die verworfene Recherche bleibt in der Kostenübersicht
	j.Result, j.CostUSD, j.LimitHit, j.JoplinID, j.JoplinPushedAt = "", nil, false, "", 0
	if err := a.DB.SaveJob(j); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if old != "" {
		if err := a.detach(old, 0); err != nil {
			logf("Umhängen: alte Planung %s: %v", old, err)
		}
	}
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// detach bringt eine Planung nach dem Entfernen ihrer letzten Runde auf den Stand davor; ohne verbleibende Runde
// verschwindet sie samt Joplin-Notiz: mit trashAt > 0 in den Papierkorb (Stempel der gelöschten Runde), mit 0 endgültig.
// Nach dem Wiederherstellen einer Runde zieht detach den Stand ebenso auf die jüngste Runde mit Ergebnis.
func (a *App) detach(planningID string, trashAt int64) error {
	p, err := a.DB.Planning(planningID)
	if err != nil {
		return err
	}
	rounds, err := a.DB.Rounds(planningID)
	if err != nil {
		return err
	}
	prev := ""
	for _, r := range rounds {
		if r.Result != "" {
			prev = r.Result
		}
	}
	if prev == "" && len(rounds) > 0 {
		// Es bleiben nur Runden ohne Ergebnis (noch in Arbeit oder gescheitert): Planung ohne Stand behalten.
		p.Plan, p.JoplinDirty = "", false
		return a.DB.SavePlanning(p)
	}
	if prev == "" && trashAt > 0 {
		return a.trashPlanning(p, trashAt)
	}
	if prev == "" {
		// Die Planung verschwindet: ihr Punkt in einer Liste auch.
		if items, err := a.DB.ItemsByPlanning(planningID); err == nil {
			for _, it := range items {
				a.DB.TouchList(it.ListID)
				a.hub.send("liste:" + it.ListID)
			}
		}
		if err := a.DB.DeleteItemsOf("", planningID); err != nil {
			return err
		}
		if p.JoplinPushedAt != 0 {
			if err := a.DB.AddJoplinDelete(p.JoplinID); err != nil {
				return err
			}
		}
		return a.DB.DeletePlanning(planningID)
	}
	p.Plan, p.JoplinDirty = prev, true
	if plan := parsePlan(prev); plan != nil {
		p.Title = plan.Titel
	}
	return a.DB.SavePlanning(p)
}

func (a *App) handleClose(w http.ResponseWriter, r *http.Request) {
	p, err := a.DB.Planning(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Planung nicht gefunden")
		return
	}
	if p.Status != "geschlossen" {
		p.Status, p.ClosedAt, p.ClosedBy, p.JoplinDirty = "geschlossen", store.Now(), "nutzer", true
		if err := a.DB.SavePlanning(p); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		a.hub.send("planung:" + p.ID)
		a.Wake()
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleAnswer nimmt eine Antwort aus der Web-App an (Text oder Aufnahme) – als neue Runde
// einer offenen Planung oder, ohne Planungs-Id, als neue Planung.
func (a *App) handleAnswer(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.HookMaxBytes)
	if err := r.ParseMultipartForm(a.Cfg.HookMaxBytes); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "gross", "Antwort zu groß oder nicht lesbar")
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if clientID == "" || len(clientID) > 100 {
		writeErr(w, 400, "ungueltig", "client_id fehlt")
		return
	}
	delivery := "web:" + clientID
	if old, err := a.DB.JobByDelivery(delivery); err == nil {
		writeJSON(w, 200, map[string]any{"id": old.ID, "bekannt": true})
		return
	}
	force := "neu"
	if target != "" {
		p, err := a.DB.Planning(target)
		if err != nil {
			writeErr(w, http.StatusConflict, "geschlossen", "Die Planung gibt es nicht mehr")
			return
		}
		if p.Status != "offen" {
			writeErr(w, http.StatusConflict, "geschlossen", "Die Planung wurde inzwischen geschlossen")
			return
		}
		force = p.ID
	}
	now := store.Now()
	j := &store.Job{ID: store.NewID(), DeliveryID: delivery, Source: "web", Mode: "planung", ReceivedAt: now, RecordedAt: now,
		State: store.StateNew, ForcePlanning: force, Transcript: strings.TrimSpace(r.FormValue("text"))}
	if ms, err := strconv.ParseInt(r.FormValue("zeit"), 10, 64); err == nil && ms > 0 && ms <= now {
		j.RecordedAt = ms
	}
	if f, hdr, err := r.FormFile("audio"); err == nil {
		data, rerr := io.ReadAll(f)
		f.Close()
		if rerr != nil || len(data) == 0 {
			writeErr(w, 400, "ungueltig", "Aufnahme nicht lesbar")
			return
		}
		mimeType := hdr.Header.Get("Content-Type")
		if !strings.HasPrefix(mimeType, "audio/") && !strings.HasPrefix(mimeType, "video/") {
			writeErr(w, 400, "ungueltig", "Die Datei ist keine Aufnahme")
			return
		}
		path, err := a.saveAudio(j.ID, j.Mode, data, mimeType)
		if err != nil {
			logf("Antwort: Audio speichern: %v", err)
			writeErr(w, http.StatusInsufficientStorage, "speicher", "Speicher nicht verfügbar")
			return
		}
		j.AudioPath, j.AudioMime, j.Transcript = path, mimeType, ""
	}
	if j.Transcript == "" && j.AudioPath == "" {
		writeErr(w, 400, "leer", "Antwort ist leer")
		return
	}
	created, err := a.DB.InsertJob(j)
	if err != nil || !created {
		if j.AudioPath != "" {
			os.Remove(j.AudioPath)
		}
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		old, _ := a.DB.JobByDelivery(delivery)
		writeJSON(w, 200, map[string]any{"id": old.ID, "bekannt": true})
		return
	}
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"id": j.ID})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	counts, err := a.DB.Counts()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := map[string]any{
		"version": a.Version, "audio_unbekannt": a.scanAudio(), "auftraege": counts.Total, "audio_dateien": a.audioStats(), "auftraege_teile": map[string]int{"audio": counts.Audio, "web": counts.Web, "nur_text": counts.TextOnly, "notizen": counts.Notes, "runden": counts.Rounds, "termine": counts.Appointments, "planungen": counts.Plannings}, "warteschlange": counts.Open, "fehlgeschlagen": counts.Failed,
		"einstellungen": map[string]any{
			"halten": a.Cfg.TriggerSingle, "doppelklick_halten": a.Cfg.TriggerDouble, "transkription": a.Cfg.Transcription,
			"transkriptions_modell": a.Cfg.TranscribeModel, "recherche_modell": a.Cfg.PlanModel,
			"diktat_modell": a.Cfg.DictationModel, "leer_stunden": a.Cfg.EmptyRetentionHours,
			"limit_usd": a.Cfg.ResearchLimitUSD, "papierkorb_tage": a.Cfg.TrashRetentionDays, "schliessen_tage": a.Cfg.AutoCloseDays,
			"listen_aktiv_minuten": a.Cfg.DoneActiveMinutes, "besuch_pause_minuten": a.Cfg.VisitGapMinutes, "preise_bekannt": a.Cfg.PricePlanIn != nil && a.Cfg.PricePlanOut != nil && a.Cfg.PriceSearchCall != nil,
		},
	}
	// Je Ablage: Pfad, von der App belegt, frei und gesamt auf dem Dateisystem.
	disk := func(name, path string) map[string]any {
		m := map[string]any{"name": name, "pfad": path}
		free, total, err := diskSpace(path)
		if err != nil {
			m["fehler"] = err.Error()
			return m
		}
		m["belegt"], m["frei"], m["gesamt"], m["knapp"] = dirSize(path), free, total, free < a.Cfg.MinFreeBytes
		return m
	}
	out["speicher"] = []map[string]any{disk("Daten", a.Cfg.DataDir), disk("Aufnahmen", a.Cfg.AudioDir), disk("Backups", a.Cfg.BackupDir)}
	tj, _ := a.DB.TrashedJobs(0)
	tp, _ := a.DB.TrashedPlannings(0)
	out["papierkorb"] = len(tj) + len(tp)
	if a.Notes == nil {
		out["joplin"] = map[string]any{"aktiv": false}
	} else {
		a.joplinMu.Lock()
		at, jerr := a.joplinAt, a.joplinErr
		a.joplinMu.Unlock()
		if r.URL.Query().Get("pruefen") == "1" || at == 0 {
			perr := a.Notes.Ping()
			a.joplinState(perr)
			at, jerr = store.Now(), ""
			if perr != nil {
				jerr = perr.Error()
			}
		}
		dirty, _ := a.DB.DirtyPlannings()
		out["joplin"] = map[string]any{"aktiv": true, "geprueft": at, "fehler": jerr, "offene_planungen": len(dirty)}
	}
	failed, _ := a.DB.History(0, 500)
	list := []map[string]any{}
	for _, j := range failed {
		if j.Failed || (j.Error != "" && j.State != store.StateDone) {
			list = append(list, a.jobSummary(j))
		}
	}
	out["probleme"] = list
	subs, _ := a.DB.PushSubs()
	out["push_abos"] = len(subs)
	writeJSON(w, 200, out)
}

// handleCosts liefert die KI-Kosten der letzten 30 Tage: je Tag und Art, getrennt nach Diktat und Planung.
// Der Tag ist der Eingangstag des Auftrags in der Zeitzone des Servers.
func (a *App) handleCosts(w http.ResponseWriter, r *http.Request) {
	const days = 30
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	jobs, err := a.DB.Since(start.UnixMilli())
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	type day struct {
		Tag     string  `json:"tag"`
		Diktat  float64 `json:"diktat"`
		Planung float64 `json:"planung"`
	}
	type total struct {
		USD        float64 `json:"usd"`
		Anzahl     int     `json:"anzahl"`
		OhneKosten int     `json:"ohne_kosten"`
	}
	series := make([]day, days)
	index := map[string]int{}
	for i := range series {
		series[i].Tag = start.AddDate(0, 0, i).Format("2006-01-02")
		index[series[i].Tag] = i
	}
	totals := map[string]*total{"diktat": {}, "planung": {}}
	kinds := map[string]float64{"transkription": 0, "titel_zuordnung": 0, "recherche": 0}
	val := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	for _, j := range jobs {
		t := totals[j.Mode]
		sum := val(j.CostTranscribe) + val(j.CostAux) + val(j.CostUSD)
		t.USD += sum
		t.Anzahl++
		// Ein KI-Schritt lief, aber sein Betrag fehlt (Preis nicht konfiguriert oder Altbestand): sichtbar zählen.
		if (j.TranscriptSource == "openai" && j.CostTranscribe == nil) ||
			(j.Mode == "diktat" && j.Result != "" && j.CostUSD == nil) ||
			(j.Mode == "planung" && j.AssignHow == "ki" && j.CostAux == nil) ||
			(j.Mode == "planung" && j.Result != "" && j.CostUSD == nil) {
			t.OhneKosten++
		}
		kinds["transkription"] += val(j.CostTranscribe)
		kinds["titel_zuordnung"] += val(j.CostAux)
		kinds["recherche"] += val(j.CostUSD)
		if i, ok := index[time.UnixMilli(j.ReceivedAt).In(now.Location()).Format("2006-01-02")]; ok {
			if j.Mode == "diktat" {
				series[i].Diktat += sum
			} else {
				series[i].Planung += sum
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"tage": series, "summen": totals, "arten": kinds, "limit_usd": a.Cfg.ResearchLimitUSD,
		"preise": map[string]bool{
			"transkription": a.Cfg.PriceTranscribeMin != nil,
			"titel":         a.Cfg.PriceTitleIn != nil && a.Cfg.PriceTitleOut != nil,
			"recherche":     a.Cfg.PricePlanIn != nil && a.Cfg.PricePlanOut != nil && a.Cfg.PriceSearchCall != nil,
		},
	})
}

// handlePebbleSetup liefert die Werte, die in der Pebble-App einzutragen sind (nur mit Sitzung).
func (a *App) handlePebbleSetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"url": a.Cfg.PublicURL + "/hook", "header": a.Cfg.HookTokenHeader, "token": a.Cfg.HookToken, "secret": a.Cfg.HookSecret,
		"halten": a.Cfg.TriggerSingle, "doppelklick_halten": a.Cfg.TriggerDouble, "transkription": a.Cfg.Transcription,
		"max_mb": a.Cfg.HookMaxBytes >> 20,
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if _, err := a.DB.Counts(); err != nil {
		http.Error(w, "Datenbank", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}
