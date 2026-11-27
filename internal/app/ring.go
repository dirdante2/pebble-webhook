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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/openai"
	"pebble-webhook/internal/store"
)

// Ring-Lebensdauer: der Pebble Index 01 hat einen fest verbauten Akku (Pebble: 12–15 h Aufnahme). Jede Aufnahme
// wird beim Eingang mit Länge, Art und Geste in ring_recordings protokolliert und bleibt dort, auch wenn Eintrag
// oder Audiodatei gelöscht werden. Schwellen 60/80/95/100 % zeigen einen Hinweis auf allen Seiten, bis er
// bestätigt ist; bei 100 % lässt sich die Kapazität um 30 min erhöhen. Jeder Ring wird an seiner Kennung aus der
// Zustellungs-ID erkannt und für sich gezählt; mehrere Ringe laufen nebeneinander, einen Tausch gibt es nicht.

var ringMu sync.Mutex

// ringLevels: Warnschwellen in Prozent.
var ringLevels = []int{60, 80, 95, 100}

// ringForecastDays: Zeitraum für die Vorhersage bei gleichbleibender Nutzung.
const ringForecastDays = 30

// ringIdleDays: so lange unbenutzt, während ein anderer Ring weiterläuft, fragt die App „inaktiv setzen?“.
const ringIdleDays = 14

// ringIDRe: Ring-IDs haben die Form „#449“ (Raute, dann Ziffern und A–F): das Kürzel eines Rings sind die letzten
// drei Zeichen seiner Kennung. Ringe aus der Zeit des eigenen Headers (bis 4.4.1) tragen bis zu 12 Zeichen.
var ringIDRe = regexp.MustCompile(`^#[0-9A-Fa-f]{1,12}$`)

// ringKennungRe: die Pebble-App nennt den Ring in der Zustellungs-ID: ring_<12 Hex>-<Zähler>-<uuid>
// (gemessen 26.11.2026; vermutlich die Bluetooth-Adresse). Die Kennung identifiziert den Ring.
var ringKennungRe = regexp.MustCompile(`^ring_([0-9A-Fa-f]{12})-`)

