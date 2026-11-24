package app

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/store"
)

// Docker-Container: kein install.sh. Was dort der Installer erledigt, macht der Dienst selbst beim Start, bevor er
// die Datenbanken öffnet (der Prozess läuft dann schon als PUID:PGID):
//   1. DATA_DIR/restore-request (aus der App oder per Befehl wiederherstellen) → Backup einspielen.
//   2. Andere Version als beim letzten Start (DATA_DIR/docker-version) → Backup „vor Update“, danach migriert Prepare.

const (
	restoreRequestFile = "restore-request"
	dockerVersionFile  = "docker-version"
)

// DockerStart: siehe oben. Ein gescheitertes Wiederherstellen bricht den Start nicht ab (Daten bleiben, Meldung in
// der App); ein gescheitertes Backup vor einer neuen Version schon – sonst würde ohne Sicherung migriert.
func DockerStart(boot config.Bootstrap, version string) error {
	restored := false
	if b, err := os.ReadFile(filepath.Join(boot.DataDir, restoreRequestFile)); err == nil {
		os.Remove(filepath.Join(boot.DataDir, restoreRequestFile))
		name := strings.TrimSpace(string(b))
		setStatus(boot, "running restore "+name)
		if err := restoreArchive(boot, version, name); err != nil {
			logf("Wiederherstellen %s: %v", name, err)
			setStatus(boot, "error: restore "+name+": "+err.Error())
		} else {
			logf("Wiederhergestellt: %s", name)
			setStatus(boot, "done restore "+name)
			restored = true
		}
	}
	marker := filepath.Join(boot.DataDir, dockerVersionFile)
	last := ""
	if b, err := os.ReadFile(marker); err == nil {
		last = strings.TrimSpace(string(b))
	}
	if last != version && !restored && dataPresent(boot.DataDir) {
		if _, err := offlineBackup(boot, last); err != nil {
			return fmt.Errorf("Backup vor dem Start von %s fehlgeschlagen (Daten unverändert, nichts migriert): %w", version, err)
		}
	}
	return os.WriteFile(marker, []byte(version+"\n"), 0o640)
}

func setStatus(boot config.Bootstrap, s string) {
	os.WriteFile(filepath.Join(boot.DataDir, "update-status"), []byte(s+"\n"), 0o640)
}

func dataPresent(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "system.db"))
	return err == nil
}

// offlineBackup sichert die Daten bei gestopptem Dienst (Art „update“, ohne Aufnahmen), benannt nach der Version,
// mit der sie zuletzt liefen – genau diese Version passt zum Backup, falls man zurück will.
func offlineBackup(boot config.Bootstrap, lastVersion string) (backupEntry, error) {
	channel := "lokal"
	if _, ch, ok := parseVersion(lastVersion); ok {
		channel = ch
	}
	if lastVersion == "" {
		lastVersion = "unbekannt"
	}
	return makeBackup(boot, lastVersion, channel, "update", false, func(dir string) ([]backupUser, error) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		sysPath := filepath.Join(boot.DataDir, "system.db")
		if err := store.SnapshotFile(sysPath, filepath.Join(dir, "system.db")); err != nil {
			return nil, err
		}
		db, err := store.OpenReadOnly(filepath.Join(dir, "system.db"), nil)
		if err != nil {
			return nil, err
		}
		defer db.Close()
		rows, err := db.Query(`SELECT id, key_id FROM users ORDER BY id`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var users []backupUser
		for rows.Next() {
			var u backupUser
			if err := rows.Scan(&u.ID, &u.KeyID); err != nil {
				return nil, err
			}
			users = append(users, u)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		// Nutzer-Datenbanken als Datei samt WAL: der Dienst läuft nicht, also ist der Stand in sich stimmig.
		for _, u := range users {
			ud := UserDir(dir, u.ID)
			if err := os.MkdirAll(ud, 0o700); err != nil {
				return nil, err
			}
			for _, f := range []string{"daten.db", "daten.db-wal"} {
				if err := copyIfExists(filepath.Join(UserDir(boot.DataDir, u.ID), f), filepath.Join(ud, f)); err != nil {
					return nil, fmt.Errorf("Nutzer %d: %w", u.ID, err)
				}
			}
		}
		return users, nil
	})
}

// RequestRestore prüft ein Backup aus BACKUP_DIR und merkt es zum Einspielen beim nächsten Start vor (Docker).
// Lehnt ab, wenn die Datenbank im Backup neuer ist, als dieses Programm kennt.
func RequestRestore(boot config.Bootstrap, name string) error {
	if err := checkRestorable(boot, name); err != nil {
		return err
	}
	setStatus(boot, "requested restore "+name)
	return os.WriteFile(filepath.Join(boot.DataDir, restoreRequestFile), []byte(name+"\n"), 0o640)
}

func checkRestorable(boot config.Bootstrap, name string) error {
	m := archiveNameRe.FindStringSubmatch(name)
	if m == nil {
		return i18n.Errorf("Kein Backup-Name: %s", name)
	}
	p := filepath.Join(boot.BackupDir, name)
	if _, err := os.Stat(p); err != nil {
		return i18n.Errorf("Backup nicht gefunden: %s", name)
	}
	if err := checkBackupArchive(p, m, boot.BackupDir); err != nil {
		return i18n.Errorf("Kein gültiges Backup: %s", err.Error())
	}
	schema, err := archiveSchema(p, boot.BackupDir)
	if err != nil {
		return err
	}
	if schema > store.SchemaVersion {
		return i18n.Errorf("Das Backup stammt von Version %s mit neuerer Datenbank – erst das Image dieser Version starten, dann wiederherstellen", m[1])
	}
	return nil
}

// archiveSchema: Schema-Stand der Datenbank im Archiv (system.db, bis 2.x pebble.db).
func archiveSchema(p, tmpDir string) (int, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return 0, i18n.Errorf("Backup ohne Datenbank")
		}
		if h.Name != "system.db" && h.Name != "pebble.db" {
			continue
		}
		tmp, err := os.CreateTemp(tmpDir, ".schema-*.db")
		if err != nil {
			return 0, err
		}
		defer os.Remove(tmp.Name())
		_, err = io.Copy(tmp, tr)
		tmp.Close()
		if err != nil {
			return 0, err
		}
		return store.DBSchema(tmp.Name())
	}
}

