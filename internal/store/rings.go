package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Ringe und ihre Aufnahmen: der Pebble Index 01 hat einen fest verbauten Akku für rund 12–15 Stunden Aufnahme.
// Jede Aufnahme wird beim Eingang mit Länge, Art und Geste protokolliert; die Zeile bleibt, wenn Eintrag oder
// Audiodatei gelöscht werden. Ein Ringwechsel beendet den aktiven Ring und startet einen neuen; alte Ringe
// bleiben zum Vergleich.
const ringSchema = `
CREATE TABLE IF NOT EXISTS rings (
  nr INTEGER PRIMARY KEY AUTOINCREMENT,
  hardware_id TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  ended_at INTEGER NOT NULL DEFAULT 0,
  capacity_seconds INTEGER NOT NULL,
  warned_level INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS ring_recordings (
  job_id TEXT PRIMARY KEY,
  ring_nr INTEGER NOT NULL,
  hardware_id TEXT NOT NULL DEFAULT '',
  received_at INTEGER NOT NULL,
  seconds REAL NOT NULL DEFAULT -1,
  seconds_source TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT '',
  trigger TEXT NOT NULL DEFAULT '',
  origin TEXT NOT NULL DEFAULT 'eingang'
);
CREATE INDEX IF NOT EXISTS ring_recordings_ring ON ring_recordings(ring_nr, received_at);
`

// Herkunft der Sekunden einer Aufnahme.
const (
	SecondsFile     = "datei"      // aus dem Kopf der Audiodatei (mvhd)
	SecondsOpenAI   = "openai"     // von der Transkription gemeldete Länge
	SecondsCost     = "kosten"     // aus den Transkriptionskosten zurückgerechnet
	SecondsEstimate = "schaetzung" // geschätzt (z. B. früher gelöschte leere Aufnahmen)
)

type Ring struct {
	Nr              int64  `json:"nr"`
	HardwareID      string `json:"hardware_id"`
	StartedAt       int64  `json:"start"`
	EndedAt         int64  `json:"ende"`
	CapacitySeconds int64  `json:"kapazitaet_sekunden"`
	WarnedLevel     int    `json:"gewarnt"`
	Kennung         string `json:"kennung"`      // Kennung des Rings aus der Zustellungs-ID (12 Hex-Zeichen); "" = noch keine gesehen
	AppAgent        string `json:"app_agent"`    // User-Agent der Pebble-App bei der letzten Aufnahme dieses Rings
	Description     string `json:"beschreibung"` // freier Text des Nutzers (z. B. „silber“)
}

type RingRecording struct {
	JobID         string
	RingNr        int64
	HardwareID    string // volle Kennung des Rings (bis 4.4.1: die von Hand gesetzte ID „#12345“)
	ReceivedAt    int64
	Seconds       float64 // -1 = unbekannt
	SecondsSource string
	Mode          string
	Trigger       string
	Origin        string // eingang | nachtrag | schaetzung
}

const ringCols = `nr, hardware_id, started_at, ended_at, capacity_seconds, warned_level, kennung, app_agent, beschreibung`

