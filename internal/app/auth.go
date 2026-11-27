package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

const (
	sessionCookie = "__Host-sitzung"
	sessionTTL    = 90 * 24 * time.Hour
	enrollTTL     = 15 * time.Minute
)

type ceremony struct {
	data    webauthn.SessionData
	token   string // Einmal-Link bei der Einrichtung
	name    string
	uid     int64 // Gerät für diesen Nutzer (0 = neuer Nutzer aus einer Einladung)
	invite  *store.Enroll
	newWID  string // WebAuthn-Id des eingeladenen Nutzers (erst nach dem Passkey angelegt)
	dek     *schluessel.DEK // Schlüssel aus dem Wiederherstellungscode (Gerät eines Nutzers, dessen Schlüsseldatei fehlt)
	expires time.Time
}

// waUser: ein Nutzer mit allen seinen Passkeys (für WebAuthn).
type waUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return "sprachnotizen" }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *System) initAuth() error {
	wa, err := webauthn.New(&webauthn.Config{RPID: s.Cfg.Host(), RPDisplayName: "Sprachnotizen", RPOrigins: []string{s.Cfg.PublicURL}})
	if err != nil {
		return err
	}
	s.wa = wa
	s.checkAddress()
	return nil
}

func displayName(name string) string {
	if name == "" {
		return "Sprachnotizen"
	}
	return "Sprachnotizen · " + name
}

// loadUser: Nutzer mit seinen Passkeys (Prüfzugänge ohne Passkey zählen nicht).
func (s *System) loadUser(u *store.User) (*waUser, error) {
	raw, err := base64.RawURLEncoding.DecodeString(u.WebAuthnID)
	if err != nil {
		return nil, err
	}
	keys, err := s.DB.Passkeys(u.ID)
	if err != nil {
		return nil, err
	}
	w := &waUser{id: raw, name: displayName(u.Name)}
	for _, k := range keys {
		var c webauthn.Credential
		if json.Unmarshal([]byte(k.Credential), &c) == nil && len(c.ID) > 0 {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

func (s *System) putCeremony(c ceremony) string {
	s.cerMu.Lock()
	defer s.cerMu.Unlock()
	for k, v := range s.cer {
		if time.Now().After(v.expires) {
			delete(s.cer, k)
		}
	}
	if len(s.cer) >= 1000 { // offene Anmeldevorgänge sind Speicher ohne Anmeldung: begrenzen
		return ""
	}
	id := store.NewID()
	c.expires = time.Now().Add(5 * time.Minute)
	s.cer[id] = c
	return id
}

func (s *System) takeCeremony(id string) (ceremony, bool) {
	s.cerMu.Lock()
	defer s.cerMu.Unlock()
	c, ok := s.cer[id]
	delete(s.cer, id)
	return c, ok && time.Now().Before(c.expires)
}

func (s *System) setSession(w http.ResponseWriter, uid int64, passkeyID string) error {
	token, err := s.DB.NewSession(uid, passkeyID, sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	return nil
}

// session: angemeldeter Nutzer und Gerät der Anfrage.
type session struct {
	uid       int64
	passkeyID string
}

func (s *System) sessionOf(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	uid, pk, ok := s.DB.Session(c.Value, sessionTTL)
	return session{uid: uid, passkeyID: pk}, ok
}

type ctxKey int

const (
	machineKey ctxKey = iota + 1
	sessionKey
)

// isMachine: die Anfrage kam über den Maschinenzugang, nicht aus der Web-App.
func isMachine(r *http.Request) bool {
	v, _ := r.Context().Value(machineKey).(bool)
	return v
}

// sessionUID: Nutzer der Sitzung (0 ohne Sitzung).
func sessionUID(r *http.Request) int64 {
	v, _ := r.Context().Value(sessionKey).(session)
	return v.uid
}

// currentPasskey: Gerät der Sitzung (leer beim Maschinenzugang).
func currentPasskey(r *http.Request) string {
	v, _ := r.Context().Value(sessionKey).(session)
	return v.passkeyID
}

// userHandler: Handler auf der App des angemeldeten Nutzers.
type userHandler func(*App, http.ResponseWriter, *http.Request)

// withApp löst den Nutzer zur App auf; sind seine Daten nicht geöffnet, antwortet sie 423 mit dem Grund.
func (s *System) withApp(w http.ResponseWriter, r *http.Request, uid int64, h userHandler) {
	a, reason := s.app(uid)
	if a == nil {
		writeJSON(w, http.StatusLocked, map[string]any{"code": "gesperrt", "grund": reason,
			"fehler": "Deine Daten sind gesperrt"})
		return
	}
	h(a, &langWriter{ResponseWriter: w, lang: a.Cfg.Language}, r)
}

// user schützt eine Route der Web-App: gültige Sitzung, bei Schreibzugriffen zusätzlich gleiche Herkunft.
func (s *System) user(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sameOrigin(w, r) {
			return
		}
		sess, ok := s.sessionOf(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Nicht angemeldet")
			return
		}
		s.withApp(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)), sess.uid, h)
	}
}

// signedIn: Route mit Sitzung, die auch bei gesperrten Daten erreichbar ist (Wiederherstellen, Abmelden).
func (s *System) signedIn(h func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sameOrigin(w, r) {
			return
		}
		sess, ok := s.sessionOf(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Nicht angemeldet")
			return
		}
		u, err := s.DB.User(sess.uid)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Nicht angemeldet")
			return
		}
		if u.LockedAt > 0 { // vom Admin gesperrt: auch keine Verwaltungs- und Wiederherstellungswege
			writeErr(w, http.StatusForbidden, "gesperrt", "Zugang gesperrt")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)), u)
	}
}

