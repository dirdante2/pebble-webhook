package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/store"
)

// Ring-Lebensdauer: der Pebble Index 01 hat einen fest verbauten Akku (Pebble: 12–15 h Aufnahme). Jede Aufnahme
// wird beim Eingang mit Länge, Art und Geste in ring_recordings protokolliert und bleibt dort, auch wenn Eintrag
// oder Audiodatei gelöscht werden. Schwellen 60/80/95/100 % zeigen einen Hinweis auf allen Seiten, bis er
// bestätigt ist; bei 100 % lässt sich die Kapazität um 30 min erhöhen. Ringwechsel: neuer Ring, alte bleiben.

var ringMu sync.Mutex

// ringLevels: Warnschwellen in Prozent.
var ringLevels = []int{60, 80, 95, 100}

// ringForecastDays: Zeitraum für die Vorhersage bei gleichbleibender Nutzung.
const ringForecastDays = 30

// ringIDRe: Ring-IDs haben immer die Form „#12345“ (Raute und Ziffern).
var ringIDRe = regexp.MustCompile(`^#[0-9]{1,12}$`)

// ringIDErrorKey: zuletzt mit ungültiger Ring-ID empfangener Header {wert, zeit}; leer = kein Fehler.
const ringIDErrorKey = "ring-id-ungueltig"

// ringExtraSeconds: „+30 min“ bei 100 %.
const ringExtraSeconds = 30 * 60

// ringLevel: höchste erreichte Schwelle (0 = keine).
func ringLevel(seconds float64, capacity int64) int {
	if capacity <= 0 {
		return 0
	}
	p := seconds / float64(capacity) * 100
	level := 0
	for _, l := range ringLevels {
		if p >= float64(l) {
			level = l
		}
	}
	return level
}

// audioSeconds: Länge einer Aufnahme aus dem Dateikopf (-1 = unbekannt; lesbar sind m4a/mp4).
func (a *App) audioSeconds(path string) float64 {
	if path == "" {
		return -1
	}
	switch strings.ToLower(filepath.Ext(plainName(path))) {
	case ".m4a", ".mp4":
		b, err := a.readAudio(path)
		if err != nil {
			return -1
		}
		s := mp4Seconds(b)
		if math.IsNaN(s) || math.IsInf(s, 0) || s < 0 {
			return -1
		}
		return s
	}
	return -1
}

// costSeconds: Länge aus den Transkriptionskosten zurückgerechnet (-1 = nicht möglich).
func (a *App) costSeconds(j *store.Job) float64 {
	if j.CostTranscribe == nil || *j.CostTranscribe <= 0 || a.Cfg.PriceTranscribeMin == nil || *a.Cfg.PriceTranscribeMin <= 0 {
		return -1
	}
	return *j.CostTranscribe / *a.Cfg.PriceTranscribeMin * 60
}

// Schätzwerte, wenn eine Aufnahme keine lesbare Länge hat (die Transkription trägt die echte Länge später nach):
// Audio des Rings hat rund 12 kB je Sekunde (gemessen an den Aufnahmen bis 22.11.2026), gesprochen werden
// rund 2,5 Wörter je Sekunde.
const (
	ringBytesPerSecond = 12_000
	ringWordsPerSecond = 2.5
)

// ringTrigger: es gibt nur zwei Gesten; alles Unklare zählt als die erste („Halten“).
func ringTrigger(t string) string {
	if t == "double-click-hold" {
		return t
	}
	return "single-click-hold"
}

// estimateSeconds: Länge aus Dateigröße oder Pebble-Text (mindestens 1 s); jede Aufnahme hat eine Länge.
func estimateSeconds(audioPath, text string) float64 {
	if fi, err := os.Stat(audioPath); audioPath != "" && err == nil && fi.Size() > 0 {
		return math.Max(1, float64(fi.Size())/ringBytesPerSecond)
	}
	return math.Max(1, float64(len(strings.Fields(text)))/ringWordsPerSecond)
}

