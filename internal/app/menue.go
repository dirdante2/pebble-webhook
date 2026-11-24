package app

import (
	"net/http"
	"slices"
	"sort"
	"strconv"
)

// Zahlen für das Seitenmenü (Nutzervorgabe 23.11.2026: Menü bleibt aktuell, auch ohne Verlauf oder Listen zu öffnen).
// GET /api/zaehler?menue=1&seit=…&tag=… liefert sie mit; die Web-App lädt das bei jedem Seitenwechsel und jedem
// Live-Ereignis (erledigt, gelöscht, Liste/Gruppe angelegt oder gelöscht …).

// menuCounts: zahlen (offene Einträge je Verlauf-Filter, wie im Verlauf mit Tag-Filter), neu (seit zuletzt gesehen)
// und gruppen [[Name oder "-", offene Punkte, davon neu], …], Gruppe mit dem jüngsten Punkt zuerst.
func (a *App) menuCounts(r *http.Request) (map[string]any, error) {
	var tags []string
	for _, t := range r.URL.Query()["tag"] {
		if t = cleanTag(t); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	seit, _ := strconv.ParseInt(r.URL.Query().Get("seit"), 10, 64)

	counts := map[string]any{"fragen": a.openQuestionsTotal()}
	upcoming, err := a.DB.Appointments(false)
	if err != nil {
		return nil, err
	}
	counts["termin"] = len(upcoming)
	for name, k := range map[string]string{"alles": "", "diktat": "diktat", "planung": "planung"} {
		n, err := a.DB.HistoryOpenCount(k, tags, 0)
		if err != nil {
			return nil, err
		}
		counts[name] = n
	}
	newCounts := map[string]int{}
	if seit > 0 {
		for name, k := range map[string]string{"alles": "", "diktat": "diktat", "termin": "termin", "planung": "planung"} {
			n, err := a.DB.HistoryUnreadCount(k, tags, seit)
			if err != nil {
				return nil, err
			}
			newCounts[name] = n
		}
	}

	lists, err := a.DB.Lists()
	if err != nil {
		return nil, err
	}
	items, err := a.DB.ItemsActive(a.pastBefore())
	if err != nil {
		return nil, err
	}
	unread, err := a.DB.UnreadRefs()
	if err != nil {
		return nil, err
	}
	fresh := map[string]int{}
	for _, it := range items {
		ref := it.JobID
		if it.PlanningID != "" {
			ref = it.PlanningID
		}
		if it.DoneAt == 0 && ref != "" && unread[ref] && seit > 0 && it.CreatedAt >= seit {
			fresh[it.ListID]++
		}
	}
	type group struct {
		name        string
		open, fresh int
		newest      int64
	}
	var groups []*group
	byName := map[string]*group{}
	sorted := slices.Clone(lists)
	sort.SliceStable(sorted, func(i, k int) bool { return sorted[i].NewestAt > sorted[k].NewestAt })
	for _, l := range sorted {
		name := l.Project
		if name == "" {
			name = "-"
		}
		g := byName[name]
		if g == nil {
			g = &group{name: name, newest: l.NewestAt}
			byName[name] = g
			groups = append(groups, g)
		}
		g.open += l.Open
		g.fresh += fresh[l.ID]
	}
	out := make([][]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, []any{g.name, g.open, g.fresh})
	}
	return map[string]any{"zahlen": counts, "neu": newCounts, "gruppen": out}, nil
}

// openQuestionsTotal: offene Rückfragen an offenen Planungen und an Notizen (Zahl am Filter „Fragen“).
func (a *App) openQuestionsTotal() int {
	total := 0
	if open, err := a.DB.OpenPlannings(); err == nil {
		for _, p := range open {
			if rounds, err := a.DB.Rounds(p.ID); err == nil {
				total += a.openQuestions(p, rounds)
			}
		}
	}
	if jobs, err := a.DB.JobsAsking(); err == nil {
		for _, j := range jobs {
			if d := parseDictation(j.Result); d != nil {
				total += len(d.Fragen)
			}
		}
	}
	return total
}
