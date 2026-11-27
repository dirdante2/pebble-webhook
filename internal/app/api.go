package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/config"
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
	lang := i18n.Lang()
	if lw, ok := w.(*langWriter); ok {
		lang = lw.lang
	}
	writeJSON(w, status, map[string]string{"code": code, "fehler": i18n.In(lang, msg)})

}

// langWriter trägt die Sprache des angemeldeten Nutzers zu writeErr.
type langWriter struct {
	http.ResponseWriter
	lang string
}

func (w *langWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *langWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
			// Film/Serie: Regie und Schauspieler unter dem Titel der Karte im Verlauf (die Notizansicht hat sie in den Eckdaten).
			if d := parseDictation(j.Result); d != nil && len(items) == 1 {
				if line := castLine(d.Eckdaten); line != "" {
					m["besetzung"] = line
				}
			}
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
		m["schritt"] = a.T(stepLabel[j.State])
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
		m["schritt"] = a.T(stepLabel[last.State])
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

// historyPageSize: Einträge je nachgeladener Seite des Verlaufs.
const historyPageSize = 50

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
		// Ohne offen=1 auch die bestätigten.
		list := upcoming
		if r.URL.Query().Get("offen") != "1" {
			list = all
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
		resp := map[string]any{"eintraege": out, "mehr": false, "fragen_offen": total, "besuch_pause_minuten": a.Cfg.VisitGapMinutes}
		maps.Copy(resp, termine)
		writeJSON(w, 200, resp)
		return
	}
	if kind == "fragen" {
		if asking == nil {
			asking = []map[string]any{}
		}
		sort.Slice(asking, func(i, k int) bool { return asking[i]["eingegangen"].(int64) > asking[k]["eingegangen"].(int64) })
		resp := map[string]any{"eintraege": asking, "mehr": false, "fragen_offen": total, "besuch_pause_minuten": a.Cfg.VisitGapMinutes}
		maps.Copy(resp, termine)
		writeJSON(w, 200, resp)
		return
	}
	if kind != "diktat" && kind != "planung" {
		kind = ""
	}
	// offen=1: nur offene Einträge (Notiz/Termin nicht erledigt, Planung nicht geschlossen).
	mode := ""
	if r.URL.Query().Get("offen") == "1" {
		mode = "offen"
	}
	// Seitenweise, neueste zuerst. Erste Seite (ohne vor=): alles seit zuletzt gesehen (seit=, Neu) und
	// dazu die jüngsten HistoryMinEntries; weitere Seiten (vor=) je HistoryPageSize, die Web-App lädt beim Scrollen nach.
	seit, _ := strconv.ParseInt(r.URL.Query().Get("seit"), 10, 64)
	limit := historyPageSize
	if before <= 0 {
		limit = a.Cfg.HistoryMinEntries
		if limit <= 0 {
			limit = 10
		}
		if seit > 0 {
			n, err := a.DB.HistoryCount(kind, tags, seit, mode)
			if err != nil {
				writeErr(w, 500, "speicher", "Datenbankfehler")
				return
			}
			// Lange weg gewesen: höchstens 100 auf einmal, der Rest kommt beim Scrollen.
			limit = min(limit+n, 100)
		}
	}
	refs, err := a.DB.HistoryRefs(kind, tags, before, limit+1, 0, mode)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	// Zahlen der Filterknöpfe: alle offenen Einträge ihrer Art, auch die noch nicht geladenen.
	counts := map[string]any{"fragen": total, "termin": len(upcoming)}
	for name, k := range map[string]string{"alles": "", "diktat": "diktat", "planung": "planung"} {
		n, err := a.DB.HistoryOpenCount(k, tags, 0)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		counts[name] = n
	}
	// neu: noch nicht aufgerufene Einträge seit zuletzt gesehen (seit=) je Filter – die Web-App zeigt sie gelb im Menü.
	newCounts := map[string]int{}
	if seit > 0 {
		for name, k := range map[string]string{"alles": "", "diktat": "diktat", "termin": "termin", "planung": "planung"} {
			n, err := a.DB.HistoryUnreadCount(k, tags, seit)
			if err != nil {
				writeErr(w, 500, "speicher", "Datenbankfehler")
				return
			}
			newCounts[name] = n
		}
	}
	more := len(refs) > limit
	if more {
		refs = refs[:limit]
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
	// vor_naechste: Zeitpunkt des letzten Eintrags dieser Seite (Planungen zählen mit ihrer jüngsten Runde).
	cursor := int64(0)
	if len(refs) > 0 {
		cursor = refs[len(refs)-1].At
	}
	resp := map[string]any{"eintraege": out, "mehr": more, "vor_naechste": cursor, "fragen_offen": total, "zahlen": counts, "neu": newCounts, "besuch_pause_minuten": a.Cfg.VisitGapMinutes}
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
			rm["schritt"] = a.T(stepLabel[rd.State])
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
	if _, ext := externOf(j); ext != "" {
		m["extern_id"] = ext
	}
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
	// KI-Anfragen dieses Auftrags aus dem Protokoll: je Bereich, Anbieter und Modell mit rohen Tokens und Kosten.
	// Ältere Aufträge haben keine Zeilen; für sie bleibt nur kosten_usd.
	if usage, err := a.System.DB.AIUsageByRef(a.UserID, j.ID); err != nil {
		logf("KI-Anfragen %s: %v", j.ID, err)
	} else if len(usage) > 0 {
		m["ki_anfragen"] = aiUsageJSON(usage)
	}
	text := j.Transcript
	switch j.Mode {
	case "diktat":
		if d := parseDictation(j.Result); d != nil {
			m["analyse"] = d
			// Fortsetzung einer früheren Notiz: Verweis mit Titel (nur solange es sie gibt).
			if d.Bezug > 0 {
				if p, err := a.DB.JobByNr(d.Bezug); err == nil {
					m["bezug"] = map[string]any{"id": p.ID, "nr": p.Nr, "titel": shortTitle(p.Title)}
				}
			}
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
	st, err := os.Stat(j.AudioPath)
	var data []byte
	if err == nil {
		data, err = a.readAudio(j.AudioPath)
	}
	if err != nil {
		writeErr(w, 404, "fehlt", "Aufnahme nicht lesbar")
		return
	}
	mt := j.AudioMime
	if !strings.HasPrefix(mt, "audio/") && !strings.HasPrefix(mt, "video/") {
		mt = "application/octet-stream" // Altbestand mit fremdem Typ: nie als Seite darstellen
	}
	w.Header().Set("Content-Type", mt)
	// Eine Aufnahme ändert sich nie: einmal geladen, kommt sie 30 Tage aus dem Browser-Speicher.
	w.Header().Set("Cache-Control", "private, max-age=2592000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), bytes.NewReader(data))
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
	if !a.saveWebAudio(w, r, j) {
		return
	}
	if j.Transcript == "" && j.AudioPath == "" {
		writeErr(w, 400, "leer", "Antwort ist leer")
		return
	}
	a.insertWebJob(w, j)
}

// saveWebAudio speichert eine mitgeschickte Aufnahme (Feld audio) als <id>-web.<ext>; ohne Feld nichts.
// false = Fehler, Antwort ist schon geschrieben.
func (a *App) saveWebAudio(w http.ResponseWriter, r *http.Request, j *store.Job) bool {
	f, hdr, err := r.FormFile("audio")
	if err != nil {
		return true
	}
	data, rerr := io.ReadAll(f)
	f.Close()
	if rerr != nil || len(data) == 0 {
		writeErr(w, 400, "ungueltig", "Aufnahme nicht lesbar")
		return false
	}
	mimeType := hdr.Header.Get("Content-Type")
	if !strings.HasPrefix(mimeType, "audio/") && !strings.HasPrefix(mimeType, "video/") {
		writeErr(w, 400, "ungueltig", "Die Datei ist keine Aufnahme")
		return false
	}
	path, err := a.saveAudio(j.ID, j.Mode, data, mimeType, true)
	if err != nil {
		logf("Web-App: Audio speichern: %v", err)
		writeErr(w, http.StatusInsufficientStorage, "speicher", "Speicher nicht verfügbar")
		return false
	}
	j.AudioPath, j.AudioMime, j.Transcript = path, mimeType, ""
	return true
}

// insertWebJob legt den Eintrag an und bestätigt erst danach; gleiche Zustellung → bekannter Eintrag.
func (a *App) insertWebJob(w http.ResponseWriter, j *store.Job) {
	created, err := a.DB.InsertJob(j)
	if err != nil || !created {
		if j.AudioPath != "" {
			os.Remove(j.AudioPath)
		}
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		old, _ := a.DB.JobByDelivery(j.DeliveryID)
		writeJSON(w, 200, map[string]any{"id": old.ID, "bekannt": true})
		return
	}
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"id": j.ID})
}

// webNoteMaxBytes: Obergrenze einer Notiz-Aufnahme aus der Web-App (60 s sind je nach Browser 0,5–1 MB).
const webNoteMaxBytes = 8 << 20

// handleWebNote: Notiz aus der Web-App – aufgenommen (höchstens 60 s) oder getippt (Feld text). Wird verarbeitet wie eine Notiz vom
// Ring, zählt aber nicht zur Ring-Nutzung (kein recordRecording, Datei <id>-web.<ext>).
func (a *App) handleWebNote(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, webNoteMaxBytes)
	if err := r.ParseMultipartForm(webNoteMaxBytes); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "gross", "Aufnahme zu groß oder nicht lesbar")
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
	now := store.Now()
	j := &store.Job{ID: store.NewID(), DeliveryID: delivery, Source: "web", Trigger: "web", Mode: "diktat",
		ReceivedAt: now, RecordedAt: now, State: store.StateNew}
	if ms, err := strconv.ParseInt(r.FormValue("zeit"), 10, 64); err == nil && ms > 0 && ms <= now {
		j.RecordedAt = ms
	}
	// Getippt statt gesprochen: der Text ist der Wortlaut (keine Transkription).
	j.Transcript = store.Preview(strings.TrimSpace(r.FormValue("text")), webNoteTextMax)
	if !a.saveWebAudio(w, r, j) {
		return
	}
	if j.AudioPath == "" && j.Transcript == "" {
		writeErr(w, 400, "leer", "Notiz ist leer")
		return
	}
	// Kontext: in der Web-App gerade offene Gruppe/Liste („Gruppe X · Liste L8 Y“), Hinweis fürs Einsortieren.
	// Vor dem Einfügen ablegen, damit die Auswertung ihn sicher sieht.
	if k := store.Preview(strings.TrimSpace(r.FormValue("kontext")), 200); k != "" {
		a.DB.Set(contextKey(j.ID), k)
	}
	a.insertWebJob(w, j)
}

