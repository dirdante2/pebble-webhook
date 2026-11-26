package app

import (
	"regexp"
	"strings"
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

var (
	directorFacts = map[string]bool{"regie": true, "idee": true, "schöpfer": true, "serienschöpfer": true, "creator": true, "showrunner": true}
	castFacts     = map[string]bool{"schauspieler": true, "hauptdarsteller": true, "darsteller": true, "besetzung": true, "mit": true}
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