// recordingOf: Protokollzeile zu einem Eintrag; Länge aus der Datei, sonst aus den Kosten, sonst geschätzt.
func (a *App) recordingOf(j *store.Job, ringNr int64, hw, origin string) store.RingRecording {
	r := store.RingRecording{JobID: j.ID, RingNr: ringNr, HardwareID: hw, ReceivedAt: j.ReceivedAt,
		Mode: j.Mode, Trigger: ringTrigger(j.Trigger), Origin: origin}
	if s := a.audioSeconds(j.AudioPath); s >= 0 {
		r.Seconds, r.SecondsSource = s, store.SecondsFile
	} else if s := a.costSeconds(j); s >= 0 {
		r.Seconds, r.SecondsSource = s, store.SecondsCost
	} else {
		r.Seconds, r.SecondsSource = estimateSeconds(j.AudioPath, j.PebbleText), store.SecondsEstimate
	}
	return r
}

// ensureRingLocked: gibt es noch keinen Ring, wird Ring 1 angelegt und der Bestand nachgetragen (Einträge vom
// Ring samt Papierkorb, Audiodateien ohne Eintrag). ringMu ist gehalten.
func (a *App) ensureRingLocked() (*store.Ring, error) {
	g, err := a.DB.ActiveRing()
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return g, err
	}
	jobs, err := a.DB.RingJobs()
	if err != nil {
		return nil, err
	}
	start := store.Now()
	for _, j := range jobs {
		if j.ReceivedAt < start {
			start = j.ReceivedAt
		}
	}
	g, err = a.DB.StartRing("", start, int64(a.Cfg.RingCapacityHours)*3600, "")
	if err != nil {
		return nil, err
	}
	n := 0
	for _, j := range jobs {
		if ok, err := a.DB.AddRecording(a.recordingOf(j, g.Nr, "", "nachtrag")); err != nil {
			return nil, err
		} else if ok {
			n++
		}
	}
	// Dateien ohne Eintrag (z. B. aus einem älteren Backup): der Dateiname ist die ID des früheren Eintrags.
	root := a.audioRoot()
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || audioMimeOf(p) == "" {
			return nil
		}
		id, web := audioIDOf(p)
		if web {
			return nil // in der Web-App aufgenommen, nicht mit dem Ring
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		r := store.RingRecording{JobID: id, RingNr: g.Nr, ReceivedAt: fi.ModTime().UnixMilli(), Seconds: a.audioSeconds(p),
			Mode: modeOfPath(rel), Trigger: ringTrigger(""), Origin: "nachtrag", SecondsSource: store.SecondsFile}
		if r.Seconds < 0 {
			r.Seconds, r.SecondsSource = estimateSeconds(p, ""), store.SecondsEstimate
		}
		if ok, err := a.DB.AddRecording(r); err == nil && ok {
			n++
		} else if err != nil {
			logf("Ring: Nachtrag %s: %v", p, err)
		}
		return nil
	})
	logf("Ring 1 angelegt, %d Aufnahmen aus dem Bestand nachgetragen", n)
	return g, nil
}

// activeRingID: ID des aktiven Rings ("" = keiner oder ohne ID).
func (a *App) activeRingID() string {
	g, err := a.DB.ActiveRing()
	if err != nil {
		return ""
	}
	return g.HardwareID
}

// initRing: beim Start Ring 1 samt Nachtrag anlegen, falls es noch keinen gibt.
func (a *App) initRing() {
	ringMu.Lock()
	defer ringMu.Unlock()
	if _, err := a.ensureRingLocked(); err != nil {
		logf("Ring anlegen: %v", err)
	}
}

