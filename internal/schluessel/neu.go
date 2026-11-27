package schluessel

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Neuer Datenschlüssel während der Neuverschlüsselung: KEY_DIR/<id>.key.neu hält den neuen DEK und den neuen Code,
// bis alle Daten umgeschlüsselt sind. Nach einem Abbruch (Absturz, Neustart) setzt der Dienst damit fort.

type pendingFile struct {
	V    int    `json:"v"`
	User int64  `json:"user"`
	Key  string `json:"key"`
	Code string `json:"code"`
}

// PendingPath: Datei des neuen Schlüssels während der Neuverschlüsselung.
func PendingPath(dir string, uid int64) string {
	return filepath.Join(dir, strconv.FormatInt(uid, 10)+".key.neu")
}

// HasPending: für den Nutzer läuft (oder hängt) eine Neuverschlüsselung.
func HasPending(dir string, uid int64) bool {
	_, err := os.Lstat(PendingPath(dir, uid))
	return err == nil
}

// WritePending legt den neuen Schlüssel samt Code atomar ab (0600; tmp, fsync, rename).
func WritePending(dir string, uid int64, k DEK, code string) error {
	data, _ := json.Marshal(pendingFile{V: 1, User: uid, Key: hex.EncodeToString(k[:]), Code: code})
	return writeAtomic(PendingPath(dir, uid), append(data, '\n'))
}

// ReadPending liest den neuen Schlüssel und Code.
func ReadPending(dir string, uid int64) (DEK, string, error) {
	var k DEK
	data, err := os.ReadFile(PendingPath(dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return k, "", ErrMissing
	}
	if err != nil {
		return k, "", err
	}
	var f pendingFile
	if err := json.Unmarshal(data, &f); err != nil || f.V != 1 || f.User != uid {
		return k, "", fmt.Errorf("Datei %s ungültig", PendingPath(dir, uid))
	}
	raw, err := hex.DecodeString(f.Key)
	if err != nil || len(raw) != len(k) {
		return k, "", fmt.Errorf("Datei %s ungültig", PendingPath(dir, uid))
	}
	if _, err := normCode(f.Code); err != nil {
		return k, "", fmt.Errorf("Datei %s ungültig", PendingPath(dir, uid))
	}
	copy(k[:], raw)
	return k, f.Code, nil
}

// RemovePending: Neuverschlüsselung abgeschlossen.
func RemovePending(dir string, uid int64) error {
	err := os.Remove(PendingPath(dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		syncDir(dir)
	}
	return err
}

// ResealAudio schlüsselt eine mit SealAudio geschriebene Datei vom alten auf den neuen Schlüssel um. done = die Datei
// ist schon mit dem neuen Schlüssel verschlüsselt (Fortsetzen nach Abbruch), out ist dann nil.
func ResealAudio(oldKey, newKey []byte, name string, data []byte) (out []byte, done bool, err error) {
	plain, err := OpenAudio(oldKey, name, data)
	if err != nil {
		if _, err2 := OpenAudio(newKey, name, data); err2 == nil {
			return nil, true, nil
		}
		return nil, false, err
	}
	out, err = SealAudio(newKey, name, plain)
	return out, false, err
}

// writeAtomic schreibt path über eine tmp-Datei (0600, fsync, rename, Ordner-fsync).
func writeAtomic(p string, data []byte) error {
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
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
		return err
	}
	syncDir(filepath.Dir(p))
	return nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
