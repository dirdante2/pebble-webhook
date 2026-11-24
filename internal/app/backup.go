package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/schluessel"
)

// Backups: ein Archiv pebble-<version>-<kanal>-<zeit>-<art>[-audio].tar.gz in BACKUP_DIR mit (Format 3, ab 4.0)
//   meta.json                      Version, Build, Kanal, Zeitpunkt, Art, Audio ja/nein, Nutzer mit Schlüssel-Kennung
//   env                            Startwerte (LISTEN, DATA_DIR, MEDIA_DIR, BACKUP_DIR)
//   system.db                      Nutzer, Anmeldung (nur Hashes und öffentliche Schlüssel), Systemeinstellungen
//   users/<id>/daten.db            Daten je Nutzer, verschlüsselt mit seinem Schlüssel (in sich stimmige Kopie)
//   medien/<id>/audio|bilder/….enc verschlüsselte Aufnahmen und Bilder (nur mit BACKUP_AUDIO oder Häkchen)
// Die Schlüssel (KEY_DIR) sind nie im Archiv: ohne sie oder den Wiederherstellungscode des Nutzers bleiben seine
// Daten unlesbar. Format 2 (3.x): Aufnahmen unter audio/<id>/….enc, keine Bilder. Format 1 (bis 2.x): pebble.db im
// Klartext statt system.db und users/.
// Art: update = vom Installer vor jedem Update/Versionswechsel (30 Tage), plan = geplant (BACKUP_SCHEDULE),
// manuell = „Jetzt sichern“; beide BACKUP_RETENTION_DAYS. Das Programm wird nicht gesichert: Wiederherstellen
// installiert die vermerkte Version aus ihrem Kanal neu (install.sh restore).

var archiveNameRe = regexp.MustCompile(`^pebble-([0-9A-Za-z.+-]+)-(stable|beta|nightly|lokal)-(\d{8}-\d{6})-(update|plan|manuell)(-audio)?\.tar\.gz$`)

// legacyNameRe: Backups bis 1.28.x als Ordner DATA_DIR/backup/pebble-<version>-<zeit> mit Programm und Datenbank.
var legacyNameRe = regexp.MustCompile(`^pebble-([0-9A-Za-z.+-]+)-(\d{8}-\d{6})$`)

const updateBackupRetention = 30 * 24 * time.Hour

type backupEntry struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Channel    string `json:"kanal"`
	Kind       string `json:"art"` // update | plan | manuell
	At         int64  `json:"zeit"`
	Audio      bool   `json:"audio"`
	Size       int64  `json:"groesse"`
	Restorable bool   `json:"wiederherstellbar"`
	Legacy     bool   `json:"alt,omitempty"` // Ordner im alten Format (nur Datenbank + Programm)
}

type backupMeta struct {
	Format   int          `json:"format"`
	Users    []backupUser `json:"nutzer,omitempty"`
	Version  string       `json:"version"`
	Build    string       `json:"build"`
	Channel  string       `json:"kanal"`
	Created  string       `json:"erstellt"`
	Kind     string       `json:"art"`
	Audio    bool         `json:"audio"`
	DataDir  string       `json:"data_dir"`
	MediaDir string       `json:"media_dir,omitempty"` // ab Format 3
	AudioDir string       `json:"audio_dir,omitempty"` // bis Format 2
}

type backupUser struct {
	ID    int64  `json:"id"`
	KeyID string `json:"key_id"`
}

var backupMu sync.Mutex

// backups: alle Backups, neueste zuerst (Archive in BACKUP_DIR und Ordner im alten Format).
func (a *System) backups() []backupEntry {
	out := []backupEntry{}
	entries, _ := os.ReadDir(a.Cfg.BackupDir)
	for _, e := range entries {
		m := archiveNameRe.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			continue
		}
		at, err := time.ParseInLocation("20060102-150405", m[3], time.Local)
		fi, errInfo := e.Info()
		if err != nil || errInfo != nil {
			continue
		}
		out = append(out, backupEntry{Name: e.Name(), Version: m[1], Channel: m[2], Kind: m[4], At: at.UnixMilli(),
			Audio: m[5] != "", Size: fi.Size(), Restorable: true})
	}
	legacy, _ := os.ReadDir(filepath.Join(a.Cfg.DataDir, "backup"))
	for _, e := range legacy {
		m := legacyNameRe.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		at, err := time.ParseInLocation("20060102-150405", m[2], time.Local)
		if err != nil {
			continue
		}
		dir := filepath.Join(a.Cfg.DataDir, "backup", e.Name())
		_, errBin := os.Stat(filepath.Join(dir, "pebble-webhook"))
		_, errDB := os.Stat(filepath.Join(dir, "pebble.db"))
		channel := ""
		if b, err := os.ReadFile(filepath.Join(dir, "kanal")); err == nil {
			channel = strings.TrimSpace(string(b))
		} else if _, ch, ok := parseVersion(m[1]); ok && ch != "stable" {
			channel = ch
		}
		var size int64 // ganzer Ordner (mit Programm)
		filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					size += fi.Size()
				}
			}
			return nil
		})
		out = append(out, backupEntry{Name: e.Name(), Version: m[1], Channel: channel, Kind: "update", At: at.UnixMilli(),
			Size: size, Restorable: errBin == nil && errDB == nil, Legacy: true})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].At > out[k].At })
	return out
}