// recordRecording protokolliert eine eingegangene Aufnahme (Webhook, kein Test-Event) und meldet neu erreichte
// Schwellen per Push. hw ist die Ring-ID aus dem eigenen Header (Pflicht). Fehlt sie oder hat sie nicht die Form
// „#12345“, ist das ein Fehler: die Aufnahme zählt beim aktiven Ring ohne ID (Pebble wiederholt abgewiesene
// Zustellungen nur zehnmal – die Aufnahme ginge sonst verloren), der Fehler steht auf allen Seiten (false).
func (a *App) recordRecording(j *store.Job, hw string) bool {
	if j.IsTest {
		return true
	}
	valid := true
	if !ringIDRe.MatchString(hw) {
		valid = false
		logf("Ring: ungültige Ring-ID %q (Eintrag %s) – erwartet #12345", store.Preview(hw, 40), j.ID)
		b, _ := json.Marshal(map[string]any{"wert": store.Preview(hw, 40), "zeit": j.ReceivedAt})
		if err := a.DB.Set(ringIDErrorKey, string(b)); err != nil {
			logf("Ring: %v", err)
		}
		hw = ""
		a.hub.send("ring")
	} else {
		if err := a.DB.Set(ringIDErrorKey, ""); err != nil {
			logf("Ring: %v", err)
		}
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	g, err := a.ensureRingLocked()
	if err != nil {
		logf("Ring: %v", err)
		return valid
	}
	before, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		logf("Ring: %v", err)
		return valid
	}
	if _, err := a.DB.AddRecording(a.recordingOf(j, g.Nr, hw, "eingang")); err != nil {
		logf("Ring: Aufnahme %s protokollieren: %v", j.ID, err)
		return valid
	}
	// Erste Ring-ID für einen Ring ohne ID: übernehmen, ohne Hinweis.
	if hw != "" && g.HardwareID == "" {
		if err := a.DB.UpdateRingID(g.Nr, hw); err != nil {
			logf("Ring: ID übernehmen: %v", err)
		} else {
			g.HardwareID = hw
		}
	}
	after, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		logf("Ring: %v", err)
		return valid
	}
	if l := ringLevel(after.Seconds, g.CapacitySeconds); l > ringLevel(before.Seconds, g.CapacitySeconds) && l > g.WarnedLevel {
		a.notify(config.NotifyRing, a.T("Ring-Nutzung"), a.Tf("Der Ring hat %d %% seiner Aufnahmezeit erreicht.", l), "/status/ring")
	}
	if hw != "" && g.HardwareID != "" && hw != g.HardwareID {
		a.hub.send("ring")
	}
	return valid
}

// ringSeconds trägt die von der Transkription gemeldete Länge nach, wenn die Datei keine lieferte.
func (a *App) ringSeconds(jobID string, seconds float64) {
	if seconds <= 0 {
		return
	}
	if err := a.DB.SetRecordingSeconds(jobID, seconds, store.SecondsOpenAI); err != nil {
		logf("Ring: Länge nachtragen %s: %v", jobID, err)
	}
}

// ringMismatch: die letzte Aufnahme mit Ring-ID trägt eine andere ID als der aktive Ring (seit dessen Start).
func (a *App) ringMismatch(g *store.Ring) map[string]any {
	hw, at, err := a.DB.LastRecordingHW()
	if err != nil {
		logf("Ring: %v", err)
		return nil
	}
	if hw == "" || g.HardwareID == "" || hw == g.HardwareID || at < g.StartedAt {
		return nil
	}
	return map[string]any{"alt": g.HardwareID, "neu": hw, "seit": at}
}

// ringSummary: aktiver Ring kurz (Menü, Hinweis auf allen Seiten).
func (a *App) ringSummary() map[string]any {
	g, err := a.DB.ActiveRing()
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return map[string]any{"fehler": err.Error()}
		}
		return nil
	}
	t, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		return map[string]any{"fehler": err.Error()}
	}
	level := ringLevel(t.Seconds, g.CapacitySeconds)
	var idErr map[string]any
	if v, err := a.DB.Get(ringIDErrorKey); err != nil {
		return map[string]any{"fehler": err.Error()}
	} else if v != "" {
		json.Unmarshal([]byte(v), &idErr)
	}
	return map[string]any{"id_fehler": idErr, "header": a.Cfg.RingIDHeader, "nr": g.Nr, "hardware_id": g.HardwareID, "sekunden": t.Seconds, "kapazitaet_sekunden": g.CapacitySeconds,
		"prozent": t.Seconds / float64(g.CapacitySeconds) * 100, "stufe": level, "bestaetigt": g.WarnedLevel,
		"hinweis": level > g.WarnedLevel, "abweichung": a.ringMismatch(g)}
}

// ringForecast: Datum, an dem bei gleichbleibender Nutzung (Mittel der letzten 30 Tage, bei jüngerem Ring seit
// Start) 80 und 100 % erreicht sind; 0 = schon erreicht, nil = keine Nutzung.
func (a *App) ringForecast(g *store.Ring, used float64) (map[string]any, error) {
	now := store.Now()
	since := now - ringForecastDays*86_400_000
	if g.StartedAt > since {
		since = g.StartedAt
	}
	secs, err := a.DB.RingSecondsSince(g.Nr, since)
	if err != nil {
		return nil, err
	}
	days := float64(now-since) / 86_400_000
	if days < 1 {
		days = 1
	}
	perDay := secs / days
	out := map[string]any{"sekunden_je_tag": perDay, "basis_tage": math.Round(days*10) / 10}
	for _, p := range []int{80, 100} {
		key := "datum" + strconv.Itoa(p)
		rest := float64(g.CapacitySeconds)*float64(p)/100 - used
		switch {
		case rest <= 0:
			out[key] = 0
		case perDay <= 0:
			out[key] = nil
		default:
			out[key] = now + int64(rest/perDay*86_400_000)
		}
	}
	return out, nil
}

