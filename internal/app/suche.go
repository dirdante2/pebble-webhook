package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// searchField ist ein durchsuchbarer Text eines Eintrags mit seiner Bezeichnung für die Fundstelle.
type searchField struct{ name, text string }

// jsonStrings sammelt alle Texte einer gespeicherten Auswertung (ohne URLs), in Dokumentreihenfolge.
func jsonStrings(raw string) []string {
	var v any
	if raw == "" || json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	var out []string
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch x := v.(type) {
		case string:
			if key != "url" && strings.TrimSpace(x) != "" {
				out = append(out, x)
			}
		case []any:
			for _, e := range x {
				walk(key, e)
			}
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(k, x[k])
			}
		}
	}
	walk("", v)
	return out
}

func lowerRunes(s string) []rune {
	r := []rune(s)
	for i, c := range r {
		r[i] = unicode.ToLower(c)
	}
	return r
}

func runeIndex(hay, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(hay) {
		return -1
	}
outer:
	for i := 0; i+len(needle) <= len(hay); i++ {
		for k, c := range needle {
			if hay[i+k] != c {
				continue outer
			}
		}
		return i
	}
	return -1
}

// snippet schneidet um die Fundstelle einen Ausschnitt von etwa 140 Zeichen aus.
func snippet(text string, at, n int) string {
	r := []rune(text)
	start, end := max(0, at-60), min(len(r), at+n+80)
	s := string(r[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) {
		s += "…"
	}
	return s
}

// matchFields prüft, ob alle Suchwörter in den Feldern vorkommen, und liefert die Fundstelle des ersten Worts
// (bevorzugt im Titel). ok=false: mindestens ein Wort fehlt.
func matchFields(fields []searchField, terms [][]rune) (name, snip string, inTitle, ok bool) {
	var all []rune
	norm := make([]string, len(fields))
	for i, f := range fields {
		norm[i] = strings.Join(strings.Fields(f.text), " ")
		all = append(all, lowerRunes(norm[i])...)
		all = append(all, '\n')
	}
	for _, t := range terms {
		if runeIndex(all, t) < 0 {
			return "", "", false, false
		}
	}
	for _, t := range terms {
		for i, f := range fields {
			if at := runeIndex(lowerRunes(norm[i]), t); at >= 0 {
				return f.name, snippet(norm[i], at, len(t)), i == 0, true
			}
		}
	}
	return "", "", false, true
}

// handleSearch: GET /api/suche?q=… – Volltextsuche über alle Notizen und Planungen (Titel, Wortlaut, Auswertung,
// Tags). Alle Wörter müssen vorkommen, Groß-/Kleinschreibung egal; „#46“ findet die Nummer. Treffer im Titel zuerst,
// sonst die neuesten; höchstens 100.
func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, 200, map[string]any{"treffer": []any{}, "q": q})
		return
	}
	var nr int64 = -1
	if n, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
		nr = n
	}
	var terms [][]rune
	for _, t := range strings.Fields(q) {
		terms = append(terms, lowerRunes(t))
	}
	type hit struct {
		m       map[string]any
		inTitle bool
		at      int64
	}
	var hits []hit
	add := func(m map[string]any, number int64, at int64, fields []searchField) {
		if nr >= 0 && number == nr {
			m["fundstelle"] = map[string]any{"feld": "Nummer", "text": "#" + strconv.FormatInt(nr, 10)}
			hits = append(hits, hit{m, true, at})
			return
		}
		name, snip, inTitle, ok := matchFields(fields, terms)
		if !ok {
			return
		}
		m["fundstelle"] = map[string]any{"feld": name, "text": snip}
		hits = append(hits, hit{m, inTitle, at})
	}

	jobs, err := a.DB.SearchableJobs()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	for _, j := range jobs {
		m := a.jobSummary(j)
		fields := []searchField{{"Titel", m["titel"].(string)}, {"Wortlaut", j.Transcript}}
		for _, s := range jsonStrings(j.Result) {
			fields = append(fields, searchField{"Auswertung", s})
		}
		if t, ok := m["tags"].([]string); ok && len(t) > 0 {
			fields = append(fields, searchField{"Tags", strings.Join(t, " ")})
		}
		add(m, j.Nr, j.ReceivedAt, fields)
	}
	plannings, err := a.DB.AllPlannings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	for _, p := range plannings {
		rounds, err := a.DB.Rounds(p.ID)
		if err != nil || len(rounds) == 0 {
			continue
		}
		m := a.planningSummary(p, rounds)
		fields := []searchField{{"Titel", m["titel"].(string)}}
		for _, s := range jsonStrings(p.Plan) {
			fields = append(fields, searchField{"Ergebnis", s})
		}
		for _, rd := range rounds {
			fields = append(fields, searchField{"Wortlaut", rd.Transcript})
		}
		if t, ok := m["tags"].([]string); ok && len(t) > 0 {
			fields = append(fields, searchField{"Tags", strings.Join(t, " ")})
		}
		add(m, p.Nr, rounds[len(rounds)-1].ReceivedAt, fields)
	}
	sort.SliceStable(hits, func(i, k int) bool {
		if hits[i].inTitle != hits[k].inTitle {
			return hits[i].inTitle
		}
		return hits[i].at > hits[k].at
	})
	out := make([]map[string]any, 0, min(len(hits), 100))
	for _, h := range hits[:min(len(hits), 100)] {
		out = append(out, h.m)
	}
	writeJSON(w, 200, map[string]any{"treffer": out, "q": q, "anzahl": len(hits)})
}
