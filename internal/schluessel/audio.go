package schluessel

import (
	"bytes"
	"crypto/rand"
	"errors"
)

// Verschlüsselte Aufnahmen (Endung .enc): "PBA1" | Nonce 12 | AES-256-GCM(Inhalt), aad = Name der Datei ohne .enc.
// Aufnahmen sind klein (Hook-Grenze) und werden ohnehin ganz gelesen (Transkription, Wiedergabe).
const AudioExt = ".enc"

var audioMagic = []byte("PBA1")

// SealAudio verschlüsselt eine Aufnahme; name bindet den Inhalt an seine Datei (kein Vertauschen).
func SealAudio(key []byte, name string, data []byte) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append(append([]byte{}, audioMagic...), nonce...), g.Seal(nil, nonce, data, []byte(name))...)
	return out, nil
}

// OpenAudio entschlüsselt eine mit SealAudio geschriebene Aufnahme.
func OpenAudio(key []byte, name string, data []byte) ([]byte, error) {
	if !IsSealed(data) || len(data) < len(audioMagic)+12+16 {
		return nil, errors.New("Aufnahme nicht verschlüsselt oder beschädigt")
	}
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	n := len(audioMagic)
	plain, err := g.Open(nil, data[n:n+12], data[n+12:], []byte(name))
	if err != nil {
		return nil, errors.New("Aufnahme lässt sich nicht entschlüsseln (falscher Schlüssel oder beschädigt)")
	}
	return plain, nil
}

func IsSealed(data []byte) bool { return bytes.HasPrefix(data, audioMagic) }
