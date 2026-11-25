package whisper

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"pebble-webhook/internal/i18n"
)

// engine: gepackte Programme je Architektur (deploy/whisper-engine.sh), README.md hält den Ordner in Git.
//
//go:embed engine
var engine embed.FS

// EngineStand: Versionen der eingebetteten Engine („whisper.cpp 1.9.6, ffmpeg 8.1.3“); leer = nicht in diesem Build.
func EngineStand() string {
	b, err := engine.ReadFile("engine/" + runtime.GOARCH + "/STAND")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// variant wählt den schnellsten whisper-server, die die CPU kann (x86: avx512 → avx2 → basis).
func variant() string {
	if runtime.GOARCH != "amd64" {
		return "basis"
	}
	b, _ := os.ReadFile("/proc/cpuinfo")
	var flags map[string]bool
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "flags") {
			flags = map[string]bool{}
			if _, list, ok := strings.Cut(line, ":"); ok {
				for _, f := range strings.Fields(list) {
					flags[f] = true
				}
			}
			break
		}
	}
	has := func(fs ...string) bool {
		for _, f := range fs {
			if !flags[f] {
				return false
			}
		}
		return true
	}
	switch {
	case has("avx512f", "avx512bw", "avx512vl", "avx2", "fma", "f16c"):
		return "avx512"
	case has("avx2", "fma", "f16c", "bmi2"):
		return "avx2"
	}
	return "basis"
}

var extractMu sync.Mutex

// extract entpackt den passenden whisper-server nach <dir>/bin/<stand>/ (einmal je Engine-Stand).
func extract(dir string) (string, error) { return extractFile(dir, "whisper-server-"+variant()) }

// extractFFmpeg entpackt ffmpeg (Umwandlung der Aufnahme in 16-kHz-WAV).
func extractFFmpeg(dir string) (string, error) { return extractFile(dir, "ffmpeg") }

func extractFile(dir, name string) (string, error) {
	stand := EngineStand()
	if stand == "" {
		return "", i18n.Errorf("Dieser Build enthält keine Whisper-Engine für %s", runtime.GOARCH)
	}
	extractMu.Lock()
	defer extractMu.Unlock()
	sum := sha256.Sum256([]byte(stand))
	binDir := filepath.Join(dir, "bin", hex.EncodeToString(sum[:6]))
	p := filepath.Join(binDir, name)
	if _, err := os.Stat(p); err != nil {
		if err := unpack("engine/"+runtime.GOARCH+"/"+name+".gz", p); err != nil {
			return "", err
		}
	}
	cleanOldEngines(filepath.Join(dir, "bin"), filepath.Base(binDir))
	return p, nil
}

func unpack(src, dst string) error {
	b, err := engine.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return i18n.Errorf("Dieser Build enthält keine Whisper-Engine für %s", runtime.GOARCH)
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return err
	}
	tmp := dst + ".neu"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o750)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, zr); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// cleanOldEngines entfernt entpackte Engines früherer Versionen.
func cleanOldEngines(binRoot, keep string) {
	entries, _ := os.ReadDir(binRoot)
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			os.RemoveAll(filepath.Join(binRoot, e.Name()))
		}
	}
}
