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

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/store"
)

// Kurz-Code (6 Ziffern) für zwei Wege mit demselben Ablauf: ein weiteres Gerät eines Nutzers und eine Einladung.
// Ein angemeldetes Gerät (der Nutzer selbst bzw. ein Admin) zeigt den Code höchstens 5 Minuten, nur im Speicher und nur
// solange die Seite mit dem Code offen ist: sie meldet sich alle 10 s (codeAlive), beim Verlassen löscht sie ihn. Das
// neue Gerät gibt ihn auf der Anmeldeseite ein und bekommt einen Einmal-Link zur Passkey-Einrichtung (Gerät: für den
// Nutzer; Einladung: der Link der Einladung, der alte gilt danach nicht mehr).
// Raten bremsen zwei Grenzen: ein Versuch je 5 s und Adresse, höchstens 30 Versuche je Minute für alle zusammen – in
// 5 Minuten also höchstens 150 von 1 000 000 Möglichkeiten (0,015 %). Fehlversuche verbrennen keinen Code, so kann ein
// Fremder das Einrichten nicht durch falsche Eingaben verhindern.

const (
	codeTTL       = 5 * time.Minute
	codeAlive     = 25 * time.Second
	codeEvery     = 5 * time.Second // höchstens ein Eingabeversuch je 5 s und Adresse
	codeGlobalMin = 30              // Versuche je Minute für alle zusammen
	codeDigits    = 6
)

type codeEntry struct {
	code    string
	owner   int64  // Nutzer, dessen Seite den Code zeigt
	uid     int64  // Ziel Gerät: Nutzer, der ein Gerät bekommt
	invite  string // Ziel Einladung: Id der Einladung
	expires time.Time
	seen    time.Time // letztes Lebenszeichen der Seite mit dem Code
}

type deviceCode struct {
	mu    sync.Mutex
	codes map[string]*codeEntry // je Ziel: "geraet:<uid>" bzw. "einladung:<id>"
	tried map[string]time.Time  // letzter Eingabeversuch je Adresse
}

// valid: Code gesetzt, nicht abgelaufen, Seite noch offen.
func (e *codeEntry) valid(now time.Time) bool {
	return e != nil && e.code != "" && now.Before(e.expires) && now.Sub(e.seen) < codeAlive
}

func deviceKey(uid int64) string { return fmt.Sprintf("geraet:%d", uid) }
func inviteKey(id string) string { return "einladung:" + id }

// codeNew erzeugt den Code für ein Ziel (ersetzt dessen alten; andere bleiben).
func (s *System) codeNew(w http.ResponseWriter, key string, e *codeEntry) {
	c := &s.devCode
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.codes == nil {
		c.codes = map[string]*codeEntry{}
	}
	now := time.Now()
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			writeErr(w, 500, "zufall", "Code nicht erzeugt")
			return
		}
		e.code = fmt.Sprintf("%06d", n.Int64())
		clash := false
		for k, o := range c.codes {
			clash = clash || (k != key && o.valid(now) && o.code == e.code)
		}
		if !clash {
			break
		}
	}
	e.expires, e.seen = now.Add(codeTTL), now
	c.codes[key] = e
	writeJSON(w, 200, map[string]any{"code": e.code, "bis": e.expires.UnixMilli(), "gueltig_minuten": int(codeTTL.Minutes())})
}

// codeKeep hält den Code am Leben, solange die Seite des Besitzers offen ist.
func (s *System) codeKeep(w http.ResponseWriter, key string, owner int64) {
	c := &s.devCode
	c.mu.Lock()
	e := c.codes[key]
	ok := e.valid(time.Now()) && e.owner == owner
	if ok {
		e.seen = time.Now()
	}
	c.mu.Unlock()
	writeJSON(w, 200, map[string]any{"gueltig": ok})
}

// codeDrop löscht den Code (Seite verlassen).
func (s *System) codeDrop(w http.ResponseWriter, key string, owner int64) {
	c := &s.devCode
	c.mu.Lock()
	if e := c.codes[key]; e != nil && e.owner == owner {
		delete(c.codes, key)
	}
	c.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true})
}

// Weiteres Gerät: Code des angemeldeten Nutzers für sich selbst.
func (a *App) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	a.codeNew(w, deviceKey(a.UserID), &codeEntry{owner: a.UserID, uid: a.UserID})
}
func (a *App) handleDeviceCodeKeep(w http.ResponseWriter, r *http.Request) {
	a.codeKeep(w, deviceKey(a.UserID), a.UserID)
}
func (a *App) handleDeviceCodeDrop(w http.ResponseWriter, r *http.Request) {
	a.codeDrop(w, deviceKey(a.UserID), a.UserID)
}

// Einladung: Code für eine offene Einladung (Admin).
func (s *System) handleInviteCode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.DB.Invite(id); !ok {
		writeErr(w, 404, "fehlt", "Einladung nicht gefunden oder abgelaufen")
		return
	}
	s.codeNew(w, inviteKey(id), &codeEntry{owner: sessionUID(r), invite: id})
}
func (s *System) handleInviteCodeKeep(w http.ResponseWriter, r *http.Request) {
	s.codeKeep(w, inviteKey(r.PathValue("id")), sessionUID(r))
}
func (s *System) handleInviteCodeDrop(w http.ResponseWriter, r *http.Request) {
	s.codeDrop(w, inviteKey(r.PathValue("id")), sessionUID(r))
}

// handleDeviceCodeRedeem tauscht einen Code gegen einen Einmal-Link zur Passkey-Einrichtung (öffentlich).
func (a *System) handleDeviceCodeRedeem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	code := strings.TrimSpace(in.Code)
	ip := a.clientIP(r)
	c := &a.devCode
	c.mu.Lock()
	now := time.Now()
	if c.tried == nil {
		c.tried = map[string]time.Time{}
	}
	if wait := codeEvery - now.Sub(c.tried[ip]); wait > 0 {
		c.mu.Unlock()
		secs := int((wait + time.Second - 1) / time.Second)
		writeErr(w, http.StatusTooManyRequests, "warten", i18n.Tf("Nächster Versuch in %d s", secs))
		return
	}
	if !a.allow("geraetecode", codeGlobalMin) {
		c.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "warten", i18n.Tf("Nächster Versuch in %d s", 60))
		return
	}
	for k, t := range c.tried {
		if now.Sub(t) > codeEvery {
			delete(c.tried, k)
		}
	}
	c.tried[ip] = now
	var hit *codeEntry
	if len(code) == codeDigits {
		for k, e := range c.codes {
			if e.valid(now) && subtle.ConstantTimeCompare([]byte(code), []byte(e.code)) == 1 {
				hit = e
				delete(c.codes, k)
			}
		}
	}
	c.mu.Unlock()
	if hit == nil {
		writeErr(w, http.StatusForbidden, "code", "Code falsch oder abgelaufen – auf dem angemeldeten Gerät einen neuen erzeugen")
		return
	}
	var token string
	var err error
	if hit.invite != "" {
		var ok bool
		if token, ok = a.DB.RotateInvite(hit.invite); !ok {
			writeErr(w, http.StatusForbidden, "code", "Einladung abgelaufen oder schon genutzt")
			return
		}
	} else if token, err = a.DB.NewEnrollToken(store.Enroll{UserID: hit.uid, Purpose: store.EnrollDevice}, enrollTTL); err != nil {
		writeErr(w, 500, "speicher", "Einrichtung nicht möglich")
		return
	}
	writeJSON(w, 200, map[string]any{"token": token})
}
