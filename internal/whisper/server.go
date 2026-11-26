package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"pebble-webhook/internal/i18n"
)

// server: laufender whisper-server mit einem Modell im RAM, erreichbar nur über einen Unix-Socket.
// Er bleibt nach der letzten Aufnahme Keep lang geladen (0 = dauerhaft), danach wird er beendet.
type server struct {
	model   string
	cmd     *exec.Cmd
	client  *http.Client
	done    chan struct{}
	stderr  *tail
	since   time.Time
	lastUse time.Time
	dir     *os.File // Modell-Ordner, offen für den kurzen Socket-Pfad
}

func (s *server) alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *server) stop() {
	if s.alive() {
		s.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			s.cmd.Process.Kill()
			<-s.done
		}
	}
}

// tail hält die letzten Zeilen der Fehlerausgabe für Meldungen.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > 4000 {
		t.buf = t.buf[len(t.buf)-4000:]
	}
	t.mu.Unlock()
	return len(b), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.TrimSpace(string(t.buf))
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

// SetKeep: wie lange das Modell nach der letzten Aufnahme im RAM bleibt; 0 = dauerhaft.
func (m *Manager) SetKeep(d time.Duration) {
	m.mu.Lock()
	m.keep = d
	m.mu.Unlock()
}

// InRAM: Modell, das gerade im RAM liegt, und seit wann (ms); leer = keins.
func (m *Manager) InRAM() (string, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.srv == nil || !m.srv.alive() {
		return "", 0
	}
	return m.srv.model, m.srv.since.UnixMilli()
}

// serverFor liefert den Server mit dem Modell md; startet ihn bei Bedarf (ein anderes Modell wird beendet).
// Nur mit belegtem m.run aufrufen.
func (m *Manager) serverFor(md Model) (*server, error) {
	m.mu.Lock()
	cur := m.srv
	m.mu.Unlock()
	if cur != nil && cur.alive() && cur.model == md.ID {
		return cur, nil
	}
	if cur != nil {
		cur.stop()
	}
	bin, err := extract(m.Dir)
	if err != nil {
		return nil, err
	}
	// Unix-Socket relativ im Modell-Ordner (Pfade über 107 Zeichen gehen nicht); erreicht über das offene
	// Verzeichnis unter /proc/self/fd – so bleibt der Pfad kurz, egal wo DATA_DIR liegt.
	os.Remove(filepath.Join(m.Dir, "server.sock"))
	dir, err := os.Open(m.Dir)
	if err != nil {
		return nil, err
	}
	sock := fmt.Sprintf("/proc/self/fd/%d/server.sock", dir.Fd())
	start := time.Now()
	cmd := exec.Command(bin, "-m", m.path(md), "--host", "server.sock", "-t", fmt.Sprint(runtime.NumCPU()))
	cmd.Dir = m.Dir
	s := &server{model: md.ID, cmd: cmd, done: make(chan struct{}), stderr: &tail{}, since: start, lastUse: start, dir: dir}
	cmd.Stdout, cmd.Stderr = io.Discard, s.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL} // stirbt mit dem Dienst
	if err := cmd.Start(); err != nil {
		dir.Close()
		return nil, err
	}
	go func() { cmd.Wait(); dir.Close(); close(s.done) }()
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	// Bereit, sobald der Socket antwortet (das Modell ist dann geladen).
	for {
		if !s.alive() {
			return nil, i18n.Errorf("Whisper-Server beendet: %s", s.stderr.String())
		}
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		if time.Since(start) > 3*time.Minute {
			s.stop()
			return nil, i18n.Errorf("Whisper-Server startet nicht")
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.mu.Lock()
	m.srv = s
	m.mu.Unlock()
	log.Printf("Whisper: Modell %s im RAM (%.1f s)", md.Name, time.Since(start).Seconds())
	return s, nil
}

// infer schickt WAV an den Server und liefert den Text. prompt: Begriffe als Hinweis für die Erkennung (leer = keiner).
func (s *server) infer(ctx context.Context, wav []byte, language, prompt string) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "aufnahme.wav")
	part.Write(wav)
	w.WriteField("language", language)
	if prompt != "" {
		w.WriteField("prompt", prompt)
	}
	w.WriteField("response_format", "json")
	w.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://whisper/inference", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !s.alive() {
			return "", i18n.Errorf("Whisper-Server beendet: %s", s.stderr.String())
		}
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", errors.New(out.Error)
	}
	if resp.StatusCode != 200 {
		return "", i18n.Errorf("Whisper-Server: HTTP %d", resp.StatusCode)
	}
	return out.Text, nil
}

// Keepalive beendet den Server nach Keep ohne Aufnahme und mit dem Dienst (ctx). Läuft bis ctx endet.
func (m *Manager) Keepalive(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.run <- struct{}{}
			m.stopServer("Dienst endet")
			<-m.run
			return
		case <-t.C:
		}
		select {
		case m.run <- struct{}{}:
		default:
			continue // wandelt gerade um
		}
		m.mu.Lock()
		idle := m.srv != nil && m.keep > 0 && time.Since(m.srv.lastUse) > m.keep
		m.mu.Unlock()
		if idle {
			m.stopServer("ungenutzt")
		}
		<-m.run
	}
}

func (m *Manager) stopServer(why string) {
	m.mu.Lock()
	s := m.srv
	m.srv = nil
	m.mu.Unlock()
	if s != nil && s.alive() {
		s.stop()
		log.Printf("Whisper: Modell aus dem RAM (%s)", why)
	}
}

// Preload legt das Modell gleich in den RAM, sobald es geladen ist – die erste Aufnahme wartet dann nicht.
func (m *Manager) Preload(ctx context.Context, id string) {
	md, ok := ByID(id)
	if !ok {
		return
	}
	go func() {
		for !m.Loaded(id) {
			m.mu.Lock()
			p := m.loading[id]
			m.mu.Unlock()
			if p == nil || p.Err != "" {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		select {
		case m.run <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-m.run }()
		if _, err := m.serverFor(md); err != nil {
			log.Printf("Whisper: Modell %s nicht in den RAM geladen: %v", md.Name, err)
		}
	}()
}
