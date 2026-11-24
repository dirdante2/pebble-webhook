package app

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/store"
)

// Einstellungen: alle Werte außer den Start-Werten liegen in der Datenbank (kv „einstellung:…“) und werden
// über die Web-Oberfläche gepflegt – beim ersten Start im Einrichtungsassistenten, danach unter Status →
// Einstellungen. Nach dem Speichern beendet sich der Dienst; systemd startet ihn mit den neuen Werten neu.

// ErrNotConfigured: es gibt noch keine Einstellungen (Ersteinrichtung nötig).
var ErrNotConfigured = fmt.Errorf("noch nicht eingerichtet")

// LoadSettings liest die Einstellungen aus der Datenbank. Gibt es noch keine, übernimmt es einmalig die Werte
// einer alten env-Datei (Umgebung), sofern dort eine öffentliche Adresse steht.
func LoadSettings(db *store.System, boot config.Bootstrap) (*config.Config, error) {
	values, err := db.Settings()
	if err != nil {
		return nil, err
	}
	if values["PUBLIC_URL"] == "" {
		imported := map[string]string{}
		for _, f := range config.Fields {
			if v := strings.TrimSpace(os.Getenv(f.Key)); v != "" {
				imported[f.Key] = v
			}
		}
		if imported["PUBLIC_URL"] == "" {
			return nil, ErrNotConfigured
		}
		if err := db.SaveSettings(imported); err != nil {
			return nil, err
		}
		log.Printf("Einstellungen: %d Werte aus der env-Datei übernommen; gepflegt werden sie ab jetzt in der Web-App", len(imported))
		values = imported
	}
	return config.FromValues(boot, func(k string) string { return values[k] })
}

// restartSoon beendet den Dienst kurz nach der Antwort; systemd (Restart=always) startet ihn neu.
func restartSoon() {
	go func() {
		time.Sleep(800 * time.Millisecond)
		log.Printf("Einstellungen gespeichert: Neustart")
		syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
}

// Bereiche der Einstellungen: alle (Einrichtungsassistent), nur System (Admin) oder nur persönliche (je Nutzer).
const (
	scopeAll = iota
	scopeSystem
	scopePersonal
)

func inScope(f config.Field, scope int) bool {
	return scope == scopeAll || (scope == scopePersonal) == f.Personal
}

// settingsView liefert die Felder mit den gespeicherten Werten; Geheimnisse nur als „gesetzt“.
func settingsView(values map[string]string, generate bool, scope int) []map[string]any {
	out := make([]map[string]any, 0, len(config.Fields))
	for _, f := range config.Fields {
		if !inScope(f, scope) {
			continue
		}
		v, set := values[f.Key]
		m := map[string]any{"key": f.Key, "gruppe": f.Group, "name": f.Label, "hilfe": f.Help, "art": f.Kind,
			"vorgabe": f.Default, "pflicht": f.Required, "auswahl": f.Choices, "persoenlich": f.Personal}
		switch {
		case f.Kind == "secret":
			m["gesetzt"] = set && v != ""
			m["wert"] = ""
			if generate && f.Generate && !(set && v != "") {
				m["wert"] = config.NewSecret() // im Assistenten vorgeschlagen; sichtbar, damit man ihn in die Pebble-App übernimmt
			}
		case set:
			m["wert"] = v
		default:
			m["wert"] = f.Default
		}
		out = append(out, m)
	}
	return out
}

// mergeSettings übernimmt die eingegebenen Werte: nur bekannte Schlüssel; ein leeres Geheimnis behält den alten Wert.
func mergeSettings(old, in map[string]string, scope int) map[string]string {
	merged := map[string]string{}
	for k, v := range old {
		merged[k] = v
	}
	for _, f := range config.Fields {
		if !inScope(f, scope) {
			continue
		}
		v, ok := in[f.Key]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if f.Kind == "secret" && v == "" {
			continue
		}
		if f.Kind == "url" {
			v = strings.TrimRight(v, "/")
		}
		merged[f.Key] = v
	}
	return merged
}

// --- Status → Einstellungen (angemeldet) ---

func (a *System) handleSettings(w http.ResponseWriter, r *http.Request) {
	values, err := a.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"felder": settingsView(values, false, scopeSystem)})
}

