package app

import (
	"net/http"
	"sync"
	"time"

	"pebble-webhook/internal/store"
)

// Erste Einrichtung ohne Link: In den ersten 5 Minuten nach dem Start zeigt die Anmeldeseite „Einrichten“, solange es
// noch keinen Passkey gibt (bzw. im Einrichtungsassistenten keine Einstellungen). Nur der erste Browser bekommt den
// Zugang; danach oder nach Ablauf geht es erst nach einem Neustart des Dienstes wieder.

const setupWindowDur = 5 * time.Minute

// Zustände des Fensters.
const (
	setupOpen    = "offen"
	setupClaimed = "vergeben"
	setupExpired = "abgelaufen"
	setupLocked  = "gesperrt" // Assistent einer schon benutzten Installation (Einstellungen ungültig): nur mit Code aus dem Log
)

type setupWindow struct {
	start   time.Time
	mu      sync.Mutex
	claimed bool
	locked  bool             // nie offen
	now     func() time.Time // Tests
}

func newSetupWindow() *setupWindow { return &setupWindow{start: time.Now(), now: time.Now} }

func (w *setupWindow) state() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stateLocked()
}

func (w *setupWindow) stateLocked() string {
	switch {
	case w.locked:
		return setupLocked
	case w.claimed:
		return setupClaimed
	case w.now().Sub(w.start) >= setupWindowDur:
		return setupExpired
	}
	return setupOpen
}

// rest: verbleibende Sekunden, solange das Fenster offen ist (sonst 0).
func (w *setupWindow) rest() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stateLocked() != setupOpen {
		return 0
	}
	return int((setupWindowDur - w.now().Sub(w.start)) / time.Second)
}

// claim: true, wenn das Fenster offen war – dann ist es ab jetzt vergeben.
func (w *setupWindow) claim() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stateLocked() != setupOpen {
		return false
	}
	w.claimed = true
	return true
}

// setupClosedMsg: Meldung, wenn „Einrichten“ nicht (mehr) geht.
func setupClosedMsg(state string) string {
	if state == setupLocked {
		return "Die Einstellungen sind ungültig. Den Link aus dem Log des Dienstes öffnen (pebble ersteinrichtung)."
	}
	if state == setupClaimed {
		return "Die Einrichtung wurde schon in einem anderen Browser begonnen. Für einen neuen Versuch den Dienst neu starten."
	}
	return "Die Einrichtung ist nur in den ersten 5 Minuten nach dem Start möglich. Bitte den Dienst neu starten."
}

// handleFirstEnroll: „Einrichten“ ohne Passkey – Einmal-Link für den ersten Admin, nur im Fenster und nur einmal.
func (s *System) handleFirstEnroll(w http.ResponseWriter, r *http.Request) {
	if any, err := s.DB.AnyPasskey(); err != nil || any {
		writeErr(w, http.StatusConflict, "eingerichtet", "Schon eingerichtet – bitte mit dem Passkey anmelden")
		return
	}
	users, err := s.DB.Users()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	var admin *store.User
	for _, u := range users {
		if u.Admin() && u.LockedAt == 0 {
			admin = u
			break
		}
	}
	if admin == nil {
		writeErr(w, http.StatusConflict, "fehlt", "Kein Admin vorhanden")
		return
	}
	if !s.setupWin.claim() {
		writeErr(w, http.StatusForbidden, "fenster", setupClosedMsg(s.setupWin.state()))
		return
	}
	token, err := s.DB.NewEnrollToken(store.Enroll{UserID: admin.ID, Purpose: store.EnrollDevice}, enrollTTL)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	logf("Einrichtung: Einmal-Link für %s über „Einrichten“ ausgegeben", admin.Name)
	writeJSON(w, 200, map[string]any{"weiter": "/einrichten#" + token})
}

// handleSetupStart: „Einrichten“ im Einrichtungsassistenten – gibt einen neuen Code des Assistenten aus (ein früher
// ausgegebener gilt damit nicht mehr), nur im Fenster, nur einmal und nur aus der Web-App (X-App, gleiche Herkunft).
func (s *SetupServer) handleSetupStart(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-App") != "1" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeErr(w, http.StatusForbidden, "herkunft", "Anfrage nicht aus der Web-App")
		return
	}
	if !s.win.claim() {
		writeErr(w, http.StatusForbidden, "fenster", setupClosedMsg(s.win.state()))
		return
	}
	code, err := NewSetupCode(s.DB)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	logf("Einrichtung: Assistent über „Einrichten“ geöffnet")
	writeJSON(w, 200, map[string]any{"code": code})
}
