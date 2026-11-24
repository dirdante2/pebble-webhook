package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/go-webauthn/webauthn/webauthn"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Mehrere Nutzer: System hält, was alle teilen (system.db, Einstellungen, Anmeldung, Updates, Backups); je Nutzer
// gibt es eine App mit eigener, verschlüsselter Datenbank users/<id>/daten.db, eigenem Worker, eigenen Live-Meldungen
// und Medien unter MEDIA_DIR/<id>/ (audio/, bilder/). Handler der Web-App laufen immer auf der App des angemeldeten Nutzers –
// fremde Daten erreicht keine Route, auch nicht als Admin.

// System: geteilter Teil des Dienstes.
type System struct {
	Cfg     *config.Config
	DB      *store.System
	AI      AI
	Version string
	// NewNotes baut das Joplin-Ziel eines Nutzers aus seinen Einstellungen (nil = Joplin aus).
	NewNotes func(*config.Config) Notes

	rateMu sync.Mutex
	rate   map[string][]int64

	wa        *webauthn.WebAuthn
	cerMu     sync.Mutex
	cer       map[string]ceremony
	vapidPub  string
	vapidPriv string

	updates updateCache
	devCode deviceCode  // Kurz-Code für ein weiteres Gerät (gehört dem Nutzer, der ihn erzeugt hat)
	oauth   oauthServer // OAuth für den MCP-Server (Einmal-Codes, Zugriff auf kv „oauth“ in system.db)

	appsMu sync.RWMutex
	apps   map[int64]*App
	locked map[int64]string // Nutzer, deren Daten nicht geöffnet sind → Grund (gesperrt, Schlüssel fehlt …)
	runCtx context.Context  // Lebensdauer der Worker (für später geöffnete Nutzer)
}

// App: ein Nutzer – seine Datenbank, sein Worker, seine Live-Meldungen.
type App struct {
	*System
	UserID int64
	Cfg    *config.Config // Einstellungen des Systems, überlagert mit den persönlichen des Nutzers
	DB     *store.Store
	Notes  Notes

	audioKey []byte // Schlüssel der Aufnahmen (nil = Klartext, nur in Tests)

	wake chan struct{}
	hub  *hub

	joplinMu sync.Mutex

	runMu   sync.Mutex // Auftrag, an dem der Worker gerade arbeitet (zum Abbrechen beim Löschen)
	runID   string
	runStop context.CancelFunc
	runDone chan struct{}
	stop    context.CancelFunc // beendet den Worker (Sperren, Löschen)
	stopped chan struct{}

	trainNow  atomic.Bool    // Lernlauf von Hand angefordert
	mediaBusy atomic.Bool    // Trailer werden gerade nachgetragen
	sortFails map[string]int // Planung → gescheiterte Versuche beim Einsortieren (nur im Worker)
	joplinErr string
	joplinAt  int64
}

// Gründe, warum die Daten eines Nutzers nicht geöffnet sind.
const (
	lockedByAdmin = "gesperrt"
	lockedNoKey   = "schluessel"
	lockedError   = "fehler"
)

// NewSystem baut den geteilten Teil; die Nutzer öffnet OpenUsers.
func NewSystem(cfg *config.Config, db *store.System, ai AI, version string, notes func(*config.Config) Notes) (*System, error) {
	i18n.Set(cfg.Language)
	if notes == nil {
		notes = func(*config.Config) Notes { return nil }
	}
	s := &System{Cfg: cfg, DB: db, AI: ai, Version: version, NewNotes: notes, rate: map[string][]int64{},
		cer: map[string]ceremony{}, apps: map[int64]*App{}, locked: map[int64]string{}}
	if err := s.initAuth(); err != nil {
		return nil, err
	}
	if err := s.initPush(); err != nil {
		return nil, err
	}
	return s, nil
}

// UserDir: Ordner eines Nutzers unter DATA_DIR (daten.db).
func UserDir(dataDir string, uid int64) string {
	return filepath.Join(dataDir, "users", strconv.FormatInt(uid, 10))
}

