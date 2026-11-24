package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pebble-webhook/internal/store"
)

// Einträge über den MCP-Server anlegen (Nutzervorgaben 23.11.2026): Import nur als Text. Jeder Eintrag wird eine
// Notiz („Import von <quelle>: <text>“, Quelle „mcp“), die die interne KI wie ein Diktat auswertet und selbst
// einsortiert (Listen, Gruppen, Termine). Die KI am MCP bestimmt keine Liste.

const (
	mcpImportMax = 100  // Einträge je Aufruf
	mcpTextMax   = 2000 // Zeichen je Eintrag
)

var mcpToolsCreate = []map[string]any{
	{
		"name": "listen_struktur",
		"description": "Alle Gruppen und Listen mit Nummer (L12), Beschreibung und Zahl offener/aller Punkte – ohne die Punkte selbst. " +
			"Punkte einer Liste zeigt listen_uebersicht mit liste=\"L12\".",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name": "notizen_importieren",
		"description": fmt.Sprintf("Legt Einträge als Notizen an, nur als Text – z. B. eine Filmliste: je Film ein Eintrag \"Film: Jojo Rabbit (2019)\". "+
			"Die Notiz-KI wertet jeden Eintrag wie ein Diktat aus und sortiert ihn selbst ein (Liste, Gruppe, Termin, Eckdaten, Tags); keine Liste angeben. "+
			"Höchstens %d Einträge je Aufruf, je Eintrag eine KI-Auswertung (Kosten).", mcpImportMax),
		"inputSchema": map[string]any{"type": "object", "required": []string{"eintraege"}, "properties": map[string]any{
			"quelle": map[string]any{"type": "string", "description": "Woher der Import stammt, z. B. \"Letterboxd\"; steht in jeder Notiz als „Import von …“"},
			"eintraege": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "Je Eintrag ein kurzer Text mit Art und Name, z. B. \"Film: Arrival (2016)\", \"Serie: Severance\", \"Aufgabe: Reifen wechseln\"; ein Kommentar dazu darf folgen"},
		}},
	},
	{
		"name":        "notizen_anreichern",
		"description": fmt.Sprintf("Lässt fertige Notizen von der KI nachträglich auswerten, z. B. nach einem Import ohne Auswertung: Eckdaten, Beschreibung, Hinweise und Tags kommen dazu, Liste und Namen der Punkte bleiben. Kostet je Notiz eine KI-Auswertung; höchstens %d Notizen je Aufruf. Notizen mit abgehakten Punkten werden übersprungen.", enrichNotesMax),
		"inputSchema": map[string]any{"type": "object", "required": []string{"nummern"}, "properties": map[string]any{
			"nummern": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Nummern wie \"#97\""},
		}},
	},
}

const (
	enrichItemsMax = 20 // so viele Punkte verarbeitet eine Auswertung
	enrichNotesMax = 50
	// enrichInstruction: Anweisung für die nachträgliche Auswertung eines Imports.
	enrichInstruction = "Nachträglich auswerten: in der bisherigen Liste lassen; Titel und Namen der Punkte übernehmen (Film und Serie mit Jahr in Klammern); Eckdaten (bei Film und Serie mit „Zu sehen bei“), Beschreibung, Hinweise und Tags ergänzen."
)

// mcpEnrich: fertige Notizen nachträglich auswerten lassen (Anweisung enrichInstruction, ohne Gedächtnis-Eintrag).
func (a *App) mcpEnrich(refs []string) (string, error) {
	if len(refs) == 0 {
		return "", a.Errorf("Keine Nummern angegeben")
	}
	if len(refs) > enrichNotesMax {
		return "", a.Errorf("Zu viele Notizen (höchstens %d je Aufruf)", enrichNotesMax)
	}
	var b strings.Builder
	n := 0
	for _, ref := range refs {
		nr, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(ref), "#"), 10, 64)
		if err != nil {
			fmt.Fprintf(&b, "%s: %s\n", ref, a.T("keine Nummer"))
			continue
		}
		msg := a.enrichJob(nr)
		if msg == "" {
			n++
			continue
		}
		fmt.Fprintf(&b, "#%d: %s\n", nr, msg)
	}
	return strings.TrimSpace(a.Tf("%d Notizen werden ausgewertet.", n) + "\n" + b.String()), nil
}

