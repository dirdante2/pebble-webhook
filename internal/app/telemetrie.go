package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/store"
	"pebble-webhook/internal/umschlag"
)

// Anonyme Statistik: einmal im Monat (am 1., zufällige Uhrzeit; verpasst → im selben Monat nachgeholt) gehen
// App-Version, Kanal, App-ID und je Ring eine Kennung (Hash) mit der gesamten Nutzungszeit an den Entwickler;
// jedes Inaktiv- und Aktiv-Setzen eines Rings geht sofort. Je Nutzer abschaltbar (TELEMETRY, Vorgabe an); es zählen
// nur Ringe von Nutzern, die es nicht abgeschaltet haben. Rückmeldungen (Kommentar, Fehler) schickt der Nutzer
// selbst. Ziel, Pepper und öffentlicher Schlüssel des Empfängers kommen beim Build ins Binary (TelemetryURL,
// TelemetryPepper, TelemetryKey); fehlt eins, ist alles aus. Jeder Bericht geht verschlüsselt (internal/umschlag):
// nur der Empfänger kann ihn öffnen.

// Build-Werte, gesetzt von cmd/pebble-webhook (-ldflags -X).
var (
	TelemetryURL    string // Basis der Empfangs-API ohne Slash am Ende
	TelemetryPepper string // geheimer Zusatz für den Ring-Hash (gleich in allen Builds)
	TelemetryKey    string // öffentlicher X25519-Schlüssel des Empfängers (base64)
)

const (
	telemetryIDKey      = "telemetrie:app_id"   // System-kv: App-ID der Installation
	telemetryDueKey     = "telemetrie:faellig"  // System-kv: „JJJJ-MM <ms>“ nächster Termin
	telemetrySentKey    = "telemetrie:gesendet" // System-kv: „JJJJ-MM <ms>“ letzter erfolgreicher Versand
	telemetryErrKey     = "telemetrie:fehler"   // System-kv: letzter Fehler („“ = keiner)
	telemetryPendingKey = "telemetrie:offen"    // Nutzer-kv: Ring-Ereignisse, die noch nicht angekommen sind
	telemetryLastKey    = "telemetrie:zuletzt"  // System-kv: ms des letzten gesendeten Berichts (Sofort-Versand höchstens stündlich)
	telemetryCatchUpMax = 6 * time.Hour         // Nachholen: zufällig bis zu so lange nach dem Start
	telemetryRetry      = time.Hour             // nach einem Fehler
	telemetryMinGap     = time.Hour             // Sofort-Bericht (Start, Einschalten) höchstens so oft
)

var (
	telemetryMu sync.Mutex // Warteschlangen (nie über einen Versand gehalten)
	appIDMu     sync.Mutex // App-ID anlegen
)

// Grenzen eines Monatsberichts (der Empfänger lehnt mehr ab).
const (
	telemetryMaxRings = 1000
)

// errRejected: der Empfänger lehnt den Bericht ab (4xx) – ein neuer Versuch ändert nichts.
var errRejected = errors.New("Empfänger lehnt den Bericht ab")

var telemetryHTTP = &http.Client{Timeout: 20 * time.Second}

func telemetryAvailable() bool {
	return TelemetryURL != "" && TelemetryPepper != "" && TelemetryKey != ""
}

// appID: Kennung der Installation – beim ersten Start einmal aus Maschine, Hostname und Zufall gebildet, danach fest.
func (s *System) appID() (string, error) {
	appIDMu.Lock()
	defer appIDMu.Unlock()
	if id, err := s.DB.Get(telemetryIDKey); err != nil || id != "" {
		return id, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h := sha256.New()
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		h.Write(bytes.TrimSpace(b))
	}
	host, _ := os.Hostname()
	h.Write([]byte("\x00" + host + "\x00"))
	h.Write(salt)
	id := hex.EncodeToString(h.Sum(nil))[:32]
	if err := s.DB.Set(telemetryIDKey, id); err != nil {
		return "", err
	}
	logf("Statistik: App-ID angelegt")
	return id, nil
}