// admin schützt eine Route der Systemverwaltung (Updates, Backups, Einstellungen des Systems, Nutzer).
func (s *System) admin(h http.HandlerFunc) http.HandlerFunc {
	return s.signedIn(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if !u.Admin() {
			writeErr(w, http.StatusForbidden, "rolle", "Nur für Administratoren")
			return
		}
		h(w, r)
	})
}

// machine öffnet eine Route zusätzlich für den Maschinenzugang (Authorization: Bearer <API-Token des Nutzers>).
// Trägt die Anfrage den Header, zählt nur der Token – kein Rückfall auf die Sitzung. Ohne Header gilt der Weg der Web-App.
func (s *System) machine(h userHandler) http.HandlerFunc {
	session := s.user(h)
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			session(w, r)
			return
		}
		if !s.allow("api:"+s.clientIP(r), 300) {
			writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Anfragen")
			return
		}
		tok, isBearer := strings.CutPrefix(auth, "Bearer ")
		t, ok := s.DB.TokenUser(store.TokenAPI, tok)
		if !isBearer || !ok {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Token ungültig")
			return
		}
		s.withApp(w, r.WithContext(context.WithValue(r.Context(), machineKey, true)), t.UserID, h)
	}
}

// sameOrigin weist Schreibzugriffe ab, die nicht aus der Web-App selbst stammen.
func (s *System) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if o := r.Header.Get("Origin"); r.Header.Get("X-App") != "1" || (o != "" && o != s.Cfg.PublicURL) {
		writeErr(w, http.StatusForbidden, "herkunft", "Anfrage nicht aus der Web-App")
		return false
	}
	return true
}

func (s *System) public(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.allow("auth:"+s.clientIP(r), 30) {
			writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Versuche")
			return
		}
		if s.sameOrigin(w, r) {
			next(w, r)
		}
	}
}

// handleSession: Anmeldestand für die Web-App – mit Name und Rolle, gesperrten Daten und offenem Wiederherstellungscode.
func (s *System) handleSession(w http.ResponseWriter, r *http.Request) {
	setUp, _ := s.DB.AnyPasskey()
	out := map[string]any{"angemeldet": false, "eingerichtet": setUp, "neue_adresse": s.adminNeedsPasskey()}
	if !setUp {
		out["einrichtung"], out["einrichtung_rest"] = s.setupWin.state(), s.setupWin.rest()
	}
	sess, ok := s.sessionOf(r)
	if !ok {
		writeJSON(w, 200, out)
		return
	}
	u, err := s.DB.User(sess.uid)
	if err != nil {
		writeJSON(w, 200, out)
		return
	}
	out["angemeldet"], out["id"], out["name"], out["rolle"] = true, u.ID, u.Name, u.Role
	if a, reason := s.app(u.ID); a == nil {
		out["gesperrt"] = reason
	} else if code, _ := a.DB.Get(recoveryPendingKey); code != "" {
		out["code_offen"] = true
	}
	writeJSON(w, 200, out)
}

