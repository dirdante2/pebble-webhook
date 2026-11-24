// Package store kapselt die SQLite-Datenbank: Aufträge, Planungen, Anmeldung, Push.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  delivery_id TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL,
  trigger TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL,
  is_test INTEGER NOT NULL DEFAULT 0,
  recorded_at INTEGER NOT NULL,
  received_at INTEGER NOT NULL,
  audio_path TEXT NOT NULL DEFAULT '',
  audio_mime TEXT NOT NULL DEFAULT '',
  audio_deleted_at INTEGER NOT NULL DEFAULT 0,
  pebble_text TEXT NOT NULL DEFAULT '',
  transcript TEXT NOT NULL DEFAULT '',
  transcript_source TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  failed INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_try_at INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  planning_id TEXT NOT NULL DEFAULT '',
  round_no INTEGER NOT NULL DEFAULT 0,
  assign_how TEXT NOT NULL DEFAULT '',
  force_planning TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  cost_usd REAL,
  cost_transcribe_usd REAL,
  cost_aux_usd REAL,
  limit_hit INTEGER NOT NULL DEFAULT 0,
  joplin_id TEXT NOT NULL DEFAULT '',
  joplin_pushed_at INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_received ON jobs(received_at);
CREATE INDEX IF NOT EXISTS jobs_planning ON jobs(planning_id, round_no);
CREATE TABLE IF NOT EXISTS plannings (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  plan TEXT NOT NULL DEFAULT '',
  joplin_id TEXT NOT NULL,
  joplin_dirty INTEGER NOT NULL DEFAULT 0,
  joplin_pushed_at INTEGER NOT NULL DEFAULT 0,
  joplin_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  closed_at INTEGER NOT NULL DEFAULT 0,
  closed_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS joplin_deletes (joplin_id TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS passkeys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  credential TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  passkey_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS enroll_tokens (
  token_hash TEXT PRIMARY KEY,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS push_subs (
  endpoint TEXT PRIMARY KEY,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS memory (
 id TEXT PRIMARY KEY, text TEXT NOT NULL, status TEXT NOT NULL, source TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS memory_status ON memory(status, updated_at);
CREATE TABLE IF NOT EXISTS lists (` + listsBody + `);
CREATE TABLE IF NOT EXISTS list_items (
  id TEXT PRIMARY KEY,
  list_id TEXT NOT NULL,
  job_id TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  facts TEXT NOT NULL DEFAULT '[]',
  done_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS list_items_list ON list_items(list_id);
CREATE INDEX IF NOT EXISTS list_items_job ON list_items(job_id);
CREATE TABLE IF NOT EXISTS list_groups (
  name TEXT PRIMARY KEY COLLATE NOCASE,
  description TEXT NOT NULL DEFAULT '',
  described_at INTEGER NOT NULL DEFAULT 0
);
`

// listsBody: Spalten der Tabelle lists. project leer = gewöhnliche Liste; sonst ist die Liste ein Thema dieses Projekts.
// Derselbe Name darf je Projekt einmal vorkommen.
const listsBody = `
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  project TEXT NOT NULL DEFAULT '',
  joplin_id TEXT NOT NULL,
  joplin_dirty INTEGER NOT NULL DEFAULT 0,
  joplin_pushed_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(project, name)
`

// Verarbeitungsstände eines Auftrags, in Reihenfolge.
const (
	StateNew         = "neu"           // Audio liegt, noch nicht transkribiert
	StateTranscribed = "transkribiert" // Text liegt vor
	StateAssigned    = "zugeordnet"    // Planung: Runde hängt an einer Planung
	StateProcessed   = "bearbeitet"    // Titel bzw. Recherche fertig, Joplin offen
	StateDone        = "fertig"
)

var ErrNotFound = errors.New("nicht gefunden")

type Store struct {
	DB  *sql.DB
	key []byte // Schlüssel der Datei (nil = Klartext); Snapshots werden gleich verschlüsselt
}

type Job struct {
	ID               string
	DeliveryID       string
	Source           string // pebble | web
	Trigger          string
	Mode             string // diktat | planung | test
	IsTest           bool
	RecordedAt       int64
	ReceivedAt       int64
	AudioPath        string
	AudioMime        string
	AudioDeletedAt   int64
	PebbleText       string
	Transcript       string
	TranscriptSource string
	Title            string
	State            string
	Failed           bool
	Attempts         int
	NextTryAt        int64
	Error            string
	PlanningID       string
	RoundNo          int
	AssignHow        string   // schluesselwort | ki | neu | web | umgehaengt
	ForcePlanning    string   // "", "neu" oder Planungs-Id: Zuordnung steht fest
	Result           string   // JSON: Planung nach dieser Runde bzw. Auswertung des Diktats
	CostUSD          *float64 // Recherche
	CostTranscribe   *float64 // Transkription; nil = unbekannt
	CostAux          *float64 // Kurztitel und Zuordnung; nil = unbekannt
	LimitHit         bool
	JoplinID         string
	JoplinPushedAt   int64
	UpdatedAt        int64
	Empty            bool  // keine Sprache erkannt (Fehlauslösung)
	FailedAt         int64 // Zeitpunkt des Dauerfehlers; 0 = keiner
	ReadAt           int64 // Zeitpunkt des ersten Öffnens in der Web-App; 0 = ungelesen
	Nr               int64 // fortlaufende Nummer der Notiz (#N); 0 bei Planungsrunden, dort trägt die Planung die Nummer
	// Termin (nur Notizen der Art termin; 0 = kein Termin): Beginn, ganztägig, Zeitpunkt der Erinnerung,
	// Zeitpunkt der gesendeten Erinnerung und der Bestätigung durch den Nutzer.
	TerminAt     int64
	TerminAllDay bool
	RemindAt     int64
	RemindedAt   int64
	// DoneAt: vom Nutzer als erledigt markiert (Notiz, Termin); 0 = offen. Unabhängig von der Auswertung:
	// SaveJob schreibt es nie, nur SetJobDone.
	DoneAt int64
	// FlagsChanged: RemindedAt wurde an diesem Struct bewusst geändert. Nur dann schreibt SaveJob es; sonst
	// überschriebe ein Worker, der den Auftrag vorher geladen hat, eine inzwischen gesendete Erinnerung.
	FlagsChanged bool
	DeletedAt    int64 // im Papierkorb seit; 0 = sichtbar
}

type Planning struct {
	ID             string
	Title          string
	Status         string // offen | geschlossen
	Plan           string
	JoplinID       string
	JoplinDirty    bool
	JoplinPushedAt int64
	JoplinError    string
	CreatedAt      int64
	UpdatedAt      int64
	ClosedAt       int64
	ClosedBy       string // nutzer | automatik
	Nr             int64  // fortlaufende Nummer (#N), ein Zähler gemeinsam mit den Notizen
	DeletedAt      int64  // im Papierkorb seit; 0 = sichtbar
}

func Now() int64 { return time.Now().UnixMilli() }

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// DerivedID liefert eine feste 32-stellige Id (Joplin-Format) zu einem Schlüssel.
func DerivedID(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:16])
}

func Hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// OpenLegacy öffnet die Datenbank bis 2.x (pebble.db, Klartext) und bringt sie nur auf deren Endstand (Schema 1) –
// für den Umzug auf system.db + daten.db je Nutzer.
func OpenLegacy(path string) (*Store, error) {
	db, err := openSQLite(path, nil, false)
	if err != nil {
		return nil, err
	}
	if err := quickCheck(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateTo(db, 0, 1); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

// OpenUser öffnet die Datenbank eines Nutzers (key nil = Klartext, nur in Tests), bringt sie auf SchemaVersion und
// prüft, dass alle Zeilen ihm gehören – eine vertauschte Datei startet nicht.
func OpenUser(path string, key []byte, uid int64) (*Store, error) {
	db, err := openSQLite(path, key, false)
	if err != nil {
		return nil, err
	}
	if err := quickCheck(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateTo(db, uid, SchemaVersion); err != nil {
		db.Close()
		return nil, err
	}
	for _, t := range userTables {
		var x int
		err := db.QueryRow(`SELECT 1 FROM `+t+` WHERE user_id != ? LIMIT 1`, uid).Scan(&x)
		if err == nil {
			db.Close()
			return nil, fmt.Errorf("Datenbank %s gehört nicht zu Nutzer %d (Tabelle %s)", path, uid, t)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			db.Close()
			return nil, fmt.Errorf("Tabelle %s: %w", t, err)
		}
	}
	return &Store{DB: db, key: key}, nil
}

// quickCheck: beschädigte (oder mit falschem Schlüssel geöffnete) Datenbank – nicht starten.
func quickCheck(db *sql.DB) error {
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		if err == nil {
			err = fmt.Errorf("%s", check)
		}
		return fmt.Errorf("Datenbank beschädigt oder Schlüssel falsch (quick_check): %w", err)
	}
	return nil
}

// SchemaVersion: Stand der Datenbank, den dieses Programm kennt (PRAGMA user_version) – in system.db und in jeder
// daten.db derselbe. Jede Änderung an Tabellen oder Daten ist ein weiterer Schritt in migrations (Nutzerdaten)
// bzw. sysMigrations (system.db); Schritte werden nie geändert oder entfernt, nur angehängt.
var SchemaVersion = len(migrations)

// migrations: Eintrag i hebt die Datenbank eines Nutzers von Stand i auf i+1 und läuft samt neuem Stand in einer
// Transaktion (scheitert er, bleibt die Datenbank auf Stand i). Eintrag 0 bleibt leer: Stand 1 ist migrateBase.
var migrations = []func(tx *sql.Tx, uid int64) error{
	nil,         // Stand 1: migrateBase (Endstand bis 2.x)
	splitByUser, // Stand 2 (3.0.0): Daten eines Nutzers – Anmeldung und Systemwerte liegen in system.db
}

// userTables: Tabellen einer daten.db, alle mit Spalte user_id (= Besitzer der Datei).
var userTables = []string{"jobs", "plannings", "lists", "list_items", "list_groups", "memory", "rings", "ring_recordings",
	"push_subs", "joplin_deletes", "kv"}

// systemKV: Systemwerte, die bis 2.x in pebble.db lagen und ab 3.0 nur in system.db stehen (oder in user_tokens).
var systemKV = []string{"ersteinrichtung:code", "update-releases", "auto-update-letzt", "backup-letzt", "spenden-link",
	"vapid_public", "vapid_private", "webauthn_user", "oauth"}

// splitByUser: Anmeldung und Systemwerte entfernen, user_id an jede Tabelle.
func splitByUser(tx *sql.Tx, uid int64) error {
	for _, t := range []string{"passkeys", "sessions", "enroll_tokens"} {
		if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM kv WHERE k LIKE ?`, settingPrefix+"%"); err != nil {
		return err
	}
	for _, k := range systemKV {
		if _, err := tx.Exec(`DELETE FROM kv WHERE k = ?`, k); err != nil {
			return err
		}
	}
	for _, t := range userTables {
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN user_id INTEGER NOT NULL DEFAULT %d`, t, uid)); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}

// migrateTo bringt die Datenbank Schritt für Schritt auf Stand target – auch über übersprungene Versionen hinweg.
// Eine neuere Datenbank (nach Rückwechsel auf eine ältere Version) wird nicht angefasst: zurück nur über ein Backup.
func migrateTo(db *sql.DB, uid int64, target int) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("Schema: %w", err)
	}
	if v > SchemaVersion {
		return fmt.Errorf("Datenbank hat Schema %d, diese Version kennt nur %d – zurück nur über ein Backup", v, SchemaVersion)
	}
	if v == 0 {
		// Bestand bis 2.0.0: idempotent mit eigenen Transaktionen; bricht er ab, läuft er beim nächsten Start erneut.
		if err := migrateBase(db); err != nil {
			return err
		}
		if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
			return fmt.Errorf("Schema 1: %w", err)
		}
		v = 1
	}
	for ; v < target; v++ {
		if err := migrateStep(db, v, func(tx *sql.Tx) error { return migrations[v](tx, uid) }); err != nil {
			return fmt.Errorf("Schema %d: %w", v+1, err)
		}
	}
	return nil
}

func migrateStep(db *sql.DB, v int, step func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step != nil {
		if err := step(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
		return err
	}
	return tx.Commit()
}

// DBSchema liest den Schema-Stand einer Datenbankdatei nur lesend (für den Installer: Wechsel nur, wenn das Ziel ihn kennt).
func DBSchema(path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	db, err := OpenReadOnly(path, nil)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var v int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

// migrateBase: Schema-Stand 1 – alle Tabellen, Spalten und Datenumbauten bis 2.0.0 (je Schritt idempotent).
func migrateBase(db *sql.DB) error {
	if _, err := db.Exec(schema + ringSchema); err != nil {
		return fmt.Errorf("Schema: %w", err)
	}
	// Spalten, die nach der ersten Fassung dazukamen (bestehende Datenbanken).
	for _, col := range []string{"jobs ADD COLUMN cost_transcribe_usd REAL", "jobs ADD COLUMN cost_aux_usd REAL",
		"plannings ADD COLUMN closed_by TEXT NOT NULL DEFAULT ''",
		"jobs ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'", "jobs ADD COLUMN tag_suggestions TEXT NOT NULL DEFAULT '[]'",
		"jobs ADD COLUMN joplin_tags TEXT NOT NULL DEFAULT '[]'", "jobs ADD COLUMN joplin_dirty INTEGER NOT NULL DEFAULT 0",
		"plannings ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'", "plannings ADD COLUMN tag_suggestions TEXT NOT NULL DEFAULT '[]'",
		"plannings ADD COLUMN joplin_tags TEXT NOT NULL DEFAULT '[]'",
		"jobs ADD COLUMN empty INTEGER NOT NULL DEFAULT 0", "jobs ADD COLUMN failed_at INTEGER NOT NULL DEFAULT 0",
		"list_items ADD COLUMN planning_id TEXT NOT NULL DEFAULT ''",
		"jobs ADD COLUMN nr INTEGER NOT NULL DEFAULT 0", "plannings ADD COLUMN nr INTEGER NOT NULL DEFAULT 0",
		"jobs ADD COLUMN termin_at INTEGER NOT NULL DEFAULT 0", "jobs ADD COLUMN termin_ganztag INTEGER NOT NULL DEFAULT 0",
		"jobs ADD COLUMN erinnerung_at INTEGER NOT NULL DEFAULT 0", "jobs ADD COLUMN erinnert_at INTEGER NOT NULL DEFAULT 0",
		"jobs ADD COLUMN termin_ok_at INTEGER NOT NULL DEFAULT 0",
		// Papierkorb: Zeitpunkt des Löschens durch den Nutzer; 0 = sichtbar. Gemeinsam Gelöschtes trägt denselben Stempel.
		"jobs ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0", "plannings ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0",
		"list_items ADD COLUMN deleted_at INTEGER NOT NULL DEFAULT 0"} {
		if _, err := db.Exec(`ALTER TABLE ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("Schema: %w", err)
		}
	}
	// Gelesen-Stand: Was beim Anlegen der Spalte schon da ist, gilt als gelesen.
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN read_at INTEGER NOT NULL DEFAULT 0`); err == nil {
		if _, err := db.Exec(`UPDATE jobs SET read_at = received_at`); err != nil {
			return fmt.Errorf("Schema: %w", err)
		}
	} else if !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("Schema: %w", err)
	}
	// Erledigt-Stand je Notiz (21.11.2026): übernimmt bestätigte Termine (termin_ok_at, seitdem unbenutzt) und
	// Notizen, deren Listenpunkte alle abgehakt sind.
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN erledigt_at INTEGER NOT NULL DEFAULT 0`); err == nil {
		for _, q := range []string{
			`UPDATE jobs SET erledigt_at = termin_ok_at WHERE termin_ok_at > 0`,
			`UPDATE jobs SET erledigt_at = (SELECT MAX(done_at) FROM list_items WHERE job_id = jobs.id)
			 WHERE erledigt_at = 0 AND mode = 'diktat' AND EXISTS (SELECT 1 FROM list_items WHERE job_id = jobs.id)
			 AND NOT EXISTS (SELECT 1 FROM list_items WHERE job_id = jobs.id AND done_at = 0)`,
		} {
			if _, err := db.Exec(q); err != nil {
				return fmt.Errorf("Schema: %w", err)
			}
		}
	} else if !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("Schema: %w", err)
	}
	// Einsortieren abgeschlossener Planungen: was beim Anlegen der Spalte schon geschlossen ist, gilt als beurteilt.
	if _, err := db.Exec(`ALTER TABLE plannings ADD COLUMN sorted_at INTEGER NOT NULL DEFAULT 0`); err == nil {
		if _, err := db.Exec(`UPDATE plannings SET sorted_at = updated_at WHERE status = 'geschlossen'`); err != nil {
			return fmt.Errorf("Schema: %w", err)
		}
	} else if !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("Schema: %w", err)
	}
	// Altbestand: Dauerfehler ohne Zeitpunkt bekommen den der letzten Änderung; leere Transkriptionen werden als leer markiert.
	for _, q := range []string{
		`UPDATE jobs SET failed_at = updated_at WHERE failed = 1 AND failed_at = 0`,
		`UPDATE jobs SET empty = 1 WHERE failed = 1 AND empty = 0 AND transcript = ''
			AND (error LIKE '%Transkription ist leer%' OR error LIKE '%kein Transkript mitgeschickt%')`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("Schema: %w", err)
		}
	}
	// Fehlaufnahmen und Gelöschtes je Aufnahme (22.11.2026): dauerhaft in ring_recordings, weil leere Aufnahmen und
	// Endgelöschtes aus jobs verschwinden. Beim Anlegen fehlt ein Eintrag nur nach dem Purge leerer Aufnahmen
	// (der Papierkorb wurde vor den Ringen nie endgültig geleert) – solche Zeilen gelten als Fehlaufnahme.
	if err := migrateRecordingFlags(db); err != nil {
		return fmt.Errorf("Schema: Aufnahmen: %w", err)
	}
	if err := migrateLists(db); err != nil {
		return fmt.Errorf("Schema: Listen: %w", err)
	}
	// Beschreibung je Liste (von der KI wöchentlich aus den jüngsten Punkten gepflegt).
	for _, col := range []string{"lists ADD COLUMN description TEXT NOT NULL DEFAULT ''", "lists ADD COLUMN described_at INTEGER NOT NULL DEFAULT 0"} {
		if _, err := db.Exec(`ALTER TABLE ` + col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("Schema: %w", err)
		}
	}
	// Feste Nummer je Liste (L12): beim Anlegen der Spalte nach Alter vergeben.
	if _, err := db.Exec(`ALTER TABLE lists ADD COLUMN nr INTEGER NOT NULL DEFAULT 0`); err == nil {
		if _, err := db.Exec(`UPDATE lists SET nr = (SELECT COUNT(*) FROM lists o WHERE o.created_at < lists.created_at OR (o.created_at = lists.created_at AND o.id <= lists.id))`); err != nil {
			return fmt.Errorf("Schema: Listennummern: %w", err)
		}
	} else if !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("Schema: %w", err)
	}
	// Jede Gruppe, die schon Listen hat, steht auch in list_groups.
	if _, err := db.Exec(`INSERT OR IGNORE INTO list_groups (name) SELECT DISTINCT project FROM lists WHERE project != ''`); err != nil {
		return fmt.Errorf("Schema: Gruppen: %w", err)
	}
	// Termine (21.11.2026): ganztägig nur noch, wenn ausdrücklich gesagt; bisherige ganztägige werden einmalig 07:00.
	if _, err := db.Exec(`INSERT INTO kv (k, v) VALUES ('termine_7uhr', '1')`); err == nil {
		if _, err := db.Exec(`UPDATE jobs SET termin_at = termin_at + 7*3600000, termin_ganztag = 0 WHERE termin_ganztag = 1`); err != nil {
			return fmt.Errorf("Schema: Termine: %w", err)
		}
	} else if !strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("Schema: %w", err)
	}
	// Nachtrag: deren Erinnerung stand auf Mitternacht; sie gilt jetzt zum Termin (07:00).
	if _, err := db.Exec(`INSERT INTO kv (k, v) VALUES ('termine_7uhr_erinnerung', '1')`); err == nil {
		if _, err := db.Exec(`UPDATE jobs SET erinnerung_at = termin_at WHERE termin_at > 0 AND erinnerung_at = termin_at - 7*3600000 AND erinnert_at = 0`); err != nil {
			return fmt.Errorf("Schema: Termine: %w", err)
		}
	} else if !strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("Schema: %w", err)
	}
	if err := backfillNr(db); err != nil {
		return fmt.Errorf("Schema: Nummern: %w", err)
	}
	// Suchindex der ersten Fassung (ohne eigene Tags-Spalte) verwerfen; SearchSync baut ihn neu auf.
	var ftsTags int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('suche') WHERE name = 'tags'`).Scan(&ftsTags); err == nil && ftsTags == 0 {
		db.Exec(`DROP TABLE IF EXISTS suche`)
		db.Exec(`DELETE FROM kv WHERE k = 'suche_stand'`)
	}
	// Indizes für großen Bestand (zehntausende Einträge): Verlauf, Zähler und Termine lesen nur den Index,
	// nicht die breiten Zeilen mit Wortlaut und Auswertung.
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS jobs_verlauf ON jobs(planning_id, deleted_at, received_at, mode, termin_at, erledigt_at, read_at, empty)`,
		`CREATE INDEX IF NOT EXISTS jobs_termin ON jobs(termin_at) WHERE termin_at > 0`,
		`CREATE INDEX IF NOT EXISTS list_items_done ON list_items(list_id, deleted_at, done_at)`,
		// Suchindex (Volltext, Trigramme: Teilwörter ab 3 Zeichen, Groß/klein egal auch bei Umlauten). Je Notiz bzw.
		// Planung eine Zeile; SearchSync hält ihn über updated_at nach.
		`CREATE VIRTUAL TABLE IF NOT EXISTS suche USING fts5(kind UNINDEXED, ref UNINDEXED, text, tags, tokenize = 'trigram case_sensitive 0')`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("Schema: Index: %w", err)
		}
	}
	return nil
}