// ringHash: Kennung eines Rings für die Statistik – gleich in jeder Installation, ohne den Pepper nicht auf die
// Kennung (Bluetooth-Adresse) zurückzuführen. "" = Ring ohne Kennung.
func ringHash(kennung string) string {
	if kennung == "" || TelemetryPepper == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(TelemetryPepper + ":" + strings.ToLower(kennung)))
	return hex.EncodeToString(sum[:])[:24]
}

type telemetryRing struct {
	Ring    string  `json:"ring"`
	Seconds float64 `json:"sekunden"`
}

type telemetryMonth struct {
	AppID   string          `json:"app_id"`
	Version string          `json:"version"`
	Channel string          `json:"kanal"`
	Month   string          `json:"monat"`
	Rings   []telemetryRing `json:"ringe"`
	Users   int             `json:"-"` // Nutzer mit eingeschalteter Statistik (0 = nichts senden), nicht gesendet
}

type telemetryEvent struct {
	AppID   string  `json:"app_id"`
	Version string  `json:"version"`
	Channel string  `json:"kanal"`
	Ring    string  `json:"ring"`
	Seconds float64 `json:"sekunden"`
	Event   string  `json:"ereignis"` // inaktiv | aktiv
	Reason  string  `json:"grund,omitempty"`
	Day     string  `json:"tag"` // JJJJ-MM-TT
}

// telemetryRings: aktive Ringe eines Nutzers für die Statistik; inaktive (deren Stand kam mit dem Ereignis) und
// Ringe ohne Kennung werden nicht gesendet.
func (a *App) telemetryRings() (out []telemetryRing, err error) {
	rings, err := a.DB.Rings()
	if err != nil {
		return nil, err
	}
	for _, g := range rings {
		h := ringHash(g.Kennung)
		if h == "" || g.EndedAt > 0 {
			continue
		}
		secs, err := a.DB.RingSecondsSince(g.Nr, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, telemetryRing{Ring: h, Seconds: float64(int64(secs))})
	}
	return out, nil
}

// telemetryMonthly baut den Monatsbericht über alle geöffneten Nutzer mit eingeschalteter Statistik.
func (s *System) telemetryMonthly(month string, only *App) (*telemetryMonth, error) {
	id, err := s.appID()
	if err != nil {
		return nil, err
	}
	m := &telemetryMonth{AppID: id, Version: shortVersion(s.Version), Channel: s.installedChannel(), Month: month, Rings: []telemetryRing{}}
	apps := s.Apps()
	if only != nil {
		apps = []*App{only}
	}
	for _, a := range apps {
		if !a.telemetryOn() {
			continue
		}
		m.Users++
		rings, err := a.telemetryRings()
		if err != nil {
			return nil, err
		}
		m.Rings = append(m.Rings, rings...)
	}
	if len(m.Rings) > telemetryMaxRings {
		logf("Statistik: %d Ringe, gesendet werden die ersten %d", len(m.Rings), telemetryMaxRings)
		m.Rings = m.Rings[:telemetryMaxRings]
	}
	return m, nil
}

