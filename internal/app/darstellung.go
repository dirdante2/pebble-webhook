package app

import (
	"encoding/json"
	"net/http"
	"regexp"
)

// Darstellung (Tag/Nacht, Farbpalette): gilt je Gerät (Browser). Der Server merkt sich nur die zuletzt gewählte,
// damit ein neues Gerät beim ersten Öffnen dieselbe übernimmt (kv „darstellung“).

const darstellungKey = "darstellung"

type darstellung struct {
	Modus   string `json:"modus"`   // auto | hell | dunkel
	Palette string `json:"palette"` // blau | gruen | violett | bernstein | graphit | eigene
	Farbe   string `json:"farbe,omitempty"`
}

var farbeRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (d darstellung) valid() bool {
	switch d.Modus {
	case "auto", "hell", "dunkel":
	default:
		return false
	}
	switch d.Palette {
	case "blau", "gruen", "violett", "bernstein", "graphit":
		return d.Farbe == "" || farbeRe.MatchString(d.Farbe)
	case "eigene":
		return farbeRe.MatchString(d.Farbe)
	}
	return false
}

func (a *App) handleDarstellung(w http.ResponseWriter, r *http.Request) {
	v, err := a.DB.Get(darstellungKey)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	var d darstellung
	if v == "" || json.Unmarshal([]byte(v), &d) != nil || !d.valid() {
		writeJSON(w, 200, map[string]any{})
		return
	}
	writeJSON(w, 200, d)
}

func (a *App) handleDarstellungSave(w http.ResponseWriter, r *http.Request) {
	var d darstellung
	if readJSON(r, &d) != nil || !d.valid() {
		writeErr(w, 400, "ungueltig", "Darstellung ungültig")
		return
	}
	data, _ := json.Marshal(d)
	if err := a.DB.Set(darstellungKey, string(data)); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	writeJSON(w, 200, d)
}
