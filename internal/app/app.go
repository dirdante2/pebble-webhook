// Package app enthält den Dienst: Webhook-Empfang, Verarbeitung, Web-App-API.
package app

import (
	"context"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/joplin"
	"pebble-webhook/internal/openai"
)

// AI ist die Schnittstelle zu OpenAI (in Tests ersetzt).
type AI interface {
	TranscribeStep(ctx context.Context, id, name string, audio []byte, mime, language string) (text string, seconds float64, cost *float64, err error)
	Respond(ctx context.Context, r openai.Request) (*openai.Result, error)
	First(area string) *openai.Step
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

// Wake stößt den Worker an.
func (a *App) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// clientIP liefert die Adresse des Aufrufers; hinter dem eingetragenen Proxy dessen X-Real-IP.
func (a *System) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a.Cfg.TrustedProxy != "" && host == a.Cfg.TrustedProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			host = ip
		}
	}
	// IPv6: ein Anschluss hat meist ein ganzes /64 – als eine Adresse zählen, sonst umgeht man jedes Limit.
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return host
}

// allow begrenzt Anfragen je Adresse und Minute.
func (a *System) allow(key string, perMin int) bool {
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
		// Abgelaufene Zähler entfernen; laufende bleiben (früher wurde alles geleert – ein Weg, Limits zurückzusetzen).
		for k, ts := range a.rate {
			if len(ts) == 0 || now-ts[len(ts)-1] >= 60_000 {
				delete(a.rate, k)
			}
		}
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

// diskSpace: freier und gesamter Platz des Dateisystems unter path.
func diskSpace(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}

// dirSize: Summe aller Dateien unter path.
func dirSize(path string) (n int64) {
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

func logf(format string, args ...any) { log.Printf(format, args...) }

// respond ruft die KI auf; bei Englisch bekommt jede Anweisung die Sprachregel dazu.
func (a *App) respond(ctx context.Context, r openai.Request) (*openai.Result, error) {
	if rule := i18n.LanguageRuleIn(a.Cfg.Language); rule != "" {
		r.Instructions = strings.ReplaceAll(r.Instructions, "Antworte auf Deutsch", "Antworte auf Englisch") + "\n\n" + rule
	}
	return a.AI.Respond(ctx, r)
}