// handleEnrollKind: Art eines Einrichtungs-Links (Einladung oder weiteres Gerät) – die Seite fragt bei einer
// Einladung zusätzlich den Namen des neuen Nutzers ab.
func (s *System) handleEnrollKind(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	e, ok := (*store.Enroll)(nil), false
	if readJSON(r, &in) == nil {
		e, ok = s.DB.EnrollToken(in.Token)
	}
	if !ok {
		writeErr(w, http.StatusForbidden, "link", "Der Einrichtungs-Link ist ungültig oder abgelaufen")
		return
	}
	out := map[string]any{"art": e.Purpose, "rolle": e.Role, "name": e.Name}
	if e.Purpose == store.EnrollDevice {
		if a, _ := s.app(e.UserID); a == nil {
			out["code_noetig"] = true // Schlüsseldatei fehlt (neuer Server): Wiederherstellungscode mit eingeben
		}
	}
	writeJSON(w, 200, out)
}

func (s *System) handleEnrollStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token  string `json:"token"`
		Name   string `json:"name"`
		Nutzer string `json:"nutzer"` // Einladung: Name des neuen Nutzers (gibt er selbst ein)
		Code   string `json:"wiederherstellungscode"` // Gerät, Schlüsseldatei fehlt
	}
	e, ok := (*store.Enroll)(nil), false
	if readJSON(r, &in) == nil {
		e, ok = s.DB.EnrollToken(in.Token)
	}
	if !ok {
		writeErr(w, http.StatusForbidden, "link", "Der Einrichtungs-Link ist ungültig oder abgelaufen")
		return
	}
	c := ceremony{token: in.Token, name: store.Preview(in.Name, 40)}
	if c.name == "" {
		c.name = "Gerät"
	}
	var wu *waUser
	if e.Purpose == store.EnrollInvite {
		// Neuer Nutzer: angelegt wird er erst mit dem fertigen Passkey; den Namen gibt er selbst ein.
		if n := store.Preview(strings.TrimSpace(in.Nutzer), 40); n != "" {
			inv := *e
			inv.Name = n
			e = &inv
		}
		if strings.TrimSpace(e.Name) == "" {
			writeErr(w, 400, "name", "Bitte deinen Namen eingeben")
			return
		}
		c.invite, c.newWID = e, base64.RawURLEncoding.EncodeToString([]byte(store.NewID()+store.NewID()))
		raw, _ := base64.RawURLEncoding.DecodeString(c.newWID)
		wu = &waUser{id: raw, name: displayName(e.Name)}
	} else {
		u, err := s.DB.User(e.UserID)
		if err == nil {
			wu, err = s.loadUser(u)
		}
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		c.uid = u.ID
		if a, _ := s.app(u.ID); a == nil && u.LockedAt == 0 {
			// Daten noch zu (Schlüsseldatei fehlt): der Code gehört mit dazu, sonst bliebe der Nutzer ausgesperrt.
			if strings.TrimSpace(in.Code) == "" {
				writeErr(w, 400, "wiederherstellungscode", "Bitte deinen Wiederherstellungscode eingeben")
				return
			}
			dek, err := schluessel.Unwrap(in.Code, u.KeyWrap, u.ID)
			if err != nil || dek.ID() != u.KeyID {
				writeErr(w, http.StatusForbidden, "code", "Wiederherstellungscode falsch")
				return
			}
			c.dek = &dek
		}
	}
	var exclude []protocol.CredentialDescriptor
	for _, cr := range wu.creds {
		exclude = append(exclude, cr.Descriptor())
	}
	opts, data, err := s.wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		writeErr(w, 500, "webauthn", "Passkey-Einrichtung nicht möglich")
		return
	}
	c.data = *data
	id := s.putCeremony(c)
	if id == "" {
		writeErr(w, http.StatusServiceUnavailable, "voll", "Gerade zu viele Anmeldungen – gleich noch einmal versuchen")
		return
	}
	writeJSON(w, 200, map[string]any{"vorgang": id, "optionen": opts})
}

