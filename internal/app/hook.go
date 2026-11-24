package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

const signatureWindow = 300 // Sekunden

// VerifySignature prüft die Pebble-Signatur über Kopfdaten und Rohkörper.
func VerifySignature(secret, timestamp, delivery, trigger string, isTest bool, body []byte, signature string, now time.Time) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errBadTimestamp
	}
	if d := now.Unix() - ts; d > signatureWindow || d < -signatureWindow {
		return errExpired
	}
	test := "0"
	if isTest {
		test = "1"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v1\n" + timestamp + "\n" + delivery + "\n" + trigger + "\n" + test + "\n"))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(strings.ToLower(strings.TrimSpace(signature)))) {
		return errBadSignature
	}
	return nil
}

var (
	errBadTimestamp = errors.New("Zeitstempel ungültig")
	errExpired      = errors.New("Anfrage abgelaufen")
	errBadSignature = errors.New("Signatur ungültig")
)

func hookReply(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"status":%q}`+"\n", msg)
}

// handleHook nimmt eine Pebble-Aufnahme an: prüfen, dauerhaft speichern, erst dann 2xx.
func (a *System) handleHook(w http.ResponseWriter, r *http.Request) {
	if !a.allow("hook:"+a.clientIP(r), a.Cfg.HookRatePerMin) {
		hookReply(w, http.StatusTooManyRequests, "zu viele Anfragen")
		return
	}
	// Der Token ordnet die Aufnahme ihrem Nutzer zu (nur sein Hash steht in system.db).
	tok, ok := a.DB.TokenUser(store.TokenHook, r.Header.Get(a.Cfg.HookTokenHeader))
	if !ok {
		hookReply(w, http.StatusUnauthorized, "nicht angemeldet")
		return
	}
	app, reason := a.app(tok.UserID)
	if app == nil {
		logf("hook: Daten von Nutzer %d nicht geöffnet (%s) – Aufnahme abgewiesen, die Pebble-App wiederholt sie", tok.UserID, reason)
		hookReply(w, http.StatusServiceUnavailable, "Daten gesperrt")
		return
	}
	access, err := app.pebbleAccess()
	if err != nil || access.Secret == "" || subtle.ConstantTimeCompare([]byte(store.Hash(access.Token)), []byte(store.Hash(r.Header.Get(a.Cfg.HookTokenHeader)))) != 1 {
		logf("hook: Zugang von Nutzer %d unvollständig: %v", tok.UserID, err)
		hookReply(w, http.StatusUnauthorized, "nicht angemeldet")
		return
	}
	if v := r.Header.Get("X-Index-Webhook-Version"); v != "1" {
		hookReply(w, http.StatusBadRequest, "Webhook-Version nicht unterstützt")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.Cfg.HookMaxBytes))
	if err != nil {
		hookReply(w, http.StatusRequestEntityTooLarge, "Aufnahme zu groß")
		return
	}
	delivery := strings.TrimSpace(r.Header.Get("X-Index-Delivery"))
	trigger := strings.TrimSpace(r.Header.Get("X-Index-Trigger"))
	isTest := r.Header.Get("X-Index-Test") == "true"
	if delivery == "" || len(delivery) > 200 || trigger == "" {
		hookReply(w, http.StatusBadRequest, "Zustellungskennung oder Geste fehlt")
		return
	}
	switch err := VerifySignature(access.Secret, r.Header.Get("X-Index-Timestamp"), delivery, trigger, isTest, body,
		r.Header.Get("X-Index-Signature"), time.Now()); err {
	case nil:
	case errBadTimestamp:
		hookReply(w, http.StatusBadRequest, err.Error())
		return
	default:
		hookReply(w, http.StatusUnauthorized, err.Error())
		return
	}
	app.acceptHook(w, r, body, delivery, trigger, isTest)
}

// acceptHook speichert eine geprüfte Aufnahme beim Nutzer.
func (a *App) acceptHook(w http.ResponseWriter, r *http.Request, body []byte, delivery, trigger string, isTest bool) {
	known, err := a.DB.DeliveryKnown(delivery)
	if err != nil {
		logf("hook: Datenbank: %v", err)
		hookReply(w, http.StatusInternalServerError, "Speicherfehler")
		return
	}
	if known {
		hookReply(w, http.StatusOK, "bereits angenommen")
		return
	}

	fields, audio, audioMime, err := parseMultipart(r.Header.Get("Content-Type"), body)
	if err != nil {
		hookReply(w, http.StatusBadRequest, "Formular nicht lesbar")
		return
	}
	now := store.Now()
	j := &store.Job{ID: store.NewID(), DeliveryID: delivery, Source: "pebble", Trigger: trigger, IsTest: isTest || fields["test"] == "true",
		ReceivedAt: now, RecordedAt: now, PebbleText: strings.TrimSpace(fields["transcription"]), State: store.StateNew}
	if ms, err := strconv.ParseInt(fields["recordedAt"], 10, 64); err == nil && ms > 0 {
		j.RecordedAt = ms
	}
	switch {
	case j.IsTest || trigger == "test-event":
		// Test-Event: nur anzeigen, nichts verarbeiten.
		j.IsTest, j.Mode, j.State = true, "test", store.StateDone
		j.Transcript, j.TranscriptSource, j.Title = j.PebbleText, "pebble", a.T("Test-Event")
	case trigger == "single-click-hold":
		j.Mode = a.Cfg.TriggerSingle
	case trigger == "double-click-hold":
		j.Mode = a.Cfg.TriggerDouble
	default:
		// Unbekannte Geste: sichtbar abweisen statt still verwerfen.
		j.FailedAt = now
		j.Mode, j.Failed, j.Error = "unbekannt", true, a.Tf("Unbekannte Geste „%s“ – keine Aktion zugeordnet", store.Preview(trigger, 60))
	}
	// Ring-ID ist Pflicht: das Test-Event beim Einrichten scheitert ohne gültige ID, damit der Header gesetzt wird.
	if j.IsTest && !ringIDRe.MatchString(strings.TrimSpace(r.Header.Get(a.Cfg.RingIDHeader))) {
		hookReply(w, http.StatusBadRequest, "Ring-ID fehlt oder ungültig: Header "+a.Cfg.RingIDHeader+" mit Wert #12345 eintragen")
		return
	}
	if !j.IsTest && len(audio) == 0 && j.PebbleText == "" {
		hookReply(w, http.StatusBadRequest, "weder Audio noch Transkript")
		return
	}
	if len(audio) > 0 {
		path, err := a.saveAudio(j.ID, j.Mode, audio, audioMime, false)
		if err != nil {
			logf("hook: Audio speichern: %v", err)
			hookReply(w, http.StatusInsufficientStorage, "Speicher nicht verfügbar")
			return
		}
		j.AudioPath, j.AudioMime = path, audioMime
	}
	created, err := a.DB.InsertJob(j)
	if err != nil || !created {
		if j.AudioPath != "" {
			os.Remove(j.AudioPath)
		}
		if err != nil {
			logf("hook: Auftrag speichern: %v", err)
			hookReply(w, http.StatusInternalServerError, "Speicherfehler")
			return
		}
		hookReply(w, http.StatusOK, "bereits angenommen")
		return
	}
	ringOK := a.recordRecording(j, strings.TrimSpace(r.Header.Get(a.Cfg.RingIDHeader)))
	a.changed(j.ID)
	if j.Failed {
		a.notify(config.NotifyErrors, a.T("Notiz fehlgeschlagen"), store.Preview(j.Error, 120), "/eintrag/"+j.ID)
	}
	a.Wake()
	if !ringOK {
		hookReply(w, http.StatusOK, "angenommen, aber Ring-ID fehlt oder ungültig (Header "+a.Cfg.RingIDHeader+": #12345)")
		return
	}
	hookReply(w, http.StatusOK, "angenommen")
}

// parseMultipart liest Textfelder und den Audio-Teil aus dem Rohkörper.
func parseMultipart(contentType string, body []byte) (fields map[string]string, audio []byte, audioMime string, err error) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		return nil, nil, "", errors.New("kein Multipart")
	}
	fields = map[string]string{}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, "", err
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, "", err
		}
		if part.FormName() == "audio" {
			audio = data
			audioMime = part.Header.Get("Content-Type")
			if audioMime == "" {
				audioMime = "audio/mp4"
			}
		} else if len(data) <= 1<<20 {
			fields[part.FormName()] = string(data)
		}
	}
	return fields, audio, audioMime, nil
}

func audioExt(mimeType string) string {
	mt, _, _ := mime.ParseMediaType(mimeType)
	switch mt {
	case "audio/webm", "video/webm":
		return ".webm"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	}
	return ".m4a"
}

// saveAudio schreibt die Aufnahme dauerhaft (fsync) unter MEDIA_DIR/<nutzer>/audio/<diktat|planung>/JJJJ/MM/<id>.<ext>.enc,
// verschlüsselt mit dem Schlüssel des Nutzers. Aufnahmen aus der Web-App (web) heißen <id>-web.<ext>.enc, damit ein
// Reimport sie nicht dem Ring zuschreibt.
func (a *App) saveAudio(id, mode string, data []byte, mimeType string, web bool) (string, error) {
	free, err := freeBytes(a.Cfg.MediaDir)
	if err != nil {
		return "", err
	}
	if free < a.Cfg.MinFreeBytes+uint64(len(data)) {
		return "", fmt.Errorf("nur noch %d MB frei", free>>20)
	}
	dir := filepath.Join(a.audioRoot(), audioSub(mode), time.Now().Format("2006/01"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name := id
	if web {
		name += webMark
	}
	name += audioExt(mimeType)
	if a.audioKey != nil {
		if data, err = schluessel.SealAudio(a.audioKey, name, data); err != nil {
			return "", err
		}
		name += schluessel.AudioExt
	}
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// mediaRoot: Ablage der Medien dieses Nutzers (MEDIA_DIR/<id>, darunter audio/ und bilder/).
func (a *App) mediaRoot() string {
	return filepath.Join(a.Cfg.MediaDir, strconv.FormatInt(a.UserID, 10))
}

// audioRoot: Ablage der Aufnahmen dieses Nutzers.
func (a *App) audioRoot() string { return filepath.Join(a.mediaRoot(), "audio") }

// imageRoot: Ablage der Bilder dieses Nutzers.
func (a *App) imageRoot() string { return filepath.Join(a.mediaRoot(), "bilder") }

// readAudio liest eine Aufnahme; verschlüsselte (.enc) werden mit dem Schlüssel des Nutzers geöffnet.
// Aufnahmen bis 2.x liegen bis zum bestätigten Umzug unverschlüsselt am alten Ort.
func (a *App) readAudio(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name, sealed := strings.CutSuffix(filepath.Base(path), schluessel.AudioExt)
	if !sealed {
		return data, nil
	}
	if a.audioKey == nil {
		return nil, errors.New("Aufnahme verschlüsselt, Schlüssel fehlt")
	}
	return schluessel.OpenAudio(a.audioKey, name, data)
}
