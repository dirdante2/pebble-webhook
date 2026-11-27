package app

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Neuer Wiederherstellungscode mit neuem Datenschlüssel: Der Nutzer bekommt einen neuen DEK, alle Aufnahmen und Bilder
// sowie seine Datenbank werden damit neu verschlüsselt, der alte Code (und der alte Schlüssel) öffnen danach nichts
// Aktuelles mehr. Ablauf und Fortsetzen nach Abbruch: ABLAUF.md, „Neuer Wiederherstellungscode“.

const lockedRekey = "umschluesselung" // Grund der Sperre, solange die Daten neu verschlüsselt werden

// rekeyState: Fortschritt der Neuverschlüsselung eines Nutzers (nur im Speicher; nach Neustart neu gezählt).
type rekeyState struct {
	Phase      string `json:"phase"` // vorbereiten | medien | datenbank | abschluss | fertig | fehler
	Done       int    `json:"erledigt"`
	Total      int    `json:"gesamt"`
	Unreadable int    `json:"unlesbar"`
	Err        string `json:"fehler,omitempty"`
	Running    bool   `json:"laeuft"`
}

func (s *System) rekeyGet(uid int64) (rekeyState, bool) {
	s.rekeyMu.Lock()
	defer s.rekeyMu.Unlock()
	st, ok := s.rekey[uid]
	if !ok {
		return rekeyState{}, false
	}
	return *st, true
}

func (s *System) rekeySet(uid int64, f func(*rekeyState)) {
	s.rekeyMu.Lock()
	defer s.rekeyMu.Unlock()
	st := s.rekey[uid]
	if st == nil {
		st = &rekeyState{}
		s.rekey[uid] = st
	}
	f(st)
}

// handleRecoveryNew startet die Neuverschlüsselung mit neuem Code. Nur kurz nach einer Anmeldung mit Passkey –
// eine gestohlene Sitzung allein reicht dafür nicht.
const recoveryFresh = 10 * time.Minute