// migrateLists baut die Tabelle lists einmalig um: früher war der Name allein eindeutig,
// jetzt ist es Projekt + Name. SQLite kann eine Spalten-Eindeutigkeit nicht nachträglich lösen.
func migrateLists(db *sql.DB) error {
	var def string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'lists'`).Scan(&def); err != nil {
		return err
	}
	if strings.Contains(def, "project") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE lists_neu (` + listsBody + `)`,
		`INSERT INTO lists_neu (id, name, project, joplin_id, joplin_dirty, joplin_pushed_at, created_at, updated_at)
			SELECT id, name, '', joplin_id, joplin_dirty, joplin_pushed_at, created_at, updated_at FROM lists`,
		`DROP TABLE lists`,
		`ALTER TABLE lists_neu RENAME TO lists`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const nrKey = "next_nr"

// backfillNr nummeriert einmalig den Bestand: Notizen und Planungen gemischt in der Reihenfolge ihres Eingangs.
// Danach wird alles Übertragene neu nach Joplin geschoben, damit die Nummer auch dort steht.
func backfillNr(db *sql.DB) error {
	var have int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kv WHERE k = ?`, nrKey).Scan(&have); err != nil || have > 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT 'j', id, received_at FROM jobs WHERE mode = 'diktat' AND planning_id = ''
		UNION ALL SELECT 'p', id, created_at FROM plannings ORDER BY 3, 2`)
	if err != nil {
		return err
	}
	type ref struct{ kind, id string }
	var refs []ref
	for rows.Next() {
		var r ref
		var at int64
		if err := rows.Scan(&r.kind, &r.id, &at); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, r := range refs {
		table := "jobs"
		if r.kind == "p" {
			table = "plannings"
		}
		if _, err := tx.Exec(`UPDATE `+table+` SET nr = ? WHERE id = ?`, i+1, r.id); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE jobs SET joplin_dirty = 1 WHERE mode = 'diktat' AND joplin_pushed_at != 0`,
		`UPDATE plannings SET joplin_dirty = 1 WHERE plan != ''`,
		`UPDATE lists SET joplin_dirty = 1`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO kv (k, v) VALUES (?, ?)`, nrKey, fmt.Sprint(len(refs)+1)); err != nil {
		return err
	}
	return tx.Commit()
}

// takeNr vergibt die nächste fortlaufende Nummer innerhalb einer Transaktion.
func takeNr(tx *sql.Tx) (int64, error) {
	var v string
	if err := tx.QueryRow(`SELECT v FROM kv WHERE k = ?`, nrKey).Scan(&v); err != nil {
		return 0, fmt.Errorf("Nummernzähler: %w", err)
	}
	var nr int64
	if _, err := fmt.Sscan(v, &nr); err != nil || nr < 1 {
		return 0, fmt.Errorf("Nummernzähler unlesbar: %q", v)
	}
	_, err := tx.Exec(`UPDATE kv SET v = ? WHERE k = ?`, fmt.Sprint(nr+1), nrKey)
	return nr, err
}

func (s *Store) Close() error { return s.DB.Close() }

const jobCols = `id, delivery_id, source, trigger, mode, is_test, recorded_at, received_at, audio_path, audio_mime,
 audio_deleted_at, pebble_text, transcript, transcript_source, title, state, failed, attempts, next_try_at, error,
 planning_id, round_no, assign_how, force_planning, result, cost_usd, limit_hit, joplin_id, joplin_pushed_at, updated_at,
 cost_transcribe_usd, cost_aux_usd, empty, failed_at, read_at, nr, termin_at, termin_ganztag, erinnerung_at, erinnert_at, erledigt_at, deleted_at`

type scanner interface{ Scan(...any) error }

func scanJob(r scanner) (*Job, error) {
	var j Job
	var cost, costT, costA sql.NullFloat64
	err := r.Scan(&j.ID, &j.DeliveryID, &j.Source, &j.Trigger, &j.Mode, &j.IsTest, &j.RecordedAt, &j.ReceivedAt,
		&j.AudioPath, &j.AudioMime, &j.AudioDeletedAt, &j.PebbleText, &j.Transcript, &j.TranscriptSource, &j.Title,
		&j.State, &j.Failed, &j.Attempts, &j.NextTryAt, &j.Error, &j.PlanningID, &j.RoundNo, &j.AssignHow,
		&j.ForcePlanning, &j.Result, &cost, &j.LimitHit, &j.JoplinID, &j.JoplinPushedAt, &j.UpdatedAt, &costT, &costA, &j.Empty, &j.FailedAt, &j.ReadAt, &j.Nr,
		&j.TerminAt, &j.TerminAllDay, &j.RemindAt, &j.RemindedAt, &j.DoneAt, &j.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if cost.Valid {
		j.CostUSD = &cost.Float64
	}
	if costT.Valid {
		j.CostTranscribe = &costT.Float64
	}
	if costA.Valid {
		j.CostAux = &costA.Float64
	}
	return &j, nil
}

// InsertJob legt einen Auftrag an. created=false heißt: die Zustellungskennung war schon da.
func (s *Store) InsertJob(j *Job) (created bool, err error) {
	j.UpdatedAt = Now()
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Nur Notizen tragen eine Nummer; Planungsrunden laufen unter der Nummer ihrer Planung.
	j.Nr = 0
	if j.Mode == "diktat" {
		if j.Nr, err = takeNr(tx); err != nil {
			return false, err
		}
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO jobs (`+jobCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.DeliveryID, j.Source, j.Trigger, j.Mode, j.IsTest, j.RecordedAt, j.ReceivedAt, j.AudioPath, j.AudioMime,
		j.AudioDeletedAt, j.PebbleText, j.Transcript, j.TranscriptSource, j.Title, j.State, j.Failed, j.Attempts,
		j.NextTryAt, j.Error, j.PlanningID, j.RoundNo, j.AssignHow, j.ForcePlanning, j.Result, j.CostUSD, j.LimitHit,
		j.JoplinID, j.JoplinPushedAt, j.UpdatedAt, j.CostTranscribe, j.CostAux, j.Empty, j.FailedAt, j.ReadAt, j.Nr,
		j.TerminAt, j.TerminAllDay, j.RemindAt, j.RemindedAt, j.DoneAt, j.DeletedAt)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		// Zustellung schon bekannt: die Nummer wird nicht verbraucht.
		j.Nr = 0
		return false, nil
	}
	return true, tx.Commit()
}

func (s *Store) DeliveryKnown(id string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM jobs WHERE delivery_id = ?`, id).Scan(&n)
	return n > 0, err
}

// Job liefert einen sichtbaren Auftrag; einer im Papierkorb gilt als nicht vorhanden (dafür JobAny).
func (s *Store) Job(id string) (*Job, error) {
	return scanJob(s.DB.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id = ? AND deleted_at = 0`, id))
}

// JobAny liefert einen Auftrag auch aus dem Papierkorb.
func (s *Store) JobAny(id string) (*Job, error) {
	return scanJob(s.DB.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
}

func (s *Store) JobByDelivery(id string) (*Job, error) {
	return scanJob(s.DB.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE delivery_id = ?`, id))
}

// DeliveriesWithPrefix liefert die Zustell-Kennungen mit diesem Präfix (ohne Präfix), auch aus dem Papierkorb.
func (s *Store) DeliveriesWithPrefix(prefix string) ([]string, error) {
	rows, err := s.DB.Query(`SELECT substr(delivery_id, ?) FROM jobs WHERE substr(delivery_id, 1, ?) = ? ORDER BY received_at`,
		len(prefix)+1, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) queryJobs(q string, args ...any) ([]*Job, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// History liefert Aufträge absteigend nach Eingang; before=0 beginnt beim neuesten.
func (s *Store) History(before int64, limit int) ([]*Job, error) {
	if before <= 0 {
		before = 1 << 62
	}
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND received_at < ? ORDER BY received_at DESC LIMIT ?`, before, limit)
}

// HistRef verweist auf einen Eintrag des Verlaufs: einen einzelnen Auftrag oder eine ganze Planung.
type HistRef struct {
	Kind string // job | planung
	ID   string
	At   int64 // Eingang des Auftrags bzw. der jüngsten Runde
}

// historyQuery baut die Unterabfrage des Verlaufs: Planungen zählen als ein Eintrag mit dem Zeitpunkt
// ihrer jüngsten Runde. kind filtert auf "diktat" oder "planung"; leer = alles. tags: jeder muss vorkommen (UND).
// since > 0 beschränkt auf Ungelesenes und alles ab diesem Zeitpunkt; 0 = der ganze Verlauf.
// mode: "" = alle; "offen" = Notiz/Termin nicht erledigt, Planung nicht geschlossen (Zähler);
// "ungelesen" = noch nicht aufgerufen (Zähler „neu“).
// before/limit > 0 (nur HistoryRefs): jeder Teil liest höchstens limit Einträge vor before – über den Index in
// Zeitfolge, statt den ganzen Bestand zu sammeln und zu sortieren.
func historyQuery(kind string, tags []string, since int64, mode string, before int64, limit int) (string, []any) {
	var args []any
	// Tags liegen als JSON-Liste in der Spalte; Tag-Namen enthalten nie Anführungs- oder LIKE-Zeichen.
	var likes []any
	for _, t := range tags {
		if t != "" {
			likes = append(likes, `%"`+t+`"%`)
		}
	}
	tagWhere := strings.Repeat(` AND tags LIKE ?`, len(likes))
	q := `SELECT 'job' AS kind, id, received_at AS at FROM jobs WHERE deleted_at = 0 AND planning_id = ''` + tagWhere
	args = append(args, likes...)
	switch kind {
	case "diktat":
		q += ` AND mode = 'diktat' AND termin_at = 0`
	case "termin":
		q += ` AND mode = 'diktat' AND termin_at > 0`
	case "planung":
		q += ` AND mode = 'planung'`
	}
	if since > 0 {
		q += ` AND received_at >= ?`
		args = append(args, since)
	}
	switch mode {
	case "offen":
		q += ` AND erledigt_at = 0`
	case "ungelesen":
		q += ` AND read_at = 0`
	}
	if limit > 0 {
		q = `SELECT * FROM (` + q + ` AND received_at < ? ORDER BY received_at DESC LIMIT ?)`
		args = append(args, before, limit)
	}
	if kind != "diktat" && kind != "termin" {
		q += ` UNION ALL SELECT 'planung', planning_id, MAX(received_at) FROM jobs WHERE deleted_at = 0 AND planning_id != ''
			AND planning_id IN (SELECT id FROM plannings WHERE deleted_at = 0)`
		switch mode {
		case "offen":
			q += ` AND planning_id IN (SELECT id FROM plannings WHERE status != 'geschlossen')`
		case "ungelesen":
			q += ` AND planning_id IN (SELECT planning_id FROM jobs WHERE planning_id != '' AND deleted_at = 0 AND read_at = 0)`
		}
		if len(likes) > 0 {
			q += ` AND planning_id IN (SELECT id FROM plannings WHERE 1` + tagWhere + `)`
			args = append(args, likes...)
		}
		q += ` GROUP BY planning_id`
		var having []string
		if since > 0 {
			having = append(having, `MAX(received_at) >= ?`)
			args = append(args, since)
		}
		if limit > 0 {
			having = append(having, `MAX(received_at) < ?`)
			args = append(args, before)
		}
		if len(having) > 0 {
			q += ` HAVING ` + strings.Join(having, ` AND `)
		}
	}
	return q, args
}

// HistoryRefs liefert den Verlauf absteigend; before=0 beginnt beim neuesten. mode wie historyQuery ("" oder "offen").
func (s *Store) HistoryRefs(kind string, tags []string, before int64, limit int, since int64, mode string) ([]HistRef, error) {
	if before <= 0 {
		before = 1 << 62
	}
	q, args := historyQuery(kind, tags, since, mode, before, limit)
	args = append(args, before, limit)
	rows, err := s.DB.Query(`SELECT kind, id, at FROM (`+q+`) WHERE at < ? ORDER BY at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistRef
	for rows.Next() {
		var r HistRef
		if err := rows.Scan(&r.Kind, &r.ID, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HistoryCount zählt die Einträge des Verlaufs unter denselben Filtern wie HistoryRefs.
func (s *Store) HistoryCount(kind string, tags []string, since int64, mode string) (n int, err error) {
	q, args := historyQuery(kind, tags, since, mode, 0, 0)
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM (`+q+`)`, args...).Scan(&n)
	return
}

// HistoryUnreadCount zählt die noch nicht aufgerufenen Einträge eines Filters seit since (Zähler „neu“).
func (s *Store) HistoryUnreadCount(kind string, tags []string, since int64) (n int, err error) {
	q, args := historyQuery(kind, tags, since, "ungelesen", 0, 0)
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM (`+q+`)`, args...).Scan(&n)
	return
}

// ItemRefs liefert je Listenpunkt seine Kennung: „59“ bzw. „59.k“, wenn die Notiz/Planung mehrere Punkte hat
// (k nach Anlage, über alle Listen; Punkte im Papierkorb zählen nicht).
// ItemTags: Tags jedes Listenpunkts aus seiner Notiz bzw. Planung (nur Punkte mit Tags) – für den Tag-Filter
// der Listen in der Web-App.
func (s *Store) ItemTags() (map[string][]string, error) {
	rows, err := s.DB.Query(`SELECT i.id, CASE WHEN i.planning_id != '' THEN COALESCE(p.tags, '[]') ELSE COALESCE(j.tags, '[]') END FROM list_items i
		LEFT JOIN jobs j ON i.job_id != '' AND j.id = i.job_id
		LEFT JOIN plannings p ON i.planning_id != '' AND p.id = i.planning_id
		WHERE i.deleted_at = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id, t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		if l := decodeList(t); len(l) > 0 {
			out[id] = l
		}
	}
	return out, rows.Err()
}

func (s *Store) ItemRefs() (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT i.id, COALESCE(j.nr, p.nr, 0) FROM list_items i
		LEFT JOIN jobs j ON i.job_id != '' AND j.id = i.job_id
		LEFT JOIN plannings p ON i.planning_id != '' AND p.id = i.planning_id
		WHERE i.deleted_at = 0 ORDER BY 2, i.created_at, i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ref struct {
		id string
		nr int64
	}
	var all []ref
	count := map[int64]int{}
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.nr); err != nil {
			return nil, err
		}
		all = append(all, r)
		count[r.nr]++
	}
	out := map[string]string{}
	pos := map[int64]int{}
	for _, r := range all {
		if r.nr == 0 {
			continue
		}
		pos[r.nr]++
		if count[r.nr] == 1 {
			out[r.id] = fmt.Sprint(r.nr)
		} else {
			out[r.id] = fmt.Sprintf("%d.%d", r.nr, pos[r.nr])
		}
	}
	return out, rows.Err()
}

// UnreadRefs liefert die Ids noch nicht aufgerufener Notizen und Planungen (mit ungelesener Runde).
func (s *Store) UnreadRefs() (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT CASE WHEN planning_id != '' THEN planning_id ELSE id END FROM jobs WHERE deleted_at = 0 AND read_at = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// HistoryOpenCount zählt die offenen Einträge eines Filters (Zähler der Reiter und im Menü).
func (s *Store) HistoryOpenCount(kind string, tags []string, since int64) (n int, err error) {
	q, args := historyQuery(kind, tags, since, "offen", 0, 0)
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM (`+q+`)`, args...).Scan(&n)
	return
}

// MarkJobRead merkt einen Auftrag als gelesen; changed=false heißt: er war es schon.
func (s *Store) MarkJobRead(id string) (changed bool, err error) {
	return s.markRead(`id = ?`, id)
}

// MarkPlanningRead merkt alle Runden einer Planung als gelesen.
func (s *Store) MarkPlanningRead(planningID string) (changed bool, err error) {
	return s.markRead(`planning_id = ?`, planningID)
}

func (s *Store) markRead(where, id string) (bool, error) {
	res, err := s.DB.Exec(`UPDATE jobs SET read_at = ? WHERE `+where+` AND read_at = 0`, Now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// NextDue liefert den ältesten Auftrag, der jetzt bearbeitet werden kann.
func (s *Store) NextDue(now int64) (*Job, error) {
	j, err := scanJob(s.DB.QueryRow(`SELECT `+jobCols+` FROM jobs
		WHERE deleted_at = 0 AND state != ? AND failed = 0 AND next_try_at <= ? AND `+importHold+` ORDER BY received_at LIMIT 1`, StateDone, now, StateDone))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return j, err
}

// importHold: importierte Planungsrunden warten, bis alle importierten Notizen durch sind (fertig oder
// endgültig fehlgeschlagen) – dann kennt die Zuordnung deren Ergebnisse. Ein Platzhalter (?) = StateDone.
const importHold = `NOT (source = 'import' AND mode = 'planung' AND EXISTS (SELECT 1 FROM jobs d WHERE d.deleted_at = 0
	AND d.source = 'import' AND d.mode = 'diktat' AND d.state != ? AND d.failed = 0))`

// NextWake liefert den frühesten Wiederholungszeitpunkt offener Aufträge oder die nächste fällige
// Termin-Erinnerung (0 = keiner).
func (s *Store) NextWake() (int64, error) {
	var t sql.NullInt64
	err := s.DB.QueryRow(`SELECT MIN(t) FROM (
		SELECT MIN(next_try_at) AS t FROM jobs WHERE deleted_at = 0 AND state != ? AND failed = 0 AND `+importHold+`
		UNION ALL SELECT MIN(erinnerung_at) FROM jobs WHERE deleted_at = 0 AND erinnerung_at > 0 AND erinnert_at = 0 AND erledigt_at = 0)`, StateDone, StateDone).Scan(&t)
	return t.Int64, err
}

// terminWhere: Notizen mit Termin.
// Unabhängig vom Verarbeitungsstand: eine Neuauswertung lässt den Termin bis zu ihrem Ergebnis stehen.
const terminWhere = `deleted_at = 0 AND mode = 'diktat' AND planning_id = '' AND termin_at > 0`

// Appointments liefert die Termine, der früheste zuerst. Ohne withDone nur die noch nicht bestätigten
// (kommende und verpasste); mit withDone auch die bestätigten.
func (s *Store) Appointments(withDone bool) ([]*Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs WHERE ` + terminWhere
	if !withDone {
		q += ` AND erledigt_at = 0`
	}
	return s.queryJobs(q + ` ORDER BY termin_at, received_at`)
}

// SearchableJobs liefert alle eigenständigen Notizen für die Suche (keine Planungsrunden, keine leeren Aufnahmen).
func (s *Store) SearchableJobs() ([]*Job, error) {
	return s.queryJobs(`SELECT ` + jobCols + ` FROM jobs WHERE deleted_at = 0 AND mode = 'diktat' AND planning_id = '' AND empty = 0 ORDER BY received_at DESC`)
}

// AllPlannings liefert alle Planungen (für die Suche).
func (s *Store) AllPlannings() ([]*Planning, error) {
	return s.queryPlannings(`SELECT ` + planCols + ` FROM plannings WHERE deleted_at = 0`)
}

// DueReminders liefert Termine, deren Erinnerung fällig und noch nicht gesendet ist.
func (s *Store) DueReminders(now int64) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE `+terminWhere+`
		AND erinnerung_at > 0 AND erinnerung_at <= ? AND erinnert_at = 0 AND erledigt_at = 0 ORDER BY erinnerung_at`, now)
}

// MarkReminded merkt die gesendete Erinnerung.
func (s *Store) MarkReminded(id string) error {
	_, err := s.DB.Exec(`UPDATE jobs SET erinnert_at = ?, updated_at = ? WHERE id = ?`, Now(), Now(), id)
	return err
}

// SetJobDone setzt den Erledigt-Stand einer Notiz (0 = offen). Der einzige Weg, erledigt_at zu schreiben.
func (s *Store) SetJobDone(id string, at int64) error {
	_, err := s.DB.Exec(`UPDATE jobs SET erledigt_at = ?, updated_at = ? WHERE id = ?`, at, Now(), id)
	return err
}

// SavePlanResult schreibt nur das Ergebnis einer Recherche (Stand, Titel) in die Planung. Status und Schließen
// bleiben unberührt: der Nutzer kann die Planung währenddessen schließen oder wieder öffnen.
func (s *Store) SavePlanResult(id, plan, title string) error {
	_, err := s.DB.Exec(`UPDATE plannings SET plan = ?, title = ?, joplin_dirty = 1, updated_at = ? WHERE id = ?`, plan, title, Now(), id)
	return err
}

func (s *Store) Rounds(planningID string) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND planning_id = ? ORDER BY round_no, received_at`, planningID)
}

// SaveJob schreibt alle veränderlichen Felder zurück.
func (s *Store) SaveJob(j *Job) error {
	j.UpdatedAt = Now()
	_, err := s.DB.Exec(`UPDATE jobs SET mode=?, audio_path=?, audio_deleted_at=?, transcript=?, transcript_source=?, title=?,
		state=?, failed=?, attempts=?, next_try_at=?, error=?, planning_id=?, round_no=?, assign_how=?, force_planning=?,
		result=?, cost_usd=?, limit_hit=?, joplin_id=?, joplin_pushed_at=?, updated_at=?, cost_transcribe_usd=?, cost_aux_usd=?, empty=?, failed_at=?,
		termin_at=?, termin_ganztag=?, erinnerung_at=?,
		erinnert_at = CASE WHEN ? THEN ? ELSE erinnert_at END WHERE id=?`,
		j.Mode, j.AudioPath, j.AudioDeletedAt, j.Transcript, j.TranscriptSource, j.Title, j.State, j.Failed, j.Attempts,
		j.NextTryAt, j.Error, j.PlanningID, j.RoundNo, j.AssignHow, j.ForcePlanning, j.Result, j.CostUSD, j.LimitHit,
		j.JoplinID, j.JoplinPushedAt, j.UpdatedAt, j.CostTranscribe, j.CostAux, j.Empty, j.FailedAt,
		j.TerminAt, j.TerminAllDay, j.RemindAt, j.FlagsChanged, j.RemindedAt, j.ID)
	if err == nil {
		j.FlagsChanged = false
	}
	return err
}

// Since liefert Diktate und Planungen ab einem Zeitpunkt (für die Kostenübersicht).
// Latest liefert die jüngsten n Aufträge (Notizen und Planungsrunden), älteste zuerst – wie Since, aber ohne den
// ganzen Bestand zu lesen.
func (s *Store) Latest(n int) ([]*Job, error) {
	return s.queryJobs(`SELECT * FROM (SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND mode IN ('diktat','planung')
		ORDER BY received_at DESC LIMIT ?) ORDER BY received_at`, n)
}

func (s *Store) Since(from int64) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND received_at >= ? AND mode IN ('diktat','planung') ORDER BY received_at`, from)
}

// JobsAsking liefert fertige Notizen, an denen noch Rückfragen der KI offen sind.
func (s *Store) JobsAsking() ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND mode = 'diktat' AND planning_id = '' AND state = ? AND failed = 0
		AND erledigt_at = 0 AND result LIKE '%"rueckfragen":[{%' ORDER BY received_at DESC`, StateDone)
}

// FailedJobs liefert alle dauerhaft fehlgeschlagenen Aufträge (auch leere Aufnahmen).
func (s *Store) FailedJobs() ([]*Job, error) {
	return s.queryJobs(`SELECT ` + jobCols + ` FROM jobs WHERE deleted_at = 0 AND failed = 1 ORDER BY received_at`)
}

// DictationsBefore: Notizen (ohne Planungsrunden, Tests, Leere und Gelöschte), aufgenommen in [since, before),
// außer except; jüngste zuerst, höchstens limit.
func (s *Store) DictationsBefore(since, before int64, except string, limit int) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND mode = 'diktat' AND planning_id = '' AND is_test = 0 AND empty = 0
		AND id != ? AND recorded_at >= ? AND recorded_at < ? ORDER BY recorded_at DESC LIMIT ?`, except, since, before, limit)
}

// JobByNr: Notiz mit der Nummer #nr (nicht im Papierkorb).
func (s *Store) JobByNr(nr int64) (*Job, error) {
	jobs, err := s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE nr = ? AND deleted_at = 0 LIMIT 1`, nr)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, ErrNotFound
	}
	return jobs[0], nil
}

// RingJobs: alle Eingänge vom Ring (Webhook und importierte Aufnahmen) samt Papierkorb, ohne Test-Events.
func (s *Store) RingJobs() ([]*Job, error) {
	return s.queryJobs(`SELECT ` + jobCols + ` FROM jobs WHERE source IN ('pebble', 'import') AND is_test = 0 ORDER BY received_at`)
}

func (s *Store) DeleteJob(id string) error {
	_, err := s.DB.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

type Counts struct {
	Total, Open, Failed  int
	Audio, Web, TextOnly int // mit Aufnahme; Text-Antworten aus der Web-App; nur Text der Pebble-App
	Notes, Rounds        int // Notizen; Planungsrunden
	Appointments         int // Notizen mit Termin (in Notes enthalten)
	Plannings            int // Planungen (je mit einer oder mehreren Runden)
}

func (s *Store) Counts() (c Counts, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(state != ? AND failed = 0),0), COALESCE(SUM(failed),0),
		COALESCE(SUM(audio_path != ''),0), COALESCE(SUM(audio_path = '' AND source = 'web'),0), COALESCE(SUM(audio_path = '' AND source != 'web' AND is_test = 0),0),
		COALESCE(SUM(mode = 'diktat'),0), COALESCE(SUM(mode = 'planung'),0), COALESCE(SUM(mode = 'diktat' AND termin_at > 0),0),
		(SELECT COUNT(*) FROM plannings WHERE deleted_at = 0)
		FROM jobs WHERE deleted_at = 0`, StateDone).
		Scan(&c.Total, &c.Open, &c.Failed, &c.Audio, &c.Web, &c.TextOnly, &c.Notes, &c.Rounds, &c.Appointments, &c.Plannings)
	return
}

// TranscriptWords: Wortzahl je Ring-Aufnahme mit Transkript ab since (ohne Web-App-Aufnahmen, Fehlaufnahmen, Tests und Gelöschte).
func (s *Store) TranscriptWords(since int64) ([]int, error) {
	rows, err := s.DB.Query(`SELECT transcript FROM jobs WHERE deleted_at = 0 AND audio_path != '' AND source != 'web' AND is_test = 0 AND empty = 0 AND transcript != '' AND received_at >= ?`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if n := len(strings.Fields(t)); n > 0 {
			out = append(out, n)
		}
	}
	return out, rows.Err()
}

// --- Planungen ---

const planCols = `id, title, status, plan, joplin_id, joplin_dirty, joplin_pushed_at, joplin_error, created_at, updated_at, closed_at, closed_by, nr, deleted_at`

func scanPlanning(r scanner) (*Planning, error) {
	var p Planning
	err := r.Scan(&p.ID, &p.Title, &p.Status, &p.Plan, &p.JoplinID, &p.JoplinDirty, &p.JoplinPushedAt, &p.JoplinError,
		&p.CreatedAt, &p.UpdatedAt, &p.ClosedAt, &p.ClosedBy, &p.Nr, &p.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) CreatePlanning() (*Planning, error) {
	p := &Planning{ID: NewID(), Status: "offen", CreatedAt: Now(), UpdatedAt: Now()}
	p.JoplinID = DerivedID("planung:" + p.ID)
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if p.Nr, err = takeNr(tx); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO plannings (id, status, joplin_id, created_at, updated_at, nr) VALUES (?,?,?,?,?,?)`,
		p.ID, p.Status, p.JoplinID, p.CreatedAt, p.UpdatedAt, p.Nr); err != nil {
		return nil, err
	}
	return p, tx.Commit()
}

// Planning liefert eine sichtbare Planung; eine im Papierkorb gilt als nicht vorhanden (dafür PlanningAny).
func (s *Store) Planning(id string) (*Planning, error) {
	return scanPlanning(s.DB.QueryRow(`SELECT `+planCols+` FROM plannings WHERE id = ? AND deleted_at = 0`, id))
}

// PlanningAny liefert eine Planung auch aus dem Papierkorb.
func (s *Store) PlanningAny(id string) (*Planning, error) {
	return scanPlanning(s.DB.QueryRow(`SELECT `+planCols+` FROM plannings WHERE id = ?`, id))
}

func (s *Store) queryPlannings(q string, args ...any) ([]*Planning, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Planning
	for rows.Next() {
		p, err := scanPlanning(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OpenPlannings liefert offene Planungen, die schon ein Ergebnis haben, neueste zuerst.
func (s *Store) OpenPlannings() ([]*Planning, error) {
	return s.queryPlannings(`SELECT ` + planCols + ` FROM plannings WHERE deleted_at = 0 AND status = 'offen' AND plan != '' ORDER BY updated_at DESC`)
}

// StalePlannings liefert offene Planungen, deren jüngste Runde vor dem Stichtag einging
// und bei denen keine Runde mehr in Arbeit ist.
func (s *Store) StalePlannings(cutoff int64) ([]*Planning, error) {
	// Eine von Hand wieder geöffnete Planung (closed_by 'wiedergeoeffnet', closed_at = Zeitpunkt des Öffnens)
	// bleibt ab dann wieder die volle Frist offen.
	return s.queryPlannings(`SELECT `+planCols+` FROM plannings p WHERE deleted_at = 0 AND status = 'offen' AND plan != ''
		AND (SELECT MAX(received_at) FROM jobs WHERE planning_id = p.id AND deleted_at = 0) < ?
		AND NOT (closed_by = 'wiedergeoeffnet' AND closed_at >= ?)
		AND NOT EXISTS (SELECT 1 FROM jobs WHERE planning_id = p.id AND deleted_at = 0 AND state != ? AND failed = 0)`, cutoff, cutoff, StateDone)
}

func (s *Store) DirtyPlannings() ([]*Planning, error) {
	return s.queryPlannings(`SELECT ` + planCols + ` FROM plannings WHERE deleted_at = 0 AND joplin_dirty = 1 AND plan != ''`)
}

func (s *Store) SavePlanning(p *Planning) error {
	p.UpdatedAt = Now()
	_, err := s.DB.Exec(`UPDATE plannings SET title=?, status=?, plan=?, joplin_dirty=?, joplin_pushed_at=?, joplin_error=?,
		updated_at=?, closed_at=?, closed_by=? WHERE id=?`, p.Title, p.Status, p.Plan, p.JoplinDirty, p.JoplinPushedAt, p.JoplinError,
		p.UpdatedAt, p.ClosedAt, p.ClosedBy, p.ID)
	return err
}

// PendingAnswers zählt Antworten auf eine Planung, die noch nicht als Runde an ihr hängen.
func (s *Store) PendingAnswers(planningID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM jobs WHERE deleted_at = 0 AND force_planning = ? AND planning_id = '' AND failed = 0 AND state != ?`,
		planningID, StateDone).Scan(&n)
	return n, err
}

// PendingAnswerJobs liefert die Antworten auf eine Planung, die noch nicht als Runde an ihr hängen.
func (s *Store) PendingAnswerJobs(planningID string) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND force_planning = ? AND planning_id = ''`, planningID)
}

func (s *Store) DeletePlanning(id string) error {
	_, err := s.DB.Exec(`DELETE FROM plannings WHERE id = ?`, id)
	return err
}

// --- Papierkorb ---

// execAll führt die Anweisungen in einer Transaktion aus.
func (s *Store) execAll(stmts ...[]any) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, st := range stmts {
		if _, err := tx.Exec(st[0].(string), st[1:]...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TrashJob legt einen Auftrag samt seinen Listenpunkten mit dem Stempel at in den Papierkorb.
func (s *Store) TrashJob(id string, at int64) error {
	return s.execAll(
		[]any{`UPDATE jobs SET deleted_at = ? WHERE id = ? AND deleted_at = 0`, at, id},
		[]any{`UPDATE list_items SET deleted_at = ? WHERE job_id = ? AND deleted_at = 0`, at, id},
	)
}

// TrashPlanning legt eine Planung samt ihrem Listenpunkt mit dem Stempel at in den Papierkorb (die Runden einzeln über TrashJob).
func (s *Store) TrashPlanning(id string, at int64) error {
	return s.execAll(
		[]any{`UPDATE plannings SET deleted_at = ? WHERE id = ? AND deleted_at = 0`, at, id},
		[]any{`UPDATE list_items SET deleted_at = ? WHERE planning_id = ? AND deleted_at = 0`, at, id},
	)
}

// RestoreJob holt einen Auftrag und seine mit ihm gelöschten Listenpunkte zurück.
func (s *Store) RestoreJob(id string) error {
	return s.execAll(
		[]any{`UPDATE list_items SET deleted_at = 0 WHERE job_id = ? AND deleted_at = (SELECT deleted_at FROM jobs WHERE id = ?)`, id, id},
		[]any{`UPDATE jobs SET deleted_at = 0 WHERE id = ?`, id},
	)
}

// RestorePlanning holt eine Planung zurück: mit ihr gelöschte Runden, offene Antworten und Listenpunkte.
// Vorher einzeln Gelöschtes (anderer Stempel) bleibt im Papierkorb.
func (s *Store) RestorePlanning(id string) error {
	const at = `(SELECT deleted_at FROM plannings WHERE id = ?)`
	return s.execAll(
		[]any{`UPDATE list_items SET deleted_at = 0 WHERE deleted_at = ` + at + ` AND (planning_id = ?
			OR job_id IN (SELECT id FROM jobs WHERE planning_id = ? OR (force_planning = ? AND planning_id = '')))`, id, id, id, id},
		[]any{`UPDATE jobs SET deleted_at = 0 WHERE deleted_at = ` + at + ` AND (planning_id = ? OR (force_planning = ? AND planning_id = ''))`, id, id, id},
		[]any{`UPDATE plannings SET deleted_at = 0 WHERE id = ?`, id},
	)
}

// TrashedJobs liefert Aufträge im Papierkorb, die einzeln dort liegen (keine Runden einer gelöschten Planung),
// gelöscht vor before (0 = alle), neueste zuerst.
func (s *Store) TrashedJobs(before int64) ([]*Job, error) {
	if before <= 0 {
		before = 1 << 62
	}
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs j WHERE deleted_at > 0 AND deleted_at < ?
		AND NOT EXISTS (SELECT 1 FROM plannings p WHERE p.id = j.planning_id AND p.deleted_at > 0)
		AND NOT EXISTS (SELECT 1 FROM plannings p WHERE j.planning_id = '' AND p.id = j.force_planning AND p.deleted_at = j.deleted_at)
		ORDER BY deleted_at DESC`, before)
}