type ringDay struct {
	Day     string  `json:"tag"` // JJJJ-MM-TT
	Diktat  float64 `json:"diktat"`
	Planung float64 `json:"planung"`
	Count   int     `json:"anzahl"`
}

// ringDays: Aufnahmezeit je Tag (Ortszeit) eines Rings, lückenlos vom ersten Aufnahmetag bis until.
// Wochen und Monate fasst die Oberfläche daraus zusammen.
func (a *App) ringDays(nr int64, until int64) ([]ringDay, error) {
	rows, err := a.DB.RingRecordings(nr)
	if err != nil || len(rows) == 0 {
		return []ringDay{}, err
	}
	by := map[string]*ringDay{}
	for _, r := range rows {
		k := time.UnixMilli(r.ReceivedAt).Format("2006-01-02")
		d := by[k]
		if d == nil {
			d = &ringDay{Day: k}
			by[k] = d
		}
		d.Count++
		if r.Seconds > 0 {
			if r.Mode == "planung" {
				d.Planung += r.Seconds
			} else {
				d.Diktat += r.Seconds
			}
		}
	}
	first := time.UnixMilli(rows[0].ReceivedAt)
	cur := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	end := time.UnixMilli(until)
	out := []ringDay{}
	for !cur.After(end) {
		k := cur.Format("2006-01-02")
		if d := by[k]; d != nil {
			out = append(out, *d)
		} else {
			out = append(out, ringDay{Day: k})
		}
		cur = cur.AddDate(0, 0, 1)
	}
	return out, nil
}

func (a *App) ringView(g *store.Ring) (map[string]any, error) {
	t, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		return nil, err
	}
	until := store.Now()
	if g.EndedAt > 0 {
		until = g.EndedAt
	}
	v := map[string]any{"nr": g.Nr, "hardware_id": g.HardwareID, "start": g.StartedAt, "ende": g.EndedAt,
		"kapazitaet_sekunden": g.CapacitySeconds, "sekunden": t.Seconds, "prozent": t.Seconds / float64(g.CapacitySeconds) * 100,
		"anzahl": t.Count, "fehlaufnahmen": t.Empty, "geloescht": t.Deleted,
		"diktat":  map[string]any{"sekunden": t.SecondsByMode["diktat"], "anzahl": t.CountByMode["diktat"]},
		"planung": map[string]any{"sekunden": t.SecondsByMode["planung"], "anzahl": t.CountByMode["planung"]},
		"halten":  t.ByTrigger["single-click-hold"], "klick_halten": t.ByTrigger["double-click-hold"],
		"tage": math.Max(1, float64(until-g.StartedAt)/86_400_000)}
	return v, nil
}

// handleRing: Auswertung – aktiver bzw. gewählter Ring (?ring=Nr) mit Tagesverlauf und Vorhersage, alle Ringe zum Vergleich.
func (a *App) handleRing(w http.ResponseWriter, r *http.Request) {
	ringMu.Lock()
	_, err := a.ensureRingLocked()
	ringMu.Unlock()
	if err == nil {
		err = a.DB.SyncRecordingFlags()
	}
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	rings, err := a.DB.Rings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	sel := rings[0]
	if s := r.URL.Query().Get("ring"); s != "" {
		nr, _ := strconv.ParseInt(s, 10, 64)
		if sel, err = a.DB.RingByNr(nr); err != nil {
			writeErr(w, 404, "fehlt", "Ring nicht gefunden")
			return
		}
	}
	all := []map[string]any{}
	var selView map[string]any
	for _, g := range rings {
		v, err := a.ringView(g)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		all = append(all, v)
		if g.Nr == sel.Nr {
			selView = v
		}
	}
	until := store.Now()
	if sel.EndedAt > 0 {
		until = sel.EndedAt
	}
	days, err := a.ringDays(sel.Nr, until)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := map[string]any{"ring": selView, "verlauf": days, "ringe": all, "aktiv": a.ringSummary(),
		"vorgabe_stunden": a.Cfg.RingCapacityHours, "header": a.Cfg.RingIDHeader, "plus_minuten": ringExtraSeconds / 60}
	if sel.EndedAt == 0 {
		f, err := a.ringForecast(sel, selView["sekunden"].(float64))
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		out["prognose"] = f
	}
	writeJSON(w, 200, out)
}