// restoreArchive spielt ein Backup ein wie install.sh: prüfen, entpacken, jetzigen Stand sichern, Datenbanken ersetzen,
// Medien dazukopieren. Die Version bleibt die des Images; Prepare bringt die Daten danach auf ihren Stand.
func restoreArchive(boot config.Bootstrap, version, name string) error {
	if err := checkRestorable(boot, name); err != nil {
		return err
	}
	work := filepath.Join(boot.BackupDir, ".wiederherstellen-"+time.Now().Format("20060102-150405"))
	os.RemoveAll(work)
	defer os.RemoveAll(work)
	if err := extractArchive(filepath.Join(boot.BackupDir, name), work); err != nil {
		return fmt.Errorf("entpacken: %w", err)
	}
	if dataPresent(boot.DataDir) {
		last := version
		if b, err := os.ReadFile(filepath.Join(boot.DataDir, dockerVersionFile)); err == nil {
			last = strings.TrimSpace(string(b))
		}
		if _, err := offlineBackup(boot, last); err != nil {
			return fmt.Errorf("jetzigen Stand sichern: %w", err)
		}
	}
	// DATA_DIR bekommt genau den Datenbestand aus dem Backup (users/ kann ein eigener Mount sein: nur leeren).
	for _, f := range []string{"pebble.db", "pebble.db-wal", "pebble.db-shm", "system.db", "system.db-wal", "system.db-shm"} {
		if err := os.Remove(filepath.Join(boot.DataDir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	usersDir := filepath.Join(boot.DataDir, "users")
	if entries, err := os.ReadDir(usersDir); err == nil {
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(usersDir, e.Name())); err != nil {
				return err
			}
		}
	}
	for _, f := range []string{"system.db", "pebble.db"} {
		if err := copyTree(filepath.Join(work, f), filepath.Join(boot.DataDir, f)); err != nil {
			return err
		}
	}
	if err := copyTree(filepath.Join(work, "users"), usersDir); err != nil {
		return err
	}
	// Medien: ab 4.0 medien/<id>/audio|bilder; 3.x audio/<id>/… → MEDIA_DIR/<id>/audio/; bis 2.x audio/<art>/… unverändert.
	if err := copyTree(filepath.Join(work, "medien"), boot.MediaDir); err != nil {
		return err
	}
	if entries, err := os.ReadDir(filepath.Join(work, "audio")); err == nil {
		for _, e := range entries {
			src := filepath.Join(work, "audio", e.Name())
			if n, err := strconv.Atoi(e.Name()); err == nil && n > 0 {
				err = copyTree(src, filepath.Join(boot.MediaDir, e.Name(), "audio"))
				if err != nil {
					return err
				}
			} else if err := copyTree(src, filepath.Join(boot.MediaDir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractArchive entpackt ein (vorher mit checkBackupArchive geprüftes) Archiv: nur normale Dateien, Namen ohne
// Ausbruch aus dir.
func extractArchive(p, dir string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("%s ist keine normale Datei", h.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(h.Name))
		if rel, err := filepath.Rel(dir, dst); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unerlaubter Name %s", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
}

// copyTree kopiert eine Datei oder einen Ordner (nur normale Dateien) nach dst; vorhandene Dateien werden ersetzt.
// Fehlt src, passiert nichts.
func copyTree(src, dst string) error {
	fi, err := os.Lstat(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := dst + ".kopie"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// BackupList: Backups in BACKUP_DIR für den Befehl wiederherstellen (Name, ob dieses Programm es einspielen kann).
func BackupList(boot config.Bootstrap) []string {
	a := &System{Cfg: &config.Config{BackupDir: boot.BackupDir, DataDir: boot.DataDir}}
	var out []string
	for _, b := range a.backups() {
		if b.Legacy {
			continue
		}
		line := b.Name
		if s, err := archiveSchema(filepath.Join(boot.BackupDir, b.Name), boot.BackupDir); err == nil && s > store.SchemaVersion {
			line += "  (neuere Datenbank – erst Image " + b.Version + " starten)"
		}
		out = append(out, line)
	}
	return out
}