// enrichJob startet die nachträgliche Auswertung; Rückgabe = Grund, warum nicht (leer = gestartet).
func (a *App) enrichJob(nr int64) string {
	j, err := a.DB.JobByNr(nr)
	if err != nil {
		return a.T("gibt es nicht")
	}
	d := parseDictation(j.Result)
	if j.Mode != "diktat" || j.PlanningID != "" || j.State != store.StateDone || j.Failed || d == nil {
		return a.T("keine fertige Notiz")
	}
	items, err := a.DB.ItemsByJob(j.ID)
	if err != nil {
		return a.T("Datenbankfehler")
	}
	if len(items) > enrichItemsMax {
		return a.Tf("mehr als %d Punkte – nicht in einer Auswertung", enrichItemsMax)
	}
	for _, it := range items {
		if it.DoneAt != 0 {
			return a.T("hat abgehakte Punkte – übersprungen")
		}
	}
	if len(d.Anweisungen) >= 10 {
		return a.T("Höchstens 10 Anweisungen je Notiz")
	}
	// Titel und Punkte ausdrücklich nennen: der Text eines Imports kann ein Kommentar ohne den Namen sein.
	instruction := enrichInstruction + " " + a.Tf("Titel: „%s“.", d.Titel)
	if len(items) > 0 {
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = "„" + it.Text + "“"
		}
		instruction += " " + a.Tf("Punkte: %s.", strings.Join(names, ", "))
	}
	if err := a.reevaluate(j, d, store.Preview(instruction, 2000), store.Now()); err != nil {
		return a.T("Datenbankfehler")
	}
	return ""
}

// mcpStructure: Gruppen (mit Beschreibung) und ihre Listen, ohne Punkte.
func (a *App) mcpStructure() (string, error) {
	lists, err := a.DB.Lists()
	if err != nil {
		return "", a.Errorf("Datenbankfehler")
	}
	if len(lists) == 0 {
		return a.T("Noch keine Listen."), nil
	}
	groups, _ := a.DB.Groups()
	desc := map[string]string{}
	for _, g := range groups {
		desc[strings.ToLower(g.Name)] = g.Description
	}
	byGroup := map[string][]*store.List{}
	var order []string
	for _, l := range lists {
		if _, ok := byGroup[l.Project]; !ok {
			order = append(order, l.Project)
		}
		byGroup[l.Project] = append(byGroup[l.Project], l)
	}
	var b strings.Builder
	for _, g := range order {
		name := g
		if name == "" {
			name = a.T("Ohne Gruppe")
		}
		fmt.Fprintf(&b, "# %s", name)
		if d := desc[strings.ToLower(g)]; d != "" {
			fmt.Fprintf(&b, " – %s", d)
		}
		b.WriteString("\n")
		for _, l := range byGroup[g] {
			fmt.Fprintf(&b, "- L%d %s (%s)", l.Nr, l.Name, a.Tf("%d von %d offen", l.Open, l.Total))
			if l.Description != "" {
				fmt.Fprintf(&b, ": %s", store.Preview(l.Description, 200))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// mcpImport legt je Eintrag eine Notiz als Text an; die interne KI wertet sie aus und sortiert sie ein.
func (a *App) mcpImport(raw json.RawMessage) (string, error) {
	var in struct {
		Quelle    string   `json:"quelle"`
		Eintraege []string `json:"eintraege"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return "", a.Errorf("Argumente nicht lesbar")
	}
	var texts []string
	seen := map[string]bool{}
	for _, t := range in.Eintraege {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		if len([]rune(t)) > mcpTextMax {
			return "", a.Errorf("Eintrag zu lang (höchstens %d Zeichen): %s", mcpTextMax, store.Preview(t, 60))
		}
		seen[strings.ToLower(t)] = true
		texts = append(texts, t)
	}
	if len(texts) == 0 {
		return "", a.Errorf("Keine Einträge angegeben")
	}
	if len(texts) > mcpImportMax {
		return "", a.Errorf("Zu viele Einträge (höchstens %d je Aufruf) – bitte aufteilen", mcpImportMax)
	}
	prefix := a.T("Import")
	if q := store.Preview(strings.TrimSpace(in.Quelle), 60); q != "" {
		prefix = a.Tf("Import von %s", q)
	}
	var nrs []string
	for _, t := range texts {
		now := store.Now()
		j := &store.Job{ID: store.NewID(), Source: "mcp", Trigger: "mcp", Mode: "diktat", ReceivedAt: now, RecordedAt: now,
			State: store.StateNew, Transcript: prefix + ": " + t}
		j.DeliveryID = "mcp-" + j.ID
		if _, err := a.DB.InsertJob(j); err != nil {
			return "", a.Errorf("Datenbankfehler")
		}
		nrs = append(nrs, a.mcpNr(j.ID))
		a.changed(j.ID)
	}
	a.Wake()
	return a.Tf("%d Notizen angelegt (%s); die KI wertet sie nacheinander aus und sortiert sie ein.", len(nrs), mcpRange(nrs)), nil
}

// mcpRange: „#5“ bzw. „#5–#9“ (fortlaufende Nummern eines Imports).
func mcpRange(nrs []string) string {
	if len(nrs) == 1 {
		return nrs[0]
	}
	return nrs[0] + "–" + nrs[len(nrs)-1]
}

func (a *App) mcpNr(id string) string {
	if j, err := a.DB.Job(id); err == nil && j.Nr > 0 {
		return fmt.Sprintf("#%d", j.Nr)
	}
	return id
}