// webNoteTextMax: Zeichen einer getippten Notiz bzw. Anweisung aus der Web-App.
const webNoteTextMax = 4000

// wordDays: Zeitraum der Wortzahl-Auswertung auf der Statusseite.
const wordDays = 30

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	counts, err := a.DB.Counts()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := map[string]any{
		"version": a.Version, "statistik": telemetryAvailable(), "audio_unbekannt": a.scanAudio(), "warteschlange": counts.Open, "fehlgeschlagen": counts.Failed,
		"einstellungen": map[string]any{
			"halten": a.Cfg.TriggerSingle, "doppelklick_halten": a.Cfg.TriggerDouble, "leer_stunden": a.Cfg.EmptyRetentionHours,
			"limit_usd": a.Cfg.ResearchLimitUSD, "papierkorb_tage": a.Cfg.TrashRetentionDays, "schliessen_tage": a.Cfg.AutoCloseDays,
			"listen_aktiv_minuten": a.Cfg.DoneActiveMinutes, "besuch_pause_minuten": a.Cfg.VisitGapMinutes,
		},
	}
	// Je Ablage: Pfad, von der App belegt, frei und gesamt auf dem Dateisystem.
	// size: von der App belegt (nil = ganzer Ordner).
	disk := func(name, path string, size func() int64) map[string]any {
		m := map[string]any{"name": name}
		admin := a.isAdmin()
		if admin {
			m["pfad"] = path // Serverpfade nur für Admins
		}
		// Frei/gesamt vom Dateisystem; Datenbank und App-Versionen liegen in DATA_DIR (releases/ gibt es evtl. noch nicht).
		fs := path
		if _, err := os.Stat(path); err != nil && size != nil {
			fs = a.Cfg.DataDir // z. B. releases/ gibt es noch nicht
		}
		free, total, err := diskSpace(fs)
		if err != nil {
			m["fehler"] = i18n.T("nicht lesbar")
			if admin {
				m["fehler"] = err.Error()
			}
			return m
		}
		if size == nil {
			size = func() int64 { return dirSize(path) }
		}
		m["belegt"], m["frei"], m["gesamt"], m["knapp"] = size(), free, total, free < a.Cfg.MinFreeBytes
		return m
	}
	// Eigene Ablagen des Nutzers; der Admin sieht dazu die des Systems (Datenbanken, App-Versionen, alle Aufnahmen, Backups).
	files := func(paths ...string) func() (n int64) {
		return func() (n int64) {
			for _, p := range paths {
				if fi, err := os.Stat(p); err == nil {
					n += fi.Size()
				}
			}
			return n
		}
	}
	own := UserDB(a.Cfg.DataDir, a.UserID)
	mediaRoot := a.mediaRoot()
	// art: eigen (zugeklappt sichtbar), alle (Summe aller Nutzer), system.
	mit := func(art string, m map[string]any) map[string]any { m["art"] = art; return m }
	speicher := []map[string]any{mit("eigen", disk("Deine Daten", own, files(own, own+"-wal", own+"-shm"))),
		mit("eigen", disk("Deine Medien", mediaRoot, func() int64 { return dirSize(mediaRoot) }))}
	speicher[1]["aufnahmen"] = true // knapp hier = neue Aufnahmen werden abgewiesen
	// App-Versionen und Backups sieht jeder Nutzer (nur Größe); Summen aller Nutzer und Aufräumen nur Admins.
	releases := filepath.Join(a.Cfg.DataDir, "releases")
	speicher = append(speicher, mit("system", disk("App-Versionen", releases, func() int64 { return dirSize(releases) })),
		mit("system", disk("Backups", a.Cfg.BackupDir, nil)))
	out["transkription"] = a.transcriptionStatus()
	if a.isAdmin() {
		if _, err := os.Stat(a.Whisper.Dir); err == nil {
			speicher = append(speicher, mit("system", disk("Whisper-Modelle", a.Whisper.Dir, nil)))
		}
		sysDB := filepath.Join(a.Cfg.DataDir, "system.db")
		users := filepath.Join(a.Cfg.DataDir, "users")
		speicher = append(speicher,
			mit("alle", disk("Datenbanken (alle Nutzer)", users, func() int64 { return files(sysDB, sysDB+"-wal", sysDB+"-shm")() + dirSize(users) })),
			mit("alle", disk("Medien (alle Nutzer)", a.Cfg.MediaDir, nil)))
		speicher[len(speicher)-1]["aufnahmen"] = true
		cb, cr, cbb, crb := a.cleanupPlan()
		out["aufraeumen"] = map[string]any{"backups": len(cb), "backups_groesse": cbb, "versionen": len(cr), "versionen_groesse": crb}
	}
	out["speicher"] = speicher
	if a.isAdmin() {
		out["speicher_nutzer"] = a.System.storageByUser()
	}
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

