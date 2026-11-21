package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/store"
)

// MCP-Werkzeuge über die Listen hinaus: Termine, Suche in allen Notizen, Notiz per Nummer lesen.

var mcpToolsNotes = []map[string]any{
	{
		"name": "termine",
		"description": "Termine aus den Sprachnotizen mit Datum, Uhrzeit, Ort und Nummer (#N). Vorgabe: alle offenen ab heute und verpasste unbestätigte. " +
			"Für Fragen wie „Was steht an?“, „Termine diese Woche?“. Die Antwort beginnt mit dem heutigen Datum.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"tage":       map[string]any{"type": "integer", "description": "Nur Termine der nächsten N Tage (Vorgabe: alle kommenden)"},
			"vergangene": map[string]any{"type": "integer", "description": "Zusätzlich vergangene Termine der letzten N Tage, auch bestätigte"},
		}},
	},
	{
		"name":        "notizen_suchen",
		"description": "Volltextsuche in allen Notizen, Terminen und Planungen (Titel, Text, Wortlaut, Tags). Liefert Nummer, Art, Titel und Fundstelle; Details mit notiz_lesen.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"suche"}, "properties": map[string]any{
			"suche": map[string]any{"type": "string", "description": "Suchbegriffe (alle müssen vorkommen) oder eine Nummer wie \"#63\""},
		}},
	},
	{
		"name":        "notiz_lesen",
		"description": "Volltext einer Notiz, eines Termins oder einer Planung über die Nummer (#63), auch wenn sie in keiner Liste steht.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"nummern"}, "properties": map[string]any{
			"nummern": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Nummern wie \"#63\""},
		}},
	},
}

func mcpWhen(t time.Time) string {
	return weekdayDE[t.Weekday()] + ", " + t.Format("02.01.2006")
}

func (a *App) mcpAppointments(days, past int) (string, error) {
	jobs, err := a.DB.Appointments(true)
	if err != nil {
		return "", err
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var b strings.Builder
	fmt.Fprintf(&b, "Heute ist %s, %s Uhr.\n\n", mcpWhen(now), now.Format("15:04"))
	n := 0
	for _, j := range jobs {
		t := time.UnixMilli(j.TerminAt)
		end := appointmentEnd(j)
		upcoming := end > now.UnixMilli()
		missed := !upcoming && j.DoneAt == 0
		switch {
		case upcoming:
			if days > 0 && !t.Before(today.AddDate(0, 0, days+1)) {
				continue
			}
		case missed:
			if past > 0 && t.Before(today.AddDate(0, 0, -past)) {
				continue
			}
		default: // vergangen und bestätigt
			if past <= 0 || t.Before(today.AddDate(0, 0, -past)) {
				continue
			}
		}
		when := mcpWhen(t)
		if j.TerminAllDay {
			when += ", ganztägig"
		} else {
			when += ", " + t.Format("15:04") + " Uhr"
		}
		fmt.Fprintf(&b, "- #%d %s – %s", j.Nr, shortTitle(j.Title), when)
		if d := parseDictation(j.Result); d != nil && d.Termin != nil && d.Termin.Ort != "" {
			fmt.Fprintf(&b, " · Ort: %s", d.Termin.Ort)
		}
		switch {
		case missed:
			b.WriteString(" [verpasst, nicht bestätigt]")
		case !upcoming:
			b.WriteString(" [vorbei]")
		}
		b.WriteString("\n")
		n++
	}
	if n == 0 {
		b.WriteString("Keine Termine im gewählten Zeitraum.")
	}
	return strings.TrimSpace(b.String()), nil
}

// mcpSearch nutzt die Suche der Web-App (gleiche Treffer und Reihenfolge).
func (a *App) mcpSearch(q string) string {
	rec := httptest.NewRecorder()
	a.handleSearch(rec, httptest.NewRequest("GET", "/api/suche?q="+url.QueryEscape(q), nil))
	var out struct {
		Treffer []map[string]any `json:"treffer"`
		Anzahl  int              `json:"anzahl"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		return "Suche fehlgeschlagen"
	}
	if len(out.Treffer) == 0 {
		return "Keine Treffer für „" + q + "“ (mindestens 2 Zeichen)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d Treffer für „%s“", out.Anzahl, q)
	if out.Anzahl > 30 {
		b.WriteString(" (die ersten 30)")
	}
	b.WriteString(":\n")
	for i, t := range out.Treffer {
		if i == 30 {
			break
		}
		art := "notiz"
		if t["typ"] == "planung" {
			art = "planung"
		}
		if _, ok := t["termin"]; ok {
			art = "termin"
		}
		fmt.Fprintf(&b, "- #%v [%s] %v", t["nr"], art, t["titel"])
		if f, ok := t["fundstelle"].(map[string]any); ok && f["feld"] != "Titel" {
			fmt.Fprintf(&b, " — %v: %v", f["feld"], f["text"])
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// mcpReadNumbers: Notiz oder Planung je Nummer im Volltext.
func (a *App) mcpReadNumbers(refs []string) (string, error) {
	jobs, err := a.DB.SearchableJobs()
	if err != nil {
		return "", err
	}
	plans, err := a.DB.AllPlannings()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, ref := range refs {
		nr, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(ref), "#"), 10, 64)
		if err != nil {
			fmt.Fprintf(&b, "%s: keine Nummer\n\n", ref)
			continue
		}
		if text, ok := a.mcpJobByNr(jobs, nr); ok {
			b.WriteString(text + "\n\n")
		} else if text, ok := a.mcpPlanningByNr(plans, nr); ok {
			b.WriteString(text + "\n\n")
		} else {
			fmt.Fprintf(&b, "#%d: nicht gefunden (oder im Papierkorb)\n\n", nr)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (a *App) mcpJobByNr(jobs []*store.Job, nr int64) (string, bool) {
	for _, j := range jobs {
		if j.Nr != nr {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "## #%d %s", j.Nr, shortTitle(j.Title))
		if j.DoneAt > 0 {
			b.WriteString(" [erledigt]")
		}
		fmt.Fprintf(&b, "\nAufgenommen: %s, %s Uhr\n", mcpWhen(time.UnixMilli(j.ReceivedAt)), time.UnixMilli(j.ReceivedAt).Format("15:04"))
		if j.TerminAt > 0 {
			fmt.Fprintf(&b, "Termin: %s\n", appointmentText(j))
		}
		b.WriteString("\n")
		if d := parseDictation(j.Result); d != nil && d.Notiz != "" {
			fmt.Fprintf(&b, "%s\n\n", d.Notiz)
		}
		b.WriteString(renderDictation(j))
		return strings.TrimSpace(b.String()), true
	}
	return "", false
}

func (a *App) mcpPlanningByNr(plans []*store.Planning, nr int64) (string, bool) {
	for _, p := range plans {
		if p.Nr != nr {
			continue
		}
		rounds, _ := a.DB.Rounds(p.ID)
		plan := parsePlan(p.Plan)
		if plan == nil {
			return fmt.Sprintf("## #%d %s (Planung ohne Stand)", p.Nr, p.Title), true
		}
		return fmt.Sprintf("## #%d Planung: %s [%s]\n\n%s", p.Nr, plan.Titel, p.Status, a.renderPlan(plan, rounds)), true
	}
	return "", false
}