// telemetryPost schickt einen Bericht an die Empfangs-API (path z. B. "/statistik").
func (s *System) telemetryPost(ctx context.Context, path string, body any) error {
	if !telemetryAvailable() {
		return errors.New("Statistik in diesem Build nicht verfügbar")
	}
	plain, err := json.Marshal(body)
	if err != nil {
		return err
	}
	b, err := umschlag.Versiegeln(TelemetryKey, strings.TrimPrefix(path, "/"), plain)
	if err != nil {
		return fmt.Errorf("verschlüsseln: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TelemetryURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pebble-webhook/"+shortVersion(s.Version))
	resp, err := telemetryHTTP.Do(req)
	if err != nil {
		// Die Ursache nennt die Ziel-Adresse: nur ins Log, Nutzer und kv bekommen die allgemeine Meldung.
		logf("Statistik: %s: %v", path, err)
		return errors.New("Empfänger nicht erreichbar")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 4 && resp.StatusCode != http.StatusTooManyRequests {
		return fmt.Errorf("%w (%d)", errRejected, resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Empfänger antwortet %d", resp.StatusCode)
	}
	return nil
}

// randDuration: zufällig in [0, max).
func randDuration(max time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return max / 2
	}
	return time.Duration(n.Int64())
}

// telemetryDue: Termin des Monats von now – zufällig am 1.; liegt er schon zurück (Dienst lief nicht, erster Start
// im Monat), zufällig in den nächsten Stunden.
func telemetryDue(now time.Time) time.Time {
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	due := first.Add(randDuration(24 * time.Hour))
	if due.Before(now) {
		due = now.Add(randDuration(telemetryCatchUpMax))
	}
	return due
}

// kvMonth: „JJJJ-MM <ms>“ → Monat und Zeitpunkt.
func kvMonth(v string) (string, time.Time) {
	f := strings.Fields(v)
	if len(f) != 2 {
		return "", time.Time{}
	}
	var ms int64
	fmt.Sscan(f[1], &ms)
	return f[0], time.UnixMilli(ms)
}

// TelemetrySchedule läuft im Hintergrund: einmal kurz nach dem Start (auch nach der Installation) ein Bericht,
// dann der Monatsbericht und liegengebliebene Ring-Ereignisse.
func (s *System) TelemetrySchedule(ctx context.Context) {
	if !telemetryAvailable() {
		return
	}
	if _, err := s.appID(); err != nil {
		logf("Statistik: App-ID: %v", err)
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	lastFlush := time.Now()
	started := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if !started { // eine Minute nach dem Start: die Nutzer sind geöffnet
				started = true
				s.telemetrySendNow(ctx, "Start", now)
			}
			s.telemetryTick(ctx, now)
			if now.Sub(lastFlush) >= telemetryRetry {
				lastFlush = now
				for _, a := range s.Apps() {
					a.telemetryFlush(ctx)
				}
			}
		}
	}
}

func (s *System) telemetryTick(ctx context.Context, now time.Time) {
	month := now.Format("2006-01")
	if v, _ := s.DB.Get(telemetrySentKey); strings.HasPrefix(v, month+" ") {
		return
	}
	v, err := s.DB.Get(telemetryDueKey)
	if err != nil {
		return
	}
	dueMonth, due := kvMonth(v)
	if dueMonth != month {
		due = telemetryDue(now)
		if err := s.DB.Set(telemetryDueKey, month+" "+fmt.Sprint(due.UnixMilli())); err != nil {
			logf("Statistik: %v", err)
			return
		}
	}
	if now.Before(due) {
		return
	}
	m, err := s.telemetryMonthly(month, nil)
	if err == nil && m.Users > 0 {
		err = s.telemetryPost(ctx, "/statistik", m)
	}
	if err != nil {
		logf("Statistik: Monatsbericht %s: %v", month, err)
		s.DB.Set(telemetryErrKey, fmt.Sprintf("%d %s", now.UnixMilli(), err))
		if errors.Is(err, errRejected) {
			// Abgelehnt: ein neuer Versuch ändert nichts – der Monat ist erledigt, der Fehler bleibt sichtbar.
			s.DB.Set(telemetrySentKey, month+" "+fmt.Sprint(now.UnixMilli()))
			return
		}
		s.DB.Set(telemetryDueKey, month+" "+fmt.Sprint(now.Add(telemetryRetry).UnixMilli()))
		return
	}
	s.DB.Set(telemetryErrKey, "")
	s.DB.Set(telemetrySentKey, month+" "+fmt.Sprint(now.UnixMilli()))
	if m.Users > 0 {
		s.DB.Set(telemetryLastKey, fmt.Sprint(now.UnixMilli()))
	}
	if m.Users == 0 {
		logf("Statistik: %s nichts gesendet (bei allen Nutzern abgeschaltet)", month)
	} else {
		logf("Statistik: Monatsbericht %s gesendet (%d Ringe)", month, len(m.Rings))
	}
}

var telemetryNowMu sync.Mutex

// telemetrySendNow: alle Daten sofort (Start der App, Statistik eingeschaltet) – Bericht mit den aktiven Ringen, je
// inaktivem Ring sein Stand mit Grund, dann liegengebliebene Ring-Ereignisse; höchstens einmal je telemetryMinGap. Der Monatsbericht am 1. bleibt davon unberührt.
func (s *System) telemetrySendNow(ctx context.Context, why string, now time.Time) {
	if !telemetryAvailable() {
		return
	}
	telemetryNowMu.Lock()
	defer telemetryNowMu.Unlock()
	var last int64
	fmt.Sscan(kvVal(s.DB.Get(telemetryLastKey)), &last)
	if last > 0 && now.Sub(time.UnixMilli(last)) < telemetryMinGap {
		return
	}
	m, err := s.telemetryMonthly(now.Format("2006-01"), nil)
	if err != nil || m.Users == 0 {
		return
	}
	if err := s.telemetryPost(ctx, "/statistik", m); err != nil {
		logf("Statistik: Bericht (%s): %v", why, err)
		return
	}
	s.DB.Set(telemetryLastKey, fmt.Sprint(now.UnixMilli()))
	inactive := 0
	for _, a := range s.Apps() {
		if a.telemetryOn() {
			inactive += a.telemetryInactiveRings(ctx)
			a.telemetryFlush(ctx)
		}
	}
	logf("Statistik: Bericht gesendet (%s, %d aktive, %d inaktive Ringe)", why, len(m.Rings), inactive)
}

// telemetryOn: Statistik der Installation eingeschaltet (Admin-Einstellung, Vorgabe an) – aus den gespeicherten
// Einstellungen, damit es direkt nach dem Umschalten stimmt.
func (s *System) telemetryOn() bool {
	v, err := s.DB.Settings()
	if err != nil {
		return s.Cfg.Telemetry
	}
	return v[config.Telemetry] != "inaktiv"
}

func (a *App) telemetryOn() bool { return a.System.telemetryOn() }

// handleTelemetrySave (Admin): Statistik der Installation ein- oder ausschalten – ohne Neustart. Ein: sofort alle
// Daten senden (höchstens stündlich); aus: Wartendes aller Nutzer verwerfen.
func (s *System) handleTelemetrySave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active *bool `json:"aktiv"`
	}
	if readJSON(r, &in) != nil || in.Active == nil {
		writeErr(w, 400, "ungueltig", "aktiv fehlt")
		return
	}
	was := s.telemetryOn()
	v := "inaktiv"
	if *in.Active {
		v = "aktiv"
	}
	vals, err := s.DB.Settings()
	if err == nil {
		vals[config.Telemetry] = v
		err = s.DB.SaveSettings(vals)
	}
	if err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	s.Cfg.Telemetry = *in.Active
	switch {
	case *in.Active && !was:
		go s.telemetrySendNow(context.Background(), "eingeschaltet", time.Now())
	case !*in.Active && was:
		for _, a := range s.Apps() {
			a.telemetryQueue(func([]telemetryEvent) []telemetryEvent { return nil })
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "aktiv": *in.Active})
}