// createBackup schreibt ein Archiv nach BACKUP_DIR (erst unter .tmp-Namen, dann umbenannt).
func (a *System) createBackup(kind string, audio bool) (backupEntry, error) {
	return makeBackup(a.bootstrap(), a.Version, a.installedChannel(), kind, audio, a.snapshot)
}

func (a *System) bootstrap() config.Bootstrap {
	return config.Bootstrap{Listen: a.Cfg.Listen, DataDir: a.Cfg.DataDir, MediaDir: a.Cfg.MediaDir, BackupDir: a.Cfg.BackupDir, KeyDir: a.Cfg.KeyDir}
}

// makeBackup: gemeinsamer Teil für laufende App und Start im Container – snap legt die Datenbank-Kopien in ein
// Arbeitsverzeichnis, daraus wird das Archiv pebble-<version>-<kanal>-<zeit>-<art>[-audio].tar.gz.
func makeBackup(b config.Bootstrap, build, channel, kind string, audio bool, snap func(dir string) ([]backupUser, error)) (backupEntry, error) {
	backupMu.Lock()
	defer backupMu.Unlock()
	now := time.Now()
	ver := strings.Fields(build + " ")[0]
	if ver == "" {
		ver = "unbekannt"
	}
	name := fmt.Sprintf("pebble-%s-%s-%s-%s", ver, channel, now.Format("20060102-150405"), kind)
	if audio {
		name += "-audio"
	}
	name += ".tar.gz"
	if !archiveNameRe.MatchString(name) {
		return backupEntry{}, i18n.Errorf("Backup-Name ungültig: %s", name)
	}
	if err := os.MkdirAll(b.BackupDir, 0o750); err != nil {
		return backupEntry{}, i18n.Errorf("Backup-Ordner %s nicht anlegbar: %v", b.BackupDir, err)
	}
	snapDir := filepath.Join(b.BackupDir, ".snapshot-"+now.Format("20060102-150405"))
	os.RemoveAll(snapDir)
	defer os.RemoveAll(snapDir)
	users, err := snap(snapDir)
	if err != nil {
		return backupEntry{}, i18n.Errorf("Datenbank-Kopie fehlgeschlagen: %v", err)
	}
	tmp := filepath.Join(b.BackupDir, "."+name+".tmp")
	if err := writeArchive(b, tmp, snapDir, backupMeta{Format: 3, Users: users, Version: ver, Build: build, Channel: channel,
		Created: now.Format(time.RFC3339), Kind: kind, Audio: audio, DataDir: b.DataDir, MediaDir: b.MediaDir}); err != nil {
		os.Remove(tmp)
		return backupEntry{}, i18n.Errorf("Backup nicht schreibbar: %v", err)
	}
	final := filepath.Join(b.BackupDir, name)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return backupEntry{}, err
	}
	fi, _ := os.Stat(final)
	e := backupEntry{Name: name, Version: ver, Channel: channel, Kind: kind, At: now.UnixMilli(), Audio: audio, Restorable: true}
	if fi != nil {
		e.Size = fi.Size()
	}
	logf("Backup: %s (%d KB)", name, e.Size/1024)
	return e, nil
}

