package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
)

// Konto verbinden statt API-Schlüssel (OAuth PKCE): bisher OpenRouter. Ablauf:
// 1. Web-App → …/start → Server merkt sich state → code_verifier (15 min, nur im Speicher) und liefert die Adresse
//    der Freigabe bei OpenRouter (callback = <Adresse>/api/v1/ai/connect/openrouter; ohne https: Code von Hand einfügen).
// 2. Nutzer gibt frei → OpenRouter leitet mit code und state auf /api/v1/ai/connect/openrouter → die Seite reicht beides an das
//    Fenster weiter, das die Verbindung begonnen hat (eigene Sitzung bzw. Einrichtungs-Code) → …/fertig.
// 3. Server tauscht code + verifier gegen den API-Schlüssel des Nutzers (POST /api/v1/auth/keys) und legt den
//    Zugang an (Status → KI) bzw. gibt ihn an den Assistenten zurück, der ihn mit der Einrichtung speichert.

const (
	openRouterAuthURL  = "https://openrouter.ai/auth"
	openRouterKeysURL  = "https://openrouter.ai/api/v1/auth/keys"
	kiConnectTTL       = 15 * time.Minute
	kiConnectCallback  = pathConnectOpenRouter
	openRouterKeyLabel = "Pebble Sprachnotizen"
)

type kiFlow struct {
	verifier string
	expires  time.Time
	code     string // vom Rücksprung abgelegt (falls das Fenster die Nachricht nicht erreicht, z. B. iOS-Web-App)
}

// kiConnects: offene Verbindungen (state → verifier), für Assistent und laufenden Dienst gemeinsam.
var kiConnects = struct {
	mu    sync.Mutex
	flows map[string]kiFlow
}{flows: map[string]kiFlow{}}

// kiConnectStart legt einen neuen Ablauf an; callback leer = Code von Hand (OpenRouter zeigt ihn an).
func kiConnectStart(callback string) (authURL, state string) {
	verifier, state := randomToken(48), randomToken(24)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"key_label": {openRouterKeyLabel}}
	if callback != "" {
		q.Set("callback_url", callback)
		q.Set("state", state)
	}
	kiConnects.mu.Lock()
	defer kiConnects.mu.Unlock()
	now := time.Now()
	for k, f := range kiConnects.flows {
		if now.After(f.expires) {
			delete(kiConnects.flows, k)
		}
	}
	if len(kiConnects.flows) > 100 { // Schutz vor Füllen des Speichers
		for k := range kiConnects.flows {
			delete(kiConnects.flows, k)
			break
		}
	}
	kiConnects.flows[state] = kiFlow{verifier: verifier, expires: now.Add(kiConnectTTL)}
	return openRouterAuthURL + "?" + q.Encode(), state
}

// kiConnectVerifier liefert den verifier eines offenen Ablaufs; erst ein gelungener Tausch beendet ihn
// (kiConnectEnd), damit ein vertippter Code oder ein kurzer Fehler keinen Neustart erzwingt.
func kiConnectVerifier(state string) (string, bool) {
	kiConnects.mu.Lock()
	defer kiConnects.mu.Unlock()
	f, ok := kiConnects.flows[state]
	if !ok || time.Now().After(f.expires) {
		delete(kiConnects.flows, state)
		return "", false
	}
	return f.verifier, true
}

func kiConnectEnd(state string) {
	kiConnects.mu.Lock()
	delete(kiConnects.flows, state)
	kiConnects.mu.Unlock()
}

// kiConnectDeposit legt den Code eines Rücksprungs ab (nur zu einem offenen state); kiConnectPickup holt ihn.
func kiConnectDeposit(state, code string) bool {
	kiConnects.mu.Lock()
	defer kiConnects.mu.Unlock()
	f, ok := kiConnects.flows[state]
	if !ok || time.Now().After(f.expires) || code == "" || len(code) > 512 {
		return false
	}
	f.code = code
	kiConnects.flows[state] = f
	return true
}

func kiConnectPickup(state string) string {
	kiConnects.mu.Lock()
	defer kiConnects.mu.Unlock()
	return kiConnects.flows[state].code
}

// handleKIConnectReturn: POST …/rueckgabe {state, code} – ohne Anmeldung (der Rücksprung kommt in einem fremden
// Fenster ohne Sitzung an); state ist ein Geheimnis von 192 Bit, der Code taugt ohne den verifier nichts.
func handleKIConnectReturn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if readJSON(r, &in) != nil || !kiConnectDeposit(in.State, in.Code) {
		writeErr(w, 400, "ungueltig", "Verbindung unbekannt oder abgelaufen")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleKIConnectPoll: POST …/abholen {state} → {code} (leer, solange keiner da ist).
func handleKIConnectPoll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		State string `json:"state"`
	}
	if readJSON(r, &in) != nil || in.State == "" {
		writeErr(w, 400, "ungueltig", "state fehlt")
		return
	}
	writeJSON(w, 200, map[string]any{"code": kiConnectPickup(in.State)})
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// exchangeOpenRouter tauscht den Code der Freigabe gegen den API-Schlüssel des Nutzers.
func exchangeOpenRouter(ctx context.Context, code, verifier string) (string, error) {
	body, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier, "code_challenge_method": "S256"})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", openRouterKeysURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", i18n.Errorf("OpenRouter nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Key   string `json:"key"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(data, &out)
	if resp.StatusCode != 200 || out.Key == "" {
		msg := out.Error.Message
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return "", i18n.Errorf("OpenRouter: Verbindung abgelehnt (HTTP %d: %s)", resp.StatusCode, msg)
	}
	return out.Key, nil
}

