package app

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"pebble-webhook/internal/joplin"
)

// handleJoplinCheck prüft eingegebene Joplin-Zugangsdaten, ohne sie zu speichern (Status → Joplin).
// Ein leeres Passwort nimmt das gespeicherte.
func (a *App) handleJoplinCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL      string `json:"url"`
		Email    string `json:"email"`
		Password string `json:"passwort"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Angaben nicht lesbar")
		return
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if u, err := url.Parse(in.URL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": "Keine gültige http(s)-Adresse"})
		return
	}
	if in.Password == "" {
		values, _ := a.DB.Settings()
		in.Password = values["JOPLIN_PASSWORD"]
	}
	if strings.TrimSpace(in.Email) == "" || in.Password == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": "Konto (E-Mail) und Passwort fehlen"})
		return
	}
	c := joplin.New(in.URL, strings.TrimSpace(in.Email), in.Password)
	c.HTTP.Timeout = 10 * time.Second
	if err := c.Ping(); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
