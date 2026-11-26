package whisper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"pebble-webhook/internal/i18n"
)

// Manager: Modelle laden und löschen, Aufnahmen umwandeln. Einer je Dienst (für alle Nutzer).
type Manager struct {
	Dir string // DATA_DIR/modelle/whisper

	mu       sync.Mutex
	loading  map[string]*progress
	lastErr  string // letzter Fehler der lokalen Umwandlung (Anzeige im Status)
	lastAt   int64
	measured map[string]measure

	run  chan struct{}   // eine Umwandlung zur Zeit (und Start/Stopp des Servers): jede belegt alle Kerne
	ctx  context.Context // Lebensdauer der Downloads (aus Ensure), für neue Versuche nach einem Fehler
	srv  *server         // Modell im RAM (whisper-server); nil = keins
	keep time.Duration   // so lange nach der letzten Aufnahme im RAM halten; 0 = dauerhaft
}

type progress struct {
	Done, Total int64
	Err         string
}

// measure: gleitender Mittelwert der Rechenzeit je Sekunde Aufnahme.
type measure struct {
	Factor float64 `json:"faktor"`
	Runs   int     `json:"laeufe"`
}

const measureFile = "messung.json"

// New: Manager für DATA_DIR. Legt nichts an, bis ein Modell geladen wird.
func New(dataDir string) *Manager {
	m := &Manager{Dir: filepath.Join(dataDir, "modelle", "whisper"), loading: map[string]*progress{},
		measured: map[string]measure{}, run: make(chan struct{}, 1)}
	if b, err := os.ReadFile(filepath.Join(m.Dir, measureFile)); err == nil {
		json.Unmarshal(b, &m.measured)
	}
	return m
}

func (m *Manager) path(md Model) string { return filepath.Join(m.Dir, md.File) }

// Loaded: Modell liegt vollständig vor.
func (m *Manager) Loaded(id string) bool {
	md, ok := ByID(id)
	if !ok {
		return false
	}
	st, err := os.Stat(m.path(md))
	return err == nil && st.Size() == md.Bytes
}

// Loading: Modell wird gerade heruntergeladen (ohne Fehler).
func (m *Manager) Loading(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.loading[id]
	return p != nil && p.Err == ""
}

// ModelState: Zustand eines Modells für die Oberfläche.
type ModelState struct {
	Model
	Loaded   bool    `json:"geladen"`
	Loading  bool    `json:"laedt"`
	Percent  int     `json:"prozent"`
	Err      string  `json:"fehler,omitempty"`
	Measured float64 `json:"faktor_gemessen,omitempty"`
	Runs     int     `json:"laeufe,omitempty"`
}

// States: alle Modelle mit Zustand.
func (m *Manager) States() []ModelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ModelState, 0, len(Models))
	for _, md := range Models {
		s := ModelState{Model: md, Loaded: m.Loaded(md.ID)}
		if p := m.loading[md.ID]; p != nil {
			s.Err = p.Err
			s.Loading = p.Err == ""
			if p.Total > 0 {
				s.Percent = int(p.Done * 100 / p.Total)
			}
		}
		if ms, ok := m.measured[md.ID]; ok {
			s.Measured, s.Runs = ms.Factor, ms.Runs
		}
		out = append(out, s)
	}
	return out
}

// LastError: letzter Fehler der lokalen Umwandlung (Text, Zeitpunkt in ms); leer = keiner seit dem Start.
func (m *Manager) LastError() (string, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr, m.lastAt
}

// Ensure lädt das Modell im Hintergrund, wenn es fehlt. Andere geladene Modelle bleiben liegen (Wechsel ohne
// neuen Download); gelöscht wird nur auf Wunsch (Delete). Mehrfacher Aufruf startet keinen zweiten Download.
func (m *Manager) Ensure(ctx context.Context, id string) {
	md, ok := ByID(id)
	if !ok {
		return
	}
	m.mu.Lock()
	m.ctx = ctx
	if p := m.loading[id]; p != nil && p.Err == "" {
		m.mu.Unlock()
		return
	}
	if m.Loaded(id) {
		m.mu.Unlock()
		return
	}
	p := &progress{Total: md.Bytes}
	m.loading[id] = p
	m.mu.Unlock()
	go func() {
		err := m.download(ctx, md, p)
		m.mu.Lock()
		if err != nil {
			p.Err = err.Error()
			log.Printf("Whisper: Modell %s nicht geladen: %v", md.Name, err)
		} else {
			delete(m.loading, id)
			log.Printf("Whisper: Modell %s geladen", md.Name)
		}
		m.mu.Unlock()
	}()
}

