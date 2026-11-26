package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type fact struct {
	Name string `json:"name"`
	Wert string `json:"wert"`
}

type factItem struct {
	id, text string
	facts    []fact
}

// itemFactsToNotes (Stand 3, 4.5.0): Eckdaten stehen nur noch an der Notiz, Listenpunkte tragen keine. Die Eckdaten eines
// Punkts gehen in seine Notiz. Trägt eine Notiz mehrere Punkte mit Eckdaten (Sammlung, z. B. mehrere Filme), wird je
// weiterem Punkt eine eigene Notiz angelegt und der Punkt dorthin umgehängt – außer bei Aufnahmen vom Ring, die nie
// aufgeteilt werden: dort kommen alle Eckdaten mit dem Punktnamen davor an die eine Notiz. Danach fällt die Spalte weg.
func itemFactsToNotes(tx *sql.Tx, _ int64) error {
	rows, err := tx.Query(`SELECT id, job_id, text, facts FROM list_items
		WHERE job_id != '' AND facts NOT IN ('', '[]', 'null') ORDER BY job_id, created_at`)
	if err != nil {
		return err
	}
	byJob := map[string][]factItem{}
	var order []string
	for rows.Next() {
		var jobID, raw string
		var it factItem
		if err := rows.Scan(&it.id, &jobID, &it.text, &raw); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(raw), &it.facts) != nil || len(it.facts) == 0 {
			continue
		}
		if byJob[jobID] == nil {
			order = append(order, jobID)
		}
		byJob[jobID] = append(byJob[jobID], it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	cols, err := tableCols(tx, "jobs")
	if err != nil {
		return err
	}
	for _, jobID := range order {
		if err := moveFacts(tx, cols, jobID, byJob[jobID]); err != nil {
			return fmt.Errorf("Notiz %s: %w", jobID, err)
		}
	}
	_, err = tx.Exec(`ALTER TABLE list_items DROP COLUMN facts`)
	return err
}

func moveFacts(tx *sql.Tx, cols []string, jobID string, items []factItem) error {
	var result, source, title string
	var ring int
	err := tx.QueryRow(`SELECT result, source, title, (SELECT COUNT(*) FROM ring_recordings WHERE job_id = jobs.id) FROM jobs WHERE id = ?`,
		jobID).Scan(&result, &source, &title, &ring)
	if err == sql.ErrNoRows {
		return nil // Punkt ohne Notiz: die Eckdaten haben kein Ziel
	}
	if err != nil {
		return err
	}
	d := map[string]any{}
	if result != "" && json.Unmarshal([]byte(result), &d) != nil {
		return fmt.Errorf("Auswertung nicht lesbar")
	}
	own := readFacts(d["eckdaten"])
	fromRing := source == "pebble" || ring > 0
	if len(items) == 1 || fromRing {
		for _, it := range items {
			fs := it.facts
			if len(items) > 1 {
				fs = make([]fact, len(it.facts))
				for i, f := range it.facts {
					fs[i] = fact{Name: it.text + " · " + f.Name, Wert: f.Wert}
				}
			}
			own = mergeFacts(own, fs)
		}
		d["eckdaten"] = own
		d["punkte"] = stripPointFacts(d["punkte"], nil)
		return saveResult(tx, jobID, d)
	}
	// Sammlung: der erste Punkt bleibt an der Notiz, jeder weitere wird eine eigene Notiz.
	d["eckdaten"] = mergeFacts(own, items[0].facts)
	d["punkte"] = stripPointFacts(d["punkte"], &items[0].text)
	if t := stringOf(d["titel"]); t != "" {
		d["titel"] = items[0].text
		title = replaceTitle(title, items[0].text)
	}
	if err := saveResult(tx, jobID, d); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE jobs SET title = ? WHERE id = ?`, title, jobID); err != nil {
		return err
	}
	for _, it := range items[1:] {
		nd := map[string]any{}
		for k, v := range d {
			nd[k] = v
		}
		nd["titel"], nd["eckdaten"], nd["punkte"] = it.text, it.facts, []map[string]any{{"text": it.text}}
		delete(nd, "beschreibung")
		delete(nd, "medien")
		delete(nd, "rueckfragen")
		data, _ := json.Marshal(nd)
		nr, err := takeNr(tx)
		if err != nil {
			return err
		}
		id := NewID()
		sel := make([]string, len(cols))
		args := []any{}
		for i, c := range cols {
			switch c {
			case "id", "delivery_id", "nr", "title", "result":
				sel[i] = "?"
				args = append(args, map[string]any{"id": id, "delivery_id": "teil:" + it.id, "nr": nr,
					"title": replaceTitle(title, it.text), "result": string(data)}[c])
			case "audio_path", "joplin_id":
				sel[i] = "''"
			case "joplin_pushed_at":
				sel[i] = "0"
			default:
				sel[i] = c
			}
		}
		args = append(args, jobID)
		if _, err := tx.Exec(`INSERT INTO jobs (`+strings.Join(cols, ", ")+`) SELECT `+strings.Join(sel, ", ")+` FROM jobs WHERE id = ?`, args...); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE list_items SET job_id = ? WHERE id = ?`, id, it.id); err != nil {
			return err
		}
	}
	return nil
}

func tableCols(tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func readFacts(v any) []fact {
	data, _ := json.Marshal(v)
	var fs []fact
	json.Unmarshal(data, &fs)
	return fs
}

// mergeFacts hängt neue Paare an, ohne Doppelte (gleicher Name und Wert).
func mergeFacts(own, add []fact) []fact {
	out := append([]fact{}, own...)
	for _, f := range add {
		dup := false
		for _, o := range out {
			dup = dup || (strings.EqualFold(o.Name, f.Name) && o.Wert == f.Wert)
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
}

// stripPointFacts entfernt die Eckdaten aus den Punkten der Auswertung; only != nil behält nur den Punkt mit diesem Text.
func stripPointFacts(v any, only *string) []map[string]any {
	list, _ := v.([]any)
	out := []map[string]any{}
	for _, p := range list {
		m, _ := p.(map[string]any)
		if m == nil {
			continue
		}
		t := stringOf(m["text"])
		if only != nil && t != *only {
			continue
		}
		out = append(out, map[string]any{"text": t})
	}
	if only != nil && len(out) == 0 {
		out = append(out, map[string]any{"text": *only})
	}
	return out
}

func stringOf(v any) string { s, _ := v.(string); return s }

// replaceTitle tauscht den Kurztitel und behält das angehängte Datum („Titel – 02.01.2006“).
func replaceTitle(title, text string) string {
	if i := strings.LastIndex(title, " – "); i >= 0 {
		return text + title[i:]
	}
	return text
}

func saveResult(tx *sql.Tx, jobID string, d map[string]any) error {
	data, _ := json.Marshal(d)
	_, err := tx.Exec(`UPDATE jobs SET result = ? WHERE id = ?`, string(data), jobID)
	return err
}
