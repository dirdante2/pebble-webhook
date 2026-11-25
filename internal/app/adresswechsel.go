package app

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Neue Adresse (anderer Domainname, z. B. nach dem Einspielen eines Backups auf einem neuen Server): Passkeys gelten
// nur für die Adresse, unter der sie angelegt wurden. Der Dienst merkt sich die Adresse (kv „passkey_adresse“) und
// seit wann sie gilt; wer seitdem keinen Passkey angelegt hat, braucht einen neuen:
// 1. Admin ohne gültigen Passkey: auf der Anmeldeseite mit seinem Wiederherstellungscode → Einmal-Link → Passkey
//    (fehlt seine Schlüsseldatei, wird sie dabei gleich wieder geschrieben).
// 2. Übrige Nutzer: stehen auf der Nutzerseite mit Fehler; der Admin zeigt je Nutzer einen 6-stelligen Code (kein neuer
//    Nutzer). Beim Anlegen des Passkeys fragt die Seite den Wiederherstellungscode ab, falls der Schlüssel fehlt.

const (
	addressKV      = "passkey_adresse"
	addressSinceKV = "passkey_adresse_seit"
)

// checkAddress (beim Start): Adresse vermerken; geändert → ab jetzt brauchen alle einen neuen Passkey.
func (s *System) checkAddress() {
	host := s.Cfg.Host()
	old, _ := s.DB.Get(addressKV)
	switch {
	case old == "":
		s.DB.Set(addressKV, host)
	case old != host:
		s.DB.Set(addressKV, host)
		s.DB.Set(addressSinceKV, strconv.FormatInt(store.Now(), 10))
		logf("Neue Adresse %s (vorher %s): Passkeys müssen neu angelegt werden", host, old)
	}
}

func (s *System) addressSince() int64 {
	v, _ := s.DB.Get(addressSinceKV)
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// needsPasskey: Nach einem Adresswechsel hat der Nutzer noch keinen Passkey für die neue Adresse.
func (s *System) needsPasskey(uid int64) bool {
	since := s.addressSince()
	if since == 0 {
		return false
	}
	keys, err := s.DB.Passkeys(uid)
	if err != nil {
		return false
	}
	for _, k := range keys {
		if k.CreatedAt >= since {
			return false
		}
	}
	return true
}

// adminNeedsPasskey: Kein Admin kann sich anmelden – dann gilt der Weg über den Wiederherstellungscode.
func (s *System) adminNeedsPasskey() bool {
	if s.addressSince() == 0 {
		return false
	}
	users, _ := s.DB.Users()
	for _, u := range users {
		if u.Admin() && u.LockedAt == 0 && !s.needsPasskey(u.ID) {
			return false
		}
	}
	return true
}

// handleRecoveryLogin: Admin ohne Passkey für die neue Adresse meldet sich mit seinem Wiederherstellungscode und
// bekommt einen Einmal-Link für den Passkey. Nur solange kein Admin einen gültigen Passkey hat.
func (s *System) handleRecoveryLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allow("codeanmeldung:"+s.clientIP(r), 5) {
		writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Versuche")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Code) == "" {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	if !s.adminNeedsPasskey() {
		writeErr(w, http.StatusForbidden, "nicht_noetig", "Anmelden mit Passkey – der Admin lädt Nutzer mit Code neu ein")
		return
	}
	users, _ := s.DB.Users()
	for _, u := range users {
		if !u.Admin() || u.LockedAt > 0 || u.KeyWrap == "" || !s.needsPasskey(u.ID) {
			continue
		}
		dek, err := schluessel.Unwrap(in.Code, u.KeyWrap, u.ID)
		if err != nil || dek.ID() != u.KeyID {
			continue
		}
		if err := s.restoreUserKey(u, dek); err != nil {
			writeErr(w, 500, "schluessel", err.Error())
			return
		}
		token, err := s.DB.NewEnrollToken(store.Enroll{UserID: u.ID, Purpose: store.EnrollDevice}, enrollTTL)
		if err != nil {
			writeErr(w, 500, "speicher", "Einrichtung nicht möglich")
			return
		}
		logf("Neue Adresse: Nutzer %d per Wiederherstellungscode zum neuen Passkey", u.ID)
		writeJSON(w, 200, map[string]any{"token": token})
		return
	}
	writeErr(w, http.StatusForbidden, "code", "Code falsch")
}

// restoreUserKey: Schlüsseldatei eines Nutzers aus seinem Code schreiben (falls sie fehlt), Daten öffnen und – ist
// der Systemschlüssel an ihn gebunden – diesen mit zurückholen.
func (s *System) restoreUserKey(u *store.User, dek schluessel.DEK) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if a, _ := s.app(u.ID); a != nil {
		return nil
	}
	if old, err := schluessel.Read(s.Cfg.KeyDir, u.ID, ""); err == nil && old != dek {
		return fmt.Errorf("für Nutzer %d liegt ein anderer Schlüssel", u.ID)
	} else if err != nil {
		if _, statErr := os.Stat(schluessel.Path(s.Cfg.KeyDir, u.ID)); statErr == nil {
			return err
		}
		if err := schluessel.Write(s.Cfg.KeyDir, u.ID, dek); err != nil {
			return err
		}
	}
	if _, err := s.openUser(u); err != nil {
		return err
	}
	s.recoverSystemKeyWith(u.ID, dek)
	return nil
}

// Admin: Code für den neuen Passkey eines bestehenden Nutzers (derselbe Kurz-Code wie für Geräte und Einladungen).
func renewKey(uid int64) string { return fmt.Sprintf("neu:%d", uid) }

func (s *System) handleRenewCode(w http.ResponseWriter, r *http.Request) {
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	s.codeNew(w, renewKey(u.ID), &codeEntry{owner: sessionUID(r), uid: u.ID})
}
func (s *System) handleRenewCodeKeep(w http.ResponseWriter, r *http.Request) {
	s.codeKeep(w, renewKey(pathID(r)), sessionUID(r))
}
func (s *System) handleRenewCodeDrop(w http.ResponseWriter, r *http.Request) {
	s.codeDrop(w, renewKey(pathID(r)), sessionUID(r))
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}
