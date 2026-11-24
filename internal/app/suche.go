package app

import (
	"encoding/json"
	"net/http"
	"pebble-webhook/internal/store"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// searchField ist ein durchsuchbarer Text eines Eintrags mit seiner Bezeichnung für die Fundstelle.
type searchField struct{ name, text string }

// skipKeys: Felder der Auswertung, die die Suche nicht als Text zählt (URLs; Tags zählen nur über die Tags-Spalte).
var skipKeys = map[string]bool{"url": true, "tags": true, "neue_tags": true,
	"prompt": true, "bild": true, "quelle": true, "video": true, "fehler": true, "suche": true, "art": true, "typ": true}

// jsonStrings sammelt alle Texte einer gespeicherten Auswertung (ohne URLs und Tags), in Dokumentreihenfolge.
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
			if x = stripMediaMarkers(x); !skipKeys[key] && x != "" {
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

// matchFields prüft, ob jedes Suchwort (mindestens einmal) in den Feldern vorkommt, und liefert je Wort seine erste
// Fundstelle (bevorzugt im Titel, dann in Feldreihenfolge). Liegen mehrere Wörter im selben Ausschnitt, steht er nur
// einmal da. Wie oft ein Wort vorkommt, zählt nicht. ok=false: mindestens ein Wort fehlt.
func matchFields(fields []searchField, terms [][]rune) (funde []map[string]any, ok bool) {
	norm := make([]string, len(fields))
	low := make([][]rune, len(fields))
	for i, f := range fields {
		norm[i] = strings.Join(strings.Fields(f.text), " ")
		low[i] = lowerRunes(norm[i])
	}
	type span struct{ field, from, to int }
	var spans []span
	for _, t := range terms {
		found := false
		for i := range fields {
			at := runeIndex(low[i], t)
			if at < 0 {
				continue
			}
			found = true
			covered := false
			for _, sp := range spans {
				if sp.field == i && at >= sp.from && at+len(t) <= sp.to {
					covered = true
				}
			}
			if !covered {
				start, end := max(0, at-40), min(len(low[i]), at+len(t)+60)
				spans = append(spans, span{i, start, end})
				funde = append(funde, map[string]any{"feld": fields[i].name, "text": snippetRange(norm[i], start, end)})
			}
			break
		}
		if !found {
			return nil, false
		}
	}
	return funde, true
}

// snippetRange schneidet den Ausschnitt [start, end) aus, mit „…“ an abgeschnittenen Enden.
func snippetRange(text string, start, end int) string {
	r := []rune(text)
	s := string(r[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) {
		s += "…"
	}
	return s
}

// handleSearch: GET /api/suche?q=… – Volltextsuche über alle Notizen und Planungen (Titel, Wortlaut, Auswertung,
// Tags). Alle Wörter müssen vorkommen, Groß-/Kleinschreibung egal; „#46“ findet die Nummer. Neueste zuerst, je Seite 50;
// vor=<vor_naechste> holt die nächste Seite.
func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, 200, map[string]any{"treffer": []any{}, "q": q, "mehr": false})
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
	// Neueste zuerst, seitenweise: vor= setzt hinter dem letzten Treffer der vorigen Seite fort.
	before, _ := strconv.ParseInt(r.URL.Query().Get("vor"), 10, 64)
	if before <= 0 {
		before = 1 << 62
	}
	// Vorfilter in SQL nur mit Wörtern aus ASCII-Zeichen (dort faltet lower() die Großschreibung sicher).
	var ascii []string
	for _, t := range terms {
		if isASCII(t) {
			ascii = append(ascii, string(t))
		}
	}
	const pageSize, chunk = 50, 300
	out := []map[string]any{}
	cursor, more := int64(0), false
	tagList := func(raw string) string {
		var t []string
		json.Unmarshal([]byte(raw), &t)
		return strings.Join(t, " ")
	}
	// Notizen und Planungen je in Blöcken holen und nach Zeit mischen; bis zur Grenze, an der ein voller Block endet,
	// ist die Reihenfolge sicher.
	// Suchindex (FTS5), sobald ein Wort mindestens 3 Zeichen hat; sonst (kurze Wörter, Nummer) der Durchlauf über die
	// Tabellen mit Vorfilter. Die genaue Prüfung und die Fundstelle macht matchFields in beiden Fällen.
	var long []string
	for _, t := range terms {
		if len(t) >= 3 {
			long = append(long, string(t))
		}
	}
	useIndex := nr < 0 && len(long) > 0
	// Stufe 1: alle Wörter in Titel/Text, Stufe 2: erst mit den Tags (zählen weniger); jede Stufe neueste zuerst.
	tier := 1
	if r.URL.Query().Get("stufe") == "2" {
		tier = 2
	}
	if useIndex {
		if err := a.DB.SearchSync(); err != nil {
			logf("Suchindex: %v", err)
			useIndex = false
		}
	}
	candidates := func() ([]store.SearchRow, int64, error) {
		if useIndex {
			rows, err := a.DB.SearchIndex(long, tier, before, chunk)
			if err != nil || len(rows) < chunk {
				return rows, 0, err
			}
			return rows, rows[len(rows)-1].At, nil
		}
		jobs, err := a.DB.SearchJobs(before, chunk, ascii, nr)
		if err != nil {
			return nil, 0, err
		}
		plans, err := a.DB.SearchPlannings(before, chunk, ascii, nr)
		if err != nil {
			return nil, 0, err
		}
		bound := int64(0)
		if len(jobs) == chunk {
			bound = max(bound, jobs[len(jobs)-1].At)
		}
		if len(plans) == chunk {
			bound = max(bound, plans[len(plans)-1].At)
		}
		cands := append(jobs, plans...)
		sort.SliceStable(cands, func(i, k int) bool { return cands[i].At > cands[k].At })
		return cands, bound, nil
	}
	for !more {
		cands, bound, err := candidates()
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		for _, c := range cands {
			if c.At < bound {
				break
			}
			var fields []searchField
			if c.Kind == "job" {
				fields = []searchField{{"Titel", shortTitle(c.Title)}, {"Wortlaut", c.Transcript}}
				for _, s := range jsonStrings(c.Result) {
					fields = append(fields, searchField{"Auswertung", s})
				}
			} else {
				fields = []searchField{{"Titel", c.Title}}
				for _, s := range jsonStrings(c.Result) {
					fields = append(fields, searchField{"Ergebnis", s})
				}
				fields = append(fields, searchField{"Wortlaut", c.Transcript})
			}
			if t := tagList(c.Tags); t != "" {
				fields = append(fields, searchField{"Tags", t})
			}
			var funde []map[string]any
			if nr >= 0 && c.Nr == nr {
				funde = []map[string]any{{"feld": "Nummer", "text": "#" + strconv.FormatInt(nr, 10)}}
			} else if f, ok := matchFields(fields, terms); ok {
				funde = f
			} else {
				continue
			}
			if len(out) == pageSize {
				more = true
				break
			}
			// Zusammenfassung nur für Treffer.
			var m map[string]any
			if c.Kind == "job" {
				j, err := a.DB.Job(c.ID)
				if err != nil {
					continue
				}
				m = a.jobSummary(j)
			} else {
				p, err := a.DB.Planning(c.ID)
				if err != nil {
					continue
				}
				rounds, err := a.DB.Rounds(p.ID)
				if err != nil || len(rounds) == 0 {
					continue
				}
				m = a.planningSummary(p, rounds)
			}
			// fundstellen: je Suchwort, wo es steht (der Grund für den Treffer); fundstelle = die erste (MCP, ältere Clients).
			m["fundstellen"], m["fundstelle"] = funde, funde[0]
			out = append(out, m)
			cursor = c.At
		}
		if bound == 0 {
			// Stufe 1 erschöpft: weiter mit Stufe 2 wieder ab dem neuesten.
			if useIndex && tier == 1 && !more {
				// Endet die Seite, bevor ein Treffer aus Stufe 2 dabei ist, beginnt die nächste dort beim neuesten.
				tier, before, cursor = 2, 1<<62, 1<<62
				continue
			}
			break
		}
		before = bound
	}
	// Seite endet genau am Ende von Stufe 1: die nächste beginnt mit Stufe 2.
	writeJSON(w, 200, map[string]any{"treffer": out, "q": q, "mehr": more, "vor_naechste": cursor, "stufe": tier})
}

func isASCII(r []rune) bool {
	for _, c := range r {
		if c > 127 {
			return false
		}
	}
	return true
}