// TrashedPlannings liefert Planungen im Papierkorb, gelöscht vor before (0 = alle), neueste zuerst.
func (s *Store) TrashedPlannings(before int64) ([]*Planning, error) {
	if before <= 0 {
		before = 1 << 62
	}
	return s.queryPlannings(`SELECT `+planCols+` FROM plannings WHERE deleted_at > 0 AND deleted_at < ? ORDER BY deleted_at DESC`, before)
}

// AllRounds liefert alle Runden einer Planung, auch die im Papierkorb.
func (s *Store) AllRounds(planningID string) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE planning_id = ? ORDER BY round_no, received_at`, planningID)
}

// TrashedAnswerJobs liefert die mit einer Planung gelöschten Antworten, die noch nicht als Runde an ihr hingen.
func (s *Store) TrashedAnswerJobs(planningID string) ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at > 0 AND force_planning = ? AND planning_id = ''`, planningID)
}

// DeleteItemsOf entfernt alle Listenpunkte eines Auftrags bzw. einer Planung endgültig, auch die im Papierkorb.
func (s *Store) DeleteItemsOf(jobID, planningID string) error {
	_, err := s.DB.Exec(`DELETE FROM list_items WHERE (? != '' AND job_id = ?) OR (? != '' AND planning_id = ?)`, jobID, jobID, planningID, planningID)
	return err
}