// Delete entfernt ein geladenes Modell (nicht während es lädt).
func (m *Manager) Delete(id string) error {
	md, ok := ByID(id)
	if !ok {
		return i18n.Errorf("Unbekanntes Whisper-Modell %s", id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.loading[id]; p != nil && p.Err == "" {
		return i18n.Errorf("Das Modell wird gerade geladen")
	}
	delete(m.loading, id)
	if m.srv != nil && m.srv.model == id {
		return i18n.Errorf("Das Modell liegt gerade im RAM")
	}
	if err := os.Remove(m.path(md)); err != nil && !os.IsNotExist(err) {
		return err
	}
	log.Printf("Whisper: Modell %s gelöscht", md.Name)
	return nil
}

type countWriter struct {
	m *Manager
	p *progress
}

func (c countWriter) Write(b []byte) (int, error) {
	c.m.mu.Lock()
	c.p.Done += int64(len(b))
	c.m.mu.Unlock()
	return len(b), nil
}

// download holt das Modell nach <Dir>/<datei>.laden, prüft Größe und SHA-256 und benennt es dann um.
func (m *Manager) download(ctx context.Context, md Model, p *progress) error {
	if err := os.MkdirAll(m.Dir, 0o750); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", downloadBase+md.File, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Hour}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return i18n.Errorf("Download: HTTP %d", resp.StatusCode)
	}
	tmp := m.path(md) + ".laden"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h, countWriter{m, p}), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != md.Bytes {
		err = i18n.Errorf("Download unvollständig (%d von %d Bytes)", n, md.Bytes)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != md.SHA256 {
		err = i18n.Errorf("Prüfsumme des Modells falsch")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, m.path(md))
}

// Transcribe wandelt eine Aufnahme mit dem Modell id um. seconds = Länge der Aufnahme. prompt: Begriffe als Hinweis.
// Die Aufnahme berührt die Platte nicht: ffmpeg und whisper-cli lesen aus Speicherdateien (memfd).
func (m *Manager) Transcribe(ctx context.Context, id string, audio []byte, language, prompt string) (text string, seconds float64, err error) {
	defer func() {
		if err != nil && ctx.Err() == nil {
			m.mu.Lock()
			m.lastErr, m.lastAt = err.Error(), time.Now().UnixMilli()
			m.mu.Unlock()
		}
	}()
	md, ok := ByID(id)
	if !ok {
		return "", -1, i18n.Errorf("Unbekanntes Whisper-Modell %s", id)
	}
	if !m.Loaded(id) {
		// Gescheiterter Download (z. B. offline beim Start): mit dem nächsten Auftrag neu versuchen.
		m.mu.Lock()
		retry := m.ctx
		if p := m.loading[id]; p == nil || p.Err == "" {
			retry = nil
		}
		m.mu.Unlock()
		if retry != nil && retry.Err() == nil {
			m.Ensure(retry, id)
		}
		return "", -1, i18n.Errorf("Whisper-Modell %s ist noch nicht geladen", md.Name)
	}
	ffmpeg, err := extractFFmpeg(m.Dir)
	if err != nil {
		return "", -1, err
	}
	select {
	case m.run <- struct{}{}:
		defer func() { <-m.run }()
	case <-ctx.Done():
		return "", -1, ctx.Err()
	}

	wav, err := runWithInput(ctx, "audio", audio, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", "/proc/self/fd/3", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
	if err != nil {
		return "", -1, i18n.Errorf("Audio nicht lesbar (ffmpeg): %v", err)
	}
	if len(wav) <= 44 {
		return "", 0, nil
	}
	seconds = float64(len(wav)-44) / 32000
	srv, err := m.serverFor(md)
	if err != nil {
		return "", seconds, err
	}
	lang := language
	if lang == "" {
		lang = "auto"
	}
	start := time.Now()
	text, err = srv.infer(ctx, wav, lang, prompt)
	m.mu.Lock()
	srv.lastUse = time.Now()
	m.mu.Unlock()
	if err != nil {
		return "", seconds, i18n.Errorf("Whisper: %v", err)
	}
	if seconds >= 5 {
		m.record(md.ID, time.Since(start).Seconds()/seconds)
	}
	return cleanText(text), seconds, nil
}

// Markierungen ohne Sprache, die Whisper statt Text ausgibt: [BLANK_AUDIO], (Musik), [Applaus] …
var noSpeech = regexp.MustCompile(`^\s*[\[(][^\])]*[\])]\s*$`)

func cleanText(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" && !noSpeech.MatchString(line) {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func (m *Manager) record(id string, factor float64) {
	m.mu.Lock()
	ms := m.measured[id]
	if ms.Runs == 0 {
		ms.Factor = factor
	} else {
		ms.Factor = ms.Factor*0.8 + factor*0.2
	}
	ms.Runs++
	m.measured[id] = ms
	b, _ := json.Marshal(m.measured)
	m.mu.Unlock()
	if err := os.WriteFile(filepath.Join(m.Dir, measureFile), b, 0o640); err != nil {
		log.Printf("Whisper: Messwerte nicht gespeichert: %v", err)
	}
}

// runWithInput startet ein Programm, dem input als Speicherdatei auf fd 3 vorliegt (seekbar, anders als eine
// Pipe – m4a braucht das), und liefert seine Ausgabe. Fehlerausgabe geht in die Meldung.
func runWithInput(ctx context.Context, name string, input []byte, prog string, args ...string) ([]byte, error) {
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if _, err := f.Write(input); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.ExtraFiles = []*os.File{f}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(errOut.String())
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		if msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	return out.Bytes(), nil
}
