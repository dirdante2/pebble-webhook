package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Mitschnitt: jede Zustellung der Pebble-App roh und ungefiltert (HOOK_MITSCHNITT=aktiv), um zu sehen,
// was die App wirklich schickt. Liegt im Klartext unter DATA_DIR/mitschnitt/<nutzer>/<zeit>-<zustellung>/;
// nur der Token ist geschwärzt. Es bleiben die letzten captureKeep Zustellungen.

const (
	captureTextMax = 1 << 20
	captureKeep    = 200
)

var captureNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func (a *System) captureDir(userID int64) string {
	return filepath.Join(a.Cfg.DataDir, "mitschnitt", strconv.FormatInt(userID, 10))
}

// dropCapture entfernt den Mitschnitt eines Nutzers (Einstellung aus).
func (a *System) dropCapture(userID int64) {
	dir := a.captureDir(userID)
	if _, err := os.Stat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			logf("mitschnitt: löschen: %v", err)
		}
	}
}

// captureHook schreibt die Zustellung in einen eigenen Ordner <zeit>-<zustellung>: anfrage.txt, teile.txt, koerper.bin
// und je Datei-Teil teil-<nr>-<feld>.<ext>. Fehler stören die Annahme nicht. Liefert den Ordner ("" = nichts geschrieben).
func (a *System) captureHook(userID int64, r *http.Request, body []byte, received time.Time) string {
	base := a.captureDir(userID)
	id := captureNameRe.ReplaceAllString(strings.TrimSpace(r.Header.Get("X-Index-Delivery")), "_")
	if len(id) > 80 {
		id = id[:80]
	}
	// Zeit vorn: Namen sortieren sich nach Eingang; Nanosekunden halten Wiederholungen derselben Zustellung auseinander.
	dir := filepath.Join(base, received.Format("20060102-150405.000000000")+"-"+id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logf("mitschnitt: %v", err)
		return ""
	}
	if all, err := os.ReadDir(base); err == nil && len(all) > captureKeep {
		for _, e := range all[:len(all)-captureKeep] {
			os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			logf("mitschnitt: %s: %v", name, err)
		}
	}

	var b strings.Builder
	sum := sha256.Sum256(body)
	fmt.Fprintf(&b, "Empfangen: %s (Unix-ms %d)\n", received.Format(time.RFC3339Nano), received.UnixMilli())
	fmt.Fprintf(&b, "Absender (RemoteAddr): %s\n", r.RemoteAddr)
	fmt.Fprintf(&b, "Absender (ermittelt): %s\n", a.clientIP(r))
	fmt.Fprintf(&b, "Methode: %s\n", r.Method)
	fmt.Fprintf(&b, "Ziel (RequestURI): %s\n", r.RequestURI)
	fmt.Fprintf(&b, "Query: %s\n", r.URL.RawQuery)
	fmt.Fprintf(&b, "Protokoll: %s\n", r.Proto)
	fmt.Fprintf(&b, "Host: %s\n", r.Host)
	fmt.Fprintf(&b, "Content-Length: %d\n", r.ContentLength)
	fmt.Fprintf(&b, "Transfer-Encoding: %s\n", strings.Join(r.TransferEncoding, ", "))
	fmt.Fprintf(&b, "TLS: %t\n", r.TLS != nil)
	fmt.Fprintf(&b, "Körper: %d Bytes, SHA-256 %s\n", len(body), hex.EncodeToString(sum[:]))
	b.WriteString("\nHeader (alle; Namen in Go-Schreibweise, alphabetisch):\n")
	writeHeaders(&b, r.Header, http.CanonicalHeaderKey(a.Cfg.HookTokenHeader))
	if len(r.Trailer) > 0 {
		b.WriteString("\nTrailer:\n")
		writeHeaders(&b, r.Trailer, "")
	}
	write("anfrage.txt", []byte(b.String()))
	write("koerper.bin", body)
	write("teile.txt", []byte(captureParts(r.Header.Get("Content-Type"), body, write)))
	return dir
}

// captureReply hängt die Antwort des Servers an anfrage.txt.
func captureReply(dir string, status int, body []byte) {
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "anfrage.txt"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		logf("mitschnitt: Antwort: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\nAntwort: %d %s\n", status, strings.TrimSpace(string(body)))
}

func writeHeaders(b *strings.Builder, h map[string][]string, redact string) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range h[k] {
			if redact != "" && k == redact {
				v = fmt.Sprintf("<geschwärzt, %d Zeichen>", len(v))
			}
			fmt.Fprintf(b, "%s: %s\n", k, v)
		}
	}
}

// captureParts beschreibt jeden Multipart-Teil vollständig; Datei- und Binärteile gehen zusätzlich in eigene Dateien.
func captureParts(contentType string, body []byte, write func(name string, data []byte)) string {
	var b strings.Builder
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		fmt.Fprintf(&b, "Kein Multipart (Content-Type %q, Fehler: %v); Rohkörper siehe koerper.bin\n", contentType, err)
		return b.String()
	}
	fmt.Fprintf(&b, "Multipart %s, Boundary %s\n", mt, params["boundary"])
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for n := 1; ; n++ {
		// NextRawPart: Inhalt unverändert, auch bei Content-Transfer-Encoding.
		part, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(&b, "\nTeil %d nicht lesbar: %v\n", n, err)
			break
		}
		data, err := io.ReadAll(part)
		fmt.Fprintf(&b, "\n== Teil %d ==\nFeldname: %s\nDateiname: %s\n", n, part.FormName(), part.FileName())
		writeHeaders(&b, part.Header, "")
		fmt.Fprintf(&b, "Länge: %d Bytes\n", len(data))
		if err != nil {
			fmt.Fprintf(&b, "Lesefehler: %v\n", err)
		}
		if part.FileName() == "" && len(data) <= captureTextMax && utf8.Valid(data) {
			fmt.Fprintf(&b, "Wert:\n%s\n", data)
			continue
		}
		sum := sha256.Sum256(data)
		ext := ".bin"
		if ct := strings.ToLower(part.Header.Get("Content-Type")); strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
			ext = audioExt(ct)
		}
		name := fmt.Sprintf("teil-%02d-%s%s", n, captureNameRe.ReplaceAllString(part.FormName(), "_"), ext)
		fmt.Fprintf(&b, "SHA-256: %s\nErste Bytes (hex): %s\nDatei: %s\n", hex.EncodeToString(sum[:]), hex.EncodeToString(data[:min(len(data), 64)]), name)
		write(name, data)
	}
	return b.String()
}

// hookRecorder liest Status und Körper der Antwort für den Mitschnitt mit.
type hookRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (h *hookRecorder) WriteHeader(code int) {
	h.status = code
	h.ResponseWriter.WriteHeader(code)
}

func (h *hookRecorder) Write(p []byte) (int, error) {
	if h.status == 0 {
		h.status = http.StatusOK
	}
	h.body.Write(p)
	return h.ResponseWriter.Write(p)
}
