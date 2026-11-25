package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pebble-webhook/internal/store"
)

// Kalender-Abo (iCal): alle Termine als .ics unter einem geheimen Link. Google Kalender, Apple und Outlook
// abonnieren ihn und holen ihn selbst ab (Google nur alle paar Stunden). Nur lesend, nur diese Richtung.
const (
	icalTokenKey = "ical_token"
	icalName     = "Sprachnotizen"
	icalEventMin = 30 // Länge eines Termins mit Uhrzeit (Minuten)
)

// icalToken liefert den geheimen Teil des Links; beim ersten Aufruf (oder mit renew) wird er neu erzeugt.
func (a *App) icalToken(renew bool) (string, error) {
	if !renew {
		if tok, err := a.DB.Get(icalTokenKey); err != nil || tok != "" {
			return tok, err
		}
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	// Klartext in der (verschlüsselten) Datenbank des Nutzers für die Anzeige, der Hash in system.db zur Zuordnung.
	if err := a.System.DB.DeleteTokens(a.UserID, store.TokenICal); err != nil {
		return "", err
	}
	if err := a.System.DB.AddToken(store.UserToken{UserID: a.UserID, Kind: store.TokenICal, Label: "Kalender-Abo"}, store.Hash(tok)); err != nil {
		return "", err
	}
	return tok, a.DB.Set(icalTokenKey, tok)
}

func (a *App) icalURL(tok string) string { return a.Cfg.PublicURL + "/kalender/" + tok + ".ics" }

// handleCalendar: Link des Abos für die Seite Kalender.
func (a *App) handleCalendar(w http.ResponseWriter, r *http.Request) {
	tok, err := a.icalToken(false)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	terms, err := a.DB.Appointments(true)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"url": a.icalURL(tok), "name": icalName, "termine": len(terms)})
}

// handleCalendarNewLink macht den bisherigen Link ungültig und erzeugt einen neuen.
func (a *App) handleCalendarNewLink(w http.ResponseWriter, r *http.Request) {
	tok, err := a.icalToken(true)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"url": a.icalURL(tok)})
}

// handleCalendarFeed liefert die .ics-Datei; falscher oder alter Link → 404.
func (a *System) handleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	if !a.allow("ical:"+a.clientIP(r), 60) {
		http.Error(w, "Zu viele Anfragen", http.StatusTooManyRequests)
		return
	}
	tok, ok := strings.CutSuffix(r.PathValue("datei"), ".ics")
	t, found := a.DB.TokenUser(store.TokenICal, tok)
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	app, _ := a.app(t.UserID)
	if app == nil {
		http.Error(w, "Daten gesperrt", http.StatusServiceUnavailable)
		return
	}
	jobs, err := app.DB.Appointments(true)
	if err != nil {
		http.Error(w, "Datenbankfehler", 500)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="sprachnotizen.ics"`)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(app.icalFeed(jobs, time.Now())))
}

// icalFeed baut den Kalender (RFC 5545): Zeiten in UTC, ganztägige als Datum, erledigte mit „✓ “ im Titel.
func (a *App) icalFeed(jobs []*store.Job, now time.Time) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(icalFold(s) + "\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//pebble-webhook//Sprachnotizen//DE")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + icalText(icalName))
	line("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	line("X-PUBLISHED-TTL:PT1H")
	utc := func(ms int64) string { return time.UnixMilli(ms).UTC().Format("20060102T150405Z") }
	for _, j := range jobs {
		if j.IsTest {
			continue
		}
		title := nrPrefix(j.Nr) + j.Title
		if j.DoneAt > 0 {
			title = "✓ " + title
		}
		link := a.Cfg.PublicURL + "/eintrag/" + j.ID
		// Nie der Wortlaut: Kalenderdienste speichern den Feed bei sich; nur die kurze Notiz der Auswertung.
		text, place := "", ""
		if d := parseDictation(j.Result); d != nil {
			if d.Notiz != "" {
				text = d.Notiz
			}
			if d.Termin != nil {
				place = d.Termin.Ort
			}
		}
		stamp := j.UpdatedAt
		if stamp == 0 {
			stamp = now.UnixMilli()
		}
		line("BEGIN:VEVENT")
		line("UID:" + j.ID + "@pebble-webhook")
		line("DTSTAMP:" + utc(stamp))
		line("LAST-MODIFIED:" + utc(stamp))
		start := time.UnixMilli(j.TerminAt)
		if j.TerminAllDay {
			line("DTSTART;VALUE=DATE:" + start.Format("20060102"))
			line("DTEND;VALUE=DATE:" + start.AddDate(0, 0, 1).Format("20060102"))
		} else {
			line("DTSTART:" + utc(j.TerminAt))
			line("DTEND:" + utc(j.TerminAt+icalEventMin*60_000))
		}
		line("SUMMARY:" + icalText(title))
		if place != "" {
			line("LOCATION:" + icalText(place))
		}
		line("DESCRIPTION:" + icalText(strings.TrimSpace(text+"\n\n"+link)))
		line("URL:" + link)
		if j.RemindAt > 0 && j.RemindAt <= j.TerminAt && j.DoneAt == 0 {
			line("BEGIN:VALARM")
			line("ACTION:DISPLAY")
			line("DESCRIPTION:" + icalText(title))
			line(fmt.Sprintf("TRIGGER:-PT%dM", (j.TerminAt-j.RemindAt)/60_000))
			line("END:VALARM")
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

// icalText maskiert Text nach RFC 5545 (Backslash, Semikolon, Komma, Zeilenumbruch).
func icalText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", "").Replace(s)
}

// icalFold bricht Zeilen nach höchstens 75 Bytes um, ohne UTF-8-Zeichen zu teilen.
func icalFold(s string) string {
	if len(s) <= 75 {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}