// telemetryRingEvent meldet ein Inaktiv- oder Aktiv-Setzen sofort (im Hintergrund); geht es nicht, kommt das
// Ereignis in die Warteschlange des Nutzers und wird stündlich erneut gesendet.
func (a *App) telemetryRingEvent(g *store.Ring) {
	if !telemetryAvailable() || !a.telemetryOn() {
		return
	}
	ev, ok, err := a.ringEventOf(g)
	if err != nil {
		logf("Statistik: %v", err)
	}
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.System.telemetryPost(ctx, "/ring-ereignis", ev); err != nil && !errors.Is(err, errRejected) {
			logf("Statistik: Ring-Ereignis: %v (später erneut)", err)
			a.telemetryQueue(func(q []telemetryEvent) []telemetryEvent { return append(q, ev) })
		}
	}()
}

// ringEventOf: Stand eines Rings als Ereignis (aktiv bzw. inaktiv mit Grund und Tag des Inaktiv-Setzens); false bei
// Ringen ohne Kennung und inaktiven ohne Grund (vor 4.6).
func (a *App) ringEventOf(g *store.Ring) (telemetryEvent, bool, error) {
	h := ringHash(g.Kennung)
	if h == "" || (g.EndedAt > 0 && g.InactiveReason == "") {
		return telemetryEvent{}, false, nil
	}
	id, err := a.System.appID()
	if err != nil {
		return telemetryEvent{}, false, err
	}
	secs, err := a.DB.RingSecondsSince(g.Nr, 0)
	if err != nil {
		return telemetryEvent{}, false, err
	}
	ev := telemetryEvent{AppID: id, Version: shortVersion(a.System.Version), Channel: a.System.installedChannel(), Ring: h, Seconds: float64(int64(secs)),
		Event: "aktiv", Day: time.Now().Format("2006-01-02")}
	if g.EndedAt > 0 {
		ev.Event, ev.Reason, ev.Day = "inaktiv", g.InactiveReason, time.UnixMilli(g.EndedAt).Format("2006-01-02")
	}
	return ev, true, nil
}

