package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/store"
)

// Listen und Gruppen über MCP aufräumen (Nutzervorgabe 23.11.2026): eigenes Werkzeug, nur auf ausdrücklichen Wunsch
// des Nutzers. Ohne ausfuehren=true liefert es nur den Vorschlag (geprüft, was passieren würde); erst nach Zustimmung
// des Nutzers wird derselbe Auftrag mit ausfuehren=true ausgeführt. Ausgeführt wird über dieselben Wege wie in der
// Web-App (Handler), damit Regeln, Gedächtnis und Live-Anzeige gleich bleiben.

const mcpCleanupMax = 50 // Änderungen je Aufruf

var mcpToolsCleanup = []map[string]any{
	{
		"name": "listen_aufraeumen",
		"description": "Listen und Gruppen umbauen: Liste anlegen, umbenennen, in eine andere Gruppe stellen, zusammenführen, löschen; Punkte verschieben; Gruppe umbenennen oder auflösen. " +
			"NUR auf ausdrücklichen Wunsch des Nutzers. Ablauf: erst ohne ausfuehren aufrufen – das liefert den geprüften Vorschlag, ändert nichts –, " +
			"den Vorschlag dem Nutzer zeigen und seine Zustimmung abwarten, dann denselben Auftrag mit ausfuehren=true. Listen als Nummer (\"L12\") angeben (listen_struktur).",
		"inputSchema": map[string]any{"type": "object", "required": []string{"aenderungen"}, "properties": map[string]any{
			"aenderungen": map[string]any{"type": "array", "description": fmt.Sprintf("Änderungen in Reihenfolge, höchstens %d", mcpCleanupMax), "items": map[string]any{
				"type": "object", "required": []string{"aktion"}, "properties": map[string]any{
					"aktion": map[string]any{"type": "string", "enum": []string{"liste_anlegen", "liste_aendern", "listen_zusammenfuehren", "liste_loeschen", "punkte_verschieben", "gruppe_umbenennen", "gruppe_aufloesen"},
						"description": "liste_anlegen (name, gruppe) · liste_aendern (liste, name und/oder gruppe; gruppe \"\" = ohne Gruppe) · listen_zusammenfuehren (liste → ziel; alle Punkte nach ziel, liste wird gelöscht) · " +
							"liste_loeschen (liste; Punkte weg, Notizen bleiben) · punkte_verschieben (punkte → ziel) · gruppe_umbenennen (gruppe, name) · gruppe_aufloesen (gruppe; Listen bleiben ohne Gruppe)"},
					"liste":  map[string]any{"type": "string", "description": "Liste, z. B. \"L12\""},
					"ziel":   map[string]any{"type": "string", "description": "Ziel-Liste, z. B. \"L3\""},
					"name":   map[string]any{"type": "string", "description": "Neuer Name"},
					"gruppe": map[string]any{"type": "string", "description": "Gruppe"},
					"punkte": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Punkte wie \"#59.2\""},
				}}},
			"ausfuehren": map[string]any{"type": "boolean", "description": "true erst nach Zustimmung des Nutzers zum Vorschlag; sonst nur Vorschlag"},
		}},
	},
}

type mcpChange struct {
	Aktion string   `json:"aktion"`
	Liste  string   `json:"liste"`
	Ziel   string   `json:"ziel"`
	Name   *string  `json:"name"`
	Gruppe *string  `json:"gruppe"`
	Punkte []string `json:"punkte"`
}