func (a *System) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Werte map[string]string `json:"werte"`
	}
	if readJSON(r, &in) != nil || in.Werte == nil {
		writeErr(w, 400, "ungueltig", "Werte nicht lesbar")
		return
	}
	old, err := a.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	merged := mergeSettings(old, in.Werte, scopeSystem)
	boot := config.Bootstrap{Listen: a.Cfg.Listen, DataDir: a.Cfg.DataDir, AudioDir: a.Cfg.AudioDir, BackupDir: a.Cfg.BackupDir, KeyDir: a.Cfg.KeyDir}
	if _, err := config.FromValues(boot, func(k string) string { return merged[k] }); err != nil {
		writeErr(w, 400, "ungueltig", strings.TrimPrefix(err.Error(), "Konfiguration: "))
		return
	}
	if err := a.DB.SaveSettings(merged); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "neustart": true})
	restartSoon()
}

// --- Persönliche Einstellungen (jeder Nutzer für sich; ohne eigenen Wert gilt der des Systems) ---

func (a *App) handlePersonalSettings(w http.ResponseWriter, r *http.Request) {
	values, err := a.effectiveSettings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"felder": settingsView(values, false, scopePersonal)})
}

// effectiveSettings: Werte des Systems, überlagert mit den eigenen des Nutzers.
func (a *App) effectiveSettings() (map[string]string, error) {
	values, err := a.System.DB.Settings()
	if err != nil {
		return nil, err
	}
	own, err := a.DB.Settings()
	if err != nil {
		return nil, err
	}
	for k, v := range own {
		if f, ok := config.FieldByKey(k); ok && f.Personal {
			values[k] = v
		}
	}
	return values, nil
}

// handlePersonalSettingsSave speichert die persönlichen Werte in der Datenbank des Nutzers und lädt nur seine App neu.
func (a *App) handlePersonalSettingsSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Werte map[string]string `json:"werte"`
	}
	if readJSON(r, &in) != nil || in.Werte == nil {
		writeErr(w, 400, "ungueltig", "Werte nicht lesbar")
		return
	}
	old, err := a.effectiveSettings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	merged := mergeSettings(old, in.Werte, scopePersonal)
	boot := config.Bootstrap{Listen: a.Cfg.Listen, DataDir: a.Cfg.DataDir, AudioDir: a.Cfg.AudioDir, BackupDir: a.Cfg.BackupDir, KeyDir: a.Cfg.KeyDir}
	if _, err := config.FromValues(boot, func(k string) string { return merged[k] }); err != nil {
		writeErr(w, 400, "ungueltig", strings.TrimPrefix(err.Error(), "Konfiguration: "))
		return
	}
	own := map[string]string{}
	for _, f := range config.Fields {
		if f.Personal {
			own[f.Key] = merged[f.Key]
		}
	}
	if err := a.DB.SaveSettings(own); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
	go a.System.reloadUser(a.UserID)
}

// --- Ersteinrichtung (Dienst ohne Einstellungen) ---

const setupCodeKey = "ersteinrichtung:code"

// SetupCode liefert den Einmal-Code des Einrichtungsassistenten; legt ihn beim ersten Aufruf an.
func SetupCode(db *store.System) (string, error) {
	code, err := db.Get(setupCodeKey)
	if err != nil || code != "" {
		return code, err
	}
	code = config.NewSecret()[:24]
	return code, db.Set(setupCodeKey, code)
}

// SetupServer bedient den Einrichtungsassistenten, solange keine gültigen Einstellungen vorliegen.
type SetupServer struct {
	DB      *store.System
	Boot    config.Bootstrap
	Version string
	LoadErr error // warum der Dienst nicht normal startet (nil = noch nie eingerichtet)

	rateMu sync.Mutex
	fails  int
}

func (s *SetupServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ersteinrichtung") })
	mux.HandleFunc("GET /api/sitzung", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"angemeldet": false, "eingerichtet": false, "ersteinrichtung": true})
	})
	mux.HandleFunc("GET /api/ersteinrichtung", s.handleGet)
	mux.HandleFunc("POST /api/ersteinrichtung/openai", s.handleCheckOpenAI)
	mux.HandleFunc("POST /api/ersteinrichtung", s.handleSave)
	mux.HandleFunc("/", staticHandler(s.lang))
	// Meldungen des Assistenten in der Sprache der Seite.
	inner := securityHeaders(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i18n.Set(s.lang(r))
		inner.ServeHTTP(w, r)
	})
}