func (s *System) handleEnrollFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.takeCeremony(r.URL.Query().Get("vorgang"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "vorgang", "Vorgang abgelaufen, bitte neu beginnen")
		return
	}
	var wu *waUser
	if c.invite != nil {
		raw, _ := base64.RawURLEncoding.DecodeString(c.newWID)
		wu = &waUser{id: raw, name: displayName(c.invite.Name)}
	} else {
		u, err := s.DB.User(c.uid)
		if err == nil {
			wu, err = s.loadUser(u)
		}
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
	}
	cred, err := s.wa.FinishRegistration(wu, c.data, r)
	if err != nil {
		logf("Passkey-Einrichtung: %v", err)
		writeErr(w, http.StatusBadRequest, "webauthn", "Passkey wurde nicht angenommen")
		return
	}
	if !s.DB.UseEnrollToken(c.token) {
		writeErr(w, http.StatusForbidden, "link", "Der Einrichtungs-Link wurde schon benutzt")
		return
	}
	uid := c.uid
	if c.invite != nil {
		a, _, err := createUser(s.DB, s.Cfg.DataDir, s.Cfg.KeyDir, 0, c.invite.Name, c.invite.Role, c.newWID,
			func(u *store.User) (*App, error) { return s.openUser(u) })
		if err != nil {
			logf("Einladung: Nutzer nicht angelegt: %v", err)
			writeErr(w, 500, "speicher", "Zugang nicht angelegt")
			return
		}
		uid = a.UserID
		logf("Nutzer %d („%s“) per Einladung angelegt", uid, c.invite.Name)
	}
	data, _ := json.Marshal(cred)
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	if err := s.DB.AddPasskey(store.Passkey{ID: id, UserID: uid, Name: c.name, Credential: string(data)}); err != nil {
		writeErr(w, 500, "speicher", "Passkey nicht gespeichert")
		return
	}
	if c.dek != nil {
		if u, err := s.DB.User(uid); err == nil {
			if err := s.restoreUserKey(u, *c.dek); err != nil {
				logf("Passkey angelegt, Daten von Nutzer %d nicht geöffnet: %v", uid, err)
			}
		}
	}
	if err := s.setSession(w, uid, id); err != nil {
		writeErr(w, 500, "speicher", "Sitzung nicht angelegt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *System) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	opts, data, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		writeErr(w, 500, "webauthn", "Anmeldung nicht möglich")
		return
	}
	id := s.putCeremony(ceremony{data: *data})
	if id == "" {
		writeErr(w, http.StatusServiceUnavailable, "voll", "Gerade zu viele Anmeldungen – gleich noch einmal versuchen")
		return
	}
	writeJSON(w, 200, map[string]any{"vorgang": id, "optionen": opts})
}

