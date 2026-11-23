package app

import (
	"encoding/json"
	"regexp"
	"strings"

	"pebble-webhook/internal/store"
)

// Titel mit Jahr (Nutzervorgabe 23.11.2026): in den Listen stehen Filme und Serien immer als „Titel (Jahr)“.
// Die KI soll das selbst so schreiben; fehlt das Jahr im Namen, steht es aber in den Eckdaten, wird es angehängt.

var (
	yearInText = regexp.MustCompile(`\b(1[89]\d\d|20\d\d)\b`)
	yearFacts  = map[string]bool{"jahr": true, "startjahr": true, "erscheinungsjahr": true, "zeitraum": true, "erstausstrahlung": true}
)

// withYear hängt „ (JJJJ)“ an, wenn der Name kein Jahr hat und eine Jahres-Eckdate eins nennt.
func withYear(text string, facts []Fact) string {
	if text == "" || yearInText.MatchString(text) {
		return text
	}
	for _, f := range facts {
		if !yearFacts[strings.ToLower(strings.TrimSpace(f.Name))] {
			continue
		}
		if y := yearInText.FindString(f.Wert); y != "" {
			return text + " (" + y + ")"
		}
	}
	return text
}

// yearFixKey: Merker in kv, dass die vorhandenen Listenpunkte einmal mit Jahr nachgezogen wurden.
const yearFixKey = "punkte_jahr_v1"

// fixItemYears zieht einmalig (beim Start) vorhandene Listenpunkte nach: Name ohne Jahr + Jahr in den Eckdaten → „Name (JJJJ)“.
func (a *App) fixItemYears() {
	if v, _ := a.DB.Get(yearFixKey); v != "" {
		return
	}
	lists, err := a.DB.Lists()
	if err != nil {
		logf("Jahr nachtragen: %v", err)
		return
	}
	n := 0
	for _, l := range lists {
		items, err := a.DB.ListItems(l.ID)
		if err != nil {
			logf("Jahr nachtragen: %v", err)
			return
		}
		changed := false
		for _, it := range items {
			var facts []Fact
			json.Unmarshal([]byte(it.Facts), &facts)
			if t := withYear(it.Text, facts); t != it.Text {
				if err := a.DB.SetListItemText(it.ID, store.Preview(t, 120)); err != nil {
					logf("Jahr nachtragen: %v", err)
					return
				}
				changed, n = true, n+1
			}
		}
		if changed {
			a.listChanged(l.ID)
		}
	}
	a.DB.Set(yearFixKey, "1")
	if n > 0 {
		logf("Jahr nachtragen: %d Listenpunkte", n)
	}
}

var (
	directorFacts = map[string]bool{"regie": true, "idee": true, "schöpfer": true, "serienschöpfer": true, "creator": true, "showrunner": true}
	castFacts     = map[string]bool{"schauspieler": true, "hauptdarsteller": true, "darsteller": true, "besetzung": true, "mit": true}
	castPrefix    = regexp.MustCompile(`^(Regie|Idee|Mit)\b[^\n]*\n*`)
)

// castLine: „Regie: X · Mit: A, B, C“ aus den Eckdaten (höchstens 3 Schauspieler); leer ohne beides.
func castLine(facts []Fact) string {
	var director, cast string
	for _, f := range facts {
		name := strings.ToLower(strings.TrimSpace(f.Name))
		switch {
		case directorFacts[name] && director == "":
			label := "Regie"
			if name != "regie" {
				label = "Idee"
			}
			director = label + ": " + strings.TrimSpace(f.Wert)
		case castFacts[name] && cast == "":
			var names []string
			for _, n := range strings.Split(f.Wert, ",") {
				if n = strings.TrimSpace(n); n != "" && len(names) < 3 {
					names = append(names, n)
				}
			}
			if len(names) > 0 {
				cast = "Mit: " + strings.Join(names, ", ")
			}
		}
	}
	switch {
	case director != "" && cast != "":
		return director + " · " + cast
	case director != "":
		return director
	}
	return cast
}

// stripCast entfernt eine vorangestellte Zeile „Regie: … · Mit: …“ (stand bis 2.1.0.22 in der Beschreibung).
func stripCast(desc string) string {
	return strings.TrimSpace(castPrefix.ReplaceAllString(strings.TrimSpace(desc), ""))
}

// noteFilmKey: Merker für den einmaligen Nachlauf der Film-Notizen (v1: Zeile in die Beschreibung, v2: wieder heraus).
const noteFilmKey = "notiz_film_v2"

// fixNoteFilms zieht einmalig (beim Start) Notizen mit genau einem Listenpunkt nach: Titel mit Jahr; die Zeile
// Regie/Schauspieler kommt aus der Beschreibung wieder heraus (steht jetzt unter dem Titel).
func (a *App) fixNoteFilms() {
	if v, _ := a.DB.Get(noteFilmKey); v != "" {
		return
	}
	jobs, err := a.DB.SearchableJobs()
	if err != nil {
		logf("Film-Notizen nachtragen: %v", err)
		return
	}
	n := 0
	for _, j := range jobs {
		d := parseDictation(j.Result)
		if d == nil || len(d.Punkte) != 1 || j.State != store.StateDone {
			continue
		}
		facts := d.Punkte[0].Eckdaten
		if _, items := a.jobList(j.ID); len(items) == 1 {
			json.Unmarshal([]byte(items[0].Facts), &facts)
		}
		short, date, _ := strings.Cut(j.Title, " – ")
		title := withYear(short, facts)
		desc := stripCast(d.Beschreibung)
		if title == short && desc == d.Beschreibung {
			continue
		}
		if date != "" {
			title += " – " + date
		}
		d.Beschreibung = desc
		data, _ := json.Marshal(d)
		j.Title, j.Result = title, string(data)
		if err := a.DB.SaveJob(j); err != nil {
			logf("Film-Notizen nachtragen: %v", err)
			return
		}
		a.changed(j.ID)
		n++
	}
	a.DB.Set(noteFilmKey, "1")
	if n > 0 {
		logf("Film-Notizen nachtragen: %d Notizen", n)
	}
}
