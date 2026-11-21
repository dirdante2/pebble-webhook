package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/i18n"
)

// OAuth 2.1 für den MCP-Server (Claude, ChatGPT und andere Connectoren):
// 1. Client ruft /mcp ohne Token → 401 mit Verweis auf /.well-known/oauth-protected-resource.
// 2. Metadaten (RFC 9728, RFC 8414) → Registrierung (RFC 7591, /oauth/register).
// 3. /oauth/authorize prüft Client, Rücksprung-Adresse und PKCE (S256) → Freigabeseite /verbinden in der Web-App
//    (Passkey-Sitzung nötig) → „Erlauben“ liefert einen Einmal-Code (5 min) an die Rücksprung-Adresse.
// 4. /oauth/token tauscht Code + code_verifier gegen Zugangs-Token (1 h) und Erneuerungs-Token (90 Tage, rotierend).
// Tokens gelten nur für /mcp. Gespeichert wird alles in kv „oauth“ (Tokens nur als SHA-256), Codes nur im Speicher.

const (
	oauthKey        = "oauth"
	oauthCodeTTL    = 5 * time.Minute
	oauthAccessTTL  = time.Hour
	oauthRefreshTTL = 90 * 24 * time.Hour
	oauthMaxClients = 50
)

type oauthClient struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
	AuthMethod   string   `json:"auth_method"`
	SecretHash   string   `json:"secret_hash,omitempty"`
	Created      int64    `json:"created"`
}

type oauthGrant struct {
	ID          string `json:"id"`
	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	Created     int64  `json:"created"`
	LastUsed    int64  `json:"last_used"`
	AccessHash  string `json:"access_hash"`
	AccessExp   int64  `json:"access_exp"`
	RefreshHash string `json:"refresh_hash"`
	RefreshExp  int64  `json:"refresh_exp"`
}

type oauthData struct {
	Clients map[string]*oauthClient `json:"clients"`
	Grants  map[string]*oauthGrant  `json:"grants"`
}

type oauthCode struct {
	clientID, redirectURI, challenge string
	exp                              time.Time
}

type oauthServer struct {
	mu    sync.Mutex
	codes map[string]oauthCode
}

func oauthRandom(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func oauthHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hashEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// load/save: Aufrufer hält a.oauth.mu.
func (a *App) oauthLoad() (*oauthData, error) {
	d := &oauthData{}
	v, err := a.DB.Get(oauthKey)
	if err != nil {
		return nil, err
	}
	if v != "" {
		if err := json.Unmarshal([]byte(v), d); err != nil {
			return nil, err
		}
	}
	if d.Clients == nil {
		d.Clients = map[string]*oauthClient{}
	}
	if d.Grants == nil {
		d.Grants = map[string]*oauthGrant{}
	}
	return d, nil
}

func (a *App) oauthSave(d *oauthData) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return a.DB.Set(oauthKey, string(b))
}

// prune entfernt abgelaufene Freigaben und Clients ohne Freigabe, die älter als ein Tag sind.
func (d *oauthData) prune(now time.Time) {
	used := map[string]bool{}
	for id, g := range d.Grants {
		if g.RefreshExp < now.UnixMilli() {
			delete(d.Grants, id)
			continue
		}
		used[g.ClientID] = true
	}
	for id, c := range d.Clients {
		if !used[id] && now.UnixMilli()-c.Created > 24*time.Hour.Milliseconds() {
			delete(d.Clients, id)
		}
	}
}

func (a *App) mcpURL() string { return a.Cfg.PublicURL + "/mcp" }

func oauthCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version, Mcp-Session-Id")
	h.Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
}

func (a *App) handleOAuthOptions(w http.ResponseWriter, r *http.Request) {
	oauthCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

// --- Metadaten ---

func (a *App) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	oauthCORS(w)
	writeJSON(w, 200, map[string]any{
		"resource":                 a.mcpURL(),
		"authorization_servers":    []string{a.Cfg.PublicURL},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Sprachnotizen",
	})
}

func (a *App) handleAuthServerMeta(w http.ResponseWriter, r *http.Request) {
	oauthCORS(w)
	base := a.Cfg.PublicURL
	writeJSON(w, 200, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"mcp"},
	})
}

// --- Registrierung (RFC 7591) ---

