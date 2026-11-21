package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

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
	expires time.Time
}

// user ist der einzige Benutzer des Dienstes mit allen seinen Passkeys.
type user struct {
	id    []byte
	creds []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return u.id }
func (u *user) WebAuthnName() string                       { return "sprachnotizen" }
func (u *user) WebAuthnDisplayName() string                { return "Sprachnotizen" }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (a *App) initAuth() error {
	wa, err := webauthn.New(&webauthn.Config{RPID: a.Cfg.Host(), RPDisplayName: "Sprachnotizen", RPOrigins: []string{a.Cfg.PublicURL}})
	if err != nil {
		return err
	}
	a.wa = wa
	if id, err := a.DB.Get("webauthn_user"); err != nil {
		return err
	} else if id == "" {
		b := make([]byte, 32)
		rand.Read(b)
		return a.DB.Set("webauthn_user", base64.RawURLEncoding.EncodeToString(b))
	}
	return nil
}

func (a *App) loadUser() (*user, error) {
	id, err := a.DB.Get("webauthn_user")
	if err != nil {
		return nil, err
	}
	raw, _ := base64.RawURLEncoding.DecodeString(id)
	keys, err := a.DB.Passkeys()
	if err != nil {
		return nil, err
	}
	u := &user{id: raw}
	for _, k := range keys {
		var c webauthn.Credential
		// Prüfzugänge (CLI „zugang“) haben keinen Passkey und zählen hier nicht.
		if json.Unmarshal([]byte(k.Credential), &c) == nil && len(c.ID) > 0 {
			u.creds = append(u.creds, c)
		}
	}
	return u, nil
}

func (a *App) putCeremony(c ceremony) string {
	a.cerMu.Lock()
	defer a.cerMu.Unlock()
	for k, v := range a.cer {
		if time.Now().After(v.expires) {
			delete(a.cer, k)
		}
	}
	id := store.NewID()
	c.expires = time.Now().Add(5 * time.Minute)
	a.cer[id] = c
	return id
}

func (a *App) takeCeremony(id string) (ceremony, bool) {
	a.cerMu.Lock()
	defer a.cerMu.Unlock()
	c, ok := a.cer[id]
	delete(a.cer, id)
	return c, ok && time.Now().Before(c.expires)
}

func (a *App) setSession(w http.ResponseWriter, passkeyID string) error {
	token, err := a.DB.NewSession(passkeyID, sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	return nil
}

func (a *App) sessionOf(r *http.Request) (passkeyID string, ok bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	return a.DB.Session(c.Value, sessionTTL)
}

// requireSession schützt die API: gültige Sitzung, bei Schreibzugriffen zusätzlich gleiche Herkunft.
func (a *App) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(w, r) {
			return
		}
		if _, ok := a.sessionOf(r); !ok {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Nicht angemeldet")
			return
		}
		next(w, r)
	}
}

type ctxKey int

const machineKey ctxKey = 1

// isMachine: die Anfrage kam über den Maschinenzugang, nicht aus der Web-App.
func isMachine(r *http.Request) bool {
	v, _ := r.Context().Value(machineKey).(bool)
	return v
}

// machine öffnet eine Route zusätzlich für den Maschinenzugang (Authorization: Bearer <API_TOKEN>).
// Trägt die Anfrage den Header, zählt nur der Token – kein Rückfall auf die Sitzung. Ohne Header gilt der Weg der Web-App.
func (a *App) machine(next http.HandlerFunc) http.HandlerFunc {
	session := a.requireSession(next)
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" {
			session(w, r)
			return
		}
		if !a.allow("api:"+a.clientIP(r), 300) {
			writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Anfragen")
			return
		}
		got := sha256.Sum256([]byte(strings.TrimPrefix(h, "Bearer ")))
		want := sha256.Sum256([]byte(a.Cfg.APIToken))
		if a.Cfg.APIToken == "" || !strings.HasPrefix(h, "Bearer ") || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Token ungültig")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), machineKey, true)))
	}
}

// sameOrigin weist Schreibzugriffe ab, die nicht aus der Web-App selbst stammen.
func (a *App) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if o := r.Header.Get("Origin"); r.Header.Get("X-App") != "1" || (o != "" && o != a.Cfg.PublicURL) {
		writeErr(w, http.StatusForbidden, "herkunft", "Anfrage nicht aus der Web-App")
		return false
	}
	return true
}

func (a *App) public(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.allow("auth:"+a.clientIP(r), 30) {
			writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Versuche")
			return
		}
		if a.sameOrigin(w, r) {
			next(w, r)
		}
	}
}

func (a *App) handleSession(w http.ResponseWriter, r *http.Request) {
	_, ok := a.sessionOf(r)
	keys, _ := a.DB.Passkeys()
	writeJSON(w, 200, map[string]any{"angemeldet": ok, "eingerichtet": len(keys) > 0})
}