// telemetryInactiveRings sendet den Stand aller inaktiven Ringe (mit Grund) – der Monatsbericht enthält nur aktive.
// Was nicht ankommt, kommt in die Warteschlange.
func (a *App) telemetryInactiveRings(ctx context.Context) int {
	rings, err := a.DB.Rings()
	if err != nil {
		logf("Statistik: %v", err)
		return 0
	}
	n := 0
	for _, g := range rings {
		if g.EndedAt == 0 {
			continue
		}
		ev, ok, err := a.ringEventOf(g)
		if err != nil {
			logf("Statistik: %v", err)
			continue
		}
		if !ok {
			continue
		}
		if err := a.System.telemetryPost(ctx, "/ring-ereignis", ev); err != nil {
			if !errors.Is(err, errRejected) {
				a.telemetryQueue(func(q []telemetryEvent) []telemetryEvent { return append(q, ev) })
			}
			continue
		}
		n++
	}
	return n
}

// telemetryQueue ändert die Warteschlange der Ring-Ereignisse des Nutzers.
func (a *App) telemetryQueue(f func([]telemetryEvent) []telemetryEvent) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	var q []telemetryEvent
	if v, err := a.DB.Get(telemetryPendingKey); err != nil {
		logf("Statistik: Warteschlange: %v", err)
		return
	} else if v != "" {
		json.Unmarshal([]byte(v), &q)
	}
	q = f(q)
	if len(q) > 200 {
		q = q[len(q)-200:]
	}
	b, _ := json.Marshal(q)
	if len(q) == 0 {
		b = nil
	}
	if err := a.DB.Set(telemetryPendingKey, string(b)); err != nil {
		logf("Statistik: Warteschlange: %v", err)
	}
}

// telemetryFlush sendet liegengebliebene Ring-Ereignisse der Reihe nach, ohne die Warteschlange über den Versand zu
// sperren; beim ersten Netzfehler ist Schluss bis zum nächsten Lauf. Abgelehnte Ereignisse fallen weg.
func (a *App) telemetryFlush(ctx context.Context) {
	if !a.telemetryOn() {
		return
	}
	var q []telemetryEvent
	a.telemetryQueue(func(cur []telemetryEvent) []telemetryEvent { q = append(q, cur...); return cur })
	done := 0
	for _, ev := range q {
		if ctx.Err() != nil {
			break
		}
		err := a.System.telemetryPost(ctx, "/ring-ereignis", ev)
		if err != nil && !errors.Is(err, errRejected) {
			break
		}
		if err != nil {
			logf("Statistik: Ring-Ereignis verworfen: %v", err)
		}
		done++
	}
	if done == 0 {
		return
	}
	// Neue Ereignisse kommen nur hinten dazu: die ersten done sind erledigt.
	a.telemetryQueue(func(cur []telemetryEvent) []telemetryEvent {
		if done > len(cur) {
			return nil
		}
		return cur[done:]
	})
	logf("Statistik: %d Ring-Ereignisse nachgesendet", done)
}

// handleTelemetry: ob die Statistik in diesem Build verfügbar und für den Nutzer eingeschaltet ist.
func (a *App) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"verfuegbar": telemetryAvailable(), "aktiv": a.telemetryOn(), "admin": a.isAdmin()})
}

// Ring-Lebensdauer aller Installationen (Summen des Empfängers, öffentlich), 6 h zwischengespeichert.
var globalLife struct {
	mu    sync.Mutex
	life  json.RawMessage
	stand int64
	at    time.Time
}

const globalLifeTTL = 6 * time.Hour