// snapshot legt in dir eine in sich stimmige Kopie aller Datenbanken an: system.db und je Nutzer users/<id>/daten.db
// (verschlüsselt wie das Original; Nutzer mit gesperrten Daten werden als Datei kopiert – sie ist ebenfalls verschlüsselt).
func (a *System) snapshot(dir string) ([]backupUser, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := a.DB.Snapshot(filepath.Join(dir, "system.db")); err != nil {
		return nil, err
	}
	users, err := a.DB.Users()
	if err != nil {
		return nil, err
	}
	var out []backupUser
	for _, u := range users {
		ud := UserDir(dir, u.ID)
		if err := os.MkdirAll(ud, 0o700); err != nil {
			return nil, err
		}
		if app, _ := a.app(u.ID); app != nil {
			err = app.DB.Snapshot(filepath.Join(ud, "daten.db"))
		} else {
			for _, f := range []string{"daten.db", "daten.db-wal"} {
				if err == nil {
					err = copyIfExists(filepath.Join(UserDir(a.Cfg.DataDir, u.ID), f), filepath.Join(ud, f))
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("Nutzer %d: %w", u.ID, err)
		}
		out = append(out, backupUser{ID: u.ID, KeyID: u.KeyID})
	}
	return out, nil
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeArchive packt meta.json, env, die Kopien aus snapDir und (mit Audio) die verschlüsselten Aufnahmen.
func writeArchive(b config.Bootstrap, path, snapDir string, meta backupMeta) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	addBytes := func(name string, b []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: int64(len(b)), ModTime: time.Now()}); err != nil {
			return err
		}
		_, err := tw.Write(b)
		return err
	}
	addFile := func(name, src string) error {
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		fi, err := in.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: fi.Size(), ModTime: fi.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, in)
		return err
	}
	mj, _ := json.MarshalIndent(meta, "", "  ")
	if err := addBytes("meta.json", append(mj, '\n')); err != nil {
		return err
	}
	env := fmt.Sprintf("# Startwerte zum Zeitpunkt des Backups (/etc/pebble-webhook/env)\nLISTEN=%s\nDATA_DIR=%s\nMEDIA_DIR=%s\nBACKUP_DIR=%s\n",
		b.Listen, b.DataDir, b.MediaDir, b.BackupDir)
	if err := addBytes("env", []byte(env)); err != nil {
		return err
	}
	err = filepath.WalkDir(snapDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(snapDir, p)
		if err != nil {
			return err
		}
		return addFile(filepath.ToSlash(rel), p)
	})
	if err != nil {
		return err
	}
	if meta.Audio {
		// Nur verschlüsselte Medien unter MEDIA_DIR/<id>/ (audio/ und bilder/) – Aufnahmen bis 2.x (Klartext am
		// alten Ort) bleiben draußen, bis der Umzug sie verschlüsselt hat.
		skipped := 0
		for _, u := range meta.Users {
			root := filepath.Join(b.MediaDir, strconv.FormatInt(u.ID, 10))
			err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				if err != nil || !d.Type().IsRegular() {
					return err
				}
				if !strings.HasSuffix(p, schluessel.AudioExt) {
					skipped++
					return nil
				}
				rel, err := filepath.Rel(b.MediaDir, p)
				if err != nil {
					return err
				}
				if !mediaFileRe.MatchString("medien/" + filepath.ToSlash(rel)) {
					skipped++ // nicht unter audio/ oder bilder/ (z. B. noch nicht umgezogen)
					return nil
				}
				return addFile("medien/"+filepath.ToSlash(rel), p)
			})
			if err != nil {
				return err
			}
		}
		if skipped > 0 {
			logf("Backup: %d unverschlüsselte oder nicht einsortierte Dateien nicht gesichert", skipped)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// cleanBackups löscht abgelaufene Backups (update: 30 Tage, plan/manuell: BACKUP_RETENTION_DAYS; das jüngste
// Archiv bleibt immer), alte Backup-Ordner nach 30 Tagen und Releases im lokalen Vorrat (DATA_DIR/releases),
// die 30 Tage nicht genutzt wurden.
func (a *System) cleanBackups() {
	cache := filepath.Join(a.Cfg.DataDir, "releases")
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil || !e.IsDir() || time.Since(fi.ModTime()) < updateBackupRetention {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cache, e.Name())); err != nil {
			logf("Release-Vorrat: %s: %v", e.Name(), err)
			continue
		}
		logf("Release-Vorrat: %s gelöscht (30 Tage nicht genutzt)", e.Name())
	}
	// Reste abgebrochener Läufe (.snapshot-*.db, .<name>.tmp) nach einem Tag.
	if left, err := os.ReadDir(a.Cfg.BackupDir); err == nil {
		for _, e := range left {
			fi, err := e.Info()
			if err != nil || !strings.HasPrefix(e.Name(), ".") || e.IsDir() || time.Since(fi.ModTime()) < 24*time.Hour {
				continue
			}
			if err := os.Remove(filepath.Join(a.Cfg.BackupDir, e.Name())); err == nil {
				logf("Backups: Rest %s gelöscht", e.Name())
			}
		}
	}
	keep := time.Duration(a.Cfg.BackupRetentionDays) * 24 * time.Hour
	newest := true
	for _, b := range a.backups() {
		limit, path := keep, a.backupPath(b)
		if b.Kind == "update" {
			limit = updateBackupRetention
		}
		if !b.Legacy && newest {
			newest = false
			continue
		}
		if keep <= 0 || time.Since(time.UnixMilli(b.At)) < limit {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			logf("Backups: %s: %v", b.Name, err)
			continue
		}
		logf("Backups: %s gelöscht (abgelaufen)", b.Name)
	}
}