func (a *App) handleEnrollStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil || !a.DB.EnrollTokenValid(in.Token) {
		writeErr(w, http.StatusForbidden, "link", "Der Einrichtungs-Link ist ungültig oder abgelaufen")
		return
	}
	u, err := a.loadUser()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range u.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := a.wa.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		writeErr(w, 500, "webauthn", "Passkey-Einrichtung nicht möglich")
		return
	}
	name := store.Preview(in.Name, 40)
	if name == "" {
		name = "Gerät"
	}
	writeJSON(w, 200, map[string]any{"vorgang": a.putCeremony(ceremony{data: *data, token: in.Token, name: name}), "optionen": opts})
}

func (a *App) handleEnrollFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := a.takeCeremony(r.URL.Query().Get("vorgang"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "vorgang", "Vorgang abgelaufen, bitte neu beginnen")
		return
	}
	u, err := a.loadUser()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	cred, err := a.wa.FinishRegistration(u, c.data, r)
	if err != nil {
		logf("Passkey-Einrichtung: %v", err)
		writeErr(w, http.StatusBadRequest, "webauthn", "Passkey wurde nicht angenommen")
		return
	}
	if !a.DB.UseEnrollToken(c.token) {
		writeErr(w, http.StatusForbidden, "link", "Der Einrichtungs-Link wurde schon benutzt")
		return
	}
	data, _ := json.Marshal(cred)
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	if err := a.DB.AddPasskey(store.Passkey{ID: id, Name: c.name, Credential: string(data)}); err != nil {
		writeErr(w, 500, "speicher", "Passkey nicht gespeichert")
		return
	}
	if err := a.setSession(w, id); err != nil {
		writeErr(w, 500, "speicher", "Sitzung nicht angelegt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	opts, data, err := a.wa.BeginDiscoverableLogin()
	if err != nil {
		writeErr(w, 500, "webauthn", "Anmeldung nicht möglich")
		return
	}
	writeJSON(w, 200, map[string]any{"vorgang": a.putCeremony(ceremony{data: *data}), "optionen": opts})
}

func (a *App) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := a.takeCeremony(r.URL.Query().Get("vorgang"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "vorgang", "Vorgang abgelaufen, bitte neu beginnen")
		return
	}
	cred, err := a.wa.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		return a.loadUser()
	}, c.data, r)
	if err != nil {
		logf("Anmeldung: %v", err)
		writeErr(w, http.StatusUnauthorized, "webauthn", "Passkey nicht erkannt")
		return
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	data, _ := json.Marshal(cred)
	a.DB.UpdatePasskey(id, string(data))
	if err := a.setSession(w, id); err != nil {
		writeErr(w, 500, "speicher", "Sitzung nicht angelegt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.DB.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	current, _ := a.sessionOf(r)
	keys, err := a.DB.Passkeys()
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
	keys, err := a.DB.Passkeys()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if len(keys) <= 1 {
		writeErr(w, http.StatusConflict, "letztes", "Das letzte Gerät kann nicht entfernt werden")
		return
	}
	if err := a.DB.DeletePasskey(r.PathValue("id")); err != nil {
		writeErr(w, 500, "speicher", "Gerät nicht entfernt")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleDeviceLink erzeugt aus einer Sitzung einen Einmal-Link für ein weiteres Gerät.
func (a *App) handleDeviceLink(w http.ResponseWriter, r *http.Request) {
	url, err := a.EnrollLink()
	if err != nil {
		writeErr(w, 500, "speicher", "Link nicht erzeugt")
		return
	}
	writeJSON(w, 200, map[string]any{"link": url, "gueltig_minuten": int(enrollTTL.Minutes())})
}

// TestAccess legt ein Gerät ohne Passkey samt Sitzung an und liefert das Sitzungs-Cookie.
// Für Prüfungen im Browser ohne Authenticator; erscheint in der Geräteliste und ist dort entfernbar.
func (a *App) TestAccess(name string) (cookie string, err error) {
	id := "zugang-" + store.NewID()[:12]
	if err := a.DB.AddPasskey(store.Passkey{ID: id, Name: name, Credential: "{}"}); err != nil {
		return "", err
	}
	token, err := a.DB.NewSession(id, sessionTTL)
	if err != nil {
		return "", err
	}
	return sessionCookie + "=" + token, nil
}

// EnrollLink erzeugt einen Einmal-Link zur Passkey-Einrichtung (CLI und „weiteres Gerät“).
func (a *App) EnrollLink() (string, error) {
	token, err := a.DB.NewEnrollToken(enrollTTL)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(a.Cfg.PublicURL, "/") + "/einrichten#" + token, nil
}