// lang: Sprache des Assistenten – ?lang= (Umschalter), dann die der Seite (X-Sprache), dann eine
// gespeicherte Einstellung, sonst die Sprache des Browsers.
func (s *SetupServer) lang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l != "" {
		return i18n.Norm(l)
	}
	if l := r.Header.Get("X-Sprache"); l != "" {
		return i18n.Norm(l)
	}
	if v, err := s.DB.Settings(); err == nil && v["LANGUAGE"] != "" {
		return i18n.Norm(v["LANGUAGE"])
	}
	return i18n.Norm(r.Header.Get("Accept-Language"))
}

// codeOK prüft den Einmal-Code; nach 20 Fehlversuchen ist bis zum Neustart Schluss.
func (s *SetupServer) codeOK(code string) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.fails >= 20 {
		return false
	}
	want, err := s.DB.Get(setupCodeKey)
	if err != nil || want == "" || subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		s.fails++
		return false
	}
	return true
}

func (s *SetupServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if !s.codeOK(r.URL.Query().Get("code")) {
		writeErr(w, http.StatusForbidden, "code", "Der Einrichtungs-Code fehlt oder ist falsch. Er steht im Log des Dienstes (pebble ersteinrichtung).")
		return
	}
	values, err := s.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	users, _ := s.DB.Users()
	resp := map[string]any{"felder": settingsView(values, true, scopeAll), "version": s.Version, "eingerichtet": len(users) > 0}
	if s.LoadErr != nil && s.LoadErr != ErrNotConfigured {
		resp["fehler"] = strings.TrimPrefix(s.LoadErr.Error(), "Konfiguration: ")
	}
	writeJSON(w, 200, resp)
}

// handleCheckOpenAI prüft einen API-Schlüssel mit einem Abruf der Modell-Liste.
func (s *SetupServer) handleCheckOpenAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Key     string `json:"key"`
		BaseURL string `json:"base_url"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	err := checkOpenAI(r.Context(), in.BaseURL, in.Key)
	writeJSON(w, 200, map[string]any{"ok": err == nil, "fehler": errText(err)})
}

func (s *SetupServer) handleSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code  string            `json:"code"`
		Name  string            `json:"name"`
		Werte map[string]string `json:"werte"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Der Einrichtungs-Code fehlt oder ist falsch")
		return
	}
	users, err := s.DB.Users()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	name := store.Preview(strings.TrimSpace(in.Name), 40)
	if len(users) == 0 && name == "" {
		writeErr(w, 400, "name", "Bitte deinen Namen eingeben")
		return
	}
	old, err := s.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	merged := mergeSettings(old, in.Werte, scopeAll)
	if _, err := config.FromValues(s.Boot, func(k string) string { return merged[k] }); err != nil {
		writeErr(w, 400, "ungueltig", strings.TrimPrefix(err.Error(), "Konfiguration: "))
		return
	}
	if err := s.DB.SaveSettings(merged); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	// Erster Nutzer (Admin): Schlüssel, verschlüsselte Datenbank, Zugang der Pebble-App. Den Wiederherstellungscode
	// zeigt die Web-App nach dem ersten Passkey (Willkommen).
	uid := int64(1)
	if len(users) == 0 {
		if _, _, err := createUser(s.DB, s.Boot.DataDir, s.Boot.KeyDir, 1, name, store.RoleAdmin, "", nil); err != nil {
			logf("Ersteinrichtung: Nutzer nicht angelegt: %v", err)
			writeErr(w, 500, "speicher", "Zugang nicht angelegt: "+err.Error())
			return
		}
	} else {
		uid = users[0].ID
	}
	s.DB.Set(setupCodeKey, "")
	resp := map[string]any{"ok": true, "adresse": merged["PUBLIC_URL"]}
	// Ohne Passkey geht es direkt zur Passkey-Einrichtung (Einmal-Link, nach dem Neustart unter der öffentlichen Adresse).
	if any, _ := s.DB.AnyPasskey(); !any {
		if token, err := s.DB.NewEnrollToken(store.Enroll{UserID: uid, Purpose: store.EnrollDevice}, enrollTTL); err == nil {
			resp["weiter"] = merged["PUBLIC_URL"] + "/einrichten#" + token
		}
	}
	writeJSON(w, 200, resp)
	restartSoon()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// checkOpenAI ruft GET /models mit dem Schlüssel ab.
func checkOpenAI(ctx context.Context, base, key string) error {
	if key == "" {
		return i18n.Errorf("kein Schlüssel")
	}
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return i18n.Errorf("OpenAI antwortet %d", resp.StatusCode)
	}
	return nil
}
