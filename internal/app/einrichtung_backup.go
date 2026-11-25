package app

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Ersteinrichtung mit Backup: statt neu einzurichten ein heruntergeladenes Backup hochladen und gleich die
// Wiederherstellungscodes eingeben. Die Codes öffnen die Schlüssel der Nutzer (und über den ersten Admin bzw. den
// Systemcode den Systemschlüssel); die Schlüsseldateien liegen danach schon in KEY_DIR, wenn das eingespielte Backup
// startet. Ablauf:
// 1. POST /api/ersteinrichtung/backup?code=&name= (Archiv als Rumpf) → prüfen, ablegen, Nutzer des Backups zeigen.
// 2. POST /api/ersteinrichtung/backup/wiederherstellen {code, name, codes{id: code}, systemcode} → Codes prüfen,
//    Schlüssel schreiben, Einspielen anstoßen (install.sh bzw. Container-Neustart).

// withArchiveDB entpackt system.db aus dem Archiv in eine Zwischendatei und öffnet sie.
func withArchiveDB(p, tmpDir string, fn func(*store.System) error) error {
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
		if err != nil {
			return errors.New(i18n.T("Backup ohne system.db (zu alte Version)"))
		}
		if h.Name != "system.db" {
			continue
		}
		dir, err := os.MkdirTemp(tmpDir, ".einrichtung-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		dst := filepath.Join(dir, "system.db")
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return err
		}
		db, err := store.OpenSystem(dst)
		if err != nil {
			return err
		}
		defer db.Close()
		return fn(db)
	}
}

func archiveSystemKey(db *store.System) (systemKeyInfo, bool) {
	s := &System{DB: db}
	return s.systemKeyInfo()
}

func (s *SetupServer) handleBackupUpload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-App") != "1" || !s.codeOK(r, r.URL.Query().Get("code")) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	if users, _ := s.DB.Users(); len(users) > 0 {
		writeErr(w, http.StatusConflict, "eingerichtet", "Schon eingerichtet – Backups unter Status → Backups einspielen")
		return
	}
	name, ok := receiveBackup(w, r, s.Boot.BackupDir)
	if !ok {
		return
	}
	s.describeBackup(w, name)
}