// validRedirect: https, oder http nur für localhost/127.0.0.1 (Claude Code, Inspector); kein Fragment.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil || len(raw) > 500 {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	oauthCORS(w)
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func (a *App) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !a.allow("oauthreg:"+a.clientIP(r), 10) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "Zu viele Registrierungen")
		return
	}
	var in struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
		GrantTypes   []string `json:"grant_types"`
	}
	if readJSON(r, &in) != nil || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		oauthError(w, 400, "invalid_client_metadata", "redirect_uris fehlt")
		return
	}
	for _, u := range in.RedirectURIs {
		if !validRedirect(u) {
			oauthError(w, 400, "invalid_redirect_uri", "Rücksprung-Adresse muss https sein (http nur für localhost): "+u)
			return
		}
	}
	switch in.AuthMethod {
	case "":
		in.AuthMethod = "none"
	case "none", "client_secret_post", "client_secret_basic":
	default:
		oauthError(w, 400, "invalid_client_metadata", "token_endpoint_auth_method nicht unterstützt")
		return
	}
	name := strings.TrimSpace(in.ClientName)
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if name == "" {
		name = i18n.T("Unbenannte App")
	}
	c := &oauthClient{ID: oauthRandom(18), Name: name, RedirectURIs: in.RedirectURIs, AuthMethod: in.AuthMethod, Created: time.Now().UnixMilli()}
	secret := ""
	if c.AuthMethod != "none" {
		secret = oauthRandom(32)
		c.SecretHash = oauthHash(secret)
	}
	a.oauth.mu.Lock()
	defer a.oauth.mu.Unlock()
	d, err := a.oauthLoad()
	if err != nil {
		oauthError(w, 500, "server_error", "Datenbankfehler")
		return
	}
	d.prune(time.Now())
	if len(d.Clients) >= oauthMaxClients {
		oauthError(w, 400, "invalid_client_metadata", "Zu viele registrierte Apps")
		return
	}
	d.Clients[c.ID] = c
	if err := a.oauthSave(d); err != nil {
		oauthError(w, 500, "server_error", "Datenbankfehler")
		return
	}
	out := map[string]any{"client_id": c.ID, "client_id_issued_at": c.Created / 1000, "client_name": c.Name,
		"redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": c.AuthMethod,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	oauthCORS(w)
	writeJSON(w, 201, out)
}

// --- Freigabe ---

type authRequest struct {
	client      *oauthClient
	redirectURI string
	state       string
	challenge   string
}

// checkAuthorize prüft die Parameter von /oauth/authorize. redirectOK: Fehler dürfen an die Rücksprung-Adresse gehen.
func (a *App) checkAuthorize(q url.Values) (req authRequest, redirectOK bool, errCode, errDesc string) {
	a.oauth.mu.Lock()
	d, err := a.oauthLoad()
	a.oauth.mu.Unlock()
	if err != nil {
		return req, false, "server_error", "Datenbankfehler"
	}
	c := d.Clients[q.Get("client_id")]
	if c == nil {
		return req, false, "invalid_client", "Unbekannte App (client_id). Die Verbindung in der App neu einrichten."
	}
	ru := q.Get("redirect_uri")
	if ru == "" && len(c.RedirectURIs) == 1 {
		ru = c.RedirectURIs[0]
	}
	found := false
	for _, u := range c.RedirectURIs {
		found = found || u == ru
	}
	if !found {
		return req, false, "invalid_request", "Rücksprung-Adresse ist für diese App nicht registriert"
	}
	req = authRequest{client: c, redirectURI: ru, state: q.Get("state"), challenge: q.Get("code_challenge")}
	if q.Get("response_type") != "code" {
		return req, true, "unsupported_response_type", "nur response_type=code"
	}
	if req.challenge == "" || q.Get("code_challenge_method") != "S256" || len(req.challenge) < 43 || len(req.challenge) > 128 {
		return req, true, "invalid_request", "PKCE mit S256 ist Pflicht"
	}
	if res := q.Get("resource"); res != "" && strings.TrimRight(res, "/") != a.mcpURL() && strings.TrimRight(res, "/") != a.Cfg.PublicURL {
		return req, true, "invalid_target", "Unbekannte Ressource"
	}
	return req, true, "", ""
}

func redirectWith(base string, v url.Values) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + v.Encode()
}