// ringKennung: Kennung des Rings aus der Zustellungs-ID ("" = keine).
func ringKennung(delivery string) string {
	if m := ringKennungRe.FindStringSubmatch(delivery); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// ringShort: Kürzel eines Rings = Raute und die letzten drei Zeichen der Kennung (z. B. #449).
func ringShort(kennung string) string {
	if len(kennung) < 3 {
		return ""
	}
	return "#" + kennung[len(kennung)-3:]
}

// appUpdateKey: die Pebble-App meldet sich mit einem anderen User-Agent {alt, neu, zeit}; bleibt bis zur Bestätigung.
// appAgentKey: zuletzt gesehener User-Agent über alle Ringe – ein Update meldet sich einmal, nicht je Ring.
const (
	appUpdateKey = "pebble-app-update"
	appAgentKey  = "pebble-app-agent"
)

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
	step := a.AI.First(openai.AreaTranscribe) // Näherung: Preis des ersten Cloud-Eintrags
	if j.CostTranscribe == nil || *j.CostTranscribe <= 0 || step == nil || step.PriceMinute <= 0 {
		return -1
	}
	return *j.CostTranscribe / step.PriceMinute * 60
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
	// Kein aktiver Ring: gibt es Ringe (alle deaktiviert), gilt der neueste.
	if rings, err := a.DB.Rings(); err != nil || len(rings) > 0 {
		if err != nil {
			return nil, err
		}
		return rings[0], nil
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

// initRing: beim Start Ring 1 samt Nachtrag anlegen, falls es noch keinen gibt, und den Bestand nach der Kennung ordnen.
func (a *App) initRing() {
	ringMu.Lock()
	defer ringMu.Unlock()
	if _, err := a.ensureRingLocked(); err != nil {
		logf("Ring anlegen: %v", err)
		return
	}
	if err := a.ringBackfillLocked(); err != nil {
		logf("Ring: Bestand ordnen: %v", err)
	}
}

// ringBackfillLocked ordnet den Bestand: jede Aufnahme gehört zum Ring ihrer Kennung (aus der Zustellungs-ID ihres
// Eintrags) und trägt die volle Kennung (ring_recordings.hardware_id). Einträge ohne Kennung kommen nicht vom Ring
// (Web-App, Import, alte Daten) und zählen nicht. Das Kürzel am Ring (rings.hardware_id, „#449“) ist nur die lesbare
// Bezeichnung. Läuft bei jedem Start und ändert nur, was abweicht. ringMu ist gehalten.
func (a *App) ringBackfillLocked() error {
	rows, err := a.DB.RingDeliveries()
	if err != nil {
		return err
	}
	moved, dropped := 0, 0
	for _, d := range rows {
		k := ringKennung(d.DeliveryID)
		if k == "" {
			if err := a.DB.DeleteRecording(d.JobID); err != nil {
				return err
			}
			dropped++
			continue
		}
		g, err := a.ringOfLocked(k, d.ReceivedAt)
		if err != nil {
			return err
		}
		if d.RingNr != g.Nr || d.HardwareID != k {
			if err := a.DB.MoveRecording(d.JobID, g.Nr, k); err != nil {
				return err
			}
			moved++
		}
	}
	rings, err := a.DB.Rings()
	if err != nil {
		return err
	}
	for _, g := range rings {
		if g.Kennung == "" {
			continue
		}
		if hw := ringShort(g.Kennung); g.HardwareID != hw {
			if err := a.DB.UpdateRingID(g.Nr, hw); err != nil {
				return err
			}
			logf("Ring %d: ID %q → %s (Kürzel der Kennung)", g.Nr, g.HardwareID, hw)
			g.HardwareID = hw
		}
		// Zeilen ohne Eintrag (endgültig gelöscht, Nachtrag aus Dateien) tragen die Kennung ihres Rings.
		if err := a.DB.SetRecordingsHW(g.Nr, g.Kennung); err != nil {
			return err
		}
	}
	if moved > 0 {
		logf("Ring: %d Aufnahmen nach ihrer Kennung zugeordnet", moved)
	}
	if dropped > 0 {
		logf("Ring: %d Einträge ohne Ring-Kennung aus der Ring-Nutzung entfernt", dropped)
	}
	return nil
}

// recordRecording protokolliert eine eingegangene Aufnahme (Webhook, kein Test-Event) bei ihrem Ring und meldet neu
// erreichte Schwellen per Push. Der Ring ergibt sich aus der Kennung in der Zustellungs-ID (kennung); eine neue
// Kennung legt einen weiteren Ring an. Der Ring schickt immer eine Kennung – eine Zustellung ohne kommt nicht von
// ihm und zählt nicht. agent ist der User-Agent der Pebble-App.
func (a *App) recordRecording(j *store.Job, kennung, agent string) {
	if j.IsTest || kennung == "" {
		return
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	_, err := a.ensureRingLocked()
	var g *store.Ring
	if err == nil {
		g, err = a.ringOfLocked(kennung, j.ReceivedAt)
	}
	if err != nil {
		logf("Ring: %v", err)
		return
	}
	before, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		logf("Ring: %v", err)
		return
	}
	if _, err := a.DB.AddRecording(a.recordingOf(j, g.Nr, kennung, "eingang")); err != nil {
		logf("Ring: Aufnahme %s protokollieren: %v", j.ID, err)
		return
	}
	if g.EndedAt > 0 { // inaktiver Ring sendet wieder: aktiv setzen und melden
		if err := a.DB.ReactivateRing(g.Nr); err != nil {
			logf("Ring %d: wieder aktiv setzen: %v", g.Nr, err)
		} else if ng, err := a.DB.RingByNr(g.Nr); err == nil {
			logf("Ring %d: sendet wieder – aktiv gesetzt (war %s)", g.Nr, g.InactiveReason)
			g = ng
			a.telemetryRingEvent(g)
			a.hub.send("ring")
		}
	}
	after, err := a.DB.RingTotals(g.Nr)
	if err != nil {
		logf("Ring: %v", err)
		return
	}
	if l := ringLevel(after.Seconds, g.CapacitySeconds); g.EndedAt == 0 && l > ringLevel(before.Seconds, g.CapacitySeconds) && l > g.WarnedLevel {
		a.notify(config.NotifyRing, a.T("Ring-Nutzung"), a.Tf("Der Ring hat %d %% seiner Aufnahmezeit erreicht.", l), "/status/ring?ring="+strconv.FormatInt(g.Nr, 10))
	}
	a.ringAgent(g, agent, j.ReceivedAt)
}

// ringOfLocked: der Ring zu einer Kennung. Unbekannte Kennung: sie geht an einen Ring ohne Kennung (Bestand bis
// 4.4.1) – zuerst einen mit demselben Kürzel, dann den, in dessen Laufzeit der Zeitpunkt at fällt, dann einen ganz
// ohne ID (Ring 1 aus dem Nachtrag); sonst entsteht ein neuer Ring. ringMu ist gehalten.
func (a *App) ringOfLocked(kennung string, at int64) (*store.Ring, error) {
	hw := ringShort(kennung)
	g, err := a.DB.RingByKennung(kennung)
	if err == nil && g.HardwareID != hw {
		// ID aus der Zeit der Eingabe von Hand: das Kürzel der Kennung gilt.
		if err = a.DB.UpdateRingID(g.Nr, hw); err == nil {
			logf("Ring %d: ID %q → %s (Kürzel der Kennung)", g.Nr, g.HardwareID, hw)
			g.HardwareID = hw
		}
	}
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return g, err
	}
	rings, err := a.DB.Rings()
	if err != nil {
		return nil, err
	}
	var take *store.Ring
	for _, r := range rings {
		if r.Kennung != "" {
			continue
		}
		if strings.EqualFold(r.HardwareID, hw) {
			take = r
			break
		}
		covers := r.StartedAt <= at && (r.EndedAt == 0 || at < r.EndedAt)
		if take == nil && (covers || r.HardwareID == "") {
			take = r
		}
	}
	if take == nil {
		g, err := a.DB.AddRing(hw, kennung, at, int64(a.Cfg.RingCapacityHours)*3600)
		if err == nil {
			logf("Ring %d (%s, Kennung %s) angelegt", g.Nr, hw, kennung)
		}
		return g, err
	}
	if err := a.DB.SetRingKennung(take.Nr, kennung); err != nil {
		return nil, err
	}
	if err := a.DB.UpdateRingID(take.Nr, hw); err != nil {
		return nil, err
	}
	take.Kennung, take.HardwareID = kennung, hw
	return take, nil
}

// ringAgent hält die Pebble-App am Ring fest (bei Änderung überschrieben) und meldet ein Update der App einmal.
func (a *App) ringAgent(g *store.Ring, agent string, at int64) {
	if agent == "" {
		return
	}
	agent = store.Preview(agent, 200)
	if agent != g.AppAgent {
		if err := a.DB.SetRingAgent(g.Nr, agent); err != nil {
			logf("Ring: Pebble-App festhalten: %v", err)
			return
		}
		g.AppAgent = agent
	}
	last, err := a.DB.Get(appAgentKey)
	if err != nil || last == agent {
		return
	}
	if err := a.DB.Set(appAgentKey, agent); err != nil {
		logf("Ring: %v", err)
		return
	}
	if last != "" {
		logf("Ring: Pebble-App %q → %q", last, agent)
		b, _ := json.Marshal(map[string]any{"alt": last, "neu": agent, "zeit": at})
		if err := a.DB.Set(appUpdateKey, string(b)); err != nil {
			logf("Ring: %v", err)
		}
		a.hub.send("ring")
	}
}

// kvJSON: Hinweis aus kv als Objekt (nil = keiner).
func (a *App) kvJSON(key string) (map[string]any, error) {
	v, err := a.DB.Get(key)
	if err != nil || v == "" {
		return nil, err
	}
	var m map[string]any
	json.Unmarshal([]byte(v), &m)
	return m, nil
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

// ringSummary: Ringe kurz für alle Seiten – „balken“ ist der aktive Ring mit der höchsten Nutzung (Seitenleiste;
// fehlt, wenn keiner aktiv ist), „hinweise“ sind die aktiven Ringe mit einer erreichten, noch nicht bestätigten
// Schwelle, „frage_inaktiv“ die aktiven Ringe, die seit ringIdleDays nicht benutzt wurden, während ein anderer
// aktiver Ring danach Aufnahmen hat (Frage „inaktiv setzen?“, bis sie beantwortet ist), dazu ein Update der
// Pebble-App. nil = es gibt noch keinen Ring.
func (a *App) ringSummary() map[string]any {
	rings, err := a.DB.Rings()
	if err != nil {
		return map[string]any{"fehler": err.Error()}
	}
	if len(rings) == 0 {
		return nil
	}
	appUpdate, err := a.kvJSON(appUpdateKey)
	if err != nil {
		return map[string]any{"fehler": err.Error()}
	}
	out := map[string]any{"app_update": appUpdate}
	hints, idle := []map[string]any{}, []map[string]any{}
	var bar map[string]any
	last := map[int64]int64{}
	for _, g := range rings {
		at, err := a.DB.RingLastRecording(g.Nr)
		if err != nil {
			return map[string]any{"fehler": err.Error()}
		}
		if at == 0 {
			at = g.StartedAt
		}
		last[g.Nr] = at
	}
	idleBefore := store.Now() - ringIdleDays*86_400_000
	for i := len(rings) - 1; i >= 0; i-- { // älteste zuerst
		g := rings[i]
		t, err := a.DB.RingTotals(g.Nr)
		if err != nil {
			return map[string]any{"fehler": err.Error()}
		}
		percent := t.Seconds / float64(g.CapacitySeconds) * 100
		v := map[string]any{"nr": g.Nr, "hardware_id": g.HardwareID, "beschreibung": g.Description, "sekunden": t.Seconds,
			"kapazitaet_sekunden": g.CapacitySeconds, "prozent": percent, "stufe": ringLevel(t.Seconds, g.CapacitySeconds)}
		if l := v["stufe"].(int); g.EndedAt == 0 && l > g.WarnedLevel && l > 0 {
			hints = append(hints, v)
		}
		if g.EndedAt == 0 && g.AskDeclined == "" && last[g.Nr] < idleBefore {
			for _, o := range rings {
				if o.Nr != g.Nr && o.EndedAt == 0 && last[o.Nr] > last[g.Nr] {
					idle = append(idle, map[string]any{"nr": g.Nr, "hardware_id": g.HardwareID, "beschreibung": g.Description,
						"letzte": last[g.Nr], "neuer": map[string]any{"nr": o.Nr, "hardware_id": o.HardwareID, "beschreibung": o.Description}})
					break
				}
			}
		}
		if g.EndedAt == 0 && (bar == nil || percent >= bar["prozent"].(float64)) {
			bar = v
		}
	}
	out["hinweise"] = hints
	out["frage_inaktiv"] = idle
	if bar != nil {
		out["balken"] = bar
	}
	return out
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
	v := map[string]any{"nr": g.Nr, "hardware_id": g.HardwareID, "kennung": g.Kennung, "app_agent": g.AppAgent, "beschreibung": g.Description,
		"start": g.StartedAt, "ende": g.EndedAt, "inaktiv_grund": g.InactiveReason,
		"kapazitaet_sekunden": g.CapacitySeconds, "sekunden": t.Seconds, "prozent": t.Seconds / float64(g.CapacitySeconds) * 100,
		"anzahl": t.Count, "fehlaufnahmen": t.Empty, "geloescht": t.Deleted,
		"diktat":  map[string]any{"sekunden": t.SecondsByMode["diktat"], "anzahl": t.CountByMode["diktat"]},
		"planung": map[string]any{"sekunden": t.SecondsByMode["planung"], "anzahl": t.CountByMode["planung"]},
		"halten":  t.ByTrigger["single-click-hold"], "klick_halten": t.ByTrigger["double-click-hold"], "anteile": t.Parts, "anteile_anzahl": t.PartCounts,
		"halten_sekunden": t.SecsTrigger["single-click-hold"], "klick_halten_sekunden": t.SecsTrigger["double-click-hold"],
		"tage": math.Max(1, float64(until-g.StartedAt)/86_400_000)}
	return v, nil
}

// handleRing: Auswertung – zuletzt benutzter bzw. gewählter Ring (?ring=Nr) mit Tagesverlauf und Vorhersage, alle Ringe zum Vergleich.
func (a *App) handleRing(w http.ResponseWriter, r *http.Request) {
	ringMu.Lock()
	sel, err := a.ensureRingLocked()
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
		"vorgabe_stunden": a.Cfg.RingCapacityHours, "plus_minuten": ringExtraSeconds / 60}
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

// handleRingEdit: Kapazität und Beschreibung eines Rings ändern (die ID kommt aus der Kennung).
func (a *App) handleRingEdit(w http.ResponseWriter, r *http.Request) {
	nr, _ := strconv.ParseInt(r.PathValue("nr"), 10, 64)
	var in struct {
		Hours       float64 `json:"kapazitaet_stunden"`
		Description *string `json:"beschreibung"` // fehlt = unverändert
	}
	if readJSON(r, &in) != nil || in.Hours < 1 || in.Hours > 1000 {
		writeErr(w, 400, "ungueltig", "Kapazität 1–1000 Stunden")
		return
	}
	if in.Description != nil {
		*in.Description = strings.TrimSpace(*in.Description)
		if len([]rune(*in.Description)) > 60 {
			writeErr(w, 400, "ungueltig", "Beschreibung höchstens 60 Zeichen")
			return
		}
	}
	ringMu.Lock()
	defer ringMu.Unlock()
	g, err := a.DB.RingByNr(nr)
	if err == nil {
		err = a.DB.UpdateRing(nr, g.HardwareID, int64(math.Round(in.Hours*3600)))
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "fehlt", "Ring nicht gefunden")
			return
		}
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if in.Description != nil {
		if err := a.DB.SetRingDescription(nr, *in.Description); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
	}
	a.hub.send("ring")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleRingActive: Ring inaktiv setzen (mit Grund aus store.RingReasons) oder wieder aktiv. Ein inaktiver Ring fehlt
// in der Seitenleiste und warnt nicht mehr; jede Änderung geht als Ring-Ereignis an die Statistik.
func (a *App) handleRingActive(w http.ResponseWriter, r *http.Request) {
	nr, _ := strconv.ParseInt(r.PathValue("nr"), 10, 64)
	var in struct {
		Active bool   `json:"aktiv"`
		Reason string `json:"grund"`
	}
	if readJSON(r, &in) != nil || (!in.Active && !slices.Contains(store.RingReasons, in.Reason)) {
		writeErr(w, 400, "ungueltig", "Grund fehlt oder ist unbekannt")
		return
	}
	ringMu.Lock()
	g, err := a.DB.RingByNr(nr)
	if err == nil && in.Active != (g.EndedAt == 0) {
		at := int64(0)
		if !in.Active {
			at = store.Now()
		}
		if err = a.DB.SetRingInactive(nr, at, in.Reason); err == nil {
			g, err = a.DB.RingByNr(nr)
		}
		if err == nil {
			a.telemetryRingEvent(g)
		}
	}
	ringMu.Unlock()
	if err != nil {
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

// handleRingAskDecline: Frage „Ring inaktiv setzen?“ ablehnen; für diesen Ring kommt sie nicht wieder.
func (a *App) handleRingAskDecline(w http.ResponseWriter, r *http.Request) {
	nr, _ := strconv.ParseInt(r.PathValue("nr"), 10, 64)
	ringMu.Lock()
	_, err := a.DB.RingByNr(nr)
	if err == nil {
		err = a.DB.SetRingAskDeclined(nr, store.Now())
	}
	ringMu.Unlock()
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "fehlt", "Ring nicht gefunden")
			return
		}
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	a.hub.send("ring")
	writeJSON(w, 200, map[string]any{"ring": a.ringSummary()})
}

// handleRingConfirm: Hinweis zur erreichten Schwelle eines Rings (ring, sonst der zuletzt benutzte) bestätigen; „plus“ erhöht die Kapazität dauerhaft um 30 min
// (bei 100 %), danach meldet sich der Hinweis beim nächsten Erreichen von 100 % wieder.
func (a *App) handleRingConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plus      bool  `json:"plus"`
		AppUpdate bool  `json:"app_update"`
		Ring      int64 `json:"ring"` // 0 = zuletzt benutzter Ring
	}
	readJSON(r, &in)
	if in.AppUpdate {
		if err := a.DB.Set(appUpdateKey, ""); err != nil {
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
	if in.Ring > 0 {
		g, err = a.DB.RingByNr(in.Ring)
	}
	if err != nil {
		writeErr(w, 404, "fehlt", "Ring nicht gefunden")
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
