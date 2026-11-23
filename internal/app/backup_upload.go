package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
)

// backupUploadMax: größtes hochladbares Backup (mit Aufnahmen kann es groß sein).
const backupUploadMax = 2 << 30

// handleBackupUpload: POST /api/backups/hochladen?name=<Dateiname> mit dem Archiv als Rumpf. Angenommen wird nur genau
// die Form, die der Download liefert (Name, Einträge, Reihenfolge, meta.json passend zum Namen, lesbare Datenbank).
// Danach liegt es in BACKUP_DIR und wird sofort eingespielt – wie „Wiederherstellen“ samt Version aus dem Backup.
func (a *App) handleBackupUpload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	m := archiveNameRe.FindStringSubmatch(name)
	if m == nil {
		writeErr(w, 400, "ungueltig", "Dateiname passt nicht zu einem Backup (pebble-<version>-<kanal>-<zeit>-<art>.tar.gz)")
		return
	}
	if !updateInstallable() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", "Kein Updater installiert – Installation mit install.sh nötig")
		return
	}
	final := filepath.Join(a.Cfg.BackupDir, name)
	if _, err := os.Stat(final); err == nil {
		writeErr(w, http.StatusConflict, "vorhanden", "Dieses Backup liegt schon vor – in der Liste wiederherstellen")
		return
	}
	if err := os.MkdirAll(a.Cfg.BackupDir, 0o750); err != nil {
		writeErr(w, 500, "speicher", "Backup-Ordner nicht anlegbar")
		return
	}
	// Große Backups (mit Aufnahmen) brauchen länger als die allgemeine Lesefrist des Servers.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	b := make([]byte, 8)
	rand.Read(b)
	tmp := filepath.Join(a.Cfg.BackupDir, ".hochladen-"+hex.EncodeToString(b)+".tmp")
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		writeErr(w, 500, "speicher", "Upload nicht speicherbar")
		return
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, backupUploadMax))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeErr(w, 400, "ungueltig", "Upload abgebrochen oder zu groß")
		return
	}
	if err := checkBackupArchive(tmp, m, a.Cfg.BackupDir); err != nil {
		logf("Backup-Upload %s abgelehnt: %v", name, err)
		writeErr(w, 400, "ungueltig", i18n.Tf("Kein gültiges Backup: %s", err.Error()))
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		writeErr(w, 500, "speicher", "Backup nicht ablegbar")
		return
	}
	logf("Backup hochgeladen: %s", name)
	if err := a.requestRestore(name); err != nil {
		writeErr(w, 500, "speicher", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": name})
}

// checkBackupArchive prüft ein hochgeladenes Archiv gegen die Form von writeArchive: meta.json, env, pebble.db in dieser
// Reihenfolge, dann nur bei „-audio“ Dateien unter audio/; nur normale Dateien, keine Ordner, Links oder fremden Namen.
// m ist der Treffer von archiveNameRe auf den Dateinamen.
func checkBackupArchive(p string, m []string, tmpDir string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.New("keine gzip-Datei")
	}
	tr := tar.NewReader(gz)
	audio := m[5] != ""
	order := []string{"meta.json", "env", "pebble.db"}
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("Archiv beschädigt")
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("%s ist keine normale Datei", h.Name)
		}
		if n < len(order) {
			if h.Name != order[n] {
				return fmt.Errorf("erwartet %s, gefunden %s", order[n], h.Name)
			}
		} else if !audio {
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		} else if !strings.HasPrefix(h.Name, "audio/") || path.Clean(h.Name) != h.Name || strings.Contains(h.Name, "\\") || strings.Contains(h.Name, "..") {
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		}
		switch h.Name {
		case "meta.json":
			if h.Size > 64<<10 {
				return errors.New("meta.json zu groß")
			}
			var meta backupMeta
			dec := json.NewDecoder(tr)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&meta); err != nil {
				return errors.New("meta.json nicht lesbar")
			}
			if meta.Format != 1 || meta.Version != m[1] || meta.Channel != m[2] || meta.Kind != m[4] || meta.Audio != audio {
				return errors.New("meta.json passt nicht zum Dateinamen")
			}
		case "env":
			if h.Size > 4<<10 {
				return errors.New("env zu groß")
			}
			b, _ := io.ReadAll(tr)
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				k, _, ok := strings.Cut(line, "=")
				if strings.HasPrefix(line, "#") {
					continue
				}
				if !ok || (k != "LISTEN" && k != "DATA_DIR" && k != "AUDIO_DIR" && k != "BACKUP_DIR") {
					return errors.New("env enthält fremde Werte")
				}
			}
		case "pebble.db":
			if err := checkBackupDB(tr, tmpDir); err != nil {
				return err
			}
		}
		n++
	}
	if n < len(order) {
		return errors.New("unvollständig (meta.json, env, pebble.db)")
	}
	// Nach dem Ende-Block des Archivs darf nur noch Füllung (Nullen) folgen – auch keine angehängten gzip-Teile.
	rest, err := io.ReadAll(io.LimitReader(gz, 1<<20))
	if err != nil || len(bytes.Trim(rest, "\x00")) > 0 {
		return errors.New("weitere Daten nach dem Archiv")
	}
	return nil
}

// checkBackupDB legt die Datenbank aus dem Archiv kurz ab und öffnet sie nur lesend: SQLite, unbeschädigt, mit Aufträgen.
func checkBackupDB(r io.Reader, dir string) error {
	b := make([]byte, 8)
	rand.Read(b)
	p := filepath.Join(dir, ".pruefen-"+hex.EncodeToString(b)+".db")
	defer os.Remove(p)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	f.Close()
	if err != nil {
		return err
	}
	head := make([]byte, 16)
	if g, err := os.Open(p); err == nil {
		io.ReadFull(g, head)
		g.Close()
	}
	if string(head) != "SQLite format 3\x00" {
		return errors.New("pebble.db ist keine SQLite-Datenbank")
	}
	db, err := sql.Open("sqlite", "file:"+p+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return errors.New("pebble.db beschädigt")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil {
		return errors.New("pebble.db ist keine Datenbank dieser App")
	}
	return nil
}
