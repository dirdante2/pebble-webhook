package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// backupUploadMax: größtes hochladbares Backup (mit Aufnahmen kann es groß sein).
const backupUploadMax = 2 << 30

// handleBackupUpload: POST /api/backups/hochladen?name=<Dateiname> mit dem Archiv als Rumpf. Angenommen wird nur genau
// die Form, die der Download liefert (Name, Einträge, Reihenfolge, meta.json passend zum Namen, lesbare Datenbank).
// Danach liegt es in BACKUP_DIR und wird sofort eingespielt – wie „Wiederherstellen“ samt Version aus dem Backup.
func (a *System) handleBackupUpload(w http.ResponseWriter, r *http.Request) {
	name, ok := receiveBackup(w, r, a.Cfg.BackupDir)
	if !ok {
		return
	}
	if err := a.requestRestore(name); err != nil {
		writeErr(w, 500, "speicher", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": name})
}

// receiveBackup nimmt ein hochgeladenes Backup an (?name=, Archiv als Rumpf), prüft es und legt es in BACKUP_DIR ab.
// Fehler beantwortet es selbst (ok = false). Gemeinsam für die Backup-Seite und die Ersteinrichtung.
func receiveBackup(w http.ResponseWriter, r *http.Request, backupDir string) (string, bool) {
	name := r.URL.Query().Get("name")
	m := archiveNameRe.FindStringSubmatch(name)
	if m == nil {
		writeErr(w, 400, "ungueltig", "Dateiname passt nicht zu einem Backup (pebble-<version>-<kanal>-<zeit>-<art>.tar.gz)")
		return "", false
	}
	if !updateInstallable() && !inDocker() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", noUpdater())
		return "", false
	}
	final := filepath.Join(backupDir, name)
	if _, err := os.Stat(final); err == nil {
		writeErr(w, http.StatusConflict, "vorhanden", "Dieses Backup liegt schon vor – in der Liste wiederherstellen")
		return "", false
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		writeErr(w, 500, "speicher", "Backup-Ordner nicht anlegbar")
		return "", false
	}
	// Große Backups (mit Aufnahmen) brauchen länger als die allgemeine Lesefrist des Servers.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute))
	b := make([]byte, 8)
	rand.Read(b)
	tmp := filepath.Join(backupDir, ".hochladen-"+hex.EncodeToString(b)+".tmp")
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		writeErr(w, 500, "speicher", "Upload nicht speicherbar")
		return "", false
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, backupUploadMax))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeErr(w, 400, "ungueltig", "Upload abgebrochen oder zu groß")
		return "", false
	}
	if err := checkBackupArchive(tmp, m, backupDir); err != nil {
		logf("Backup-Upload %s abgelehnt: %v", name, err)
		writeErr(w, 400, "ungueltig", i18n.Tf("Kein gültiges Backup: %s", err.Error()))
		return "", false
	}
	if err := os.Rename(tmp, final); err != nil {
		writeErr(w, 500, "speicher", "Backup nicht ablegbar")
		return "", false
	}
	logf("Backup hochgeladen: %s", name)
	return name, true
}