// handleIntake: „Meine Daten“ auf der Ringnutzung – nur aktive Einträge, die der Ring über den Webhook geschickt hat.
func (a *App) handleIntake(w http.ResponseWriter, r *http.Request) {
	c, err := a.DB.RingIntake()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	words, err := a.DB.TranscriptWords(store.Now() - wordDays*86_400_000)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	files := a.audioStats(c.AudioPaths)
	rec := map[string]any{"anzahl": c.Audio}
	if g, ok := files["schnitt_groesse"]; ok {
		rec["schnitt_groesse"] = g
	}
	if c.WithSeconds > 0 {
		rec["schnitt_sekunden"], rec["gesamt_sekunden"] = c.Seconds/float64(c.WithSeconds), c.Seconds
	}
	out := map[string]any{"auftraege": c.Total, "audio_dateien": rec, "warteschlange": c.Open, "fehlgeschlagen": c.Failed,
		"auftraege_teile": map[string]int{"audio": c.Audio, "nur_text": c.TextOnly, "notizen": c.Notes, "termine": c.Appointments,
			"runden": c.Rounds, "planungen": c.Plannings}}
	if len(words) > 0 {
		lo, hi, sum := words[0], words[0], 0
		for _, n := range words {
			lo, hi, sum = min(lo, n), max(hi, n), sum+n
		}
		out["woerter"] = map[string]any{"tage": wordDays, "anzahl": len(words), "min": lo, "max": hi, "schnitt": float64(sum) / float64(len(words))}
	}
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
		if ((j.TranscriptSource == sourceOpenAI || j.TranscriptSource == sourceFallback) && j.CostTranscribe == nil) ||
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
	// Je Anbieter und Modell aus dem Protokoll der Anfragen: rohe Tokens und Kosten nach den Preisen, die bei der
	// Anfrage galten – auch Lernlauf, Einsortieren und alles andere ohne Auftrag.
	usage, err := a.System.DB.AIUsageSince(a.UserID, start.UnixMilli())
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{
		"tage": series, "summen": totals, "arten": kinds, "limit_usd": a.Cfg.ResearchLimitUSD, "anfragen": aiUsageJSON(usage),
	})
}