func (a *App) mcpCleanup(raw json.RawMessage) (string, error) {
	var in struct {
		Aenderungen []mcpChange `json:"aenderungen"`
		Ausfuehren  bool        `json:"ausfuehren"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return "", a.Errorf("Argumente nicht lesbar")
	}
	if len(in.Aenderungen) == 0 {
		return "", a.Errorf("Keine Änderungen angegeben")
	}
	if len(in.Aenderungen) > mcpCleanupMax {
		return "", a.Errorf("Zu viele Änderungen (höchstens %d je Aufruf)", mcpCleanupMax)
	}
	var b strings.Builder
	if in.Ausfuehren {
		b.WriteString(a.T("Ausgeführt:") + "\n")
	} else {
		b.WriteString(a.T("VORSCHLAG – noch nichts geändert. Dem Nutzer zeigen; nur nach seiner Zustimmung mit ausfuehren=true erneut aufrufen:") + "\n")
	}
	ok, failed := 0, 0
	for i, c := range in.Aenderungen {
		text, err := a.mcpChangeOne(c, in.Ausfuehren)
		if err != nil {
			failed++
			fmt.Fprintf(&b, "%d. %s: %s\n", i+1, a.T("FEHLER"), err)
			continue
		}
		ok++
		fmt.Fprintf(&b, "%d. %s\n", i+1, text)
	}
	if failed > 0 && !in.Ausfuehren {
		b.WriteString(a.Tf("%d Änderung(en) nicht möglich – vor dem Ausführen korrigieren.", failed))
	} else if in.Ausfuehren {
		b.WriteString(a.Tf("%d erledigt, %d fehlgeschlagen.", ok, failed))
	}
	return strings.TrimSpace(b.String()), nil
}

// mcpResolveList: Liste als „L12“ oder als eindeutiger Name.
func (a *App) mcpResolveList(ref string) (*store.List, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, a.Errorf("Liste fehlt")
	}
	lists, err := a.DB.Lists()
	if err != nil {
		return nil, a.Errorf("Datenbankfehler")
	}
	var found []*store.List
	for _, l := range lists {
		if strings.EqualFold(ref, fmt.Sprintf("L%d", l.Nr)) {
			return l, nil
		}
		if strings.EqualFold(ref, l.Name) {
			found = append(found, l)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, a.Errorf("Liste %s gibt es nicht", ref)
	}
	return nil, a.Errorf("Liste „%s“ gibt es mehrmals – als Nummer angeben", ref)
}

// mcpChangeOne prüft eine Änderung, beschreibt sie und führt sie bei run aus.
func (a *App) mcpChangeOne(c mcpChange, run bool) (string, error) {
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return cleanName(*p)
	}
	switch c.Aktion {
	case "liste_anlegen":
		name, group := str(c.Name), str(c.Gruppe)
		if name == "" {
			return "", a.Errorf("Name fehlt")
		}
		if l, err := a.mcpResolveList(name); err == nil && strings.EqualFold(l.Project, group) {
			return "", a.Errorf("Liste „%s“ gibt es schon (L%d)", name, l.Nr)
		}
		desc := a.Tf("Neue Liste „%s“ in %s", name, groupLabel(group))
		if !run {
			return desc, nil
		}
		l, err := a.findList(group, name)
		if err != nil {
			return "", err
		}
		a.hub.send("listen")
		return desc + fmt.Sprintf(" → L%d", l.Nr), nil

	case "liste_aendern":
		l, err := a.mcpResolveList(c.Liste)
		if err != nil {
			return "", err
		}
		name, group := l.Name, l.Project
		if c.Name != nil {
			name = str(c.Name)
		}
		if c.Gruppe != nil {
			group = str(c.Gruppe)
		}
		if name == l.Name && strings.EqualFold(group, l.Project) {
			return "", a.Errorf("L%d: keine Änderung angegeben", l.Nr)
		}
		desc := a.Tf("%s → „%s“ in %s", mcpListLabel(l), name, groupLabel(group))
		if !run {
			return desc, nil
		}
		return desc, a.mcpHandler(a.handleListEdit, "PUT", l.ID, map[string]any{"name": name, "gruppe": group})

	case "listen_zusammenfuehren":
		src, err := a.mcpResolveList(c.Liste)
		if err != nil {
			return "", err
		}
		dst, err := a.mcpResolveList(c.Ziel)
		if err != nil {
			return "", err
		}
		if src.ID == dst.ID {
			return "", a.Errorf("Liste und Ziel sind gleich")
		}
		items, err := a.DB.ListItems(src.ID)
		if err != nil {
			return "", a.Errorf("Datenbankfehler")
		}
		desc := a.Tf("%s → %s: %d Punkte verschieben, danach %s löschen", mcpListLabel(src), mcpListLabel(dst), len(items), fmt.Sprintf("L%d", src.Nr))
		if !run {
			return desc, nil
		}
		// Direkt umhängen (nicht je Punkt als Korrektur ins Gedächtnis), dann die leere Liste löschen.
		for _, it := range items {
			it.ListID = dst.ID
			if err := a.DB.SaveListItem(it); err != nil {
				return "", a.Errorf("Punkt „%s“: %v", it.Text, err)
			}
			if it.JobID != "" {
				a.changed(it.JobID)
			}
			if it.PlanningID != "" {
				a.hub.send("planung:" + it.PlanningID)
			}
		}
		a.listChanged(src.ID, dst.ID)
		a.observe("korrektur", fmt.Sprintf("Korrektur: Die Liste L%d „%s“ gehört in die Liste L%d „%s“ (zusammengeführt).", src.Nr, src.Name, dst.Nr, dst.Name))
		return desc, a.mcpHandler(a.handleListDelete, "DELETE", src.ID, nil)

	case "liste_loeschen":
		l, err := a.mcpResolveList(c.Liste)
		if err != nil {
			return "", err
		}
		desc := a.Tf("%s löschen (%d Punkte fallen weg, die Notizen bleiben im Verlauf)", mcpListLabel(l), l.Total)
		if !run {
			return desc, nil
		}
		return desc, a.mcpHandler(a.handleListDelete, "DELETE", l.ID, nil)

	case "punkte_verschieben":
		dst, err := a.mcpResolveList(c.Ziel)
		if err != nil {
			return "", err
		}
		if len(c.Punkte) == 0 {
			return "", a.Errorf("Keine Punkte angegeben")
		}
		_, items, err := a.mcpItems()
		if err != nil {
			return "", a.Errorf("Datenbankfehler")
		}
		var moves []mcpItem
		for _, ref := range c.Punkte {
			found, err := mcpFind(items, ref)
			if err != nil {
				return "", err
			}
			for _, m := range found {
				if m.list.ID != dst.ID {
					moves = append(moves, m)
				}
			}
		}
		if len(moves) == 0 {
			return "", a.Errorf("Die Punkte stehen schon in %s", mcpListLabel(dst))
		}
		names := make([]string, len(moves))
		for i, m := range moves {
			names[i] = fmt.Sprintf("#%s %s", m.ref, m.item.Text)
		}
		desc := a.Tf("%s → %s", strings.Join(names, ", "), mcpListLabel(dst))
		if !run {
			return desc, nil
		}
		for _, m := range moves {
			if err := a.mcpHandler(a.handleItemMove, "POST", m.item.ID, map[string]any{"ziel": dst.ID}); err != nil {
				return "", a.Errorf("#%s: %v", m.ref, err)
			}
		}
		return desc, nil

	case "gruppe_umbenennen":
		old, ok := a.groupName(str(c.Gruppe))
		if !ok {
			return "", a.Errorf("Gruppe „%s“ gibt es nicht", str(c.Gruppe))
		}
		name := str(c.Name)
		if name == "" {
			return "", a.Errorf("Name fehlt")
		}
		desc := a.Tf("Gruppe „%s“ → „%s“", old, name)
		if !run {
			return desc, nil
		}
		return desc, a.mcpHandler(a.handleGroupEdit, "PUT", "", map[string]any{"alt": old, "name": name})

	case "gruppe_aufloesen":
		name, ok := a.groupName(str(c.Gruppe))
		if !ok {
			return "", a.Errorf("Gruppe „%s“ gibt es nicht", str(c.Gruppe))
		}
		desc := a.Tf("Gruppe „%s“ auflösen (ihre Listen bleiben, ohne Gruppe)", name)
		if !run {
			return desc, nil
		}
		return desc, a.mcpHandler(a.handleGroupDelete, "POST", "", map[string]any{"name": name})
	}
	return "", a.Errorf("Unbekannte Aktion „%s“", c.Aktion)
}

func groupLabel(group string) string {
	if group == "" {
		return i18n.T("ohne Gruppe")
	}
	return i18n.Tf("Gruppe „%s“", group)
}

// mcpHandler ruft einen Handler der Web-App auf (gleiche Prüfungen und Folgen) und liefert dessen Fehlermeldung.
func (a *App) mcpHandler(h http.HandlerFunc, method, id string, body any) error {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "/mcp-intern", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if id != "" {
		r.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	if rec.Code < 300 {
		return nil
	}
	var out struct {
		Fehler string `json:"fehler"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Fehler == "" {
		out.Fehler = fmt.Sprintf("HTTP %d", rec.Code)
	}
	return fmt.Errorf("%s", out.Fehler)
}
