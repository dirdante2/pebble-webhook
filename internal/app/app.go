// Package app enthält den Dienst: Webhook-Empfang, Verarbeitung, Web-App-API.
package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/joplin"
	"pebble-webhook/internal/openai"
	"pebble-webhook/internal/store"
)

// AI ist die Schnittstelle zu OpenAI (in Tests ersetzt).
type AI interface {
	Transcribe(ctx context.Context, model, path, mime, language string) (text string, seconds float64, err error)
	Respond(ctx context.Context, r openai.Request) (*openai.Result, error)
}

// Notes ist das Einweg-Ziel Joplin (in Tests ersetzt); nil = Joplin aus.
type Notes interface {
	EnsureFolder(id, title, parentID string) error
	RenameFolder(id, title, parentID string) error
	PutNote(n joplin.Note) error
	Delete(id string) error
	EnsureTag(id, title string) error
	PutNoteTag(id, noteID, tagID string) error
	Ping() error
}

type App struct {
	Cfg     *config.Config
	DB      *store.Store
	AI      AI
	Notes   Notes
	Version string

	wake chan struct{}
	hub  *hub

	rateMu sync.Mutex
	rate   map[string][]int64

	wa        *webauthn.WebAuthn
	cerMu     sync.Mutex
	cer       map[string]ceremony
	vapidPub  string
	vapidPriv string

	joplinMu sync.Mutex

	runMu   sync.Mutex // Auftrag, an dem der Worker gerade arbeitet (zum Abbrechen beim Löschen)
	runID   string
	runStop context.CancelFunc
	runDone chan struct{}

	trainNow  atomic.Bool    // Lernlauf von Hand angefordert
	sortFails map[string]int // Planung → gescheiterte Versuche beim Einsortieren (nur im Worker)
	joplinErr string
	joplinAt  int64

	updates updateCache
	devCode deviceCode  // Kurz-Code für ein weiteres Gerät
	oauth   oauthServer // OAuth für den MCP-Server (Einmal-Codes, Zugriff auf kv „oauth“)
}

func New(cfg *config.Config, db *store.Store, ai AI, notes Notes) (*App, error) {
	a := &App{Cfg: cfg, DB: db, AI: ai, Notes: notes, wake: make(chan struct{}, 1), hub: newHub(),
		rate: map[string][]int64{}, cer: map[string]ceremony{}, sortFails: map[string]int{}}
	if err := a.initAuth(); err != nil {
		return nil, err
	}
	if err := a.initPush(); err != nil {
		return nil, err
	}
	if err := a.normalizeStoredTags(); err != nil {
		return nil, err
	}
	a.importMemoryFile()
	return a, nil
}

// Wake stößt den Worker an.
func (a *App) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// clientIP liefert die Adresse des Aufrufers; hinter dem eingetragenen Proxy dessen X-Real-IP.
func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a.Cfg.TrustedProxy != "" && host == a.Cfg.TrustedProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	return host
}

// allow begrenzt Anfragen je Adresse und Minute.
func (a *App) allow(key string, perMin int) bool {
	if perMin <= 0 {
		return true
	}
	now := time.Now().UnixMilli()
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	hits := a.rate[key][:0]
	for _, t := range a.rate[key] {
		if now-t < 60_000 {
			hits = append(hits, t)
		}
	}
	if len(hits) >= perMin {
		a.rate[key] = hits
		return false
	}
	a.rate[key] = append(hits, now)
	if len(a.rate) > 10_000 {
		a.rate = map[string][]int64{}
	}
	return true
}

// freeBytes liefert den freien Platz des Dateisystems unter path.
func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func logf(format string, args ...any) { log.Printf(format, args...) }