// checkBackupArchive prüft ein hochgeladenes Archiv gegen die Form von writeArchive: meta.json, env, dann die Daten –
// Format 1: pebble.db; ab Format 2: system.db und je Nutzer users/<id>/daten.db (verschlüsselt) –, dann nur bei
// „-audio“ die Medien (Format 2: audio/<id>/….enc, Format 3: medien/<id>/audio|bilder/….enc); nur normale Dateien,
// keine Ordner, Links oder fremden Namen.
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
	// Entpackt höchstens halb so viel, wie frei ist (Schutz vor einer „Zip-Bombe“, die die Platte füllt).
	limit := int64(1) << 40
	if free, err := freeBytes(tmpDir); err == nil {
		limit = int64(free / 2)
	}
	var total int64
	audio := m[5] != ""
	order := []string{"meta.json", "env", "pebble.db"}
	n, format, users := 0, 0, 0
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
		if total += h.Size; h.Size < 0 || total > limit {
			return errors.New("entpackt größer als der freie Platz erlaubt")
		}
		switch {
		case n < len(order):
			if h.Name != order[n] {
				return fmt.Errorf("erwartet %s, gefunden %s", order[n], h.Name)
			}
		case format >= 2 && userFileRe.MatchString(h.Name):
			if err := checkSealedDB(tr, h.Name); err != nil {
				return err
			}
			if !strings.HasSuffix(h.Name, "-wal") {
				users++
			}
			n++
			continue
		case !audio:
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		case path.Clean(h.Name) != h.Name || strings.Contains(h.Name, "\\") || strings.Contains(h.Name, ".."):
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		case format <= 2 && !strings.HasPrefix(h.Name, "audio/"):
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		case format == 2 && (!audioFileRe.MatchString(h.Name) || !strings.HasSuffix(h.Name, schluessel.AudioExt)):
			return fmt.Errorf("unerwartete Datei %s", h.Name)
		case format == 3 && (!mediaFileRe.MatchString(h.Name) || !strings.HasSuffix(h.Name, schluessel.AudioExt)):
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
			if meta.Format < 1 || meta.Format > 3 || meta.Version != m[1] || meta.Channel != m[2] || meta.Kind != m[4] || meta.Audio != audio {
				return errors.New("meta.json passt nicht zum Dateinamen")
			}
			format = meta.Format
			// Kein Zurück auf Versionen, die das Datenbankschema nicht kennen (wie bei Updates).
			if v, _, ok := parseVersion(meta.Version); ok && v.less(minRollback) {
				return errors.New("Backup einer zu alten Version")
			}
			if format >= 2 {
				order[2] = "system.db"
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
				if !ok || (k != "LISTEN" && k != "DATA_DIR" && k != "AUDIO_DIR" && k != "MEDIA_DIR" && k != "BACKUP_DIR") {
					return errors.New("env enthält fremde Werte")
				}
			}
		case "pebble.db", "system.db":
			want := "jobs"
			if format >= 2 {
				want = "users"
			}
			if h.Name != order[2] {
				return fmt.Errorf("unerwartete Datei %s", h.Name)
			}
			if err := checkBackupDB(tr, tmpDir, h.Name, want); err != nil {
				return err
			}
		}
		n++
	}
	if n < len(order) {
		return errors.New("unvollständig (meta.json, env, Datenbank)")
	}
	if format >= 2 && users == 0 {
		return errors.New("unvollständig (keine Nutzerdaten)")
	}
	// Nach dem Ende-Block des Archivs darf nur noch Füllung (Nullen) folgen – auch keine angehängten gzip-Teile.
	rest, err := io.ReadAll(io.LimitReader(gz, 1<<20))
	if err != nil || len(bytes.Trim(rest, "\x00")) > 0 {
		return errors.New("weitere Daten nach dem Archiv")
	}
	return nil
}

// checkBackupDB legt eine Datenbank aus dem Archiv kurz ab und öffnet sie nur lesend: SQLite, unbeschädigt, mit der
// Tabelle want (pebble.db bis 2.x: jobs; system.db: users).
func checkBackupDB(r io.Reader, dir, name, want string) error {
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
		return fmt.Errorf("%s ist keine SQLite-Datenbank", name)
	}
	db, err := store.OpenReadOnly(p, nil)
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return fmt.Errorf("%s beschädigt", name)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + want).Scan(&n); err != nil {
		return fmt.Errorf("%s ist keine Datenbank dieser App", name)
	}
	return nil
}

// checkSealedDB: die Datenbank eines Nutzers muss verschlüsselt sein (kein Klartext-Kopf) – lesen kann sie hier niemand.
func checkSealedDB(r io.Reader, name string) error {
	head := make([]byte, 16)
	n, _ := io.ReadFull(r, head)
	if n == 16 && string(head) == "SQLite format 3\x00" {
		return fmt.Errorf("%s ist nicht verschlüsselt", name)
	}
	if n < 16 {
		return fmt.Errorf("%s ist leer", name)
	}
	_, err := io.Copy(io.Discard, r)
	return err
}

var userFileRe = regexp.MustCompile(`^users/[1-9][0-9]{0,8}/daten\.db(-wal)?$`)
var audioFileRe = regexp.MustCompile(`^audio/[1-9][0-9]{0,8}/`)

// mediaFileRe: Medien im Archiv ab Format 3 (medien/<id>/audio/… oder medien/<id>/bilder/…).
var mediaFileRe = regexp.MustCompile(`^medien/[1-9][0-9]{0,8}/(audio|bilder)/`)