// handleGlobalLife: Ring-Lebensdauer je Status aller Installationen – der Server holt sie beim Empfänger (der
// Browser kennt dessen Adresse nicht) und gibt nur die Lebensdauer und den Stand weiter; dazu die eigenen Ringe
// (Status bzw. Grund und Stunden) zum Vergleich – die bleiben auf dem Server und gehen nicht an den Empfänger.
func (a *App) handleGlobalLife(w http.ResponseWriter, r *http.Request) {
	if !telemetryAvailable() {
		writeErr(w, 503, "aus", "In diesem Build nicht verfügbar")
		return
	}
	if !a.telemetryOn() {
		writeErr(w, 403, "aus", "Die Daten aller Installationen sind nur sichtbar, solange diese Installation selbst sendet")
		return
	}
	globalLife.mu.Lock()
	defer globalLife.mu.Unlock()
	if globalLife.life == nil || time.Since(globalLife.at) > globalLifeTTL {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, TelemetryURL+"/statistik", nil)
		if err != nil {
			writeErr(w, 500, "intern", "Anfrage nicht möglich")
			return
		}
		req.Header.Set("User-Agent", "pebble-webhook/"+shortVersion(a.System.Version))
		resp, err := telemetryHTTP.Do(req)
		if err != nil {
			logf("Statistik: Lebensdauer holen: %v", err)
			writeErr(w, 502, "senden", "Empfänger nicht erreichbar")
			return
		}
		defer resp.Body.Close()
		var in struct {
			Life  json.RawMessage `json:"lebensdauer"`
			Stand int64           `json:"stand"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&in) != nil || len(in.Life) == 0 {
			writeErr(w, 502, "senden", "Antwort des Empfängers unbrauchbar")
			return
		}
		globalLife.life, globalLife.stand, globalLife.at = in.Life, in.Stand, time.Now()
	}
	rings, err := a.DB.Rings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	own := []map[string]any{}
	for _, g := range rings {
		status := "aktiv"
		if g.EndedAt > 0 {
			if status = g.InactiveReason; status == "" {
				continue // inaktiv ohne Grund (vor 4.6): keiner Zeile zuzuordnen
			}
		}
		secs, err := a.DB.RingSecondsSince(g.Nr, 0)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		own = append(own, map[string]any{"status": status, "stunden": math.Round(secs/36) / 100, "ring": g.HardwareID})
	}
	writeJSON(w, 200, map[string]any{"lebensdauer": globalLife.life, "stand": globalLife.stand, "eigene": own})
}

// kvVal: Wert aus kv; ein Lesefehler zählt hier wie „nicht gesetzt“ (nur Anzeige).
func kvVal(v string, _ error) string { return v }

// emailRe: einfache Prüfung einer E-Mail-Adresse für die Antwort auf eine Rückmeldung.
var emailRe = regexp.MustCompile(`^[^\s@<>,;"]{1,64}@[^\s@<>,;"]+\.[^\s@<>,;"]{2,}$`)

// handleFeedback: Rückmeldung (Kommentar oder Fehler) des Nutzers an den Entwickler, immer mit App-Version, auf
// Wunsch mit E-Mail-Adresse für eine Antwort. Keine App-ID, keine Ringe.
func (a *App) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind  string `json:"art"`
		Text  string `json:"text"`
		Email string `json:"email"`
	}
	if readJSON(r, &in) != nil || (in.Kind != "kommentar" && in.Kind != "fehler") {
		writeErr(w, 400, "ungueltig", "Art fehlt")
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len([]rune(in.Text)) > 4000 {
		writeErr(w, 400, "ungueltig", "Text 1–4000 Zeichen")
		return
	}
	in.Email = strings.TrimSpace(in.Email)
	if in.Email != "" && (len(in.Email) > 200 || !emailRe.MatchString(in.Email)) {
		writeErr(w, 400, "ungueltig", "E-Mail-Adresse ungültig")
		return
	}
	if !telemetryAvailable() {
		writeErr(w, 503, "aus", "Rückmeldungen sind in diesem Build nicht verfügbar")
		return
	}
	body := map[string]any{"art": in.Kind, "text": in.Text, "version": shortVersion(a.System.Version), "kanal": a.System.installedChannel()}
	if in.Email != "" {
		body["email"] = in.Email
	}
	if err := a.System.telemetryPost(r.Context(), "/rueckmeldung", body); err != nil {
		logf("Rückmeldung senden: %v", err)
		writeErr(w, 502, "senden", a.Tf("Senden fehlgeschlagen: %s", a.T(err.Error())))
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