// callbackFor: Rücksprung-Adresse zu einer Basis-Adresse; leer, wenn OpenRouter sie nicht annimmt (kein https,
// außer localhost) – dann zeigt OpenRouter den Code zum Einfügen.
func callbackFor(base string) string {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1")) {
		return ""
	}
	return u.Scheme + "://" + u.Host + kiConnectCallback
}

// --- Laufender Dienst (Status → KI, Admins) ---

// handleKIConnectStart: POST /api/ki/verbinden/openrouter/start.
func (s *System) handleKIConnectStart(w http.ResponseWriter, r *http.Request) {
	authURL, state := kiConnectStart(callbackFor(s.Cfg.PublicURL))
	writeJSON(w, 200, map[string]any{"url": authURL, "state": state, "von_hand": callbackFor(s.Cfg.PublicURL) == ""})
}

// handleKIConnectDone: POST /api/ki/verbinden/openrouter/fertig {code, state, zugang?} – legt den Zugang an oder
// erneuert den Schlüssel eines vorhandenen OpenRouter-Zugangs.
func (s *System) handleKIConnectDone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code   string `json:"code"`
		State  string `json:"state"`
		Zugang string `json:"zugang"`
	}
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Code) == "" {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	verifier, ok := kiConnectVerifier(in.State)
	if !ok {
		writeErr(w, 400, "abgelaufen", "Verbindung abgelaufen – bitte neu verbinden")
		return
	}
	key, err := exchangeOpenRouter(r.Context(), strings.TrimSpace(in.Code), verifier)
	if err != nil {
		writeErr(w, 502, "openrouter", err.Error())
		return
	}
	kiConnectEnd(in.State)
	ai := s.KI().AI
	accs := append([]config.AIAccess{}, ai.Accesses...)
	id := ""
	for i := range accs {
		if accs[i].ID == in.Zugang && accs[i].Kind == "openrouter" {
			accs[i].Key, id = key, accs[i].ID
		}
	}
	if id == "" {
		id = newAccessID()
		accs = append(accs, config.AIAccess{ID: id, Name: uniqueAccessName(accs, "OpenRouter"), Kind: "openrouter", Key: key})
	}
	areas := map[string][]config.AIEntry{}
	for k, v := range ai.Areas {
		areas[k] = append([]config.AIEntry{}, v...)
	}
	if in.Zugang == "" {
		assignFreeAreas(areas, id, ensureORCatalog(r.Context(), s.DB, true))
	}
	logf("KI: OpenRouter-Konto verbunden (Zugang %s)", id)
	s.saveAI(w, accs, areas, nil, map[string]any{"zugang": id})
}

// assignFreeAreas: ein neu verbundener OpenRouter-Zugang übernimmt jeden Bereich, der noch keinen aktiven Eintrag hat,
// mit der Vorgabe aus dem Katalog (Auswertung und Recherche mit der Websuche des Anbieters). Ohne Katalog bleibt der
// Bereich frei.
func assignFreeAreas(areas map[string][]config.AIEntry, id string, cat *orCatalog) {
	t := config.ProviderTypeByID("openrouter")
	for _, area := range t.Areas {
		active := false
		for _, e := range areas[area] {
			if !e.Off {
				active = true
			}
		}
		model := cat.defaultModel(area)
		if active || model == "" {
			continue
		}
		e := config.AIEntry{ID: area + "-" + newAccessID(), Access: id, Model: model}
		if area == config.AreaDictation || area == config.AreaResearch {
			e.Search = []string{"anbieter"}
		}
		areas[area] = append([]config.AIEntry{e}, areas[area]...)
	}
}

func newAccessID() string {
	b := make([]byte, 5)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func uniqueAccessName(accs []config.AIAccess, base string) string {
	name := base
	for i := 2; ; i++ {
		taken := false
		for _, a := range accs {
			if strings.EqualFold(strings.TrimSpace(a.Name), name) {
				taken = true
			}
		}
		if !taken {
			return name
		}
		name = base + " " + strconv.Itoa(i)
	}
}

// --- Einrichtungsassistent (Einrichtungs-Code statt Sitzung) ---

// handleSetupConnectStart: POST /api/ersteinrichtung/verbinden/openrouter/start {code, adresse} – adresse ist die,
// unter der der Assistent läuft (die öffentliche gibt es noch nicht sicher).
func (s *SetupServer) handleSetupConnectStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Adresse string `json:"adresse"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(r, in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	cb := callbackFor(in.Adresse)
	authURL, state := kiConnectStart(cb)
	writeJSON(w, 200, map[string]any{"url": authURL, "state": state, "von_hand": cb == ""})
}

// handleSetupConnectDone: POST /api/ersteinrichtung/verbinden/openrouter/fertig {code, oauth_code, state} → {key};
// der Assistent speichert den Schlüssel mit der Einrichtung.
func (s *SetupServer) handleSetupConnectDone(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code      string `json:"code"`
		OAuthCode string `json:"oauth_code"`
		State     string `json:"state"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(r, in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	if strings.TrimSpace(in.OAuthCode) == "" {
		writeErr(w, 400, "ungueltig", "Code fehlt")
		return
	}
	verifier, ok := kiConnectVerifier(in.State)
	if !ok {
		writeErr(w, 400, "abgelaufen", "Verbindung abgelaufen – bitte neu verbinden")
		return
	}
	key, err := exchangeOpenRouter(r.Context(), strings.TrimSpace(in.OAuthCode), verifier)
	if err != nil {
		writeErr(w, 502, "openrouter", err.Error())
		return
	}
	kiConnectEnd(in.State)
	// Katalog der Modelle gleich abrufen und ablegen; der Assistent belegt die Bereiche mit seinen Vorgaben.
	writeJSON(w, 200, map[string]any{"key": key, "vorschlag": ensureORCatalog(r.Context(), s.DB, true).defaults()})
}