// backupPath: Ort eines Backups (Archiv in BACKUP_DIR, altes Format als Ordner in DATA_DIR/backup).
func (a *System) backupPath(b backupEntry) string {
	if b.Legacy {
		return filepath.Join(a.Cfg.DataDir, "backup", b.Name)
	}
	return filepath.Join(a.Cfg.BackupDir, b.Name)
}

// cleanupKeepCount: so viele der neuesten Backups (alle Arten zusammen) bleiben beim Aufräumen stehen
// (Nutzervorgabe 24.11.2026).
const cleanupKeepCount = 10

// cleanupPlan: was „Aufräumen“ löschen würde – alle Backups außer den 10 neuesten und alle zwischengespeicherten
// App-Versionen außer der laufenden.
func (a *System) cleanupPlan() (backups, releases []string, backupBytes, releaseBytes int64) {
	for i, b := range a.backups() {
		if i < cleanupKeepCount {
			continue
		}
		backups = append(backups, a.backupPath(b))
		backupBytes += b.Size
	}
	running := strings.Fields(a.Version + " ")[0]
	cache := filepath.Join(a.Cfg.DataDir, "releases")
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == running {
			continue
		}
		p := filepath.Join(cache, e.Name())
		releases = append(releases, p)
		releaseBytes += dirSize(p)
	}
	return
}

// handleCleanup: POST /api/speicher/aufraeumen?was=backups|versionen – löscht den Teil, den cleanupPlan nennt
// (Status → Speicher, je ein Knopf).
func (a *System) handleCleanup(w http.ResponseWriter, r *http.Request) {
	backupMu.Lock()
	defer backupMu.Unlock()
	backups, releases, bb, rb := a.cleanupPlan()
	switch r.URL.Query().Get("was") {
	case "backups":
		releases, rb = nil, 0
	case "versionen":
		backups, bb = nil, 0
	default:
		writeErr(w, 400, "ungueltig", "was=backups oder was=versionen")
		return
	}
	nb, nr := removeAll(backups), removeAll(releases)
	logf("Aufräumen: %d Backups (%d KB), %d App-Versionen (%d KB) gelöscht", nb, bb/1024, nr, rb/1024)
	writeJSON(w, 200, map[string]any{"backups": nb, "versionen": nr, "frei": bb + rb})
}

// removeAll löscht die Pfade und zählt die gelöschten; Fehler landen im Log.
func removeAll(paths []string) int {
	n := 0
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			logf("Aufräumen: %s: %v", p, err)
			continue
		}
		n++
	}
	return n
}

// handleBackupPrune: POST /api/backups/aufraeumen – alle Backups außer den 10 neuesten löschen (Backup-Seite).
func (a *System) handleBackupPrune(w http.ResponseWriter, r *http.Request) {
	backupMu.Lock()
	backups, _, bb, _ := a.cleanupPlan()
	n := removeAll(backups)
	backupMu.Unlock()
	logf("Backups: %d gelöscht (%d KB), die %d neuesten bleiben", n, bb/1024, cleanupKeepCount)
	writeJSON(w, 200, map[string]any{"backups": n, "frei": bb})
}

