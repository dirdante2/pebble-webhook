package app

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/joplin"
)

// joplinHTTP: Verbindungen zum Joplin-Server eines Nutzers. Adressen im eigenen Netz (localhost, LAN) nur für
// Admins – sonst könnte jeder Nutzer über seine Joplin-Adresse Dienste im Netz des Servers ansprechen.
// Weiterleitungen werden nicht verfolgt.
func (s *System) joplinHTTP(uid int64) *http.Client {
	internalOK := func() bool {
		u, err := s.DB.User(uid)
		return err == nil && u.Admin() && u.LockedAt == 0
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip == nil || (!publicIP(ip) && !internalOK()) {
					return errors.New(i18n.T("Adresse im eigenen Netz nur für Admins"))
				}
				return nil
			}}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// guardNotes: Joplin-Ziele eines Nutzers bekommen die eingeschränkten Verbindungen.
func (s *System) guardNotes(n Notes, uid int64) Notes {
	if c, ok := n.(*joplin.Client); ok {
		c.HTTP = s.joplinHTTP(uid)
	}
	return n
}

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
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": a.T("Keine gültige http(s)-Adresse")})
		return
	}
	if in.Password == "" {
		values, _ := a.DB.Settings()
		in.Password = values["JOPLIN_PASSWORD"]
	}
	if strings.TrimSpace(in.Email) == "" || in.Password == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": a.T("Konto (E-Mail) und Passwort fehlen")})
		return
	}
	c := joplin.New(in.URL, strings.TrimSpace(in.Email), in.Password)
	c.HTTP = a.System.joplinHTTP(a.UserID)
	c.HTTP.Timeout = 10 * time.Second
	if err := c.Ping(); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