// handleRingSwap: Ring tauschen. Mit „uebernehmen“ gilt die abweichende ID aus dem Header ab ihrer ersten
// Aufnahme; sonst beginnt der neue Ring jetzt.
func (a *App) handleRingSwap(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HardwareID string `json:"hardware_id"`
		Hours      int    `json:"kapazitaet_stunden"`
		Take       bool   `json:"uebernehmen"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Eingabe nicht lesbar")
		return
	}
	in.HardwareID = strings.TrimSpace(in.HardwareID)
	if !ringIDRe.MatchString(in.HardwareID) {
		writeErr(w, 400, "ungueltig", "Ring-ID in der Form #12345")
		return
	}
	if in.Hours == 0 {
		in.Hours = a.Cfg.RingCapacityHours
	}
	if in.Hours < 1 || in.Hours > 1000 {
		writeErr(w, 400, "ungueltig", "Kapazität 1–1000 Stunden")
		return
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	g, err := a.ensureRingLocked()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	at, move := store.Now(), ""
	if in.Take {
		m := a.ringMismatch(g)
		if m == nil || m["neu"] != in.HardwareID {
			writeErr(w, 409, "keine_abweichung", "Keine abweichende Ring-ID mehr – Seite neu laden")
			return
		}
		if at, err = a.DB.FirstRecordingWithHW(in.HardwareID, g.StartedAt); err != nil || at == 0 {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		move = in.HardwareID
	}
	n, err := a.DB.StartRing(in.HardwareID, at, int64(in.Hours)*3600, move)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	logf("Ring %d (%q) gestartet, Ring %d beendet", n.Nr, n.HardwareID, g.Nr)
	a.hub.send("ring")
	writeJSON(w, 200, map[string]any{"nr": n.Nr})
}

// handleRingEdit: Ring-ID und Kapazität eines Rings ändern.
func (a *App) handleRingEdit(w http.ResponseWriter, r *http.Request) {
	nr, _ := strconv.ParseInt(r.PathValue("nr"), 10, 64)
	var in struct {
		HardwareID string  `json:"hardware_id"`
		Hours      float64 `json:"kapazitaet_stunden"`
	}
	if readJSON(r, &in) != nil || in.Hours < 1 || in.Hours > 1000 || !ringIDRe.MatchString(strings.TrimSpace(in.HardwareID)) {
		writeErr(w, 400, "ungueltig", "Kapazität 1–1000 Stunden, Ring-ID in der Form #12345")
		return
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	if err := a.DB.UpdateRing(nr, strings.TrimSpace(in.HardwareID), int64(math.Round(in.Hours*3600))); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "fehlt", "Ring nicht gefunden")
			return
		}
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	a.hub.send("ring")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleRingConfirm: Hinweis zur erreichten Schwelle bestätigen; „plus“ erhöht die Kapazität dauerhaft um 30 min
// (bei 100 %), danach meldet sich der Hinweis beim nächsten Erreichen von 100 % wieder.
func (a *App) handleRingConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plus    bool `json:"plus"`
		IDError bool `json:"id_fehler"`
	}
	readJSON(r, &in)
	if in.IDError {
		if err := a.DB.Set(ringIDErrorKey, ""); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		a.hub.send("ring")
		writeJSON(w, 200, map[string]any{"ring": a.ringSummary()})
		return
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	g, err := a.DB.ActiveRing()
	if err != nil {
		writeErr(w, 404, "fehlt", "Kein aktiver Ring")
		return
	}
	t, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	level := ringLevel(t.Seconds, g.CapacitySeconds)
	if in.Plus {
		if err := a.DB.UpdateRing(g.Nr, g.HardwareID, g.CapacitySeconds+ringExtraSeconds); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		level = ringLevel(t.Seconds, g.CapacitySeconds+ringExtraSeconds)
	}
	if err := a.DB.SetRingWarned(g.Nr, level); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	a.hub.send("ring")
	writeJSON(w, 200, map[string]any{"ring": a.ringSummary()})
}