// aiUsageJSON: Gruppen des Protokolls für die Web-App; fehlende Werte (nicht bekannt) bleiben weg statt 0.
func aiUsageJSON(usage []store.AIUsage) []map[string]any {
	out := make([]map[string]any, 0, len(usage))
	for _, u := range usage {
		m := map[string]any{"bereich": u.Area, "zweck": u.Purpose, "eintrag": u.EntryID, "anbieter": u.Provider, "modell": u.Model,
			"anzahl": u.Requests, "suchen": u.Searches, "bilder": u.Images}
		if u.TokensIn != nil {
			m["tokens_ein"] = *u.TokensIn
		}
		if u.TokensOut != nil {
			m["tokens_aus"] = *u.TokensOut
		}
		if u.Seconds != nil {
			m["sekunden"] = *u.Seconds
		}
		if u.Cost != nil {
			m["kosten_usd"] = *u.Cost
		}
		out = append(out, m)
	}
	return out
}

// handlePebbleSetup liefert die Werte, die in der Pebble-App einzutragen sind (nur mit Sitzung).
func (a *App) handlePebbleSetup(w http.ResponseWriter, r *http.Request) {
	access, err := a.pebbleAccess()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{
		"url": a.Cfg.PublicURL + pathHook, "header": a.Cfg.HookTokenHeader, "token": access.Token, "secret": access.Secret,
		"halten": a.Cfg.TriggerSingle, "doppelklick_halten": a.Cfg.TriggerDouble, "transkription": pebbleMode(a.KI().AI),
		"max_mb": a.Cfg.HookMaxBytes >> 20, "letzter_test": a.lastPebbleTest(),
	})
}

