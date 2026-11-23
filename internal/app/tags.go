package app

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"pebble-webhook/internal/store"
)

const maxTags = 10

// cleanTag bringt einen Tag in Form: klein geschrieben (wie Joplin seine Schlagwörter führt),
// eine Zeile, höchstens 30 Zeichen, ohne Zeichen, die in der Ablage (JSON, LIKE) stören.
func cleanTag(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '"', '%', '_', '\\', ',', '#', '[', ']':
			return ' '
		}
		return r
	}, s)
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if r := []rune(s); len(r) > 30 {
		s = strings.TrimSpace(string(r[:30]))
	}
	return s
}

func hasTag(list []string, name string) bool {
	for _, t := range list {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}

// applyAITags übernimmt die Tag-Wahl der KI: Tags aus dem Vorrat werden vergeben,
// alles Neue bleibt ein Vorschlag, bis der Nutzer ihn übernimmt.
func (a *App) applyAITags(kind, id string, chosen, proposed []string) error {
	pool, err := a.DB.AllTags()
	if err != nil {
		return err
	}
	set, err := a.DB.Tags(kind, id)
	if err != nil {
		return err
	}
	canonical := func(name string) string {
		for t := range pool {
			if strings.EqualFold(t, name) {
				return t
			}
		}
		return ""
	}
	var suggestions []string
	for _, raw := range chosen {
		name := cleanTag(raw)
		if name == "" {
			continue
		}
		if c := canonical(name); c != "" {
			if !hasTag(set.Tags, c) && len(set.Tags) < maxTags {
				set.Tags = append(set.Tags, c)
			}
		} else {
			proposed = append(proposed, name) // „vorhanden“ behauptet, aber unbekannt: als Vorschlag behandeln
		}
	}
	for _, raw := range proposed {
		name := cleanTag(raw)
		if name == "" || hasTag(set.Tags, name) || hasTag(suggestions, name) || len(suggestions) >= 3 {
			continue
		}
		if c := canonical(name); c != "" { // als neu vorgeschlagen, gibt es aber schon
			if len(set.Tags) < maxTags {
				set.Tags = append(set.Tags, c)
			}
			continue
		}
		suggestions = append(suggestions, name)
	}
	return a.DB.SetTags(kind, id, set.Tags, suggestions)
}

// tagHint beschreibt der KI den Vorrat vorhandener Tags.
func (a *App) tagHint() string {
	pool, _ := a.DB.AllTags()
	if len(pool) == 0 {
		return "Vorhandene Tags: (noch keine)"
	}
	names := make([]string, 0, len(pool))
	for t := range pool {
		names = append(names, t)
	}
	sort.Strings(names)
	return "Vorhandene Tags: " + strings.Join(names, ", ")
}

const tagRules = `tags: bis zu 3 Tags aus der Liste „Vorhandene Tags“, die zum Inhalt passen – exakt so geschrieben; passt keiner, leer lassen. ` +
	`neue_tags: höchstens 2 Vorschläge für neue Tags (je ein kurzes Themenwort wie „garten“ oder „auto“, klein geschrieben), nur wenn kein vorhandener Tag das Thema trifft; sonst leer.`

// pushTags gleicht die Schlagwörter einer Notiz in Joplin an den Stand des Eintrags an.
func (a *App) pushTags(kind, id, noteID string) error {
	set, err := a.DB.Tags(kind, id)
	if err != nil {
		return err
	}
	tagID := func(name string) string { return store.DerivedID("tag:" + strings.ToLower(name)) }
	linkID := func(name string) string { return store.DerivedID("notetag:" + noteID + ":" + tagID(name)) }
	for _, t := range set.Tags {
		if hasTag(set.Pushed, t) {
			continue
		}
		if err := a.Notes.EnsureTag(tagID(t), t); err != nil {
			return err
		}
		if err := a.Notes.PutNoteTag(linkID(t), noteID, tagID(t)); err != nil {
			return err
		}
	}
	for _, t := range set.Pushed {
		if !hasTag(set.Tags, t) {
			if err := a.Notes.Delete(linkID(t)); err != nil {
				return err
			}
		}
	}
	return a.DB.SetPushedTags(kind, id, set.Tags)
}

// tagTarget löst die Pfad-Id in Art und Joplin-Notiz auf und prüft, dass es den Eintrag gibt.
func (a *App) tagTarget(kind, id string) bool {
	if kind == "planung" {
		_, err := a.DB.Planning(id)
		return err == nil
	}
	j, err := a.DB.Job(id)
	return err == nil && j.PlanningID == ""
}

func (a *App) tagsChanged(kind, id string) {
	if kind == "planung" {
		a.hub.send("planung:" + id)
	} else {
		a.changed(id)
	}
	a.Wake()
}

func (a *App) handleTags(w http.ResponseWriter, r *http.Request) {
	stats, err := a.DB.TagStats()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	// Nach Menge, bei Gleichstand nach Name.
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Name < stats[j].Name
	})
	out := make([]map[string]any, 0, len(stats))
	for _, st := range stats {
		out = append(out, map[string]any{"name": st.Name, "anzahl": st.Count, "zuletzt": st.Last})
	}
	sets, err := a.DB.TagSets()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	// kombis: Tag-Zusammenstellungen mit ihrer Zahl an Einträgen – die Web-App zählt damit beim Auswählen, was zu den gewählten Tags noch passt.
	writeJSON(w, 200, map[string]any{"tags": out, "kombis": sets})
}

