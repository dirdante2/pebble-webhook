package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"pebble-webhook/internal/store"
)

// MCP-Server (Model Context Protocol, Streamable HTTP ohne Ereignisstrom): POST /mcp mit JSON-RPC 2.0.
// Zugang wie der Maschinenzugang (Authorization: Bearer <API_TOKEN>). Werkzeuge: Listen lesen, einen Punkt im
// Volltext lesen, Punkte einzeln abhaken oder wieder öffnen. Punkte heißen „#59“ bzw. „#59.2“ (itemRefs).

const mcpProtocol = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

var mcpTools = []map[string]any{
	{
		"name":        "listen_uebersicht",
		"description": "Gruppen und Listen der Sprachnotizen mit ihren Punkten (offen [ ] / erledigt [x]). Jeder Punkt hat eine Kennung wie #59 oder #59.2, mit der er gelesen und abgehakt wird.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"gruppe": map[string]any{"type": "string", "description": "Nur diese Gruppe (z. B. \"servicemanager\"); leer = alle"},
			"liste":  map[string]any{"type": "string", "description": "Nur diese Liste, als Nummer (\"L23\") oder Name"},
			"alle":   map[string]any{"type": "boolean", "description": "Auch erledigte Punkte zeigen (Vorgabe: nur offene)"},
		}},
	},
	{
		"name":        "punkt_lesen",
		"description": "Volltext zu Listenpunkten: die Notiz mit Eckdaten, Hinweisen und Wortlaut bzw. der Stand der Planung.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"punkte"}, "properties": map[string]any{
			"punkte": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Kennungen wie \"#59.2\" oder \"#42\""},
		}},
	},
	{
		"name":        "punkte_abhaken",
		"description": "Listenpunkte einzeln abhaken (oder mit offen=true wieder öffnen). Hat eine Notiz mehrere Punkte, die einzelne Kennung #59.1, #59.2 … angeben; #59.* nimmt alle Punkte der Notiz.",
		"inputSchema": map[string]any{"type": "object", "required": []string{"punkte"}, "properties": map[string]any{
			"punkte": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Kennungen wie \"#59.2\""},
			"offen":  map[string]any{"type": "boolean", "description": "true = wieder öffnen statt abhaken"},
		}},
	},
}

func (a *App) handleMCP(w http.ResponseWriter, r *http.Request) {
	var req mcpRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req.JSONRPC != "2.0" {
		mcpReply(w, nil, nil, &mcpErr{-32700, "Anfrage nicht lesbar"})
		return
	}
	if req.ID == nil { // Benachrichtigung (z. B. notifications/initialized): keine Antwort
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		version := mcpProtocol
		if p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
		mcpReply(w, req.ID, map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": "pebble-webhook", "version": shortVersion(a.Version)},
			"instructions": "Sprachnotizen des Nutzers: Listen (listen_uebersicht, punkt_lesen, punkte_abhaken mit Kennung wie #59.2), Termine (termine), Suche in allen Notizen (notizen_suchen) und Notiz per Nummer (notiz_lesen). Fragen nach Terminen immer mit termine beantworten, nicht aus den Listen."}, nil)
	case "ping":
		mcpReply(w, req.ID, map[string]any{}, nil)
	case "tools/list":
		mcpReply(w, req.ID, map[string]any{"tools": append(append([]map[string]any{}, mcpTools...), mcpToolsNotes...)}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			mcpReply(w, req.ID, nil, &mcpErr{-32602, "Parameter nicht lesbar"})
			return
		}
		text, err := a.mcpCall(p.Name, p.Arguments)
		if err != nil {
			mcpReply(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil)
			return
		}
		mcpReply(w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}, nil)
	default:
		mcpReply(w, req.ID, nil, &mcpErr{-32601, "Unbekannte Methode: " + req.Method})
	}
}

type mcpErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func mcpReply(w http.ResponseWriter, id json.RawMessage, result any, e *mcpErr) {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		out["error"] = e
	} else {
		out["result"] = result
	}
	writeJSON(w, 200, out)
}

// mcpItem ist ein Listenpunkt mit seiner Liste und Kennung.
type mcpItem struct {
	list *store.List
	item *store.ListItem
	ref  string
}

