package app

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Weiteres Gerät per Kurz-Code: ein angemeldetes Gerät erzeugt einen 4-stelligen Code (höchstens 5 Minuten, nur im
// Speicher, immer nur einer). Er gilt nur, solange die Seite mit dem Code offen ist: sie meldet sich alle 10 s
// (codeAlive), beim Verlassen löscht sie ihn. Das neue Gerät tauscht ihn auf der Anmeldeseite gegen einen Einmal-Link zur
// Passkey-Einrichtung. Nach codeMaxFails Fehlversuchen verfällt der Code (10 000 Möglichkeiten, also höchstens
// 0,05 % Trefferchance je Code); dazu greift das allgemeine Limit für öffentliche Anmelde-Routen.

const (
	codeTTL      = 5 * time.Minute
	codeMaxFails = 5
	codeAlive    = 25 * time.Second
	codeEvery    = 5 * time.Second // höchstens ein Eingabeversuch je 5 s (für alle zusammen)
)

type deviceCode struct {
	mu      sync.Mutex
	code    string
	expires time.Time
	seen    time.Time // letztes Lebenszeichen der Seite mit dem Code
	fails   int
	tried   time.Time // letzter Eingabeversuch
}

// valid: Code gesetzt, nicht abgelaufen, Seite noch offen. Aufruf mit gesperrtem mu.
func (c *deviceCode) valid(now time.Time) bool {
	return c.code != "" && now.Before(c.expires) && now.Sub(c.seen) < codeAlive
}

// handleDeviceCodeKeep hält den Code am Leben, solange die Seite offen ist.
func (a *App) handleDeviceCodeKeep(w http.ResponseWriter, r *http.Request) {
	c := &a.devCode
	c.mu.Lock()
	ok := c.valid(time.Now())
	if ok {
		c.seen = time.Now()
	}
	c.mu.Unlock()
	writeJSON(w, 200, map[string]any{"gueltig": ok})
}

// handleDeviceCodeDrop löscht den Code (Seite verlassen).
func (a *App) handleDeviceCodeDrop(w http.ResponseWriter, r *http.Request) {
	c := &a.devCode
	c.mu.Lock()
	c.code = ""
	c.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleDeviceCode erzeugt einen neuen Code (ersetzt einen alten).
func (a *App) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		writeErr(w, 500, "zufall", "Code nicht erzeugt")
		return
	}
	c := &a.devCode
	c.mu.Lock()
	c.code, c.expires, c.seen, c.fails = fmt.Sprintf("%04d", n.Int64()), time.Now().Add(codeTTL), time.Now(), 0
	code, exp := c.code, c.expires
	c.mu.Unlock()
	writeJSON(w, 200, map[string]any{"code": code, "bis": exp.UnixMilli(), "gueltig_minuten": int(codeTTL.Minutes())})
}

// handleDeviceCodeRedeem tauscht den Code gegen einen Einrichtungs-Token (öffentlich).
func (a *App) handleDeviceCodeRedeem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	code := strings.TrimSpace(in.Code)
	c := &a.devCode
	c.mu.Lock()
	if wait := codeEvery - time.Since(c.tried); wait > 0 {
		c.mu.Unlock()
		secs := int((wait + time.Second - 1) / time.Second)
		writeErr(w, http.StatusTooManyRequests, "warten", fmt.Sprintf("Nächster Versuch in %d s", secs))
		return
	}
	c.tried = time.Now()
	ok := c.valid(time.Now()) && len(code) == 4 &&
		subtle.ConstantTimeCompare([]byte(code), []byte(c.code)) == 1
	if ok {
		c.code = ""
	} else if c.code != "" {
		c.fails++
		if c.fails >= codeMaxFails || !c.valid(time.Now()) {
			c.code = ""
		}
	}
	c.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusForbidden, "code", "Code falsch oder abgelaufen – auf dem angemeldeten Gerät einen neuen erzeugen")
		return
	}
	token, err := a.DB.NewEnrollToken(enrollTTL)
	if err != nil {
		writeErr(w, 500, "speicher", "Einrichtung nicht möglich")
		return
	}
	writeJSON(w, 200, map[string]any{"token": token})
}