// UserDB: Datenbank eines Nutzers.
func UserDB(dataDir string, uid int64) string {
	return filepath.Join(UserDir(dataDir, uid), "daten.db")
}

// OpenUsers öffnet die Daten aller Nutzer. Wer nicht zu öffnen ist (gesperrt, Schlüssel fehlt, Datei kaputt),
// bleibt gesperrt – die anderen laufen weiter.
func (s *System) OpenUsers() error {
	users, err := s.DB.Users()
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err := s.openUser(u); err != nil {
			logf("Nutzer %d: Daten nicht geöffnet: %v", u.ID, err)
		}
	}
	return nil
}

// openUser öffnet Schlüssel und Datenbank eines Nutzers und meldet seine App an (Worker startet, wenn Run läuft).
func (s *System) openUser(u *store.User) (*App, error) {
	if u.LockedAt > 0 {
		s.setLocked(u.ID, lockedByAdmin)
		return nil, errors.New("vom Admin gesperrt")
	}
	dek, err := schluessel.Read(s.Cfg.KeyDir, u.ID, u.KeyID)
	if err != nil {
		reason := lockedError
		if errors.Is(err, schluessel.ErrMissing) || errors.Is(err, schluessel.ErrWrongKey) {
			reason = lockedNoKey
		}
		s.setLocked(u.ID, reason)
		return nil, err
	}
	db, err := store.OpenUser(UserDB(s.Cfg.DataDir, u.ID), dek.DBKey(), u.ID)
	if err != nil {
		s.setLocked(u.ID, lockedError)
		return nil, err
	}
	a, err := newApp(s, u.ID, db, dek.AudioKey())
	if err != nil {
		db.Close()
		s.setLocked(u.ID, lockedError)
		return nil, err
	}
	s.appsMu.Lock()
	s.apps[u.ID] = a
	delete(s.locked, u.ID)
	ctx := s.runCtx
	s.appsMu.Unlock()
	if ctx != nil {
		s.startApp(ctx, a)
	}
	return a, nil
}

func (s *System) setLocked(uid int64, reason string) {
	s.appsMu.Lock()
	s.locked[uid] = reason
	s.appsMu.Unlock()
}

// newApp baut die App eines Nutzers zu einer geöffneten Datenbank.
func newApp(s *System, uid int64, db *store.Store, audioKey []byte) (*App, error) {
	cfg, err := s.userConfig(db)
	if err != nil {
		return nil, err
	}
	a := &App{System: s, UserID: uid, Cfg: cfg, DB: db, Notes: s.NewNotes(cfg), audioKey: audioKey,
		wake: make(chan struct{}, 1), hub: newHub(), sortFails: map[string]int{}}
	if err := a.normalizeStoredTags(); err != nil {
		return nil, err
	}
	if uid == 1 {
		a.importMemoryFile() // Altbestand (einzelner Nutzer)
	}
	return a, nil
}

// userConfig: Einstellungen des Systems, überlagert mit den persönlichen Werten des Nutzers.
func (s *System) userConfig(db *store.Store) (*config.Config, error) {
	own, err := db.Settings()
	if err != nil {
		return nil, err
	}
	if len(own) == 0 {
		return s.Cfg, nil // nichts Eigenes gesetzt: die Einstellungen des Systems
	}
	sys, err := s.DB.Settings()
	if err != nil {
		return nil, err
	}
	boot := config.Bootstrap{Listen: s.Cfg.Listen, DataDir: s.Cfg.DataDir, MediaDir: s.Cfg.MediaDir, BackupDir: s.Cfg.BackupDir, KeyDir: s.Cfg.KeyDir}
	return config.FromValues(boot, func(k string) string {
		if f, ok := config.FieldByKey(k); ok && f.Personal {
			if v, set := own[k]; set {
				return v
			}
		}
		return sys[k]
	})
}

// app liefert die App eines Nutzers; ok=false mit Grund, wenn seine Daten nicht geöffnet sind.
func (s *System) app(uid int64) (a *App, reason string) {
	s.appsMu.RLock()
	defer s.appsMu.RUnlock()
	if a = s.apps[uid]; a != nil {
		return a, ""
	}
	if r := s.locked[uid]; r != "" {
		return nil, r
	}
	return nil, lockedError
}