// describeBackup: Nutzer und Systemschlüssel des Backups – für die Eingabe der Codes.
func (s *SetupServer) describeBackup(w http.ResponseWriter, name string) {
	var users []map[string]any
	var sys systemKeyInfo
	err := withArchiveDB(filepath.Join(s.Boot.BackupDir, name), s.Boot.BackupDir, func(db *store.System) error {
		list, err := db.Users()
		if err != nil {
			return err
		}
		for _, u := range list {
			users = append(users, map[string]any{"id": u.ID, "name": u.Name, "rolle": u.Role, "code_moeglich": u.KeyWrap != ""})
		}
		sys, _ = archiveSystemKey(db)
		return nil
	})
	if err != nil {
		os.Remove(filepath.Join(s.Boot.BackupDir, name))
		writeErr(w, 400, "ungueltig", i18n.Tf("Kein gültiges Backup: %s", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "nutzer": users, "system": map[string]any{"art": sys.Art, "uid": sys.UID}})
}

func (s *SetupServer) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code       string            `json:"code"`
		Name       string            `json:"name"`
		Codes      map[string]string `json:"codes"`
		Systemcode string            `json:"systemcode"`
		Adresse    string            `json:"adresse"` // neue öffentliche Adresse (leer = die des Backups)
	}
	if readJSON(r, &in) != nil || r.Header.Get("X-App") != "1" || !s.codeOK(r, in.Code) {
		writeErr(w, http.StatusForbidden, "code", "Nicht erlaubt")
		return
	}
	if users, _ := s.DB.Users(); len(users) > 0 {
		writeErr(w, http.StatusConflict, "eingerichtet", "Schon eingerichtet – Backups unter Status → Backups einspielen")
		return
	}
	if archiveNameRe.FindStringSubmatch(in.Name) == nil {
		writeErr(w, 400, "ungueltig", "Backup fehlt")
		return
	}
	p := filepath.Join(s.Boot.BackupDir, in.Name)
	type keyFile struct {
		uid int64
		dek schluessel.DEK
	}
	var keys []keyFile
	var sysKey []byte
	var wrong []string
	err := withArchiveDB(p, s.Boot.BackupDir, func(db *store.System) error {
		deks := map[int64]schluessel.DEK{}
		for id, code := range in.Codes {
			if code == "" {
				continue
			}
			uid, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return err
			}
			u, err := db.User(uid)
			if err != nil {
				return err
			}
			dek, err := schluessel.Unwrap(code, u.KeyWrap, u.ID)
			if err != nil || dek.ID() != u.KeyID {
				wrong = append(wrong, u.Name)
				continue
			}
			deks[uid] = dek
			keys = append(keys, keyFile{uid, dek})
		}
		info, ok := archiveSystemKey(db)
		switch {
		case !ok:
		case info.Art == "code" && in.Systemcode != "":
			k, err := schluessel.UnwrapSystemCode(in.Systemcode, info.Wrap)
			if err != nil || schluessel.SystemKeyID(k) != info.KeyID {
				wrong = append(wrong, i18n.T("Systemschlüssel"))
			} else {
				sysKey = k
			}
		case info.Art == "nutzer":
			if dek, ok := deks[info.UID]; ok {
				if k, err := schluessel.UnbindSystemKey(info.Wrap, dek); err == nil && schluessel.SystemKeyID(k) == info.KeyID {
					sysKey = k
				}
			}
		}
		return nil
	})
	if err != nil {
		writeErr(w, 400, "ungueltig", i18n.Tf("Kein gültiges Backup: %s", err.Error()))
		return
	}
	if len(wrong) > 0 {
		writeJSON(w, http.StatusForbidden, map[string]any{"code": "code", "fehler": i18n.T("Code falsch") + ": " + strings.Join(wrong, ", "), "falsch": wrong})
		return
	}
	// Schlüssel ablegen – nie einen vorhandenen, anderen überschreiben.
	for _, k := range keys {
		if old, err := schluessel.Read(s.Boot.KeyDir, k.uid, ""); err == nil && old != k.dek {
			writeErr(w, http.StatusConflict, "schluessel", i18n.Tf("Für Nutzer %d liegt schon ein anderer Schlüssel", k.uid))
			return
		}
	}
	for _, k := range keys {
		if err := schluessel.Write(s.Boot.KeyDir, k.uid, k.dek); err != nil {
			writeErr(w, 500, "schluessel", "Schlüsseldatei nicht geschrieben")
			return
		}
	}
	if sysKey != nil {
		if old, err := schluessel.SystemKey(s.Boot.KeyDir, false); err == nil && string(old) != string(sysKey) {
			writeErr(w, http.StatusConflict, "schluessel", "Es liegt schon ein anderer Systemschlüssel")
			return
		}
		if err := schluessel.WriteSystemKey(s.Boot.KeyDir, sysKey); err != nil {
			writeErr(w, 500, "schluessel", "Systemschlüssel nicht geschrieben")
			return
		}
	}
	in.Adresse = strings.TrimRight(strings.TrimSpace(in.Adresse), "/")
	if in.Adresse != "" && !publicURLRe.MatchString(in.Adresse) {
		writeErr(w, 400, "adresse", "Öffentliche Adresse ungültig (https://name.domain)")
		return
	}
	if err := requestRestoreAt(s.Boot, in.Name, in.Adresse); err != nil {
		writeErr(w, 500, "speicher", err.Error())
		return
	}
	logf("Ersteinrichtung: Backup %s wird eingespielt (%d Schlüssel, Systemschlüssel %v)", in.Name, len(keys), sysKey != nil)
	writeJSON(w, 200, map[string]any{"ok": true, "schluessel": len(keys), "system": sysKey != nil})
}