// MarkJobDirty lässt die Joplin-Notiz eines Diktats neu schreiben.
func (s *Store) MarkJobDirty(id string) error {
	_, err := s.DB.Exec(`UPDATE jobs SET joplin_dirty = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) NextRoundNo(planningID string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(round_no),0)+1 FROM jobs WHERE planning_id = ?`, planningID).Scan(&n)
	return n, err
}

func (s *Store) AddJoplinDelete(id string) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO joplin_deletes (joplin_id) VALUES (?)`, id)
	return err
}

func (s *Store) JoplinDeletes() ([]string, error) {
	rows, err := s.DB.Query(`SELECT joplin_id FROM joplin_deletes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) DoneJoplinDelete(id string) error {
	_, err := s.DB.Exec(`DELETE FROM joplin_deletes WHERE joplin_id = ?`, id)
	return err
}

// --- Gedächtnis ---

// Zustände eines Gedächtnis-Eintrags: neu vorgeschlagen (wirkt noch nicht), bestätigt, vom Nutzer gelöscht.
const (
	MemoryProposed = "vorschlag"
	MemoryActive   = "aktiv"
	MemoryDeleted  = "geloescht"
	// MemoryRaw: Beobachtung (Korrektur, Antwort auf eine Rückfrage), noch kein Vorschlag. Nur der Lernlauf liest sie
	// und leitet daraus allgemeine Vorschläge ab; danach werden sie entfernt. In der Web-App unsichtbar.
	MemoryRaw = "roh"
)

