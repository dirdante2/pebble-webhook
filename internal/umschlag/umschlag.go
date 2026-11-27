// Package umschlag verschlüsselt Berichte für einen Empfänger mit dessen öffentlichem Schlüssel (X25519): je
// Bericht ein Einmal-Schlüsselpaar, daraus mit dem Empfänger-Schlüssel per HKDF-SHA256 ein AES-256-GCM-Schlüssel.
// Nur der Inhaber des privaten Schlüssels kann öffnen; der Zweck (z. B. "statistik") ist als Zusatzdaten
// gebunden, ein Bericht lässt sich also nicht an einen anderen Endpunkt umleiten.
package umschlag

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const info = "pebble-umschlag v1"

// Umschlag: verschlüsselter Bericht, wie er über die Leitung geht.
type Umschlag struct {
	V     int    `json:"v"`
	Key   string `json:"k"` // öffentlicher Einmal-Schlüssel des Absenders (base64)
	Nonce string `json:"n"`
	Data  string `json:"d"`
}

var b64 = base64.StdEncoding

// NeuesPaar erzeugt ein Schlüsselpaar des Empfängers (privat, öffentlich; base64).
func NeuesPaar() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(k.Bytes()), b64.EncodeToString(k.PublicKey().Bytes()), nil
}

// Oeffentlich: öffentlicher Schlüssel zu einem privaten (base64).
func Oeffentlich(priv string) (string, error) {
	k, err := privKey(priv)
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(k.PublicKey().Bytes()), nil
}

func privKey(s string) (*ecdh.PrivateKey, error) {
	b, err := b64.DecodeString(s)
	if err != nil {
		return nil, errors.New("privater Schlüssel: kein base64")
	}
	return ecdh.X25519().NewPrivateKey(b)
}

func pubKey(s string) (*ecdh.PublicKey, error) {
	b, err := b64.DecodeString(s)
	if err != nil {
		return nil, errors.New("öffentlicher Schlüssel: kein base64")
	}
	return ecdh.X25519().NewPublicKey(b)
}

func aead(shared, ephPub, rcptPub []byte) (cipher.AEAD, error) {
	salt := append(append([]byte{}, ephPub...), rcptPub...)
	key, err := hkdf.Key(sha256.New, shared, salt, info, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Versiegeln verschlüsselt plain für den Empfänger rcpt (öffentlicher Schlüssel, base64); zweck wird mitgebunden.
func Versiegeln(rcpt, zweck string, plain []byte) ([]byte, error) {
	pub, err := pubKey(rcpt)
	if err != nil {
		return nil, err
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(pub)
	if err != nil {
		return nil, err
	}
	g, err := aead(shared, eph.PublicKey().Bytes(), pub.Bytes())
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return json.Marshal(Umschlag{V: 1, Key: b64.EncodeToString(eph.PublicKey().Bytes()), Nonce: b64.EncodeToString(nonce),
		Data: b64.EncodeToString(g.Seal(nil, nonce, plain, []byte(zweck)))})
}

// ErrUngueltig: Umschlag nicht lesbar, falscher Schlüssel, falscher Zweck oder verändert.
var ErrUngueltig = errors.New("Umschlag ungültig")

// Oeffnen entschlüsselt einen Umschlag mit dem privaten Schlüssel priv (base64) für den Zweck zweck.
func Oeffnen(priv, zweck string, raw []byte) ([]byte, error) {
	k, err := privKey(priv)
	if err != nil {
		return nil, err
	}
	var u Umschlag
	if json.Unmarshal(raw, &u) != nil || u.V != 1 {
		return nil, ErrUngueltig
	}
	ephB, err1 := b64.DecodeString(u.Key)
	nonce, err2 := b64.DecodeString(u.Nonce)
	data, err3 := b64.DecodeString(u.Data)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, ErrUngueltig
	}
	eph, err := ecdh.X25519().NewPublicKey(ephB)
	if err != nil {
		return nil, ErrUngueltig
	}
	shared, err := k.ECDH(eph)
	if err != nil {
		return nil, ErrUngueltig
	}
	g, err := aead(shared, ephB, k.PublicKey().Bytes())
	if err != nil || len(nonce) != g.NonceSize() {
		return nil, ErrUngueltig
	}
	plain, err := g.Open(nil, nonce, data, []byte(zweck))
	if err != nil {
		return nil, ErrUngueltig
	}
	return plain, nil
}