func (s *System) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.takeCeremony(r.URL.Query().Get("vorgang"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "vorgang", "Vorgang abgelaufen, bitte neu beginnen")
		return
	}
	var who *store.User
	cred, err := s.wa.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.DB.UserByWebAuthnID(base64.RawURLEncoding.EncodeToString(userHandle))
		if err != nil {
			return nil, err
		}
		who = u
		return s.loadUser(u)
	}, c.data, r)
	if err != nil || who == nil {
		logf("Anmeldung: %v", err)
		writeErr(w, http.StatusUnauthorized, "webauthn", "Passkey nicht erkannt")
		return
	}
	if cred.Authenticator.CloneWarning {
		// Zähler des Passkeys ging zurück: Hinweis auf eine Kopie des Schlüssels – nicht anmelden.
		logf("Anmeldung Nutzer %d: Passkey-Zähler zurückgegangen (mögliche Kopie) – abgewiesen", who.ID)
		writeErr(w, http.StatusUnauthorized, "webauthn", "Passkey nicht erkannt")
		return
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	data, _ := json.Marshal(cred)
	s.DB.UpdatePasskey(who.ID, id, string(data))
	if err := s.setSession(w, who.ID, id); err != nil {
		writeErr(w, 500, "speicher", "Sitzung nicht angelegt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *System) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.DB.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode})
	// HTTP-Zwischenspeicher des Browsers leeren (Aufnahmen, Bilder); Daten der Seite räumt sie selbst (vergissGeraet).
	w.Header().Set("Clear-Site-Data", `"cache"`)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	current := currentPasskey(r)
	keys, err := a.System.DB.Passkeys(a.UserID)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, k := range keys {
		out = append(out, map[string]any{"id": k.ID, "name": k.Name, "angelegt": k.CreatedAt, "zuletzt": k.LastUsedAt, "dieses": k.ID == current})
	}
	writeJSON(w, 200, map[string]any{"geraete": out})
}

func (a *App) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	keys, err := a.System.DB.Passkeys(a.UserID)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if len(keys) <= 1 {
		writeErr(w, http.StatusConflict, "letztes", "Das letzte Gerät kann nicht entfernt werden")
		return
	}
	switch err := a.System.DB.DeletePasskey(a.UserID, r.PathValue("id")); {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "fehlt", "Gerät nicht gefunden")
		return
	case err != nil:
		writeErr(w, 500, "speicher", "Gerät nicht entfernt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleDeviceLink erzeugt aus einer Sitzung einen Einmal-Link für ein weiteres Gerät des Nutzers.
func (a *App) handleDeviceLink(w http.ResponseWriter, r *http.Request) {
	url, err := a.EnrollLink(a.UserID)
	if err != nil {
		writeErr(w, 500, "speicher", "Link nicht erzeugt")
		return
	}
	writeJSON(w, 200, map[string]any{"link": url, "gueltig_minuten": int(enrollTTL.Minutes())})
}

// TestAccess legt für einen Nutzer ein Gerät ohne Passkey samt Sitzung an und liefert das Sitzungs-Cookie.
// Für Prüfungen im Browser ohne Authenticator; erscheint in der Geräteliste und ist dort entfernbar.
func (s *System) TestAccess(uid int64, name string) (cookie string, err error) {
	if _, err := s.DB.User(uid); err != nil {
		return "", err
	}
	id := "zugang-" + store.NewID()[:12]
	if err := s.DB.AddPasskey(store.Passkey{ID: id, UserID: uid, Name: name, Credential: "{}"}); err != nil {
		return "", err
	}
	token, err := s.DB.NewSession(uid, id, sessionTTL)
	if err != nil {
		return "", err
	}
	return sessionCookie + "=" + token, nil
}

// EnrollLink erzeugt einen Einmal-Link zur Passkey-Einrichtung für ein weiteres Gerät des Nutzers.
func (s *System) EnrollLink(uid int64) (string, error) {
	token, err := s.DB.NewEnrollToken(store.Enroll{UserID: uid, Purpose: store.EnrollDevice}, enrollTTL)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(s.Cfg.PublicURL, "/") + "/einrichten#" + token, nil
}

// --- Wiederherstellungscode ---

// handleRecoveryCode zeigt den noch nicht bestätigten Code (einmalig nach Einrichtung oder Umzug).
func (a *App) handleRecoveryCode(w http.ResponseWriter, r *http.Request) {
	code, err := a.DB.Get(recoveryPendingKey)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	u, _ := a.System.DB.User(a.UserID)
	name := ""
	if u != nil {
		name = u.Name
	}
	info, _ := a.System.systemKeyInfo()
	writeJSON(w, 200, map[string]any{"code": code, "name": name, "system": info.Art == "nutzer" && info.UID == a.UserID})
}

// handleRecoveryConfirm: Name (Pflicht, falls noch leer) speichern und den Code als notiert bestätigen – er wird gelöscht.
func (a *App) handleRecoveryConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Notiert bool   `json:"notiert"`
	}
	if readJSON(r, &in) != nil || !in.Notiert {
		writeErr(w, 400, "ungueltig", "Bitte bestätigen, dass der Code notiert ist")
		return
	}
	name := store.Preview(strings.TrimSpace(in.Name), 40)
	u, err := a.System.DB.User(a.UserID)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if name == "" && u.Name == "" {
		writeErr(w, 400, "name", "Bitte einen Namen eingeben")
		return
	}
	if name != "" && name != u.Name {
		if err := a.System.DB.SetUserName(a.UserID, name); err != nil {
			writeErr(w, 500, "speicher", "Name nicht gespeichert")
			return
		}
	}
	if err := a.DB.Set(recoveryPendingKey, ""); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleRecoveryNew erzeugt einen neuen Wiederherstellungscode (der alte gilt danach nicht mehr) und zeigt ihn einmal.
// recoveryFresh: Neuer Wiederherstellungscode nur kurz nach einer Anmeldung mit Passkey – eine gestohlene Sitzung
// allein reicht dafür nicht.
const recoveryFresh = 10 * time.Minute

func (a *App) handleRecoveryNew(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || time.Since(time.UnixMilli(a.System.DB.SessionCreated(c.Value))) > recoveryFresh {
		writeErr(w, http.StatusForbidden, "frisch", "Zur Sicherheit erst ab- und wieder anmelden, dann innerhalb von 10 Minuten den neuen Code erzeugen")
		return
	}
	u, err := a.System.DB.User(a.UserID)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	dek, err := schluessel.Read(a.Cfg.KeyDir, a.UserID, u.KeyID)
	if err != nil {
		writeErr(w, 500, "schluessel", "Schlüssel nicht lesbar")
		return
	}
	code, err := schluessel.NewCode()
	if err != nil {
		writeErr(w, 500, "zufall", "Code nicht erzeugt")
		return
	}
	wrap, err := schluessel.Wrap(code, a.UserID, dek)
	if err == nil {
		err = a.System.DB.SetUserKey(a.UserID, dek.ID(), wrap)
	}
	if err == nil {
		err = a.DB.Set(recoveryPendingKey, code)
	}
	if err != nil {
		writeErr(w, 500, "speicher", "Code nicht gespeichert")
		return
	}
	writeJSON(w, 200, map[string]any{"code": code})
}

// handleRecover: gesperrte Daten (Schlüsseldatei fehlt, z. B. nach Restore auf neuer Maschine) mit dem Code öffnen.
func (s *System) handleRecover(w http.ResponseWriter, r *http.Request, u *store.User) {
	if a, _ := s.app(u.ID); a != nil {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if u.LockedAt > 0 {
		writeErr(w, http.StatusForbidden, "gesperrt", "Dein Zugang ist vom Admin gesperrt")
		return
	}
	if !s.allow("wiederherstellen:"+u.WebAuthnID, 5) {
		writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Versuche – bitte eine Minute warten")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if a, _ := s.app(u.ID); a != nil { // inzwischen schon geöffnet (zweiter Aufruf)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	dek, err := schluessel.Unwrap(in.Code, u.KeyWrap, u.ID)
	if err != nil || dek.ID() != u.KeyID {
		writeErr(w, http.StatusForbidden, "code", "Wiederherstellungscode falsch")
		return
	}
	if err := schluessel.Write(s.Cfg.KeyDir, u.ID, dek); err != nil {
		logf("Wiederherstellen Nutzer %d: %v", u.ID, err)
		writeErr(w, 500, "schluessel", "Schlüsseldatei nicht geschrieben")
		return
	}
	if _, err := s.openUser(u); err != nil {
		logf("Wiederherstellen Nutzer %d: %v", u.ID, err)
		writeErr(w, 500, "speicher", "Daten lassen sich nicht öffnen")
		return
	}
	s.recoverSystemKeyWith(u.ID, dek) // an ihn gebunden: kommt mit zurück
	logf("Nutzer %d: Schlüssel mit Wiederherstellungscode zurückgeholt", u.ID)
	writeJSON(w, 200, map[string]any{"ok": true})
}