// handleDeleteTag nimmt einen Tag aus allen Einträgen (Status → Tags).
func (a *App) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	name := cleanTag(r.PathValue("name"))
	if name == "" {
		writeErr(w, 400, "ungueltig", "Tag fehlt")
		return
	}
	n, err := a.DB.RemoveTag(name)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if n > 0 {
		a.hub.send("tags")
		a.Wake()
	}
	writeJSON(w, 200, map[string]any{"ok": true, "eintraege": n})
}

// handleSetTags setzt die Tags eines Eintrags (Bearbeitungsseite).
func (a *App) handleSetTags(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var in struct {
			Tags []string `json:"tags"`
		}
		if !a.tagTarget(kind, id) {
			writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
			return
		}
		if readJSON(r, &in) != nil {
			writeErr(w, 400, "ungueltig", "Tags nicht lesbar")
			return
		}
		pool, _ := a.DB.AllTags()
		var tags []string
		for _, raw := range in.Tags {
			name := cleanTag(raw)
			for t := range pool { // Schreibweise des Vorrats übernehmen
				if strings.EqualFold(t, name) {
					name = t
				}
			}
			if name != "" && !hasTag(tags, name) && len(tags) < maxTags {
				tags = append(tags, name)
			}
		}
		set, _ := a.DB.Tags(kind, id)
		var rest []string
		for _, s := range set.Suggestions {
			if !hasTag(tags, s) {
				rest = append(rest, s)
			}
		}
		if err := a.DB.SetTags(kind, id, tags, rest); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if strings.Join(tags, ",") != strings.Join(set.Tags, ",") {
			was := "keine"
			if len(tags) > 0 {
				was = strings.Join(tags, ", ")
			}
			a.observe("korrektur", fmt.Sprintf("Korrektur: „%s“ trägt nach Änderung des Nutzers die Tags: %s.", a.entryTitle(kind, id), was))
		}
		a.tagsChanged(kind, id)
		writeJSON(w, 200, map[string]any{"tags": tags})
	}
}

// handleTagSuggestion übernimmt oder verwirft einen KI-Vorschlag.
func (a *App) handleTagSuggestion(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var in struct {
			Tag    string `json:"tag"`
			Aktion string `json:"aktion"`
		}
		if !a.tagTarget(kind, id) {
			writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
			return
		}
		if readJSON(r, &in) != nil || (in.Aktion != "uebernehmen" && in.Aktion != "verwerfen") {
			writeErr(w, 400, "ungueltig", "Aktion fehlt")
			return
		}
		set, err := a.DB.Tags(kind, id)
		if err != nil || !hasTag(set.Suggestions, in.Tag) {
			writeErr(w, http.StatusConflict, "weg", "Den Vorschlag gibt es nicht mehr")
			return
		}
		var rest []string
		for _, s := range set.Suggestions {
			if !strings.EqualFold(s, in.Tag) {
				rest = append(rest, s)
			}
		}
		if in.Aktion == "uebernehmen" && !hasTag(set.Tags, in.Tag) && len(set.Tags) < maxTags {
			set.Tags = append(set.Tags, cleanTag(in.Tag))
		}
		if in.Aktion == "verwerfen" {
			a.observe("korrektur", fmt.Sprintf("Korrektur: Tag-Vorschlag „%s“ für „%s“ hat der Nutzer verworfen.", cleanTag(in.Tag), a.entryTitle(kind, id)))
		}
		if err := a.DB.SetTags(kind, id, set.Tags, rest); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		a.tagsChanged(kind, id)
		writeJSON(w, 200, map[string]any{"tags": set.Tags})
	}
}

// normalizeStoredTags bringt Tags aus früheren Fassungen auf die heutige Form (klein geschrieben).
func (a *App) normalizeStoredTags() error {
	for _, kind := range []string{"job", "planung"} {
		ids, err := a.DB.TaggedIDs(kind)
		if err != nil {
			return err
		}
		for _, id := range ids {
			set, err := a.DB.Tags(kind, id)
			if err != nil {
				return err
			}
			fix := func(in []string) (out []string, changed bool) {
				for _, t := range in {
					c := cleanTag(t)
					changed = changed || c != t
					if c != "" && !hasTag(out, c) {
						out = append(out, c)
					}
				}
				return
			}
			tags, c1 := fix(set.Tags)
			sugg, c2 := fix(set.Suggestions)
			if c1 || c2 {
				if err := a.DB.SetTags(kind, id, tags, sugg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