func (a *App) mcpItems() ([]*store.List, []mcpItem, error) {
	lists, err := a.DB.Lists()
	if err != nil {
		return nil, nil, err
	}
	refs, err := a.DB.ItemRefs()
	if err != nil {
		return nil, nil, err
	}
	var items []mcpItem
	for _, l := range lists {
		its, err := a.DB.ListItems(l.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range its {
			items = append(items, mcpItem{l, it, refs[it.ID]})
		}
	}
	return lists, items, nil
}

func (a *App) mcpCall(name string, raw json.RawMessage) (string, error) {
	var args struct {
		Gruppe     string   `json:"gruppe"`
		Liste      string   `json:"liste"`
		Alle       bool     `json:"alle"`
		Punkte     []string `json:"punkte"`
		Offen      bool     `json:"offen"`
		Tage       int      `json:"tage"`
		Vergangene int      `json:"vergangene"`
		Suche      string   `json:"suche"`
		Nummern    []string `json:"nummern"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &args) != nil {
		return "", fmt.Errorf("Argumente nicht lesbar")
	}
	switch name {
	case "termine":
		return a.mcpAppointments(args.Tage, args.Vergangene)
	case "notizen_suchen":
		return a.mcpSearch(strings.TrimSpace(args.Suche)), nil
	case "notiz_lesen":
		if len(args.Nummern) == 0 {
			return "", fmt.Errorf("Keine Nummern angegeben")
		}
		return a.mcpReadNumbers(args.Nummern)
	}
	lists, items, err := a.mcpItems()
	if err != nil {
		return "", fmt.Errorf("Datenbankfehler")
	}
	switch name {
	case "listen_uebersicht":
		return mcpOverview(lists, items, args.Gruppe, args.Liste, args.Alle), nil
	case "punkt_lesen":
		var b strings.Builder
		for _, ref := range args.Punkte {
			found, err := mcpFind(items, ref)
			if err != nil {
				fmt.Fprintf(&b, "%s\n\n", err)
				continue
			}
			for _, m := range found {
				fmt.Fprintf(&b, "## #%s %s  [%s]%s\n\n%s\n\n", m.ref, m.item.Text, mcpListLabel(m.list), doneMark(m.item), a.mcpFullText(m.item))
			}
		}
		return strings.TrimSpace(b.String()), nil
	case "punkte_abhaken":
		if len(args.Punkte) == 0 {
			return "", fmt.Errorf("Keine Punkte angegeben")
		}
		var b strings.Builder
		for _, ref := range args.Punkte {
			found, err := mcpFind(items, ref)
			if err != nil {
				fmt.Fprintf(&b, "%s\n", err)
				continue
			}
			for _, m := range found {
				changed, err := a.setItemDone(m.item, !args.Offen)
				state := "erledigt"
				if args.Offen {
					state = "offen"
				}
				switch {
				case err != nil:
					fmt.Fprintf(&b, "#%s: FEHLER beim Speichern\n", m.ref)
				case !changed:
					fmt.Fprintf(&b, "#%s %s: war schon %s\n", m.ref, m.item.Text, state)
				default:
					fmt.Fprintf(&b, "#%s %s: %s\n", m.ref, m.item.Text, state)
				}
			}
		}
		return strings.TrimSpace(b.String()), nil
	}
	return "", fmt.Errorf("Unbekanntes Werkzeug: %s", name)
}

func mcpListLabel(l *store.List) string {
	if l.Project != "" {
		return fmt.Sprintf("L%d %s › %s", l.Nr, l.Project, l.Name)
	}
	return fmt.Sprintf("L%d %s", l.Nr, l.Name)
}

func doneMark(it *store.ListItem) string {
	if it.DoneAt != 0 {
		return " (erledigt)"
	}
	return ""
}

func mcpOverview(lists []*store.List, items []mcpItem, group, list string, all bool) string {
	var b strings.Builder
	list = strings.TrimSpace(list)
	open := 0
	// Gruppen nacheinander (wie in der Web-App sortiert die Datenbank die Listen), Listen ohne Gruppe am Ende.
	ordered := slices.Clone(lists)
	slices.SortStableFunc(ordered, func(x, y *store.List) int {
		if (x.Project == "") != (y.Project == "") {
			if x.Project == "" {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(x.Project), strings.ToLower(y.Project))
	})
	current := "\x00"
	for _, l := range ordered {
		if group != "" && !strings.EqualFold(l.Project, group) {
			continue
		}
		if list != "" && !strings.EqualFold(list, fmt.Sprintf("L%d", l.Nr)) && !strings.EqualFold(list, l.Name) {
			continue
		}
		var lines []string
		for _, m := range items {
			if m.list.ID != l.ID || (!all && m.item.DoneAt != 0) {
				continue
			}
			mark := " "
			if m.item.DoneAt != 0 {
				mark = "x"
			} else {
				open++
			}
			ref := m.ref
			if ref == "" {
				ref = "?"
			}
			lines = append(lines, fmt.Sprintf("- [%s] #%s %s", mark, ref, m.item.Text))
		}
		if len(lines) == 0 {
			continue
		}
		if l.Project != current {
			current = l.Project
			name := l.Project
			if name == "" {
				name = "Ohne Gruppe"
			}
			fmt.Fprintf(&b, "# %s\n\n", name)
		}
		fmt.Fprintf(&b, "## L%d %s (%d offen)\n", l.Nr, l.Name, l.Open)
		if l.Description != "" {
			fmt.Fprintf(&b, "%s\n", l.Description)
		}
		b.WriteString(strings.Join(lines, "\n") + "\n\n")
	}
	if b.Len() == 0 {
		return "Keine passenden Punkte."
	}
	fmt.Fprintf(&b, "%d offene Punkte", open)
	return b.String()
}

// mcpFind löst eine Kennung auf: „#59.2“ genau ein Punkt; „#59“ nur, wenn die Notiz einen Punkt hat; „#59.*“ alle.
func mcpFind(items []mcpItem, ref string) ([]mcpItem, error) {
	r := strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if r == "" {
		return nil, fmt.Errorf("Leere Kennung")
	}
	if base, ok := strings.CutSuffix(r, ".*"); ok {
		var out []mcpItem
		for _, m := range items {
			if m.ref == base || strings.HasPrefix(m.ref, base+".") {
				out = append(out, m)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("#%s: kein Punkt", r)
		}
		return out, nil
	}
	var candidates []string
	for _, m := range items {
		if m.ref == r {
			return []mcpItem{m}, nil
		}
		if strings.HasPrefix(m.ref, r+".") {
			candidates = append(candidates, "#"+m.ref)
		}
	}
	if len(candidates) > 0 {
		return nil, fmt.Errorf("#%s hat mehrere Punkte – einzeln angeben: %s (oder #%s.* für alle)", r, strings.Join(candidates, ", "), r)
	}
	return nil, fmt.Errorf("#%s: kein Punkt", r)
}

// mcpFullText: Notiz (Kurzfassung, Eckdaten, Hinweise, Wortlaut) bzw. Stand der Planung zu einem Punkt.
func (a *App) mcpFullText(it *store.ListItem) string {
	if it.PlanningID != "" {
		p, err := a.DB.Planning(it.PlanningID)
		if err != nil {
			return "(Planung nicht gefunden)"
		}
		rounds, _ := a.DB.Rounds(p.ID)
		if plan := parsePlan(p.Plan); plan != nil {
			return "# " + plan.Titel + "\n\n" + a.renderPlan(plan, rounds)
		}
		return "(Planung ohne Stand)"
	}
	if it.JobID == "" {
		return "(Punkt ohne Notiz)"
	}
	j, err := a.DB.Job(it.JobID)
	if err != nil {
		return "(Notiz nicht gefunden)"
	}
	var b strings.Builder
	if d := parseDictation(j.Result); d != nil && d.Notiz != "" {
		fmt.Fprintf(&b, "%s\n\n", d.Notiz)
	}
	b.WriteString(renderDictation(j))
	return b.String()
}

// shortVersion: „1.22.0 (Build …)“ → „1.22.0“; leer bleibt „dev“.
func shortVersion(v string) string {
	if f := strings.Fields(v); len(f) > 0 {
		return f[0]
	}
	return "dev"
}