type MemoryEntry struct {
	ID        string
	Text      string
	Status    string
	Source    string // ki | lernlauf | korrektur | antwort | nutzer
	CreatedAt int64
	UpdatedAt int64
}

// MemoryEntries liefert die Einträge eines Zustands, die zuletzt geänderten zuerst; limit 0 = alle.
func (s *Store) MemoryEntries(status string, limit int) ([]*MemoryEntry, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.DB.Query(`SELECT id, text, status, source, created_at, updated_at FROM memory WHERE status = ?
		ORDER BY updated_at DESC, rowid DESC LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		if err := rows.Scan(&e.ID, &e.Text, &e.Status, &e.Source, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CountMemory zählt die Gedächtnis-Einträge eines Zustands.
func (s *Store) CountMemory(status string) (n int, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*) FROM memory WHERE status = ?`, status).Scan(&n)
	return
}

func (s *Store) MemoryEntry(id string) (*MemoryEntry, error) {
	var e MemoryEntry
	err := s.DB.QueryRow(`SELECT id, text, status, source, created_at, updated_at FROM memory WHERE id = ?`, id).
		Scan(&e.ID, &e.Text, &e.Status, &e.Source, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// AddMemory legt einen Eintrag an. added=false heißt: derselbe Text steht schon da – in welchem Zustand auch immer,
// ein gelöschter kommt also nicht wieder.
func (s *Store) AddMemory(text, status, source string) (added bool, err error) {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memory WHERE text = ? COLLATE NOCASE`, text).Scan(&n); err != nil || n > 0 {
		return false, err
	}
	now := Now()
	_, err = s.DB.Exec(`INSERT INTO memory (id, text, status, source, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		NewID(), text, status, source, now, now)
	return err == nil, err
}

// ResetMemory entfernt alle Gedächtnis-Einträge (aktiv, Vorschläge, gelöscht/„nicht erwünscht“); Beobachtungen bleiben
// als Lernstoff stehen.
func (s *Store) ResetMemory() (int64, error) {
	res, err := s.DB.Exec(`DELETE FROM memory WHERE status != ?`, MemoryRaw)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteMemoryIDs entfernt Einträge endgültig (verarbeitete Beobachtungen).
func (s *Store) DeleteMemoryIDs(ids []string) error {
	for _, id := range ids {
		if _, err := s.DB.Exec(`DELETE FROM memory WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveMemory(e *MemoryEntry) error {
	e.UpdatedAt = Now()
	_, err := s.DB.Exec(`UPDATE memory SET text = ?, status = ?, updated_at = ? WHERE id = ?`, e.Text, e.Status, e.UpdatedAt, e.ID)
	return err
}

// --- Schlüssel/Wert (eigene Werte des Nutzers; Systemwerte in system.db) ---

func (s *Store) Get(k string) (string, error) { return kvGet(s.DB, k) }
func (s *Store) Set(k, v string) error        { return kvSet(s.DB, k, v) }

// Settings: persönliche Einstellungen des Nutzers (Schlüssel wie in config.Fields).
func (s *Store) Settings() (map[string]string, error) { return kvSettings(s.DB) }
func (s *Store) SaveSettings(values map[string]string) error {
	return kvSaveSettings(s.DB, values)
}

// --- Push ---

type PushSub struct{ Endpoint, P256dh, Auth string }

func (s *Store) AddPushSub(p PushSub) error {
	_, err := s.DB.Exec(`INSERT INTO push_subs (endpoint, p256dh, auth, created_at) VALUES (?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth`, p.Endpoint, p.P256dh, p.Auth, Now())
	return err
}

func (s *Store) DeletePushSub(endpoint string) error {
	_, err := s.DB.Exec(`DELETE FROM push_subs WHERE endpoint = ?`, endpoint)
	return err
}

func (s *Store) PushSubs() ([]PushSub, error) {
	rows, err := s.DB.Query(`SELECT endpoint, p256dh, auth FROM push_subs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSub
	for rows.Next() {
		var p PushSub
		if err := rows.Scan(&p.Endpoint, &p.P256dh, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Preview kürzt einen Text auf eine Zeile für Listen und Titel.
func Preview(text string, max int) string {
	t := strings.Join(strings.Fields(text), " ")
	r := []rune(t)
	if len(r) <= max {
		return t
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}

// --- Tags ---
// Tags hängen an einem Diktat (Auftrag) oder an einer Planung; der Vorrat ist für beide derselbe.
// kind: "job" oder "planung".

func tagTable(kind string) string {
	if kind == "planung" {
		return "plannings"
	}
	return "jobs"
}

func decodeList(s string) []string {
	var out []string
	json.Unmarshal([]byte(s), &out)
	return out
}

func encodeList(l []string) string {
	if l == nil {
		l = []string{}
	}
	data, _ := json.Marshal(l)
	return string(data)
}

// TagSet ist der Tag-Stand eines Eintrags: vergeben, von der KI vorgeschlagen, nach Joplin übertragen.
type TagSet struct{ Tags, Suggestions, Pushed []string }

func (s *Store) Tags(kind, id string) (TagSet, error) {
	var t, sg, p string
	err := s.DB.QueryRow(`SELECT tags, tag_suggestions, joplin_tags FROM `+tagTable(kind)+` WHERE id = ?`, id).Scan(&t, &sg, &p)
	if errors.Is(err, sql.ErrNoRows) {
		return TagSet{}, ErrNotFound
	}
	return TagSet{decodeList(t), decodeList(sg), decodeList(p)}, err
}

func (s *Store) SetTags(kind, id string, tags, suggestions []string) error {
	_, err := s.DB.Exec(`UPDATE `+tagTable(kind)+` SET tags = ?, tag_suggestions = ?, joplin_dirty = 1, updated_at = ? WHERE id = ?`,
		encodeList(tags), encodeList(suggestions), Now(), id)
	return err
}

func (s *Store) SetPushedTags(kind, id string, pushed []string) error {
	_, err := s.DB.Exec(`UPDATE `+tagTable(kind)+` SET joplin_tags = ? WHERE id = ?`, encodeList(pushed), id)
	return err
}

// TaggedIDs liefert die Ids aller Einträge mit Tags oder Vorschlägen.
func (s *Store) TaggedIDs(kind string) ([]string, error) {
	rows, err := s.DB.Query(`SELECT id FROM ` + tagTable(kind) + ` WHERE deleted_at = 0 AND (tags != '[]' OR tag_suggestions != '[]')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AllTags liefert alle vergebenen Tags mit der Zahl ihrer Einträge.
func (s *Store) AllTags() (map[string]int, error) {
	out := map[string]int{}
	for _, table := range []string{"jobs", "plannings"} {
		rows, err := s.DB.Query(`SELECT tags FROM ` + table + ` WHERE deleted_at = 0 AND tags != '[]'`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				rows.Close()
				return nil, err
			}
			for _, name := range decodeList(t) {
				out[name]++
			}
		}
		rows.Close()
	}
	return out, nil
}

// TagStat ist ein Tag mit der Zahl seiner Einträge und dem Zeitpunkt der jüngsten Verwendung.
type TagStat struct {
	Name  string
	Count int
	Last  int64
}

// TagStats liefert alle vergebenen Tags; „zuletzt“ ist der Eingang des jüngsten Diktats bzw. der jüngsten Runde mit dem Tag.
func (s *Store) TagStats() ([]TagStat, error) {
	stats := map[string]*TagStat{}
	for _, q := range []string{
		`SELECT tags, received_at FROM jobs WHERE deleted_at = 0 AND tags != '[]'`,
		`SELECT p.tags, COALESCE((SELECT MAX(received_at) FROM jobs WHERE planning_id = p.id AND deleted_at = 0), p.updated_at) FROM plannings p WHERE p.deleted_at = 0 AND p.tags != '[]'`,
	} {
		rows, err := s.DB.Query(q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t string
			var at int64
			if err := rows.Scan(&t, &at); err != nil {
				rows.Close()
				return nil, err
			}
			for _, name := range decodeList(t) {
				st := stats[name]
				if st == nil {
					st = &TagStat{Name: name}
					stats[name] = st
				}
				st.Count++
				st.Last = max(st.Last, at)
			}
		}
		rows.Close()
	}
	out := make([]TagStat, 0, len(stats))
	for _, st := range stats {
		out = append(out, *st)
	}
	return out, nil
}

// RemoveTag nimmt einen Tag aus allen Notizen und Planungen (auch aus KI-Vorschlägen und dem Papierkorb);
// geänderte Einträge werden für Joplin neu geschrieben. Liefert die Zahl geänderter Einträge.
func (s *Store) RemoveTag(name string) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	without := func(l []string) ([]string, bool) {
		out := make([]string, 0, len(l))
		for _, t := range l {
			if !strings.EqualFold(t, name) {
				out = append(out, t)
			}
		}
		return out, len(out) != len(l)
	}
	changed := 0
	for _, table := range []string{"jobs", "plannings"} {
		type row struct{ id, tags, sugg string }
		rows, err := tx.Query(`SELECT id, tags, tag_suggestions FROM ` + table + ` WHERE tags != '[]' OR tag_suggestions != '[]'`)
		if err != nil {
			return 0, err
		}
		var all []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.tags, &r.sugg); err != nil {
				rows.Close()
				return 0, err
			}
			all = append(all, r)
		}
		rows.Close()
		for _, r := range all {
			tags, a := without(decodeList(r.tags))
			sugg, b := without(decodeList(r.sugg))
			if !a && !b {
				continue
			}
			if _, err := tx.Exec(`UPDATE `+table+` SET tags = ?, tag_suggestions = ?, joplin_dirty = 1, updated_at = ? WHERE id = ?`,
				encodeList(tags), encodeList(sugg), Now(), r.id); err != nil {
				return 0, err
			}
			changed++
		}
	}
	return changed, tx.Commit()
}

// TagSets liefert die Tag-Liste jedes sichtbaren Eintrags mit Tags (Notizen und Planungen), für den Tag-Filter:
// welche Tags zusammen vorkommen.
// TagCombo: eine Tag-Zusammenstellung (sortiert) und an wie vielen Einträgen sie vorkommt.
type TagCombo struct {
	Tags  []string `json:"tags"`
	Count int      `json:"anzahl"`
}

// TagSets fasst die Tags aller Einträge zu Zusammenstellungen mit ihrer Zahl zusammen (statt einer Zeile je Eintrag).
func (s *Store) TagSets() ([]TagCombo, error) {
	counts := map[string]*TagCombo{}
	var order []string
	for _, q := range []string{
		`SELECT tags, COUNT(*) FROM jobs WHERE deleted_at = 0 AND planning_id = '' AND tags != '[]' GROUP BY tags`,
		`SELECT tags, COUNT(*) FROM plannings WHERE deleted_at = 0 AND tags != '[]' GROUP BY tags`,
	} {
		rows, err := s.DB.Query(q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t string
			var n int
			if err := rows.Scan(&t, &n); err != nil {
				rows.Close()
				return nil, err
			}
			l := decodeList(t)
			if len(l) == 0 {
				continue
			}
			slices.Sort(l)
			k := strings.Join(l, "\n")
			if c, ok := counts[k]; ok {
				c.Count += n
			} else {
				counts[k] = &TagCombo{Tags: l, Count: n}
				order = append(order, k)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]TagCombo, 0, len(order))
	for _, k := range order {
		out = append(out, *counts[k])
	}
	return out, nil
}

// DirtyDictations liefert fertige Diktate, deren Joplin-Stand nachzuziehen ist (Tags geändert).
func (s *Store) DirtyDictations() ([]*Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE deleted_at = 0 AND joplin_dirty = 1 AND mode = 'diktat' AND state = ?`, StateDone)
}

func (s *Store) ClearJobDirty(id string) error {
	_, err := s.DB.Exec(`UPDATE jobs SET joplin_dirty = 0 WHERE id = ?`, id)
	return err
}

// --- Listen ---
// Eine Liste sammelt Punkte aus Diktaten (z. B. „Filme“); jeder Punkt lässt sich abhaken.

type List struct {
	ID             string
	Name           string
	Project        string // Gruppe; leer = Liste ohne Gruppe
	Description    string // von der KI gepflegt
	Nr             int64  // feste Nummer der Liste (L12), auch zum Ansprechen im Diktat
	DescribedAt    int64
	JoplinID       string
	JoplinDirty    bool
	JoplinPushedAt int64
	CreatedAt      int64
	UpdatedAt      int64
	Open, Total    int
	DoneAt         int64 // letztes Abhaken eines Punkts, 0 = nie
	NewestAt       int64 // Eingang des jüngsten Punkts, 0 = leer
}

type ListItem struct {
	ID         string
	ListID     string
	JobID      string // Notiz, aus der der Punkt stammt
	PlanningID string // oder Planung, die als Punkt einsortiert wurde
	Text       string
	Facts      string // JSON-Liste aus {name, wert}
	DoneAt     int64  // 0 = offen
	CreatedAt  int64
	Nr         int64 // Nummer der Notiz bzw. Planung, zu der der Punkt gehört (nur gelesen)
}

const listCols = `l.id, l.name, l.project, l.description, l.described_at, l.nr, l.joplin_id, l.joplin_dirty, l.joplin_pushed_at, l.created_at, l.updated_at,
 (SELECT COUNT(*) FROM list_items WHERE list_id = l.id AND deleted_at = 0 AND done_at = 0), (SELECT COUNT(*) FROM list_items WHERE list_id = l.id AND deleted_at = 0),
 (SELECT COALESCE(MAX(done_at), 0) FROM list_items WHERE list_id = l.id AND deleted_at = 0),
 (SELECT COALESCE(MAX(created_at), 0) FROM list_items WHERE list_id = l.id AND deleted_at = 0)`

func (s *Store) queryLists(where string, args ...any) ([]*List, error) {
	rows, err := s.DB.Query(`SELECT `+listCols+` FROM lists l `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.Name, &l.Project, &l.Description, &l.DescribedAt, &l.Nr, &l.JoplinID, &l.JoplinDirty, &l.JoplinPushedAt, &l.CreatedAt, &l.UpdatedAt, &l.Open, &l.Total, &l.DoneAt, &l.NewestAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// Lists liefert alle Listen: erst die gewöhnlichen, dann die Themen je Projekt.
func (s *Store) Lists() ([]*List, error) {
	return s.queryLists(`ORDER BY l.project COLLATE NOCASE, l.name COLLATE NOCASE`)
}

func (s *Store) DirtyLists() ([]*List, error) { return s.queryLists(`WHERE l.joplin_dirty = 1`) }

func (s *Store) List(id string) (*List, error) {
	ls, err := s.queryLists(`WHERE l.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, ErrNotFound
	}
	return ls[0], nil
}

func (s *Store) CreateList(name, project string) (*List, error) {
	if project != "" {
		if err := s.EnsureGroup(project); err != nil {
			return nil, err
		}
	}
	l := &List{ID: NewID(), Name: name, Project: project, CreatedAt: Now(), UpdatedAt: Now()}
	l.JoplinID = DerivedID("liste:" + l.ID)
	err := s.DB.QueryRow(`INSERT INTO lists (id, name, project, joplin_id, created_at, updated_at, nr)
		VALUES (?,?,?,?,?,?, (SELECT COALESCE(MAX(nr), 0) + 1 FROM lists)) RETURNING nr`,
		l.ID, l.Name, l.Project, l.JoplinID, l.CreatedAt, l.UpdatedAt).Scan(&l.Nr)
	return l, err
}

// TouchList merkt eine Änderung an der Liste vor: Joplin-Stand nachziehen.
func (s *Store) TouchList(id string) error {
	_, err := s.DB.Exec(`UPDATE lists SET joplin_dirty = 1, updated_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *Store) ListPushed(id string) error {
	_, err := s.DB.Exec(`UPDATE lists SET joplin_dirty = 0, joplin_pushed_at = ? WHERE id = ?`, Now(), id)
	return err
}

// DeleteList entfernt eine Liste samt ihren Punkten.
func (s *Store) DeleteList(id string) error {
	if _, err := s.DB.Exec(`DELETE FROM list_items WHERE list_id = ?`, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM lists WHERE id = ?`, id)
	return err
}

const itemCols = `id, list_id, job_id, text, facts, done_at, created_at, planning_id`

func (s *Store) queryItems(where string, args ...any) ([]*ListItem, error) {
	// Die Nummer kommt von der Notiz bzw. Planung des Punkts; where spricht die Punkte als i an.
	// Punkte im Papierkorb bleiben außen vor.
	where = "WHERE i.deleted_at = 0 AND " + strings.TrimPrefix(where, "WHERE ")
	rows, err := s.DB.Query(`SELECT i.id, i.list_id, i.job_id, i.text, i.facts, i.done_at, i.created_at, i.planning_id,
		COALESCE(j.nr, p.nr, 0) FROM list_items i
		LEFT JOIN jobs j ON i.job_id != '' AND j.id = i.job_id
		LEFT JOIN plannings p ON i.planning_id != '' AND p.id = i.planning_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ListItem
	for rows.Next() {
		var it ListItem
		if err := rows.Scan(&it.ID, &it.ListID, &it.JobID, &it.Text, &it.Facts, &it.DoneAt, &it.CreatedAt, &it.PlanningID, &it.Nr); err != nil {
			return nil, err
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

// ListItems liefert die Punkte einer Liste: offene zuerst, dann erledigte, je nach Eingang (neueste unten).
func (s *Store) ListItems(listID string) ([]*ListItem, error) {
	return s.queryItems(`WHERE i.list_id = ? ORDER BY i.done_at != 0, i.created_at`, listID)
}

// ItemsActive liefert die Punkte aller Listen in einem Zug, je Liste wie ListItems sortiert. doneAfter > 0: nur
// offene und die nach doneAfter erledigten (Listenübersicht); 0 = alle.
func (s *Store) ItemsActive(doneAfter int64) ([]*ListItem, error) {
	if doneAfter > 0 {
		return s.queryItems(`WHERE (i.done_at = 0 OR i.done_at > ?) ORDER BY i.list_id, i.done_at != 0, i.created_at`, doneAfter)
	}
	return s.queryItems(`WHERE 1 ORDER BY i.list_id, i.done_at != 0, i.created_at`)
}

func (s *Store) ItemsByJob(jobID string) ([]*ListItem, error) {
	return s.queryItems(`WHERE i.job_id = ? ORDER BY i.created_at`, jobID)
}

func (s *Store) ItemsByPlanning(planningID string) ([]*ListItem, error) {
	return s.queryItems(`WHERE i.planning_id = ? ORDER BY i.created_at`, planningID)
}

func (s *Store) ListItem(id string) (*ListItem, error) {
	its, err := s.queryItems(`WHERE i.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(its) == 0 {
		return nil, ErrNotFound
	}
	return its[0], nil
}

func (s *Store) AddListItem(it *ListItem) error {
	it.ID, it.CreatedAt = NewID(), Now()
	if it.Facts == "" {
		it.Facts = "[]"
	}
	_, err := s.DB.Exec(`INSERT INTO list_items (`+itemCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		it.ID, it.ListID, it.JobID, it.Text, it.Facts, it.DoneAt, it.CreatedAt, it.PlanningID)
	return err
}

func (s *Store) SaveListItem(it *ListItem) error {
	_, err := s.DB.Exec(`UPDATE list_items SET list_id = ?, done_at = ? WHERE id = ?`, it.ListID, it.DoneAt, it.ID)
	return err
}

// SetListItemText ändert den Namen eines Listenpunkts.
func (s *Store) SetListItemText(id, text string) error {
	_, err := s.DB.Exec(`UPDATE list_items SET text = ? WHERE id = ?`, text, id)
	return err
}

func (s *Store) DeleteListItem(id string) error {
	_, err := s.DB.Exec(`DELETE FROM list_items WHERE id = ?`, id)
	return err
}

// UnsortedPlannings liefert abgeschlossene Planungen mit Ergebnis, die noch nicht auf ein Projekt-Thema geprüft sind.
func (s *Store) UnsortedPlannings() ([]*Planning, error) {
	return s.queryPlannings(`SELECT ` + planCols + ` FROM plannings WHERE deleted_at = 0 AND status = 'geschlossen' AND plan != '' AND sorted_at = 0 ORDER BY nr`)
}

// PlanningSorted vermerkt, dass eine abgeschlossene Planung auf ein Projekt-Thema geprüft ist.
func (s *Store) PlanningSorted(id string) error {
	_, err := s.DB.Exec(`UPDATE plannings SET sorted_at = ? WHERE id = ?`, Now(), id)
	return err
}

// --- Gruppen ---
// Eine Gruppe (Spalte lists.project) fasst Listen zu einem Thema zusammen, z. B. „servicemanager“ mit den Listen
// „Grundrissplan“, „Inventur“ … Gruppen und Listen legt die KI beim Einsortieren selbst an.

type Group struct {
	Name        string
	Description string
	DescribedAt int64
}

// Groups liefert alle Gruppen, die Listen haben, nach Namen.
func (s *Store) Groups() ([]*Group, error) {
	rows, err := s.DB.Query(`SELECT g.name, g.description, g.described_at FROM list_groups g
		WHERE EXISTS (SELECT 1 FROM lists WHERE project = g.name COLLATE NOCASE) ORDER BY g.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Description, &g.DescribedAt); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

// EnsureGroup legt die Gruppe an, falls es sie noch nicht gibt.
func (s *Store) EnsureGroup(name string) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO list_groups (name) VALUES (?)`, name)
	return err
}

func (s *Store) SetGroupDescription(name, description string) error {
	_, err := s.DB.Exec(`UPDATE list_groups SET description = ?, described_at = ? WHERE name = ?`, description, Now(), name)
	return err
}

func (s *Store) SetListDescription(id, description string) error {
	_, err := s.DB.Exec(`UPDATE lists SET description = ?, described_at = ? WHERE id = ?`, description, Now(), id)
	return err
}

// GroupItems liefert die jüngsten Punkte aller Listen einer Gruppe, neueste zuerst.
func (s *Store) GroupItems(group string, limit int) ([]*ListItem, error) {
	return s.queryItems(`WHERE i.list_id IN (SELECT id FROM lists WHERE project = ? COLLATE NOCASE) ORDER BY i.created_at DESC LIMIT ?`, group, limit)
}

// RecentItems liefert die jüngsten Punkte einer Liste, neueste zuerst.
func (s *Store) RecentItems(listID string, limit int) ([]*ListItem, error) {
	return s.queryItems(`WHERE i.list_id = ? ORDER BY i.created_at DESC LIMIT ?`, listID, limit)
}

// NewestItem liefert den Eingang (ms) des jüngsten Punkts einer Liste bzw. einer Gruppe; 0 = keiner.
func (s *Store) NewestItem(listID, group string) (int64, error) {
	var n int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(i.created_at), 0) FROM list_items i JOIN lists l ON l.id = i.list_id
		WHERE i.deleted_at = 0 AND ((? != '' AND l.id = ?) OR (? != '' AND l.project = ? COLLATE NOCASE))`, listID, listID, group, group).Scan(&n)
	return n, err
}

// ErrConflict: der neue Name (bzw. Gruppe + Name) ist schon vergeben.
var ErrConflict = errors.New("schon vergeben")

// SaveList ändert Name und Gruppe einer Liste; Gruppe + Name müssen frei sein.
func (s *Store) SaveList(id, name, group string) error {
	if group != "" {
		if err := s.EnsureGroup(group); err != nil {
			return err
		}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM lists WHERE id != ? AND project = ? COLLATE NOCASE AND name = ? COLLATE NOCASE`, id, group, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	_, err := s.DB.Exec(`UPDATE lists SET name = ?, project = ?, joplin_dirty = 1, updated_at = ? WHERE id = ?`, name, group, Now(), id)
	return err
}

// RenameGroup gibt einer Gruppe einen neuen Namen; ihre Listen ziehen mit, die Beschreibung bleibt.
// Eine andere Gruppe dieses Namens darf es nicht geben (nur Groß/Klein ändern geht).
func (s *Store) RenameGroup(old, name string) error {
	if !strings.EqualFold(old, name) {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM lists WHERE project = ? COLLATE NOCASE`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM list_groups WHERE name = ? AND name != ? COLLATE NOCASE`, name, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE list_groups SET name = ? WHERE name = ?`, name, old); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE lists SET project = ?, joplin_dirty = 1, updated_at = ? WHERE project = ? COLLATE NOCASE`, name, Now(), old); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteGroup löst eine Gruppe auf: ihre Listen bleiben samt Punkten, nur ohne Gruppe.
// Gibt es schon eine Liste gleichen Namens ohne Gruppe, ist das ein Konflikt (Name der Liste in conflict).
func (s *Store) DeleteGroup(name string) (conflict string, err error) {
	err = s.DB.QueryRow(`SELECT COALESCE(MIN(l.name), '') FROM lists l WHERE l.project = ? COLLATE NOCASE
		AND EXISTS (SELECT 1 FROM lists o WHERE o.project = '' AND o.name = l.name COLLATE NOCASE)`, name).Scan(&conflict)
	if err != nil || conflict != "" {
		return conflict, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE lists SET project = '', joplin_dirty = 1, updated_at = ? WHERE project = ? COLLATE NOCASE`, Now(), name); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM list_groups WHERE name = ?`, name); err != nil {
		return "", err
	}
	return "", tx.Commit()
}

// Snapshot schreibt eine in sich stimmige Kopie der Datenbank nach path (VACUUM INTO; path darf nicht existieren).
func (s *Store) Snapshot(path string) error {
	_, err := s.DB.Exec(`VACUUM INTO ?`, snapshotURI(path, s.key))
	return err
}

// AudioRefs: Aufnahme-Pfade und Ids aller Aufträge (auch im Papierkorb) – für die Suche nach Audiodateien ohne Eintrag.
func (s *Store) AudioRefs() (paths, ids map[string]bool, err error) {
	rows, err := s.DB.Query(`SELECT id, audio_path FROM jobs`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	paths, ids = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, nil, err
		}
		ids[id] = true
		if p != "" {
			paths[p] = true
		}
	}
	return paths, ids, rows.Err()
}

// AudioJobs: Aufträge mit vorhandener Aufnahme (Id, Modus, Pfad) – zum Einsortieren in die Unterordner.
func (s *Store) AudioJobs() ([]*Job, error) {
	rows, err := s.DB.Query(`SELECT id, mode, audio_path FROM jobs WHERE audio_path != '' AND audio_deleted_at = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		j := &Job{}
		if err := rows.Scan(&j.ID, &j.Mode, &j.AudioPath); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// SetAudioPath ändert nur den Pfad der Aufnahme.
func (s *Store) SetAudioPath(id, path string) error {
	_, err := s.DB.Exec(`UPDATE jobs SET audio_path = ? WHERE id = ?`, path, id)
	return err
}

// SearchRow ist ein Kandidat der Volltextsuche: nur die durchsuchten Felder, ohne Zusammenfassung.
type SearchRow struct {
	Kind, ID                        string // Kind: "job" oder "planung"
	Nr, At                          int64
	Title, Transcript, Result, Tags string // Result: Auswertung bzw. Stand der Planung
}

// searchFilter: Vorfilter in SQL für die Wörter aus ASCII-Zeichen (lower() faltet nur ASCII); nr >= 0 lässt die
// Nummer immer durch. Die genaue Prüfung (Umlaute, Feld der Fundstelle) macht der Aufrufer.
func searchFilter(expr string, ascii []string, nr int64) (string, []any) {
	if len(ascii) == 0 {
		return "", nil
	}
	var conds []string
	var args []any
	for _, t := range ascii {
		conds = append(conds, `instr(lower(`+expr+`), ?) > 0`)
		args = append(args, t)
	}
	q := ` AND ((` + strings.Join(conds, ` AND `) + `)`
	if nr >= 0 {
		q += ` OR nr = ?`
		args = append(args, nr)
	}
	return q + `)`, args
}

// SearchJobs liefert eigenständige Notizen (keine leeren, keine Planungsrunden) vor before, neueste zuerst.
func (s *Store) SearchJobs(before int64, limit int, ascii []string, nr int64) ([]SearchRow, error) {
	f, fa := searchFilter(`title || ' ' || transcript || ' ' || result || ' ' || tags`, ascii, nr)
	args := append([]any{before}, fa...)
	args = append(args, limit)
	rows, err := s.DB.Query(`SELECT id, nr, received_at, title, transcript, result, tags FROM jobs
		WHERE planning_id = '' AND deleted_at = 0 AND mode = 'diktat' AND empty = 0 AND received_at < ?`+f+`
		ORDER BY received_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchRow
	for rows.Next() {
		r := SearchRow{Kind: "job"}
		if err := rows.Scan(&r.ID, &r.Nr, &r.At, &r.Title, &r.Transcript, &r.Result, &r.Tags); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchPlannings liefert Planungen mit dem Wortlaut aller Runden, nach ihrer jüngsten Runde vor before.
func (s *Store) SearchPlannings(before int64, limit int, ascii []string, nr int64) ([]SearchRow, error) {
	f, fa := searchFilter(`title || ' ' || plan || ' ' || tags || ' ' || t`, ascii, nr)
	args := append([]any{before}, fa...)
	args = append(args, limit)
	rows, err := s.DB.Query(`SELECT * FROM (SELECT p.id, p.nr, MAX(j.received_at) AS at, p.title, group_concat(j.transcript, ' ') AS t, p.plan, p.tags
		FROM plannings p JOIN jobs j ON j.planning_id = p.id AND j.deleted_at = 0 WHERE p.deleted_at = 0 GROUP BY p.id)
		WHERE at < ?`+f+` ORDER BY at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchRow
	for rows.Next() {
		r := SearchRow{Kind: "planung"}
		if err := rows.Scan(&r.ID, &r.Nr, &r.At, &r.Title, &r.Transcript, &r.Result, &r.Tags); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Suchindex (FTS5) ---

const searchStandKey = "suche_stand"

// searchJobText / searchPlanText: der durchsuchte Text einer Notiz bzw. einer Planung (mit dem Wortlaut aller Runden).
const searchJobInsert = `INSERT INTO suche (kind, ref, text, tags) SELECT 'job', id, title || ' ' || transcript || ' ' || CASE WHEN json_valid(result) THEN json_remove(result, '$.tags', '$.neue_tags') ELSE result END, tags
	FROM jobs WHERE id = ? AND planning_id = '' AND deleted_at = 0 AND mode = 'diktat' AND empty = 0`
const searchPlanInsert = `INSERT INTO suche (kind, ref, text, tags) SELECT 'planung', p.id, p.title || ' ' || CASE WHEN json_valid(p.plan) THEN json_remove(p.plan, '$.tags', '$.neue_tags') ELSE p.plan END || ' ' || group_concat(j.transcript, ' '), p.tags
	FROM plannings p JOIN jobs j ON j.planning_id = p.id AND j.deleted_at = 0 WHERE p.id = ? AND p.deleted_at = 0 GROUP BY p.id`

// SearchSync bringt den Suchindex auf den Stand: beim ersten Mal ganz, danach nur, was seit dem letzten Lauf geändert
// wurde (updated_at). Endgültig Gelöschtes kann als Zeile stehen bleiben; die Suche prüft jeden Treffer gegen die Tabelle.
func (s *Store) SearchSync() error {
	start := Now()
	var stand int64
	if v, err := s.Get(searchStandKey); err == nil && v != "" {
		stand, _ = strconv.ParseInt(v, 10, 64)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if stand == 0 {
		for _, q := range []string{`DELETE FROM suche`,
			`INSERT INTO suche (kind, ref, text, tags) SELECT 'job', id, title || ' ' || transcript || ' ' || CASE WHEN json_valid(result) THEN json_remove(result, '$.tags', '$.neue_tags') ELSE result END, tags
				FROM jobs WHERE planning_id = '' AND deleted_at = 0 AND mode = 'diktat' AND empty = 0`,
			`INSERT INTO suche (kind, ref, text, tags) SELECT 'planung', p.id, p.title || ' ' || CASE WHEN json_valid(p.plan) THEN json_remove(p.plan, '$.tags', '$.neue_tags') ELSE p.plan END || ' ' || group_concat(j.transcript, ' '), p.tags
				FROM plannings p JOIN jobs j ON j.planning_id = p.id AND j.deleted_at = 0 WHERE p.deleted_at = 0 GROUP BY p.id`} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
	} else {
		type ref struct{ kind, id string }
		var refs []ref
		rows, err := tx.Query(`SELECT CASE WHEN planning_id = '' THEN 'job' ELSE 'planung' END, CASE WHEN planning_id = '' THEN id ELSE planning_id END
			FROM jobs WHERE updated_at > ? UNION SELECT 'planung', id FROM plannings WHERE updated_at > ?`, stand, stand)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r ref
			if err := rows.Scan(&r.kind, &r.id); err != nil {
				rows.Close()
				return err
			}
			refs = append(refs, r)
		}
		rows.Close()
		for _, r := range refs {
			if _, err := tx.Exec(`DELETE FROM suche WHERE ref = ?`, r.id); err != nil {
				return err
			}
			ins := searchJobInsert
			if r.kind == "planung" {
				ins = searchPlanInsert
			}
			if _, err := tx.Exec(ins, r.id); err != nil {
				return err
			}
		}
	}
	// Etwas Rückstand: was während des Laufs geändert wurde, kommt beim nächsten Mal sicher mit.
	if _, err := tx.Exec(`INSERT INTO kv (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`, searchStandKey, strconv.FormatInt(start-1000, 10)); err != nil {
		return err
	}
	return tx.Commit()
}

// SearchIndex liefert Kandidaten aus dem Suchindex, neueste zuerst, vor before; alle Wörter (je mindestens 3 Zeichen)
// müssen vorkommen. Stufe 1: alle in Titel, Wortlaut oder Auswertung; Stufe 2: nur zusammen mit den Tags.
// Die Felder kommen frisch aus den Tabellen; Gelöschtes fällt dabei heraus.
func (s *Store) SearchIndex(words []string, tier int, before int64, limit int) ([]SearchRow, error) {
	var parts []string
	for _, w := range words {
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	all := `(` + strings.Join(parts, " AND ") + `)`
	match := `text : ` + all
	if tier == 2 {
		match = all + ` NOT text : ` + all
	}
	rows, err := s.DB.Query(`SELECT * FROM (
		SELECT 'job', j.id, j.nr, j.received_at AS at, j.title, j.transcript, j.result, j.tags
			FROM suche s JOIN jobs j ON s.kind = 'job' AND j.id = s.ref AND j.deleted_at = 0 AND j.planning_id = ''
			WHERE s.suche MATCH ? AND j.received_at < ?
		UNION ALL
		SELECT 'planung', p.id, p.nr, (SELECT MAX(received_at) FROM jobs WHERE planning_id = p.id AND deleted_at = 0) AS at, p.title,
			(SELECT group_concat(transcript, ' ') FROM jobs WHERE planning_id = p.id AND deleted_at = 0), p.plan, p.tags
			FROM suche s JOIN plannings p ON s.kind = 'planung' AND p.id = s.ref AND p.deleted_at = 0
			WHERE s.suche MATCH ?)
		WHERE at < ? ORDER BY at DESC LIMIT ?`, match, before, match, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchRow
	for rows.Next() {
		var r SearchRow
		var tr sql.NullString
		if err := rows.Scan(&r.Kind, &r.ID, &r.Nr, &r.At, &r.Title, &tr, &r.Result, &r.Tags); err != nil {
			return nil, err
		}
		r.Transcript = tr.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// Open öffnet eine unverschlüsselte Datenbank als Daten von Nutzer 1 (Tests und lokale Werkzeuge).
func Open(path string) (*Store, error) { return OpenUser(path, nil, 1) }
