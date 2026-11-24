// Package schluessel verwaltet die Schlüssel je Nutzer: Datenschlüssel (DEK) in einer Datei unter
// /etc/pebble-webhook/keys (nie im Backup), daraus abgeleitet der Schlüssel der Datenbank und der Aufnahmen,
// dazu ein Wiederherstellungscode, mit dem der DEK eingewickelt in system.db liegt (Restore auf neuer Maschine).
package schluessel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DEK: zufälliger Datenschlüssel eines Nutzers.
type DEK [32]byte

var (
	ErrMissing  = errors.New("Schlüsseldatei fehlt")
	ErrWrongKey = errors.New("Schlüssel passt nicht")
	ErrBadCode  = errors.New("Wiederherstellungscode falsch")
)

func New() (DEK, error) {
	var k DEK
	_, err := rand.Read(k[:])
	return k, err
}

func (k DEK) derive(label string) []byte {
	out, err := hkdf.Key(sha256.New, k[:], nil, "pebble-webhook "+label, 32)
	if err != nil {
		panic(err) // nur bei unmöglicher Länge
	}
	return out
}

// DBKey: Schlüssel der Datenbank daten.db (Adiantum).
func (k DEK) DBKey() []byte { return k.derive("sqlite") }

// AudioKey: Schlüssel der Aufnahmen (AES-256-GCM).
func (k DEK) AudioKey() []byte { return k.derive("audio") }

// ID: kurze Kennung des Schlüssels (in system.db), erkennt eine falsche oder veraltete Schlüsseldatei.
func (k DEK) ID() string {
	m := hmac.New(sha256.New, k[:])
	m.Write([]byte("id"))
	return hex.EncodeToString(m.Sum(nil)[:8])
}

type file struct {
	V     int    `json:"v"`
	User  int64  `json:"user"`
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

// Path: Schlüsseldatei des Nutzers uid.
func Path(dir string, uid int64) string {
	return filepath.Join(dir, strconv.FormatInt(uid, 10)+".key")
}

// Write legt die Schlüsseldatei atomar an (0600; tmp, fsync, rename).
func Write(dir string, uid int64, k DEK) error {
	data, _ := json.Marshal(file{V: 1, User: uid, KeyID: k.ID(), Key: hex.EncodeToString(k[:])})
	p := Path(dir, uid)
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
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
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Read liest die Schlüsseldatei und prüft sie gegen die erwartete Kennung (leer = ohne Prüfung).
func Read(dir string, uid int64, wantID string) (DEK, error) {
	var k DEK
	data, err := os.ReadFile(Path(dir, uid))
	if errors.Is(err, os.ErrNotExist) {
		return k, ErrMissing
	}
	if err != nil {
		return k, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil || f.V != 1 || f.User != uid {
		return k, fmt.Errorf("Schlüsseldatei %s ungültig", Path(dir, uid))
	}
	raw, err := hex.DecodeString(f.Key)
	if err != nil || len(raw) != len(k) {
		return k, fmt.Errorf("Schlüsseldatei %s ungültig", Path(dir, uid))
	}
	copy(k[:], raw)
	if k.ID() != f.KeyID || (wantID != "" && k.ID() != wantID) {
		return k, ErrWrongKey
	}
	return k, nil
}

// --- Wiederherstellungscode ---

// Crockford-Base32 ohne I, L, O, U – gut abzuschreiben.
var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewCode: 128 bit Zufall als 26 Zeichen in Gruppen (XXXXX-XXXXX-XXXXX-XXXXX-XXXXXX).
func NewCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := crockford.EncodeToString(b)
	return s[0:5] + "-" + s[5:10] + "-" + s[10:15] + "-" + s[15:20] + "-" + s[20:], nil
}

// normCode macht eine Eingabe vergleichbar: Gruppenzeichen weg, Großbuchstaben, verwechselbare Zeichen angeglichen.
func normCode(code string) ([]byte, error) {
	r := strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1")
	s := r.Replace(strings.ToUpper(strings.TrimSpace(code)))
	b, err := crockford.DecodeString(s)
	if err != nil || len(b) != 16 {
		return nil, ErrBadCode
	}
	return b, nil
}

func kek(code []byte, salt []byte) []byte {
	out, err := hkdf.Key(sha256.New, code, salt, "pebble-webhook wiederherstellung", 32)
	if err != nil {
		panic(err)
	}
	return out
}

// Wrap wickelt den DEK mit dem Code ein: base64(salt 16 | nonce 12 | AES-GCM(DEK), aad = Nutzer).
func Wrap(code string, uid int64, k DEK) (string, error) {
	c, err := normCode(code)
	if err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	g, err := gcm(kek(c, salt))
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	out := append(append(salt, nonce...), g.Seal(nil, nonce, k[:], aad(uid))...)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Unwrap holt den DEK mit dem Code zurück.
func Unwrap(code, wrapped string, uid int64) (DEK, error) {
	var k DEK
	c, err := normCode(code)
	if err != nil {
		return k, err
	}
	raw, err := base64.RawStdEncoding.DecodeString(wrapped)
	if err != nil || len(raw) < 16+12+len(k)+16 {
		return k, errors.New("eingewickelter Schlüssel ungültig")
	}
	g, err := gcm(kek(c, raw[:16]))
	if err != nil {
		return k, err
	}
	plain, err := g.Open(nil, raw[16:28], raw[28:], aad(uid))
	if err != nil || len(plain) != len(k) {
		return k, ErrBadCode
	}
	copy(k[:], plain)
	return k, nil
}

func aad(uid int64) []byte { return []byte("nutzer:" + strconv.FormatInt(uid, 10)) }

func gcm(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