func scanRing(r scanner) (*Ring, error) {
	var g Ring
	err := r.Scan(&g.Nr, &g.HardwareID, &g.StartedAt, &g.EndedAt, &g.CapacitySeconds, &g.WarnedLevel, &g.Kennung, &g.AppAgent, &g.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

// ActiveRing: der Ring für den Balken in der Seitenleiste – unter den aktiven (ended_at = 0) der mit der höchsten
// Nutzung (Anteil der Kapazität); ErrNotFound, wenn kein Ring aktiv ist. Ringe laufen nebeneinander, jeder wird an
// seiner Kennung erkannt; ended_at > 0 heißt „inaktiv“ (vom Nutzer gesetzt; bis 4.4.1: durch einen Ringtausch
// beendet) – ein inaktiver Ring fehlt nur in der Seitenleiste und zählt, warnt und meldet sonst wie jeder andere.
func (s *Store) ActiveRing() (*Ring, error) {
	return scanRing(s.DB.QueryRow(`SELECT ` + ringCols + ` FROM rings WHERE ended_at = 0 ORDER BY
		COALESCE((SELECT SUM(x.seconds) FROM ring_recordings x WHERE x.ring_nr = rings.nr AND x.seconds > 0), 0) * 1.0 / MAX(capacity_seconds, 1) DESC,
		nr DESC LIMIT 1`))
}

// SetRingEnded: Ring deaktivieren (at > 0) oder wieder aktivieren (0).
func (s *Store) SetRingEnded(nr, at int64) error {
	_, err := s.DB.Exec(`UPDATE rings SET ended_at = ? WHERE nr = ?`, at, nr)
	return err
}

// RingDelivery: protokollierte Aufnahme mit der Zustellungs-ID ihres Eintrags (für den Nachtrag der Kennung).
type RingDelivery struct {
	JobID, DeliveryID, HardwareID string
	RingNr, ReceivedAt            int64
}

// RingDeliveries: alle protokollierten Aufnahmen, deren Eintrag noch da ist, älteste zuerst.
func (s *Store) RingDeliveries() ([]RingDelivery, error) {
	rows, err := s.DB.Query(`SELECT r.job_id, j.delivery_id, r.hardware_id, r.ring_nr, r.received_at FROM ring_recordings r
		JOIN jobs j ON j.id = r.job_id ORDER BY r.received_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RingDelivery
	for rows.Next() {
		var d RingDelivery
		if err := rows.Scan(&d.JobID, &d.DeliveryID, &d.HardwareID, &d.RingNr, &d.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MoveRecording ordnet eine protokollierte Aufnahme einem Ring zu.
func (s *Store) MoveRecording(jobID string, ringNr int64, hardwareID string) error {
	_, err := s.DB.Exec(`UPDATE ring_recordings SET ring_nr = ?, hardware_id = ? WHERE job_id = ?`, ringNr, hardwareID, jobID)
	return err
}

// SetRecordingsHW schreibt die Kennung eines Rings an all seine Aufnahmen (Zeilen aus der Zeit vor der Kennung).
func (s *Store) SetRecordingsHW(ringNr int64, hardwareID string) error {
	_, err := s.DB.Exec(`UPDATE ring_recordings SET hardware_id = ? WHERE ring_nr = ? AND hardware_id != ?`, hardwareID, ringNr, hardwareID)
	return err
}

// DeleteRecording nimmt eine protokollierte Aufnahme aus der Ring-Nutzung.
func (s *Store) DeleteRecording(jobID string) error {
	_, err := s.DB.Exec(`DELETE FROM ring_recordings WHERE job_id = ?`, jobID)
	return err
}

// RingByKennung: der Ring mit dieser Kennung; ErrNotFound, wenn sie neu ist.
func (s *Store) RingByKennung(kennung string) (*Ring, error) {
	return scanRing(s.DB.QueryRow(`SELECT `+ringCols+` FROM rings WHERE kennung = ? ORDER BY nr DESC LIMIT 1`, kennung))
}

// AddRing legt einen weiteren Ring an; die übrigen laufen weiter.
func (s *Store) AddRing(hardwareID, kennung string, at, capacitySeconds int64) (*Ring, error) {
	res, err := s.DB.Exec(`INSERT INTO rings (hardware_id, kennung, started_at, capacity_seconds, created_at) VALUES (?, ?, ?, ?, ?)`,
		hardwareID, kennung, at, capacitySeconds, Now())
	if err != nil {
		return nil, err
	}
	nr, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.RingByNr(nr)
}

func (s *Store) RingByNr(nr int64) (*Ring, error) {
	return scanRing(s.DB.QueryRow(`SELECT `+ringCols+` FROM rings WHERE nr = ?`, nr))
}

// Rings: alle Ringe, neuester zuerst.
func (s *Store) Rings() ([]*Ring, error) {
	rows, err := s.DB.Query(`SELECT ` + ringCols + ` FROM rings ORDER BY nr DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Ring
	for rows.Next() {
		g, err := scanRing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// StartRing beendet den aktiven Ring zum Zeitpunkt at und legt einen neuen an, der dort beginnt. Aufnahmen ab at
// mit der neuen Hardware-ID (moveHW, leer = keine) wandern zum neuen Ring.
func (s *Store) StartRing(hardwareID string, at, capacitySeconds int64, moveHW string) (*Ring, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var old sql.NullInt64
	if err := tx.QueryRow(`SELECT nr FROM rings WHERE ended_at = 0 ORDER BY nr DESC LIMIT 1`).Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE rings SET ended_at = ? WHERE ended_at = 0`, at); err != nil {
		return nil, err
	}
	res, err := tx.Exec(`INSERT INTO rings (hardware_id, started_at, capacity_seconds, created_at) VALUES (?, ?, ?, ?)`,
		hardwareID, at, capacitySeconds, Now())
	if err != nil {
		return nil, err
	}
	nr, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if old.Valid && moveHW != "" {
		if _, err := tx.Exec(`UPDATE ring_recordings SET ring_nr = ? WHERE ring_nr = ? AND hardware_id = ? AND received_at >= ?`,
			nr, old.Int64, moveHW, at); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.RingByNr(nr)
}

// UpdateRing ändert Hardware-ID und Kapazität eines Rings; die Warnstufe wird neu bewertet.
func (s *Store) UpdateRing(nr int64, hardwareID string, capacitySeconds int64) error {
	res, err := s.DB.Exec(`UPDATE rings SET hardware_id = ?, capacity_seconds = ?, warned_level = 0 WHERE nr = ?`, hardwareID, capacitySeconds, nr)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateRingID setzt nur die Hardware-ID (erste ID aus dem Header).
func (s *Store) UpdateRingID(nr int64, hardwareID string) error {
	_, err := s.DB.Exec(`UPDATE rings SET hardware_id = ? WHERE nr = ?`, hardwareID, nr)
	return err
}

// ringDeviceCols: Angaben zum Gerät je Ring. Kommen ohne neuen Schema-Stand dazu: nur Spalten mit Vorgabe, ältere
// Versionen lesen und schreiben rings mit benannten Spalten und stört das nicht.
var ringDeviceCols = []string{"kennung", "app_agent", "beschreibung"}

func addRingDeviceCols(db *sql.DB) error {
	for _, c := range ringDeviceCols {
		if _, err := db.Exec(`ALTER TABLE rings ADD COLUMN ` + c + ` TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

// SetRingKennung setzt die Kennung eines Rings (erste Aufnahme mit Kennung).
func (s *Store) SetRingKennung(nr int64, kennung string) error {
	_, err := s.DB.Exec(`UPDATE rings SET kennung = ? WHERE nr = ?`, kennung, nr)
	return err
}

// SetRingAgent überschreibt den User-Agent der Pebble-App eines Rings.
func (s *Store) SetRingAgent(nr int64, agent string) error {
	_, err := s.DB.Exec(`UPDATE rings SET app_agent = ? WHERE nr = ?`, agent, nr)
	return err
}

func (s *Store) SetRingDescription(nr int64, text string) error {
	_, err := s.DB.Exec(`UPDATE rings SET beschreibung = ? WHERE nr = ?`, text, nr)
	return err
}

func (s *Store) SetRingWarned(nr int64, level int) error {
	_, err := s.DB.Exec(`UPDATE rings SET warned_level = ? WHERE nr = ?`, level, nr)
	return err
}

// AddRecording protokolliert eine Aufnahme; je Eintrag nur einmal (false = gab es schon).
func (s *Store) AddRecording(r RingRecording) (bool, error) {
	if r.Origin == "" {
		r.Origin = "eingang"
	}
	res, err := s.DB.Exec(`INSERT OR IGNORE INTO ring_recordings (job_id, ring_nr, hardware_id, received_at, seconds, seconds_source, mode, trigger, origin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.JobID, r.RingNr, r.HardwareID, r.ReceivedAt, r.Seconds, r.SecondsSource, r.Mode, r.Trigger, r.Origin)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetRecordingSeconds trägt eine Länge nach, wenn keine bekannt ist oder nur eine ungenauere (Kosten, Schätzung).
func (s *Store) SetRecordingSeconds(jobID string, seconds float64, source string) error {
	_, err := s.DB.Exec(`UPDATE ring_recordings SET seconds = ?, seconds_source = ? WHERE job_id = ? AND (seconds < 0 OR seconds_source IN (?, ?))`,
		seconds, source, jobID, SecondsCost, SecondsEstimate)
	return err
}

// syncFlags übernimmt „leer“ und „gelöscht“ aus jobs; Zeilen ohne Eintrag behalten ihren letzten Stand.
const syncFlags = `UPDATE ring_recordings SET
	empty = (SELECT j.empty FROM jobs j WHERE j.id = ring_recordings.job_id),
	deleted_at = (SELECT j.deleted_at FROM jobs j WHERE j.id = ring_recordings.job_id)
	WHERE EXISTS (SELECT 1 FROM jobs j WHERE j.id = ring_recordings.job_id)`

func migrateRecordingFlags(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE ring_recordings ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE ring_recordings ADD COLUMN empty INTEGER NOT NULL DEFAULT 0`); err == nil {
		if _, err := db.Exec(`UPDATE ring_recordings SET empty = 1 WHERE NOT EXISTS (SELECT 1 FROM jobs j WHERE j.id = ring_recordings.job_id)`); err != nil {
			return err
		}
	} else if !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	_, err := db.Exec(syncFlags)
	return err
}

// SyncRecordingFlags gleicht „leer“ und „gelöscht“ aller Aufnahmen mit ihren Einträgen ab (vor jeder Auswertung).
func (s *Store) SyncRecordingFlags() error {
	_, err := s.DB.Exec(syncFlags)
	return err
}

// SyncRecordingFlagsOf gleicht eine Aufnahme ab – vor dem endgültigen Löschen ihres Eintrags.
func (s *Store) SyncRecordingFlagsOf(jobID string) error {
	_, err := s.DB.Exec(syncFlags+` AND job_id = ?`, jobID)
	return err
}

func (s *Store) RecordingCount() (n int, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM ring_recordings`).Scan(&n)
	return
}

// RingTotals: Summen eines Rings.
type RingTotals struct {
	Count         int                `json:"anzahl"`
	Seconds       float64            `json:"sekunden"`      // Summe der bekannten Längen
	Unknown       int                `json:"ohne_dauer"`    // Aufnahmen ohne Länge
	Estimated     int                `json:"geschaetzt"`    // Aufnahmen mit geschätzter Länge
	Empty         int                `json:"fehlaufnahmen"` // ohne Sprache (Fehlauslösung)
	Deleted       int                `json:"geloescht"`     // vom Nutzer gelöscht (Papierkorb oder endgültig)
	SecondsByMode map[string]float64 `json:"sekunden_art"`  // diktat | planung | …
	CountByMode   map[string]int     `json:"anzahl_art"`
	ByTrigger     map[string]int     `json:"gesten"`
	First, Last   int64              `json:"-"`
}

func (s *Store) RingTotals(nr int64) (*RingTotals, error) {
	t := &RingTotals{SecondsByMode: map[string]float64{}, CountByMode: map[string]int{}, ByTrigger: map[string]int{}}
	rows, err := s.DB.Query(`SELECT mode, trigger, COUNT(*), COALESCE(SUM(CASE WHEN seconds >= 0 THEN seconds END), 0),
		SUM(seconds < 0), SUM(seconds_source = ?), SUM(empty), SUM(deleted_at > 0), MIN(received_at), MAX(received_at)
		FROM ring_recordings WHERE ring_nr = ? GROUP BY mode, trigger`, SecondsEstimate, nr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mode, trig string
		var n, unknown, est, empty, deleted int
		var secs float64
		var first, last int64
		if err := rows.Scan(&mode, &trig, &n, &secs, &unknown, &est, &empty, &deleted, &first, &last); err != nil {
			return nil, err
		}
		t.Count += n
		t.Seconds += secs
		t.Unknown += unknown
		t.Estimated += est
		t.Empty += empty
		t.Deleted += deleted
		t.SecondsByMode[mode] += secs
		t.CountByMode[mode] += n
		t.ByTrigger[trig] += n
		if t.First == 0 || first < t.First {
			t.First = first
		}
		if last > t.Last {
			t.Last = last
		}
	}
	return t, rows.Err()
}

// RingSecondsSince: bekannte Sekunden eines Rings ab since (für die Vorhersage).
func (s *Store) RingSecondsSince(nr, since int64) (secs float64, err error) {
	err = s.DB.QueryRow(`SELECT COALESCE(SUM(seconds), 0) FROM ring_recordings WHERE ring_nr = ? AND seconds >= 0 AND received_at >= ?`, nr, since).Scan(&secs)
	return
}

// RecordingRow: eine Aufnahme mit Zeitpunkt, für die Monatsauswertung (Monate rechnet die App in Ortszeit).
type RecordingRow struct {
	ReceivedAt int64
	Seconds    float64
	Mode       string
}

func (s *Store) RingRecordings(nr int64) ([]RecordingRow, error) {
	rows, err := s.DB.Query(`SELECT received_at, seconds, mode FROM ring_recordings WHERE ring_nr = ? ORDER BY received_at`, nr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingRow
	for rows.Next() {
		var r RecordingRow
		if err := rows.Scan(&r.ReceivedAt, &r.Seconds, &r.Mode); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastRecordingHW: Hardware-ID der letzten Aufnahme, die eine mitbrachte, mit Zeitpunkt ("" = keine).
func (s *Store) LastRecordingHW() (hw string, at int64, err error) {
	err = s.DB.QueryRow(`SELECT hardware_id, received_at FROM ring_recordings WHERE hardware_id != '' ORDER BY received_at DESC LIMIT 1`).Scan(&hw, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return
}

// FirstRecordingWithHW: erste Aufnahme mit dieser Hardware-ID ab since (0 = keine).
func (s *Store) FirstRecordingWithHW(hw string, since int64) (at int64, err error) {
	err = s.DB.QueryRow(`SELECT COALESCE(MIN(received_at), 0) FROM ring_recordings WHERE hardware_id = ? AND received_at >= ?`, hw, since).Scan(&at)
	return
}

// HasRecording: ob zur Notiz eine Ring-Aufnahme gezählt ist.
func (s *Store) HasRecording(jobID string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM ring_recordings WHERE job_id = ?`, jobID).Scan(&n)
	return n > 0, err
}
