package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Aufnahmen bis 2.x liegen nach dem Umzug noch unverschlüsselt am alten Ort (AUDIO_DIR/<diktat|planung>/…), damit ein
// Rückweg auf 2.x sie behält. 24 Stunden nach dem Umzug (oder sofort auf Wunsch des Admins) werden sie verschlüsselt in
// den Ordner des Nutzers gelegt (AUDIO_DIR/<id>/<diktat|planung>/JJJJ/MM/<name>.enc) und die Pfade angepasst.

const (
	audioMovedKey  = "audio_umzug" // „fertig“, sobald keine Aufnahme mehr am alten Ort liegt
	audioMoveDelay = 24 * time.Hour
)

// migrateLegacyAudio verschiebt und verschlüsselt die Aufnahmen bis 2.x; force ohne Wartezeit.
func (a *App) migrateLegacyAudio(force bool) (moved int, err error) {
	if a.audioKey == nil {
		return 0, nil
	}
	if done, err := a.DB.Get(audioMovedKey); err != nil || done == "fertig" {
		return 0, err
	}
	at, err := a.DB.Get(migratedAtKey)
	if err != nil {
		return 0, err
	}
	ms, _ := strconv.ParseInt(at, 10, 64)
	if !force && (ms == 0 || time.Since(time.UnixMilli(ms)) < audioMoveDelay) {
		return 0, nil
	}
	jobs, err := a.DB.AudioJobs()
	if err != nil {
		return 0, err
	}
	root := a.audioRoot()
	left := 0
	for _, j := range jobs {
		if strings.HasPrefix(j.AudioPath, root+string(filepath.Separator)) {
			continue
		}
		if err := a.sealLegacyAudio(j); err != nil {
			logf("Aufnahmen bis 2.x: %s: %v", j.AudioPath, err)
			left++
			continue
		}
		moved++
	}
	if left == 0 {
		a.DB.Set(audioMovedKey, "fertig")
		a.removeEmptyDirs(filepath.Join(a.Cfg.AudioDir, "diktat"))
		a.removeEmptyDirs(filepath.Join(a.Cfg.AudioDir, "planung"))
		for _, d := range []string{"diktat", "planung"} {
			os.Remove(filepath.Join(a.Cfg.AudioDir, d)) // nur, wenn leer
		}
	}
	if moved > 0 || left > 0 {
		logf("Aufnahmen bis 2.x: %d verschlüsselt in %s, %d übrig", moved, root, left)
	}
	return moved, nil
}

// sealLegacyAudio: eine Aufnahme lesen, verschlüsselt neu schreiben (fsync), Pfad setzen, alte Datei löschen.
func (a *App) sealLegacyAudio(j *store.Job) error {
	data, err := os.ReadFile(j.AudioPath)
	if os.IsNotExist(err) {
		return nil // fehlt schon vorher: bleibt erkennbar (Abspielen meldet es)
	}
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(a.Cfg.AudioDir, j.AudioPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(j.AudioPath)
	}
	rest := filepath.ToSlash(rel)
	for _, sub := range []string{"diktat/", "planung/"} {
		rest = strings.TrimPrefix(rest, sub)
	}
	name := filepath.Base(rest)
	sealed, err := schluessel.SealAudio(a.audioKey, name, data)
	if err != nil {
		return err
	}
	dst := filepath.Join(a.audioRoot(), audioSub(j.Mode), filepath.FromSlash(rest)) + schluessel.AudioExt
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	_, err = f.Write(sealed)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := a.DB.SetAudioPath(j.ID, dst); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Remove(j.AudioPath)
}