// handleOAuthAuthorize: gültige Anfrage → Freigabeseite der Web-App mit denselben Parametern.
func (a *App) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req, redirectOK, code, desc := a.checkAuthorize(q)
	if code != "" {
		if redirectOK {
			v := url.Values{"error": {code}, "error_description": {desc}}
			if req.state != "" {
				v.Set("state", req.state)
			}
			http.Redirect(w, r, redirectWith(req.redirectURI, v), http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(400)
		fmt.Fprintln(w, i18n.T("Verbindung nicht möglich:")+" "+i18n.T(desc))
		return
	}
	http.Redirect(w, r, "/verbinden?"+q.Encode(), http.StatusFound)
}

// handleOAuthRequest (Sitzung): was die Freigabeseite anzeigt.
func (a *App) handleOAuthRequest(w http.ResponseWriter, r *http.Request) {
	req, _, code, desc := a.checkAuthorize(r.URL.Query())
	if code != "" {
		writeErr(w, 400, "ungueltig", desc)
		return
	}
	host := req.redirectURI
	if u, err := url.Parse(req.redirectURI); err == nil {
		host = u.Host
	}
	writeJSON(w, 200, map[string]any{"name": req.client.Name, "ziel": host})
}

// handleOAuthDecide (Sitzung): Erlauben oder Ablehnen; liefert die Adresse, zu der der Browser weiterspringt.
func (a *App) handleOAuthDecide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Anfrage  string `json:"anfrage"`
		Erlauben bool   `json:"erlauben"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Anfrage nicht lesbar")
		return
	}
	q, err := url.ParseQuery(strings.TrimPrefix(in.Anfrage, "?"))
	if err != nil {
		writeErr(w, 400, "ungueltig", "Anfrage nicht lesbar")
		return
	}
	req, _, code, desc := a.checkAuthorize(q)
	if code != "" {
		writeErr(w, 400, "ungueltig", desc)
		return
	}
	v := url.Values{}
	if req.state != "" {
		v.Set("state", req.state)
	}
	if !in.Erlauben {
		v.Set("error", "access_denied")
		writeJSON(w, 200, map[string]any{"weiter": redirectWith(req.redirectURI, v)})
		return
	}
	c := oauthRandom(32)
	a.oauth.mu.Lock()
	if a.oauth.codes == nil {
		a.oauth.codes = map[string]oauthCode{}
	}
	now := time.Now()
	for k, x := range a.oauth.codes {
		if now.After(x.exp) {
			delete(a.oauth.codes, k)
		}
	}
	a.oauth.codes[oauthHash(c)] = oauthCode{clientID: req.client.ID, redirectURI: req.redirectURI, challenge: req.challenge, exp: now.Add(oauthCodeTTL)}
	a.oauth.mu.Unlock()
	v.Set("code", c)
	v.Set("iss", a.Cfg.PublicURL)
	writeJSON(w, 200, map[string]any{"weiter": redirectWith(req.redirectURI, v)})
}

// --- Token ---

// clientAuth prüft die Client-Anmeldung am Token-Endpunkt. Aufrufer hält a.oauth.mu.
func clientAuth(d *oauthData, r *http.Request) (*oauthClient, bool) {
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	c := d.Clients[id]
	if c == nil {
		return nil, false
	}
	if c.AuthMethod == "none" {
		return c, true
	}
	return c, secret != "" && hashEqual(oauthHash(secret), c.SecretHash)
}

func (a *App) issueTokens(w http.ResponseWriter, d *oauthData, g *oauthGrant) {
	access, refresh := oauthRandom(32), oauthRandom(32)
	now := time.Now()
	g.AccessHash, g.AccessExp = oauthHash(access), now.Add(oauthAccessTTL).UnixMilli()
	g.RefreshHash, g.RefreshExp = oauthHash(refresh), now.Add(oauthRefreshTTL).UnixMilli()
	g.LastUsed = now.UnixMilli()
	if err := a.oauthSave(d); err != nil {
		oauthError(w, 500, "server_error", "Datenbankfehler")
		return
	}
	oauthCORS(w)
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(oauthAccessTTL.Seconds()),
		"refresh_token": refresh, "scope": "mcp"})
}

func (a *App) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if !a.allow("oauthtok:"+a.clientIP(r), 30) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "Zu viele Anfragen")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request", "Formular nicht lesbar")
		return
	}
	f := r.PostForm
	a.oauth.mu.Lock()
	defer a.oauth.mu.Unlock()
	d, err := a.oauthLoad()
	if err != nil {
		oauthError(w, 500, "server_error", "Datenbankfehler")
		return
	}
	c, ok := clientAuth(d, r)
	if !ok {
		oauthError(w, 401, "invalid_client", "App unbekannt oder Geheimnis falsch")
		return
	}
	switch f.Get("grant_type") {
	case "authorization_code":
		key := oauthHash(f.Get("code"))
		code, found := a.oauth.codes[key]
		delete(a.oauth.codes, key) // Einmal-Code, auch bei Fehlschlag verbraucht
		if !found || time.Now().After(code.exp) || code.clientID != c.ID {
			oauthError(w, 400, "invalid_grant", "Code ungültig oder abgelaufen")
			return
		}
		if ru := f.Get("redirect_uri"); ru != "" && ru != code.redirectURI {
			oauthError(w, 400, "invalid_grant", "Rücksprung-Adresse passt nicht")
			return
		}
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if !hashEqual(base64.RawURLEncoding.EncodeToString(sum[:]), code.challenge) {
			oauthError(w, 400, "invalid_grant", "PKCE-Prüfung fehlgeschlagen")
			return
		}
		d.prune(time.Now())
		g := &oauthGrant{ID: oauthRandom(12), ClientID: c.ID, ClientName: c.Name, Created: time.Now().UnixMilli()}
		d.Grants[g.ID] = g
		a.issueTokens(w, d, g)
	case "refresh_token":
		h := oauthHash(f.Get("refresh_token"))
		for _, g := range d.Grants {
			if g.ClientID == c.ID && hashEqual(g.RefreshHash, h) && g.RefreshExp > time.Now().UnixMilli() {
				a.issueTokens(w, d, g)
				return
			}
		}
		oauthError(w, 400, "invalid_grant", "Erneuerungs-Token ungültig")
	default:
		oauthError(w, 400, "unsupported_grant_type", "nur authorization_code und refresh_token")
	}
}

// handleOAuthRevoke (RFC 7009): Token widerrufen → die ganze Freigabe endet. Antwortet immer 200.
func (a *App) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() == nil {
		h := oauthHash(r.PostForm.Get("token"))
		a.oauth.mu.Lock()
		if d, err := a.oauthLoad(); err == nil {
			if c, ok := clientAuth(d, r); ok {
				for id, g := range d.Grants {
					if g.ClientID == c.ID && (hashEqual(g.AccessHash, h) || hashEqual(g.RefreshHash, h)) {
						delete(d.Grants, id)
						a.oauthSave(d)
						break
					}
				}
			}
		}
		a.oauth.mu.Unlock()
	}
	oauthCORS(w)
	w.WriteHeader(200)
}

// --- Zugang zu /mcp: API-Token oder OAuth-Zugangs-Token ---

func (a *App) mcpAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		oauthCORS(w)
		h := r.Header.Get("Authorization")
		deny := func(desc string) {
			v := fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`, a.Cfg.PublicURL)
			if desc != "" {
				v += fmt.Sprintf(`, error="invalid_token", error_description="%s"`, desc)
			}
			w.Header().Set("WWW-Authenticate", v)
			writeErr(w, http.StatusUnauthorized, "anmeldung", "Anmeldung nötig")
		}
		if !strings.HasPrefix(h, "Bearer ") {
			deny("")
			return
		}
		if !a.allow("api:"+a.clientIP(r), 300) {
			writeErr(w, http.StatusTooManyRequests, "limit", "Zu viele Anfragen")
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		got := sha256.Sum256([]byte(tok))
		want := sha256.Sum256([]byte(a.Cfg.APIToken))
		if a.Cfg.APIToken != "" && subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
			next(w, r)
			return
		}
		th := oauthHash(tok)
		now := time.Now().UnixMilli()
		a.oauth.mu.Lock()
		d, err := a.oauthLoad()
		var hit *oauthGrant
		if err == nil {
			for _, g := range d.Grants {
				if hashEqual(g.AccessHash, th) && g.AccessExp > now {
					hit = g
					break
				}
			}
			// Zuletzt benutzt höchstens minütlich speichern.
			if hit != nil && now-hit.LastUsed > 60_000 {
				hit.LastUsed = now
				a.oauthSave(d)
			}
		}
		a.oauth.mu.Unlock()
		if hit == nil {
			deny("Token ungültig oder abgelaufen")
			return
		}
		next(w, r)
	}
}

// --- Verwaltung in der Web-App (Status): verbundene Apps ansehen und trennen ---

func (a *App) handleOAuthGrants(w http.ResponseWriter, r *http.Request) {
	a.oauth.mu.Lock()
	d, err := a.oauthLoad()
	a.oauth.mu.Unlock()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, g := range d.Grants {
		if g.RefreshExp > time.Now().UnixMilli() {
			out = append(out, map[string]any{"id": g.ID, "name": g.ClientName, "angelegt": g.Created, "zuletzt": g.LastUsed})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["zuletzt"].(int64) > out[j]["zuletzt"].(int64) })
	writeJSON(w, 200, map[string]any{"apps": out})
}

func (a *App) handleOAuthGrantDelete(w http.ResponseWriter, r *http.Request) {
	a.oauth.mu.Lock()
	defer a.oauth.mu.Unlock()
	d, err := a.oauthLoad()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if d.Grants[r.PathValue("id")] == nil {
		writeErr(w, 404, "fehlt", "Verbindung nicht gefunden")
		return
	}
	delete(d.Grants, r.PathValue("id"))
	if err := a.oauthSave(d); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