// handleBackupDelete: DELETE /api/backups/{name} – ein einzelnes Backup löschen (Backup-Seite, Mülleimer je Zeile).
func (a *System) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !archiveNameRe.MatchString(name) && !legacyNameRe.MatchString(name) {
		writeErr(w, 400, "ungueltig", "Backup unbekannt")
		return
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	for _, b := range a.backups() {
		if b.Name != name {
			continue
		}
		if err := os.RemoveAll(a.backupPath(b)); err != nil {
			writeErr(w, 500, "speicher", err.Error())
			return
		}
		logf("Backup: %s gelöscht (von Hand)", name)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	writeErr(w, 404, "fehlt", "Backup nicht gefunden")
}

const backupLastKey = "backup-letzt" // „2026-11-22 03:00 ok <name>“ oder „… fehler <meldung>“

// backupPlan: Einstellungen und letzter Lauf für die Backup-Ansicht.
func (a *System) backupPlan() map[string]any {
	last, _ := a.DB.Get(backupLastKey)
	return map[string]any{"plan": a.Cfg.BackupSchedule, "zeit": a.Cfg.BackupTime, "tage": a.Cfg.BackupRetentionDays,
		"audio": a.Cfg.BackupAudio, "ordner": a.Cfg.BackupDir, "letzt": last}
}

// backupDue: geplantes Backup heute fällig (täglich; wöchentlich montags; monatlich am 1.), ab der Uhrzeit,
// einmal am Tag – läuft der Dienst zur Uhrzeit nicht, wird es später am selben Tag nachgeholt.
func (a *System) backupDue(now time.Time, last string) bool {
	switch a.Cfg.BackupSchedule {
	case "täglich":
	case "wöchentlich":
		if now.Weekday() != time.Monday {
			return false
		}
	case "monatlich":
		if now.Day() != 1 {
			return false
		}
	default:
		return false
	}
	return now.Format("15:04") >= a.Cfg.BackupTime && !strings.HasPrefix(last, now.Format("2006-01-02"))
}

// BackupSchedule läuft im Hintergrund und legt das geplante Backup an.
func (a *System) BackupSchedule(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if last, _ := a.DB.Get(backupLastKey); !a.backupDue(now, last) {
				continue
			}
			stamp := now.Format("2006-01-02 15:04")
			e, err := a.createBackup("plan", a.Cfg.BackupAudio)
			if err != nil {
				logf("Geplantes Backup: %v", err)
				a.DB.Set(backupLastKey, stamp+" fehler "+err.Error())
				continue
			}
			a.DB.Set(backupLastKey, stamp+" ok "+e.Name)
			a.cleanBackups()
		}
	}
}

// handleBackupCreate: „Jetzt sichern“ (Aufnahmen nach Einstellung oder ausdrücklich).
func (a *System) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Audio *bool `json:"audio"`
	}
	readJSON(r, &in)
	audio := a.Cfg.BackupAudio
	if in.Audio != nil {
		audio = *in.Audio
	}
	if _, err := a.createBackup("manuell", audio); err != nil {
		writeErr(w, 500, "backup", err.Error())
		return
	}
	a.cleanBackups()
	a.handleUpdate(w, r)
}

// handleBackupDownload liefert ein Backup-Archiv als Datei.
func (a *System) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !archiveNameRe.MatchString(name) {
		writeErr(w, 400, "ungueltig", "Backup unbekannt")
		return
	}
	path := filepath.Join(a.Cfg.BackupDir, name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, 404, "fehlt", "Backup nicht gefunden")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/gzip")
	http.ServeFile(w, r, path)
}

// handleUpdateRestore: zurück zu einem Backup über den Updater (Daten einspielen, vermerkte Version installieren).
func (a *System) handleUpdateRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if readJSON(r, &in) != nil || (!archiveNameRe.MatchString(in.Name) && !legacyNameRe.MatchString(in.Name)) {
		writeErr(w, 400, "ungueltig", "Backup fehlt")
		return
	}
	if !updateInstallable() && !inDocker() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", noUpdater())
		return
	}
	found := false
	for _, b := range a.backups() {
		found = found || (b.Name == in.Name && b.Restorable)
	}
	if !found {
		writeErr(w, 404, "fehlt", "Backup nicht gefunden oder unvollständig")
		return
	}
	if err := a.requestRestore(in.Name); err != nil {
		writeErr(w, 500, "speicher", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// requestRestore stößt das Einspielen eines Backups über den Updater an (install.sh: Daten ersetzen, Version aus dem
// Backup installieren). Im Docker-Container: vormerken und neu starten; der Start spielt es ein (DockerStart).
func (a *System) requestRestore(name string) error {
	if inDocker() {
		if err := RequestRestore(a.bootstrap(), name); err != nil {
			return err
		}
		restartSoonFor("Wiederherstellen vorgemerkt")
		return nil
	}
	repo := a.Cfg.UpdateRepo
	if repo == "" {
		repo = "-"
	}
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-status"), []byte("requested restore "+name+"\n"), 0o640)
	if err := os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-request"), []byte("restore "+name+" "+repo+"\n"), 0o640); err != nil {
		return i18n.Errorf("Anfrage nicht schreibbar")
	}
	return nil
}