// Apps: alle geöffneten Nutzer, nach Id.
func (s *System) Apps() []*App {
	s.appsMu.RLock()
	out := make([]*App, 0, len(s.apps))
	for _, a := range s.apps {
		out = append(out, a)
	}
	s.appsMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

// Run startet die Hintergrundläufe des Systems (Updates, Backups) und die Worker aller Nutzer; endet mit ctx.
func (s *System) Run(ctx context.Context) {
	s.appsMu.Lock()
	s.runCtx = ctx
	apps := make([]*App, 0, len(s.apps))
	for _, a := range s.apps {
		apps = append(apps, a)
	}
	s.appsMu.Unlock()
	go s.CheckUpdates(ctx)
	go s.AutoUpdate(ctx)
	go s.BackupSchedule(ctx)
	for _, a := range apps {
		s.startApp(ctx, a)
	}
	<-ctx.Done()
	for _, a := range s.Apps() {
		if a.stopped != nil {
			<-a.stopped
		}
	}
}

func (s *System) startApp(ctx context.Context, a *App) {
	wctx, stop := context.WithCancel(ctx)
	a.stop, a.stopped = stop, make(chan struct{})
	go func() {
		defer close(a.stopped)
		a.Run(wctx)
	}()
}

// closeUser beendet Worker und Datenbank eines Nutzers (Sperren, Löschen, Wiederherstellen).
func (s *System) closeUser(uid int64, reason string) {
	s.appsMu.Lock()
	a := s.apps[uid]
	delete(s.apps, uid)
	if reason != "" {
		s.locked[uid] = reason
	} else {
		delete(s.locked, uid)
	}
	s.appsMu.Unlock()
	if a == nil {
		return
	}
	if a.stop != nil {
		a.stop()
		<-a.stopped
	}
	a.DB.Close()
}

// Close schließt alle Nutzer-Datenbanken (nach dem Ende von Run).
func (s *System) Close() {
	for _, a := range s.Apps() {
		a.DB.Close()
	}
}

// pebbleAccess: Zugangsdaten der Pebble-App eines Nutzers (Klartext nur in seiner verschlüsselten Datenbank;
// in system.db steht nur der Hash des Tokens zur Zuordnung).
type pebbleAccess struct {
	Token  string `json:"token"`
	Secret string `json:"secret"`
}

const pebbleAccessKey = "pebble_zugang"

func (a *App) pebbleAccess() (pebbleAccess, error) {
	var p pebbleAccess
	v, err := a.DB.Get(pebbleAccessKey)
	if err != nil || v == "" {
		return p, err
	}
	return p, json.Unmarshal([]byte(v), &p)
}

// NewUser legt einen Nutzer vollständig an: Schlüssel (Datei + mit Code eingewickelt), verschlüsselte Datenbank,
// Zugang der Pebble-App. Liefert die geöffnete App und den Wiederherstellungscode (nur jetzt im Klartext).
func (s *System) NewUser(name, role string) (*App, string, error) {
	return createUser(s.DB, s.Cfg.DataDir, s.Cfg.KeyDir, 0, name, role, "", func(u *store.User) (*App, error) { return s.openUser(u) })
}

// createUser: gemeinsamer Weg für Assistent, Einladung und Umzug. id 0 = nächste freie; webauthnID leer = neu.
// open öffnet den fertigen Nutzer (nil = nicht öffnen, z. B. im Einrichtungsassistenten vor dem Neustart).
func createUser(sys *store.System, dataDir, keyDir string, id int64, name, role, webauthnID string,
	open func(*store.User) (*App, error)) (*App, string, error) {
	dek, err := schluessel.New()
	if err != nil {
		return nil, "", err
	}
	code, err := schluessel.NewCode()
	if err != nil {
		return nil, "", err
	}
	if webauthnID == "" {
		webauthnID = base64.RawURLEncoding.EncodeToString([]byte(store.NewID() + store.NewID()))
	}
	u := &store.User{ID: id, Name: name, Role: role, WebAuthnID: webauthnID, KeyID: dek.ID()}
	if _, err := sys.AddUser(u); err != nil {
		return nil, "", err
	}
	fail := func(err error) (*App, string, error) {
		sys.DeleteUser(u.ID)
		os.Remove(schluessel.Path(keyDir, u.ID))
		os.RemoveAll(UserDir(dataDir, u.ID))
		return nil, "", err
	}
	wrap, err := schluessel.Wrap(code, u.ID, dek)
	if err != nil {
		return fail(err)
	}
	if err := sys.SetUserKey(u.ID, dek.ID(), wrap); err != nil {
		return fail(err)
	}
	u.KeyWrap = wrap
	if err := schluessel.Write(keyDir, u.ID, dek); err != nil {
		return fail(fmt.Errorf("Schlüsseldatei: %w", err))
	}
	if err := os.MkdirAll(UserDir(dataDir, u.ID), 0o750); err != nil {
		return fail(err)
	}
	db, err := store.OpenUser(UserDB(dataDir, u.ID), dek.DBKey(), u.ID)
	if err != nil {
		return fail(err)
	}
	access := pebbleAccess{Token: config.NewSecret(), Secret: config.NewSecret()}
	b, _ := json.Marshal(access)
	err = db.Set(pebbleAccessKey, string(b))
	if err == nil {
		err = db.Set(recoveryPendingKey, code)
	}
	db.Close()
	if err != nil {
		return fail(err)
	}
	if err := sys.AddToken(store.UserToken{UserID: u.ID, Kind: store.TokenHook, Label: "Pebble-App"}, store.Hash(access.Token)); err != nil {
		return fail(err)
	}
	if open == nil {
		return nil, code, nil
	}
	a, err := open(u)
	if err != nil {
		return fail(err)
	}
	return a, code, nil
}

// recoveryPendingKey: Wiederherstellungscode, bis der Nutzer ihn als notiert bestätigt (nur in seiner verschlüsselten DB).
const recoveryPendingKey = "wiederherstellung_offen"

// AppOf: geöffnete App eines Nutzers (nil = gesperrt oder unbekannt) – für Befehle der Kommandozeile.
func (s *System) AppOf(uid int64) *App {
	a, _ := s.app(uid)
	return a
}

// RecoverKey schreibt die Schlüsseldatei eines Nutzers aus seinem Wiederherstellungscode neu.
func RecoverKey(db *store.System, keyDir string, uid int64, code string) error {
	u, err := db.User(uid)
	if err != nil {
		return fmt.Errorf("Nutzer %d: %w", uid, err)
	}
	dek, err := schluessel.Unwrap(code, u.KeyWrap, uid)
	if err != nil {
		return err
	}
	if dek.ID() != u.KeyID {
		return schluessel.ErrBadCode
	}
	return schluessel.Write(keyDir, uid, dek)
}

// isAdmin: der Nutzer dieser App ist Administrator (liest system.db – Rollen ändern sich ohne Neustart).
func (a *App) isAdmin() bool {
	u, err := a.System.DB.User(a.UserID)
	return err == nil && u.Admin()
}

// Texte in der Sprache des Nutzers (Meldungen, Push, KI-Anweisungen); das System spricht seine eigene.
func (a *App) T(s string) string                    { return i18n.In(a.Cfg.Language, s) }
func (a *App) Tf(format string, v ...any) string    { return fmt.Sprintf(a.T(format), v...) }
func (a *App) Errorf(format string, v ...any) error { return fmt.Errorf(a.T(format), v...) }

// reloadUser öffnet die Daten eines Nutzers neu (neue persönliche Einstellungen gelten sofort, ohne Neustart des Dienstes).
func (s *System) reloadUser(uid int64) {
	u, err := s.DB.User(uid)
	if err != nil {
		return
	}
	s.closeUser(uid, "")
	if _, err := s.openUser(u); err != nil {
		logf("Nutzer %d: nach neuen Einstellungen nicht geöffnet: %v", uid, err)
	}
}