// handleHealth: system.db lesbar und die Daten aller geöffneten Nutzer lesbar (gesperrte zählen nicht als krank).
func (a *System) handleHealth(w http.ResponseWriter, r *http.Request) {
	var n int
	err := a.DB.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	for _, app := range a.Apps() {
		if err == nil {
			_, err = app.DB.Counts()
		}
	}
	if err != nil {
		http.Error(w, "Datenbank", http.StatusServiceUnavailable)
		return
	}
	fmt.Fprintln(w, "ok")
}

// pebbleMode: Was die Pebble-App schicken soll – ihren Text (erster Eintrag der Transkription) oder nur Audio.
func pebbleMode(ai config.AI) string {
	if ai.PebbleText() {
		return "pebble"
	}
	return "audio"
}

// handlePebbleTestOK: POST /api/pebble/test/ok – Hinweis „Test angekommen“ bestätigt.
func (a *App) handlePebbleTestOK(w http.ResponseWriter, r *http.Request) {
	if t := a.lastPebbleTest(); t != nil && !t.Confirmed {
		t.Confirmed = true
		b, _ := json.Marshal(t)
		if err := a.DB.Set(pebbleTestKey, string(b)); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// pendingPebbleTest: Test, dessen Hinweis noch nicht bestätigt ist (nil = keiner).
func (a *App) pendingPebbleTest() *pebbleTest {
	if t := a.lastPebbleTest(); t != nil && !t.Confirmed {
		return t
	}
	return nil
}

// lastPebbleTest: letzter Test der Verbindung (nil = noch keiner).
func (a *App) lastPebbleTest() *pebbleTest {
	v, err := a.DB.Get(pebbleTestKey)
	if err != nil || v == "" {
		return nil
	}
	var t pebbleTest
	if json.Unmarshal([]byte(v), &t) != nil {
		return nil
	}
	return &t
}
