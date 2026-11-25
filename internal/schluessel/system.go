package schluessel

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Systemschlüssel (KEY_DIR/system.key, nie im Backup): verschlüsselt die Geheimnisse des Systems (API-Schlüssel,
// Web-Push-Schlüssel) in der Kopie von system.db, die ins Backup geht. Auf demselben Server liest der Dienst sie
// beim Start zurück; auf einem neuen Server ohne Schlüssel fehlen sie und werden neu eingegeben bzw. erzeugt.

const sealedPrefix = "enc1:"

func systemKeyPath(dir string) string { return filepath.Join(dir, "system.key") }

// SystemKey liest den Systemschlüssel; create legt ihn an, wenn er fehlt.
func SystemKey(dir string, create bool) ([]byte, error) {
	p := systemKeyPath(dir)
	if b, err := os.ReadFile(p); err == nil {
		k, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(k) != 32 {
			return nil, ErrWrongKey
		}
		return k, nil
	} else if !os.IsNotExist(err) || !create {
		if os.IsNotExist(err) {
			return nil, ErrMissing
		}
		return nil, err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, err = f.WriteString(hex.EncodeToString(k) + "\n")
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Link(tmp, p) // nie einen vorhandenen überschreiben (gleichzeitiger Start)
	}
	os.Remove(tmp)
	if err != nil {
		if os.IsExist(err) {
			return SystemKey(dir, false)
		}
		return nil, err
	}
	return k, nil
}

// Sealed: Wert ist mit Seal verschlüsselt.
func Sealed(v string) bool { return strings.HasPrefix(v, sealedPrefix) }

// Seal verschlüsselt einen Wert (AES-256-GCM, der Name ist zugeordnete Angabe: Werte lassen sich nicht vertauschen).
func Seal(key []byte, name, value string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(value), []byte(name))), nil
}

// Open entschlüsselt einen mit Seal verschlüsselten Wert.
func Open(key []byte, name, sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, sealedPrefix))
	if err != nil {
		return "", err
	}
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", errors.New("zu kurz")
	}
	out, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte(name))
	if err != nil {
		return "", ErrWrongKey
	}
	return string(out), nil
}

// WriteSystemKey legt den Systemschlüssel ab (nach dem Wiederherstellen auf einem neuen Server).
func WriteSystemKey(dir string, key []byte) error {
	p := systemKeyPath(dir)
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(hex.EncodeToString(key) + "\n")
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
	}
	return err
}

// SystemKeyID: kurze Kennung des Systemschlüssels (erkennt einen falschen).
func SystemKeyID(key []byte) string {
	var k DEK
	copy(k[:], key)
	return k.ID()
}

// BindSystemKey wickelt den Systemschlüssel mit dem Datenschlüssel eines Nutzers ein: wer dessen
// Wiederherstellungscode hat (und damit den DEK), bekommt auch den Systemschlüssel zurück – ein Code für beides.
func BindSystemKey(sys []byte, k DEK) (string, error) {
	g, err := gcm(k.derive("systemschluessel"))
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(g.Seal(nonce, nonce, sys, []byte("system"))), nil
}

// UnbindSystemKey holt den Systemschlüssel mit dem Datenschlüssel des Nutzers zurück.
func UnbindSystemKey(wrapped string, k DEK) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, err
	}
	g, err := gcm(k.derive("systemschluessel"))
	if err != nil {
		return nil, err
	}
	if len(raw) < g.NonceSize() {
		return nil, ErrWrongKey
	}
	out, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte("system"))
	if err != nil || len(out) != 32 {
		return nil, ErrWrongKey
	}
	return out, nil
}

// WrapSystemCode / UnwrapSystemCode: eigener Wiederherstellungscode nur für den Systemschlüssel.
func WrapSystemCode(code string, sys []byte) (string, error) {
	var k DEK
	copy(k[:], sys)
	return Wrap(code, 0, k)
}

func UnwrapSystemCode(code, wrapped string) ([]byte, error) {
	k, err := Unwrap(code, wrapped, 0)
	if err != nil {
		return nil, err
	}
	return k[:], nil
}