func (a *App) handleRecoveryNew(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || time.Since(time.UnixMilli(a.System.DB.SessionCreated(c.Value))) > recoveryFresh {
		writeErr(w, http.StatusForbidden, "frisch", "Zur Sicherheit erst ab- und wieder anmelden, dann innerhalb von 10 Minuten den neuen Code erzeugen")
		return
	}
	if st, ok := a.rekeyGet(a.UserID); (ok && st.Running) || schluessel.HasPending(a.Cfg.KeyDir, a.UserID) {
		writeErr(w, http.StatusConflict, "laeuft", "Die Neuverschlüsselung läuft schon")
		return
	}
	dek, err := schluessel.New()
	if err != nil {
		writeErr(w, 500, "zufall", "Schlüssel nicht erzeugt")
		return
	}
	code, err := schluessel.NewCode()
	if err != nil {
		writeErr(w, 500, "zufall", "Code nicht erzeugt")
		return
	}
	if err := schluessel.WritePending(a.Cfg.KeyDir, a.UserID, dek, code); err != nil {
		logf("Nutzer %d: neuer Schlüssel nicht abgelegt: %v", a.UserID, err)
		writeErr(w, 500, "schluessel", "Neuer Schlüssel nicht gespeichert")
		return
	}
	a.rekeySet(a.UserID, func(st *rekeyState) { *st = rekeyState{Phase: "vorbereiten", Running: true} })
	go a.System.runRekey(a.UserID)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// handleRekeyState: Fortschritt der Neuverschlüsselung (auch erreichbar, während die Daten gesperrt sind).
func (s *System) handleRekeyState(w http.ResponseWriter, r *http.Request, u *store.User) {
	st, ok := s.rekeyGet(u.ID)
	if !ok && schluessel.HasPending(s.Cfg.KeyDir, u.ID) {
		st = rekeyState{Phase: "fehler", Err: "Neuverschlüsselung unterbrochen"}
	}
	writeJSON(w, 200, st)
}

// handleRekeyResume setzt eine abgebrochene Neuverschlüsselung fort.
func (s *System) handleRekeyResume(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !schluessel.HasPending(s.Cfg.KeyDir, u.ID) {
		writeErr(w, http.StatusConflict, "keine", "Keine unterbrochene Neuverschlüsselung")
		return
	}
	started := false
	s.rekeySet(u.ID, func(st *rekeyState) {
		if !st.Running {
			*st = rekeyState{Phase: "vorbereiten", Running: true}
			started = true
		}
	})
	if started {
		go s.runRekey(u.ID)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// resumeRekeys (beim Start, vor dem Öffnen der Nutzer): unterbrochene Neuverschlüsselungen fortsetzen.
func (s *System) resumeRekeys(users []*store.User) {
	for _, u := range users {
		if u.LockedAt > 0 || !schluessel.HasPending(s.Cfg.KeyDir, u.ID) {
			continue
		}
		logf("Nutzer %d: unterbrochene Neuverschlüsselung wird fortgesetzt", u.ID)
		s.setLocked(u.ID, lockedRekey)
		s.rekeySet(u.ID, func(st *rekeyState) { *st = rekeyState{Phase: "vorbereiten", Running: true} })
		go s.runRekey(u.ID)
	}
}

// runRekey: Daten des Nutzers schließen, neu verschlüsseln, mit dem neuen Schlüssel wieder öffnen. Backups warten
// so lange (kein Archiv mit halb umgeschlüsselten Dateien).
func (s *System) runRekey(uid int64) {
	backupMu.Lock()
	s.lifeMu.Lock()
	s.closeUser(uid, lockedRekey)
	s.lifeMu.Unlock()
	err := s.rekeyUser(uid)
	backupMu.Unlock()
	if err != nil {
		logf("Nutzer %d: Neuverschlüsselung abgebrochen: %v", uid, err)
		s.setLocked(uid, lockedRekey)
		s.rekeySet(uid, func(st *rekeyState) { st.Phase, st.Err, st.Running = "fehler", err.Error(), false })
		return
	}
	st, _ := s.rekeyGet(uid)
	logf("Nutzer %d: Daten neu verschlüsselt (%d Dateien, %d nicht lesbar), neuer Wiederherstellungscode gilt", uid, st.Total-1, st.Unreadable)
	// Ältere Backups öffnet nur der alte Code: gleich ein aktuelles anlegen.
	if _, err := s.createBackup("manuell", s.Cfg.BackupAudio); err != nil {
		logf("Backup nach Neuverschlüsselung: %v", err)
	}
}

// rekeyUser führt die Schritte aus; jeder lässt sich nach einem Abbruch wiederholen.
//  1. Aufnahmen und Bilder (*.enc unter MEDIA_DIR/<id>): mit altem Schlüssel öffnen, mit neuem schreiben (schon neue
//     werden übersprungen, nicht lesbare gezählt und gelassen).
//  2. Datenbank: VACUUM INTO daten.db.neu mit neuem Schlüssel, dann gegen daten.db getauscht.
//  3. system.db (key_wrap mit neuem Code), Schlüsseldatei, Bindung des Systemschlüssels, Code als „offen“ in daten.db.
//  4. <id>.key.neu löschen, Daten öffnen.
func (s *System) rekeyUser(uid int64) error {
	newK, code, err := schluessel.ReadPending(s.Cfg.KeyDir, uid)
	if err != nil {
		return err
	}
	oldK, err := schluessel.Read(s.Cfg.KeyDir, uid, "")
	if err != nil {
		return fmt.Errorf("bisheriger Schlüssel: %w", err)
	}
	if oldK.ID() != newK.ID() { // sonst ist die Schlüsseldatei schon neu: Daten fertig, nur noch Abschluss
		files, err := rekeyFiles(filepath.Join(s.Cfg.MediaDir, strconv.FormatInt(uid, 10)))
		if err != nil {
			return fmt.Errorf("Medien: %w", err)
		}
		s.rekeySet(uid, func(st *rekeyState) { st.Phase, st.Total = "medien", len(files)+1 })
		for _, p := range files {
			unreadable, err := resealFile(p, oldK.AudioKey(), newK.AudioKey())
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			if unreadable {
				logf("Nutzer %d: %s mit keinem der beiden Schlüssel lesbar – bleibt unverändert", uid, p)
			}
			s.rekeySet(uid, func(st *rekeyState) {
				st.Done++
				if unreadable {
					st.Unreadable++
				}
			})
		}
		s.rekeySet(uid, func(st *rekeyState) { st.Phase = "datenbank" })
		if err := rekeyDB(UserDB(s.Cfg.DataDir, uid), oldK, newK); err != nil {
			return fmt.Errorf("Datenbank: %w", err)
		}
		s.rekeySet(uid, func(st *rekeyState) { st.Done++ })
	}
	s.rekeySet(uid, func(st *rekeyState) { st.Phase = "abschluss" })
	wrap, err := schluessel.Wrap(code, uid, newK)
	if err != nil {
		return err
	}
	if err := s.DB.SetUserKey(uid, newK.ID(), wrap); err != nil {
		return err
	}
	if err := schluessel.Write(s.Cfg.KeyDir, uid, newK); err != nil {
		return fmt.Errorf("Schlüsseldatei: %w", err)
	}
	s.rebindSystemKey(uid, oldK, newK)
	db, err := store.OpenUser(UserDB(s.Cfg.DataDir, uid), newK.DBKey(), uid)
	if err != nil {
		return err
	}
	err = db.Set(recoveryPendingKey, code)
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := schluessel.RemovePending(s.Cfg.KeyDir, uid); err != nil {
		return err
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	u, err := s.DB.User(uid)
	if err != nil {
		return err
	}
	s.rekeySet(uid, func(st *rekeyState) { st.Phase, st.Running = "fertig", false })
	if _, err := s.openUser(u); err != nil {
		return fmt.Errorf("Daten mit neuem Schlüssel nicht geöffnet: %w", err)
	}
	return nil
}

// rekeyFiles: verschlüsselte Dateien unter root; Reste eines abgebrochenen Laufs (*.neu.tmp) werden entfernt.
func rekeyFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == root {
				return filepath.SkipDir
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		switch {
		case strings.HasSuffix(p, schluessel.AudioExt+".neu.tmp"):
			os.Remove(p)
		case strings.HasSuffix(p, schluessel.AudioExt):
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// resealFile schreibt eine Datei mit dem neuen Schlüssel (tmp, fsync, rename). unreadable = mit keinem Schlüssel lesbar.
func resealFile(p string, oldKey, newKey []byte) (unreadable bool, err error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return false, err
	}
	out, done, err := schluessel.ResealAudio(oldKey, newKey, strings.TrimSuffix(filepath.Base(p), schluessel.AudioExt), data)
	if err != nil {
		return true, nil
	}
	if done {
		return false, nil
	}
	fi, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	tmp := p + ".neu.tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return false, err
	}
	_, err = f.Write(out)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, p)
	}
	if err != nil {
		os.Remove(tmp)
		return false, err
	}
	os.Chtimes(p, fi.ModTime(), fi.ModTime()) // Zeitpunkt der Datei bleibt (Eingang beim Import)
	return false, nil
}

// rekeyDB: daten.db mit neuem Schlüssel. Fertige Kopie daten.db.neu (nach Abbruch) wird nur noch getauscht.
func rekeyDB(path string, oldK, newK schluessel.DEK) error {
	neu := path + ".neu"
	os.Remove(neu + ".tmp")
	if _, err := os.Stat(neu); err != nil {
		if store.CheckKey(path, oldK.DBKey()) != nil {
			if store.CheckKey(path, newK.DBKey()) == nil {
				return nil // schon getauscht
			}
			return errors.New("daten.db lässt sich mit keinem der beiden Schlüssel öffnen")
		}
		if err := store.Rekey(path, oldK.DBKey(), neu+".tmp", newK.DBKey()); err != nil {
			os.Remove(neu + ".tmp")
			return err
		}
		if err := os.Rename(neu+".tmp", neu); err != nil {
			return err
		}
	}
	// Altes WAL/SHM erst weg (sonst spielte SQLite es in die neue Datei), dann tauschen.
	for _, suf := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suf); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(neu, path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// rebindSystemKey: ist der Systemschlüssel an diesen Nutzer gebunden, wird er an den neuen Schlüssel gebunden.
func (s *System) rebindSystemKey(uid int64, oldK, newK schluessel.DEK) {
	info, ok := s.systemKeyInfo()
	if !ok || info.Art != "nutzer" || info.UID != uid {
		return
	}
	if _, err := schluessel.UnbindSystemKey(info.Wrap, newK); err == nil {
		return // schon neu gebunden
	}
	key, err := schluessel.UnbindSystemKey(info.Wrap, oldK)
	if err != nil {
		logf("Systemschlüssel: Bindung an Nutzer %d nicht lesbar: %v", uid, err)
		return
	}
	wrap, err := schluessel.BindSystemKey(key, newK)
	if err == nil {
		info.Wrap = wrap
		err = s.saveSystemKeyInfo(info)
	}
	if err != nil {
		logf("Systemschlüssel: nicht an den neuen Schlüssel von Nutzer %d gebunden: %v", uid, err)
	}
}
