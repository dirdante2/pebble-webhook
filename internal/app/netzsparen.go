package app

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

// apiCache: JSON-Antworten auf GET /api/… bekommen einen ETag. Kennt der Browser den Stand schon
// (If-None-Match), geht nur „304 Not Modified“ ohne Inhalt zurück; sonst wird gepackt (gzip).
// Alles andere (Ereignisstrom, Audio, Backup-Archive) läuft unverändert durch.
func apiCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		cw := &cacheWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		if cw.mode != modeBuffer {
			return
		}
		body := cw.buf.Bytes()
		h := w.Header()
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		h.Set("ETag", etag)
		// Jede Nutzung fragt nach; unverändert kostet das nur die Kopfzeilen.
		h.Set("Cache-Control", "private, no-cache")
		h.Set("Vary", "Accept-Encoding, X-Sprache")
		if etagMatch(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		var gz []byte
		if len(body) > 1024 {
			gz = gzipBytes(body)
		}
		writeMaybeGzip(w, r, body, gz)
	})
}

const (
	modeOpen = iota
	modeBuffer
	modePass
)

type cacheWriter struct {
	http.ResponseWriter
	buf  bytes.Buffer
	mode int
}

func (c *cacheWriter) WriteHeader(code int) {
	if c.mode != modeOpen {
		return
	}
	if code == http.StatusOK && strings.HasPrefix(c.Header().Get("Content-Type"), "application/json") {
		c.mode = modeBuffer
		return
	}
	c.mode = modePass
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheWriter) Write(b []byte) (int, error) {
	if c.mode == modeOpen {
		c.WriteHeader(http.StatusOK)
	}
	if c.mode == modeBuffer {
		return c.buf.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

func (c *cacheWriter) Flush() {
	if c.mode == modeOpen {
		c.WriteHeader(http.StatusOK)
	}
	if c.mode == modePass {
		if f, ok := c.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (c *cacheWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func gzipBytes(b []byte) []byte {
	var out bytes.Buffer
	z, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
	z.Write(b)
	z.Close()
	return out.Bytes()
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(enc) == "gzip" && strings.ReplaceAll(strings.TrimSpace(q), " ", "") != "q=0" {
			return true
		}
	}
	return false
}

// writeMaybeGzip schreibt die gepackte Fassung, wenn es eine gibt und der Browser sie annimmt.
func writeMaybeGzip(w http.ResponseWriter, r *http.Request, data, gz []byte) {
	if gz != nil && len(gz) < len(data) && acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		data = gz
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

func etagMatch(r *http.Request, etag string) bool {
	for _, t := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == etag || t == "*" {
			return true
		}
	}
	return false
}
