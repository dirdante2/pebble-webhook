package app

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/store"
)

// Trailer für Film- und Serien-Notizen aus der Zeit vor den Medien nachtragen: auf Knopfdruck je Aufruf die nächsten
// n Notizen (neueste zuerst), im Hintergrund; die Detailseiten aktualisieren sich über die Live-Meldungen.

// filmFacts: Eckdaten, an denen die Auswertung einen Film oder eine Serie erkennbar macht.
var filmFacts = map[string]bool{"regie": true, "idee": true, "schauspieler": true, "staffeln": true,
	"director": true, "creator": true, "cast": true, "seasons": true}

var yearParenRe = regexp.MustCompile(`\s*\((\d{4})\)\s*$`)

// filmQueries: Suchbegriffe (Titel und Jahr) aller Filme und Serien einer Notiz; leer = keine Film-Notiz.
func filmQueries(d *Dictation) []string {
	isFilm := func(facts []Fact) bool {
		for _, f := range facts {
			if filmFacts[strings.ToLower(strings.TrimSpace(f.Name))] {
				return true
			}
		}
		return false
	}
	year := func(facts []Fact) string {
		for _, f := range facts {
			if n := strings.ToLower(f.Name); (n == "jahr" || n == "year") && len(strings.TrimSpace(f.Wert)) >= 4 {
				return strings.TrimSpace(f.Wert)[:4]
			}
		}
		return ""
	}
	query := func(title string, facts []Fact) string {
		title = strings.TrimSpace(title)
		if m := yearParenRe.FindStringSubmatch(title); m != nil {
			return strings.TrimSpace(yearParenRe.ReplaceAllString(title, "")) + " " + m[1]
		}
		if y := year(facts); y != "" {
			return title + " " + y
		}
		return title
	}
	var out []string
	for _, p := range d.Punkte {
		if isFilm(p.Eckdaten) && p.Text != "" && len(out) < maxTrailers {
			out = append(out, query(p.Text, p.Eckdaten))
		}
	}
	if len(out) == 0 && isFilm(d.Eckdaten) && d.Titel != "" {
		out = append(out, query(d.Titel, d.Eckdaten))
	}
	return out
}

func hasTrailer(d *Dictation) bool {
	for _, m := range d.Medien {
		if m.Art == "trailer" {
			return true
		}
	}
	return false
}

// trailerCandidates: fertige Film-Notizen ohne Trailer (neueste zuerst).
func (a *App) trailerCandidates() ([]*store.Job, error) {
	jobs, err := a.DB.SearchableJobs()
	if err != nil {
		return nil, err
	}
	var out []*store.Job
	for _, j := range jobs {
		if j.State != store.StateDone || j.Failed {
			continue
		}
		if d := parseDictation(j.Result); d != nil && !hasTrailer(d) && len(filmQueries(d)) > 0 {
			out = append(out, j)
		}
	}
	return out, nil
}

func (a *App) handleTrailerBackfill(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if n <= 0 {
		n = 5
	}
	n = min(n, 50)
	if !a.mediaBusy.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "laeuft", "Trailer werden gerade nachgetragen")
		return
	}
	jobs, err := a.trailerCandidates()
	if err != nil {
		a.mediaBusy.Store(false)
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	todo := jobs[:min(n, len(jobs))]
	ids := make([]string, len(todo))
	for i, j := range todo {
		ids[i] = j.ID
	}
	ctx := a.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer a.mediaBusy.Store(false)
		ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		for _, id := range ids {
			if err := a.addTrailers(ctx, id); err != nil {
				logf("Trailer nachtragen %s: %v", id, err)
			}
		}
		logf("Trailer nachgetragen: %d Notizen, %d ohne Trailer übrig", len(ids), len(jobs)-len(ids))
	}()
	writeJSON(w, 200, map[string]any{"ok": true, "gestartet": len(ids), "offen": len(jobs) - len(ids)})
}

// addTrailers sucht die Trailer einer Notiz und schreibt sie in ihre Auswertung (frisch gelesen, nur Medien ergänzt).
func (a *App) addTrailers(ctx context.Context, id string) error {
	j, err := a.DB.Job(id)
	if err != nil {
		return err
	}
	d := parseDictation(j.Result)
	if d == nil || hasTrailer(d) {
		return nil
	}
	var media []Medium
	nr := 0
	for _, m := range d.Medien {
		nr = max(nr, m.Nr)
	}
	for _, q := range filmQueries(d) {
		nr++
		media = append(media, Medium{Nr: nr, Art: "trailer", Titel: "Trailer", Suche: q})
	}
	_, cost := a.resolveMedia(ctx, media)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Erneut lesen: die Notiz kann sich während der Suche geändert haben.
	if j, err = a.DB.Job(id); err != nil {
		return err
	}
	if d = parseDictation(j.Result); d == nil || hasTrailer(d) {
		return nil
	}
	d.Medien = append(d.Medien, media...)
	data, _ := json.Marshal(d)
	if err := a.DB.SetJobResult(id, string(data)); err != nil {
		return err
	}
	if cost != nil {
		if fresh, err := a.DB.Job(id); err == nil {
			addCost(&fresh.CostUSD, cost)
			a.DB.SaveJob(fresh)
		}
	}
	a.DB.MarkJobDirty(id) // Joplin bekommt die Trailer-Links
	a.changed(id)
	return nil
}
