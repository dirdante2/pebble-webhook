package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/i18n"
)

// Backups: ein Archiv pebble-<version>-<kanal>-<zeit>-<art>[-audio].tar.gz in BACKUP_DIR mit
//   meta.json  Version, Build, Kanal, Zeitpunkt, Art, Audio ja/nein
//   pebble.db  Datenbank samt aller Einstellungen (in sich stimmige Kopie)
//   env        Startwerte (LISTEN, DATA_DIR, AUDIO_DIR, BACKUP_DIR)
//   audio/…    Aufnahmen (nur mit BACKUP_AUDIO oder Häkchen)
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
	Format   int    `json:"format"`
	Version  string `json:"version"`
	Build    string `json:"build"`
	Channel  string `json:"kanal"`
	Created  string `json:"erstellt"`
	Kind     string `json:"art"`
	Audio    bool   `json:"audio"`
	DataDir  string `json:"data_dir"`
	AudioDir string `json:"audio_dir"`
}

var backupMu sync.Mutex

// backups: alle Backups, neueste zuerst (Archive in BACKUP_DIR und Ordner im alten Format).
func (a *App) backups() []backupEntry {
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
		db, errDB := os.Stat(filepath.Join(dir, "pebble.db"))
		channel := ""
		if b, err := os.ReadFile(filepath.Join(dir, "kanal")); err == nil {
			channel = strings.TrimSpace(string(b))
		} else if _, ch, ok := parseVersion(m[1]); ok && ch != "stable" {
			channel = ch
		}
		var size int64
		if errDB == nil {
			size = db.Size()
		}
		out = append(out, backupEntry{Name: e.Name(), Version: m[1], Channel: channel, Kind: "update", At: at.UnixMilli(),
			Size: size, Restorable: errBin == nil && errDB == nil, Legacy: true})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].At > out[k].At })
	return out
}

// createBackup schreibt ein Archiv nach BACKUP_DIR (erst unter .tmp-Namen, dann umbenannt).
func (a *App) createBackup(kind string, audio bool) (backupEntry, error) {
	backupMu.Lock()
	defer backupMu.Unlock()
	now := time.Now()
	ver := strings.Fields(a.Version + " ")[0]
	if ver == "" {
		ver = "unbekannt"
	}
	channel := a.installedChannel()
	name := fmt.Sprintf("pebble-%s-%s-%s-%s", ver, channel, now.Format("20060102-150405"), kind)
	if audio {
		name += "-audio"
	}
	name += ".tar.gz"
	if !archiveNameRe.MatchString(name) {
		return backupEntry{}, i18n.Errorf("Backup-Name ungültig: %s", name)
	}
	if err := os.MkdirAll(a.Cfg.BackupDir, 0o750); err != nil {
		return backupEntry{}, i18n.Errorf("Backup-Ordner %s nicht anlegbar: %v", a.Cfg.BackupDir, err)
	}
	snap := filepath.Join(a.Cfg.BackupDir, ".snapshot-"+now.Format("20060102-150405")+".db")
	os.Remove(snap)
	defer os.Remove(snap)
	if err := a.DB.Snapshot(snap); err != nil {
		return backupEntry{}, i18n.Errorf("Datenbank-Kopie fehlgeschlagen: %v", err)
	}
	tmp := filepath.Join(a.Cfg.BackupDir, "."+name+".tmp")
	if err := a.writeArchive(tmp, snap, backupMeta{Format: 1, Version: ver, Build: a.Version, Channel: channel,
		Created: now.Format(time.RFC3339), Kind: kind, Audio: audio, DataDir: a.Cfg.DataDir, AudioDir: a.Cfg.AudioDir}); err != nil {
		os.Remove(tmp)
		return backupEntry{}, i18n.Errorf("Backup nicht schreibbar: %v", err)
	}
	final := filepath.Join(a.Cfg.BackupDir, name)
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

func (a *App) writeArchive(path, dbFile string, meta backupMeta) (err error) {
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
	env := fmt.Sprintf("# Startwerte zum Zeitpunkt des Backups (/etc/pebble-webhook/env)\nLISTEN=%s\nDATA_DIR=%s\nAUDIO_DIR=%s\nBACKUP_DIR=%s\n",
		a.Cfg.Listen, a.Cfg.DataDir, a.Cfg.AudioDir, a.Cfg.BackupDir)
	if err := addBytes("env", []byte(env)); err != nil {
		return err
	}
	if err := addFile("pebble.db", dbFile); err != nil {
		return err
	}
	if meta.Audio {
		err := filepath.WalkDir(a.Cfg.AudioDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(a.Cfg.AudioDir, p)
			if err != nil {
				return err
			}
			return addFile("audio/"+filepath.ToSlash(rel), p)
		})
		if err != nil {
			return err
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
func (a *App) cleanBackups() {
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
	keep := time.Duration(a.Cfg.BackupRetentionDays) * 24 * time.Hour
	newest := true
	for _, b := range a.backups() {
		limit, path := keep, filepath.Join(a.Cfg.BackupDir, b.Name)
		if b.Kind == "update" {
			limit = updateBackupRetention
		}
		if b.Legacy {
			path = filepath.Join(a.Cfg.DataDir, "backup", b.Name)
		} else if newest {
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

const backupLastKey = "backup-letzt" // „2026-11-22 03:00 ok <name>“ oder „… fehler <meldung>“

// backupPlan: Einstellungen und letzter Lauf für die Backup-Ansicht.
func (a *App) backupPlan() map[string]any {
	last, _ := a.DB.Get(backupLastKey)
	return map[string]any{"plan": a.Cfg.BackupSchedule, "zeit": a.Cfg.BackupTime, "tage": a.Cfg.BackupRetentionDays,
		"audio": a.Cfg.BackupAudio, "ordner": a.Cfg.BackupDir, "letzt": last}
}

// backupDue: geplantes Backup heute fällig (täglich; wöchentlich montags; monatlich am 1.), ab der Uhrzeit,
// einmal am Tag – läuft der Dienst zur Uhrzeit nicht, wird es später am selben Tag nachgeholt.
func (a *App) backupDue(now time.Time, last string) bool {
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
func (a *App) BackupSchedule(ctx context.Context) {
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
func (a *App) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
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
func (a *App) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
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
func (a *App) handleUpdateRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if readJSON(r, &in) != nil || (!archiveNameRe.MatchString(in.Name) && !legacyNameRe.MatchString(in.Name)) {
		writeErr(w, 400, "ungueltig", "Backup fehlt")
		return
	}
	if !updateInstallable() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", "Kein Updater installiert – Installation mit install.sh nötig")
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
	repo := a.Cfg.UpdateRepo
	if repo == "" {
		repo = "-"
	}
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-status"), []byte("requested restore "+in.Name+"\n"), 0o640)
	if err := os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-request"), []byte("restore "+in.Name+" "+repo+"\n"), 0o640); err != nil {
		writeErr(w, 500, "speicher", "Anfrage nicht schreibbar")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
