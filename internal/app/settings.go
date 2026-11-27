package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
	"pebble-webhook/internal/whisper"
)

// Einstellungen: alle Werte außer den Start-Werten liegen in der Datenbank (kv „einstellung:…“) und werden
// über die Web-Oberfläche gepflegt – beim ersten Start im Einrichtungsassistenten, danach unter Status →
// Einstellungen. Nach dem Speichern beendet sich der Dienst; systemd startet ihn mit den neuen Werten neu.

// ErrNotConfigured: es gibt noch keine Einstellungen (Ersteinrichtung nötig).
var ErrNotConfigured = fmt.Errorf("noch nicht eingerichtet")

// LoadSettings liest die Einstellungen aus der Datenbank. Gibt es noch keine, übernimmt es einmalig die Werte
// einer alten env-Datei (Umgebung), sofern dort eine öffentliche Adresse steht.
func LoadSettings(db *store.System, boot config.Bootstrap) (*config.Config, error) {
	unsealSystemSecrets(db, boot.KeyDir)
	values, err := db.Settings()
	if err != nil {
		return nil, err
	}
	for k, v := range values {
		if schluessel.Sealed(v) {
			delete(values, k) // noch verschlüsselt (Systemschlüssel fehlt): gilt als nicht gesetzt
		}
	}
	if values["PUBLIC_URL"] == "" {
		imported := map[string]string{}
		for _, f := range config.Fields {
			if _, locked := config.EnvLocked(f.Key); locked {
				continue // gilt ohnehin aus der Umgebung, nicht in die Datenbank kopieren
			}
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

// restartSoon beendet den Dienst kurz nach der Antwort; systemd (Restart=always) bzw. Docker (restart: unless-stopped)
// startet ihn neu.
func restartSoon() { restartSoonFor("Einstellungen gespeichert") }

func restartSoonFor(reason string) {
	go func() {
		time.Sleep(800 * time.Millisecond)
		log.Printf("%s: Neustart", reason)
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
	if f.Hidden && !(f.Setup && scope == scopeAll) {
		return false
	}
	return scope == scopeAll || (scope == scopePersonal) == f.Personal
}

// envLockedSet: Wert aus der Umgebung (gesperrt, nur zu sehen) eintragen.
func envLockedSet(key string, m map[string]any) bool {
	v, locked := config.EnvLocked(key)
	if locked {
		m["wert"], m["gesetzt"], m["gesperrt"] = v, true, true
	}
	return locked
}

// settingsView liefert die Felder mit den gespeicherten Werten; Geheimnisse nur als „gesetzt“.
func settingsView(values map[string]string, generate bool, scope int) []map[string]any {
	out := make([]map[string]any, 0, len(config.Fields))
	for _, f := range config.Fields {
		if !inScope(f, scope) || dockerHidden(f.Key) {
			continue
		}
		v, set := values[f.Key]
		m := map[string]any{"key": f.Key, "gruppe": f.Group, "name": f.Label, "hilfe": f.Help, "art": f.Kind,
			"vorgabe": f.Default, "pflicht": f.Required, "auswahl": f.Choices, "persoenlich": f.Personal}
		if f.Labels != nil {
			m["bezeichnungen"] = f.Labels
		}
		switch {
		case f.Kind != "secret" && envLockedSet(f.Key, m):
		case f.Kind == "secret":
			if _, locked := config.EnvLocked(f.Key); locked {
				m["gesetzt"], m["gesperrt"], m["wert"] = true, true, ""
				break
			}
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

// dockerHidden: Felder ohne Wirkung im Container (kein Updater) – im Assistenten und in den Einstellungen nicht zeigen.
func dockerHidden(key string) bool {
	return inDocker() && (key == "AUTO_UPDATE" || key == "AUTO_UPDATE_TIME")
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
		if _, locked := config.EnvLocked(f.Key); !ok || locked {
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
	writeJSON(w, 200, map[string]any{"felder": settingsView(values, false, scopeSystem), "whisper": whisperInfo(a.Whisper)})
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
	boot := config.Bootstrap{Listen: a.Cfg.Listen, DataDir: a.Cfg.DataDir, MediaDir: a.Cfg.MediaDir, BackupDir: a.Cfg.BackupDir, KeyDir: a.Cfg.KeyDir}
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
	for _, f := range config.Fields {
		if f.Private() {
			delete(values, f.Key) // Zugangsdaten nie vom System anzeigen oder übernehmen
		}
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
	boot := config.Bootstrap{Listen: a.Cfg.Listen, DataDir: a.Cfg.DataDir, MediaDir: a.Cfg.MediaDir, BackupDir: a.Cfg.BackupDir, KeyDir: a.Cfg.KeyDir}
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
	return NewSetupCode(db)
}

// NewSetupCode ersetzt den Code des Assistenten; ein früher ausgegebener gilt nicht mehr.
func NewSetupCode(db *store.System) (string, error) {
	code := config.NewSecret()[:24]
	return code, db.Set(setupCodeKey, code)
}

// SetupServer bedient den Einrichtungsassistenten, solange keine gültigen Einstellungen vorliegen.
type SetupServer struct {
	DB      *store.System
	Boot    config.Bootstrap
	Version string
	LoadErr error // warum der Dienst nicht normal startet (nil = noch nie eingerichtet)

	win *setupWindow // „Einrichten“ in den ersten 5 Minuten nach dem Start

	rateMu sync.Mutex
	fails  map[string]int // Fehlversuche je Adresse (so sperrt ein Fremder den Betreiber nicht aus)
	total  int            // alle Fehlversuche – Obergrenze gegen Raten über viele Adressen
}

func (s *SetupServer) Handler() http.Handler {
	if s.win == nil {
		s.win = newSetupWindow()
		// Nur eine nie eingerichtete Installation öffnet „Einrichten“; sind die Einstellungen einer benutzten
		// ungültig, gilt allein der Code aus dem Log.
		users, err := s.DB.Users()
		s.win.locked = err != nil || len(users) > 0 || (s.LoadErr != nil && !errors.Is(s.LoadErr, ErrNotConfigured))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ersteinrichtung") })
	mux.HandleFunc("GET /api/sitzung", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"angemeldet": false, "eingerichtet": false, "ersteinrichtung": true, "einrichtung": s.win.state(), "einrichtung_rest": s.win.rest()})
	})
	mux.HandleFunc("POST /api/ersteinrichtung/start", s.handleSetupStart)
	mux.HandleFunc("GET /api/ersteinrichtung", s.handleGet)
	mux.HandleFunc("POST /api/ersteinrichtung/openai", s.handleCheckOpenAI)
	mux.HandleFunc("POST /api/ersteinrichtung/verbinden/openrouter/start", s.handleSetupConnectStart)
	mux.HandleFunc("POST /api/ersteinrichtung/verbinden/openrouter/fertig", s.handleSetupConnectDone)
	mux.HandleFunc("POST /api/ki/verbinden/rueckgabe", handleKIConnectReturn)
	mux.HandleFunc("POST /api/ki/verbinden/abholen", handleKIConnectPoll)
	mux.HandleFunc("POST /api/ersteinrichtung", s.handleSave)
	mux.HandleFunc("POST /api/ersteinrichtung/backup", s.handleBackupUpload)
	mux.HandleFunc("POST /api/ersteinrichtung/backup/wiederherstellen", s.handleBackupRestore)
	shell := staticHandler(s.lang)
	mux.HandleFunc("GET "+pathConnectOpenRouter, serveShell(shell))
	mux.HandleFunc("/", shell)
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

// codeOK prüft den Einmal-Code; nach 20 Fehlversuchen einer Adresse (oder 1000 insgesamt) ist bis zum Neustart Schluss.
func (s *SetupServer) codeOK(r *http.Request, code string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	if s.fails == nil {
		s.fails = map[string]int{}
	}
	if s.fails[host] >= 20 || s.total >= 1000 {
		return false
	}
	want, err := s.DB.Get(setupCodeKey)
	if err != nil || want == "" || subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		s.fails[host]++
		s.total++
		return false
	}
	return true
}

func (s *SetupServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if !s.codeOK(r, r.URL.Query().Get("code")) {
		writeErr(w, http.StatusForbidden, "code", "Der Einrichtungs-Code fehlt oder ist falsch. Auf der Startseite „Einrichten“ wählen (in den ersten 5 Minuten nach dem Start).")
		return
	}
	values, err := s.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	users, _ := s.DB.Users()
	resp := map[string]any{"felder": settingsView(values, true, scopeAll), "version": s.Version, "eingerichtet": len(users) > 0,
		"whisper": whisperInfo(whisper.New(s.Boot.DataDir)), "arten": config.ProviderTypes, "docker": inDocker()}
	if s.LoadErr != nil && s.LoadErr != ErrNotConfigured {
		resp["fehler"] = strings.TrimPrefix(s.LoadErr.Error(), "Konfiguration: ")
	}
	writeJSON(w, 200, resp)
}

// handleCheckOpenAI prüft einen Anbieter: KI-Anbieter mit einem Abruf der Modell-Liste (die Modelle gehen an die
// Auswahl im Assistenten), SearXNG mit einer Probesuche.
func (s *SetupServer) handleCheckOpenAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Key     string `json:"key"`
		BaseURL string `json:"base_url"`
		Art     string `json:"art"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(r, in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	// Der in der Umgebung gesetzte Schlüssel geht nur an die Adresse aus der Umgebung bzw. an OpenAI selbst.
	if v, locked := config.EnvLocked("OPENAI_API_KEY"); locked && in.Key == "" && (in.Art == "" || in.Art == "openai") {
		base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		envBase := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/")
		if base != "" && base != envBase && base != "https://api.openai.com/v1" {
			writeJSON(w, 200, map[string]any{"ok": false, "fehler": i18n.T("Für eine andere API-Adresse den Schlüssel eingeben")})
			return
		}
		in.Key = v
	}
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if t := config.ProviderTypeByID(in.Art); t != nil && (base == "" || t.FixedURL) && t.URL != "" {
		base = t.URL
	}
	if in.Art == "searxng" {
		err := checkSearx(r.Context(), base)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "fehler": errText(err)})
		return
	}
	if in.Art == "" {
		err := checkOpenAI(r.Context(), base, in.Key)
		writeJSON(w, 200, map[string]any{"ok": err == nil, "fehler": errText(err)})
		return
	}
	if base == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "fehler": i18n.T("Adresse fehlt")})
		return
	}
	models, err := listModels(r.Context(), base, in.Key)
	writeJSON(w, 200, map[string]any{"ok": err == nil, "fehler": errText(err), "modelle": models})
}

func (s *SetupServer) handleSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string                      `json:"code"`
		Name     string                      `json:"name"`
		Werte    map[string]string           `json:"werte"`
		Zugaenge []config.AIAccess           `json:"zugaenge"` // nil = nicht im Assistenten gewählt (Vorgaben)
		Bereiche map[string][]config.AIEntry `json:"bereiche"`
		// Systemschlüssel: „nutzer“ = an den Code des ersten Admins gebunden (ein Admin), „code“ = eigener Code
		// (mehrere Admins). Leer = Vorgabe (nutzer).
		Systemschluessel string `json:"systemschluessel"`
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(r, in.Code) {
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
	if in.Zugaenge != nil && in.Bereiche != nil && len(users) == 0 { // eingerichtete Installation: KI bleibt, wie sie ist
		// OpenAI-Schlüssel aus der Umgebung (Docker): gilt für OpenAI-Anbieter ohne eigenen Schlüssel.
		if v, locked := config.EnvLocked("OPENAI_API_KEY"); locked {
			for i := range in.Zugaenge {
				if in.Zugaenge[i].Kind == "openai" && in.Zugaenge[i].Key == "" {
					in.Zugaenge[i].Key = v
				}
			}
		}
		acc, _ := json.Marshal(in.Zugaenge)
		areas, _ := json.Marshal(in.Bereiche)
		merged["KI_ZUGAENGE"], merged["KI_BEREICHE"] = string(acc), string(areas)
	} else if len(users) == 0 {
		setupDefaults(merged)
	}
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
	if len(users) == 0 && in.Systemschluessel == "code" {
		s.DB.Set(systemKeyChoiceKV, "code")
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
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if key == "" && strings.HasPrefix(base, "https://api.openai.com/") { // lokale Dienste (Ollama) laufen ohne
		return i18n.Errorf("kein Schlüssel")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
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

// setupDefaults: Vorgaben einer neuen Installation (Einrichtungs-Assistent, erster Nutzer). Transkription lokal mit
// Whisper, damit keine Aufnahme den Server verlässt. Bestehende Installationen ohne gespeicherte KI-Bereiche behalten
// die bisherige Vorgabe (Cloud); änderbar unter Status → KI.
func setupDefaults(merged map[string]string) {
	if merged["KI_BEREICHE"] != "" || merged["TRANSCRIPTION"] != "" {
		return
	}
	if _, locked := config.EnvLocked("TRANSCRIPTION"); !locked {
		merged["TRANSCRIPTION"] = "lokal"
	}
}
