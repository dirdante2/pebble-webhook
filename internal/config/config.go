// Package config: Start-Werte aus der Umgebung, alle übrigen Einstellungen aus der Datenbank (Web-Oberfläche).
package config

import (
	"bufio"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/whisper"
)

type Config struct {
	Language     string // de | en (Oberfläche, Meldungen, KI-Texte)
	Listen       string
	PublicURL    string // https://host, ohne Slash am Ende
	DataDir      string
	MediaDir     string
	BackupDir    string // Ablage der Backups (Startwert BACKUP_DIR, vom Installer freigegeben)
	KeyDir       string // Schlüssel je Nutzer (Startwert KEY_DIR, nie im Backup)
	TrustedProxy string

	HookTokenHeader string
	HookMaxBytes    int64
	HookRatePerMin  int
	HookCapture     bool // letzte Zustellung roh nach DATA_DIR/mitschnitt/<nutzer>/
	MinFreeBytes    uint64

	TriggerSingle     string // diktat | planung
	TriggerDouble     string
	RingCapacityHours int // Akku eines neuen Rings in Stunden Aufnahme

	WhisperModel         string // Modell der lokalen Transkription (whisper.Models)
	WhisperKeepMinutes   int    // Modell so lange nach der letzten Aufnahme im RAM halten; 0 = dauerhaft
	AI                   AI     // Zugänge und je Bereich die Reihenfolge der Anbieter (KI_ZUGAENGE, KI_BEREICHE)
	ImagesEnabled        bool   // KI darf Bilder erzeugen (Bereich Bilder hat Einträge)
	PlanReasoningEffort  string
	YouTubeKey           string // YouTube Data API v3 (Trailer); leer = Suche über die KI-Websuche
	ResearchLimitUSD     float64
	UserDailyLimitUSD    float64 // KI-Kosten je Nutzer und Tag (0 = kein Limit; Admins ausgenommen)
	DecisionMinConf      float64 // Entscheidungsmodell: ab dieser Sicherheit feste Vorgabe für die Auswertung
	ResearchMaxOutTokens int

	TrashRetentionDays  int // Papierkorb: so viele Tage nach dem Löschen endgültig entfernen
	AutoCloseDays       int // offene Planungen ohne neue Runde schließen; 0 = nie
	MemoryTrainEntries  int // Lernlauf: so viele der jüngsten Notizen und Planungen auswerten
	MemoryRejected      int // so viele zuletzt gelöschte Gedächtnis-Einträge gehen als „nicht erwünscht“ an die KI
	MaxAttempts         int
	EmptyRetentionHours int // leere Aufnahmen löschen; 0 = nie
	DoneActiveMinutes   int // erledigte Punkte und Listen so lange noch im aktiven Bereich zeigen
	NoteContextMinutes  int // Notizen so viele Minuten vor einer neuen gehen als möglicher Zusammenhang an die Auswertung; 0 = aus
	VisitGapMinutes     int // Pause, nach der das Öffnen der App als neuer Besuch zählt (Grenze für „Neu“ im Verlauf)
	HistoryMinEntries   int // Standardansicht des Verlaufs: mindestens so viele Einträge, auch wenn älter als 24 Stunden

	JoplinEnabled  bool   // Schalter JOPLIN_ENABLED (Reiter „Joplin“ in der Web-App)
	JoplinURL      string // leer = Joplin aus (inaktiv oder noch nicht eingerichtet)
	JoplinEmail    string
	JoplinPassword string
	JoplinRoot     string

	VAPIDSubject   string
	UpdateRepo     string // GitHub owner/repo für die Update-Prüfung; leer = aus
	UpdateChannel  string // stable | beta | nightly
	AutoUpdate     bool   // täglich zur Uhrzeit AutoUpdateTime die neueste Version des Kanals installieren
	AutoUpdateTime string // HH:MM

	BackupSchedule      string // täglich | wöchentlich (montags) | monatlich (am 1.) | aus
	BackupTime          string // HH:MM
	BackupRetentionDays int    // geplante und manuelle Backups so lange behalten
	BackupAudio         bool   // Aufnahmen mit ins Backup

	Notify    map[string]bool // Benachrichtigungs-Kanal (NotifyUpdates …) → an
	Telemetry bool            // anonyme Statistik senden (persönlich)
}

// LoadFile setzt Variablen aus einer KEY=VALUE-Datei, ohne vorhandene zu überschreiben.
// Für CLI-Aufrufe außerhalb von systemd (enroll).
func LoadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

// Bootstrap sind die Werte, die der Dienst vor dem Öffnen der Datenbank braucht; sie bleiben in der
// Umgebung (systemd EnvironmentFile). Alle übrigen Einstellungen liegen in der Datenbank (siehe Fields).
type Bootstrap struct {
	Listen    string
	DataDir   string
	MediaDir  string
	BackupDir string
	KeyDir    string // Schlüssel je Nutzer (nie im Backup)
}

func LoadBootstrap() Bootstrap {
	get := func(k, def string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return def
	}
	return Bootstrap{Listen: get("LISTEN", "0.0.0.0:8080"), DataDir: get("DATA_DIR", "/var/lib/pebble-webhook"),
		MediaDir: get("MEDIA_DIR", get("AUDIO_DIR", "/srv/pebble-medien")), BackupDir: get("BACKUP_DIR", "/srv/pebble-backup"),
		KeyDir: get("KEY_DIR", "/etc/pebble-webhook/keys")}
}

// Load liest alle Einstellungen aus der Umgebung (Tests, Übernahme einer alten env-Datei).
func Load() (*Config, error) {
	return FromValues(LoadBootstrap(), os.Getenv)
}

// FromValues baut die Einstellungen aus den Start-Werten und einer Quelle für alle übrigen Schlüssel
// (Datenbank oder Umgebung); leere Werte gelten als nicht gesetzt.
func FromValues(boot Bootstrap, lookup func(string) string) (*Config, error) {
	var errs []string
	base := lookup
	lookup = func(k string) string {
		if v, ok := EnvLocked(k); ok {
			return v
		}
		return base(k)
	}
	// Altbestand ohne Schalter: Joplin ist aktiv, wenn ein Server eingetragen ist.
	joplinSet := strings.TrimSpace(lookup("JOPLIN_ENABLED"))
	if joplinSet == "" && strings.TrimSpace(lookup("JOPLIN_URL")) != "" {
		joplinSet = "aktiv"
	}
	lookup = WithDefaults(lookup)
	// Art der Felder prüfen (Zahl, Auswahl); die Meldungen nennen den Namen aus der Oberfläche.
	for _, f := range Fields {
		v := strings.TrimSpace(lookup(f.Key))
		switch {
		case v == "":
			continue
		case f.Kind == "int":
			n, err := strconv.Atoi(v)
			lim, ok := intLimits[f.Key]
			if !ok {
				lim = [2]int{0, 1_000_000}
			}
			switch {
			case err != nil:
				errs = append(errs, i18n.T(f.Label)+i18n.T(": keine ganze Zahl"))
			case n < lim[0] || n > lim[1]:
				errs = append(errs, i18n.Tf("%s: erlaubt %d bis %d", i18n.T(f.Label), lim[0], lim[1]))
			}
		case f.Kind == "url" && f.Key != "PUBLIC_URL": // die öffentliche Adresse prüft unten eine eigene, strengere Regel
			if u, err := url.Parse(v); err != nil || u.Host == "" || !hostRe.MatchString(u.Host) || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(v, " \t\n<>\"'") {
				errs = append(errs, i18n.T(f.Label)+i18n.T(": keine gültige http(s)-Adresse"))
			}
		case f.Kind == "num":
			if n, err := strconv.ParseFloat(v, 64); err != nil || n < 0 || n > 1_000_000 || math.IsNaN(n) {
				errs = append(errs, i18n.T(f.Label)+i18n.T(": keine Zahl"))
			}
		case f.Kind == "choice" && !slices.Contains(f.Choices, v):
			errs = append(errs, i18n.T(f.Label)+i18n.T(": unbekannter Wert"))
		case f.Kind == "multi" && v != "keine":
			for _, x := range strings.Split(v, ",") {
				if !slices.Contains(f.Choices, x) {
					errs = append(errs, i18n.T(f.Label)+i18n.T(": unbekannter Wert"))
					break
				}
			}
		}
	}
	str := func(k, def string) string {
		if v := strings.TrimSpace(lookup(k)); v != "" {
			return v
		}
		return def
	}
	need := func(k string) string {
		v := str(k, "")
		if v == "" {
			name := k
			if f, ok := FieldByKey(k); ok {
				name = i18n.T(f.Label)
			}
			errs = append(errs, i18n.Tf("%s fehlt", name))
		}
		return v
	}
	num := func(k string, def int) int {
		v := str(k, "")
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return def // gemeldet von der Prüfung der Felder
		}
		return n
	}
	mode := func(k, def string) string {
		v := str(k, def)
		if v != "diktat" && v != "planung" {
			errs = append(errs, k+i18n.T(" muss diktat oder planung sein"))
		}
		return v
	}

	c := &Config{
		Listen:       boot.Listen,
		PublicURL:    strings.TrimRight(need("PUBLIC_URL"), "/"),
		DataDir:      boot.DataDir,
		MediaDir:     boot.MediaDir,
		BackupDir:    boot.BackupDir,
		KeyDir:       boot.KeyDir,
		TrustedProxy: str("TRUSTED_PROXY", ""),

		HookTokenHeader: str("HOOK_TOKEN_HEADER", "X-Webhook-Token"),
		HookMaxBytes:    int64(num("HOOK_MAX_MB", 25)) << 20,
		HookRatePerMin:  num("HOOK_RATE_PER_MIN", 30),
		HookCapture:     str("HOOK_MITSCHNITT", "inaktiv") == "aktiv",
		MinFreeBytes:    uint64(num("MIN_FREE_MB", 500)) << 20,

		TriggerSingle:     mode("TRIGGER_SINGLE", "diktat"),
		TriggerDouble:     mode("TRIGGER_DOUBLE", "planung"),
		RingCapacityHours: num("RING_CAPACITY_HOURS", 12),

		WhisperModel:         str("WHISPER_MODEL", whisper.DefaultModel),
		WhisperKeepMinutes:   num("WHISPER_KEEP_MINUTES", 0),
		Language:             i18n.Norm(str("LANGUAGE", "de")),
		PlanReasoningEffort:  strings.TrimPrefix(str("OPENAI_PLAN_REASONING_EFFORT", ""), "standard"),
		YouTubeKey:           str("YOUTUBE_API_KEY", ""),
		ResearchMaxOutTokens: num("RESEARCH_MAX_OUTPUT_TOKENS", 6000),

		TrashRetentionDays: num("TRASH_RETENTION_DAYS", 183),
		AutoCloseDays:      num("PLANNING_AUTOCLOSE_DAYS", 3),
		MemoryTrainEntries: num("MEMORY_TRAIN_ENTRIES", 30),
		MemoryRejected:     num("MEMORY_REJECTED", 30),
		MaxAttempts:        num("MAX_ATTEMPTS", 6),

		EmptyRetentionHours: num("EMPTY_RETENTION_HOURS", 1),
		DoneActiveMinutes:   num("LIST_DONE_ACTIVE_MINUTES", 60),
		VisitGapMinutes:     num("VISIT_GAP_MINUTES", 30),
		NoteContextMinutes:  num("NOTE_CONTEXT_MINUTES", 30),
		HistoryMinEntries:   num("HISTORY_MIN_ENTRIES", 10),

		JoplinEnabled:  joplinSet == "aktiv",
		JoplinURL:      strings.TrimRight(str("JOPLIN_URL", ""), "/"),
		JoplinEmail:    str("JOPLIN_EMAIL", ""),
		JoplinPassword: str("JOPLIN_PASSWORD", ""),
		JoplinRoot:     str("JOPLIN_NOTEBOOK", "Sprachnotizen"),

		VAPIDSubject:   str("VAPID_SUBJECT", ""),
		UpdateRepo:     str("UPDATE_REPO", "dirdante2/pebble-webhook"),
		UpdateChannel:  str("UPDATE_CHANNEL", "stable"),
		AutoUpdate:     str("AUTO_UPDATE", "aktiv") == "aktiv",
		AutoUpdateTime: str("AUTO_UPDATE_TIME", "04:00"),

		BackupSchedule:      str("BACKUP_SCHEDULE", "täglich"),
		BackupTime:          str("BACKUP_TIME", "03:00"),
		BackupRetentionDays: num("BACKUP_RETENTION_DAYS", 30),
		BackupAudio:         str("BACKUP_AUDIO", "inaktiv") == "aktiv",
	}
	if c.UpdateRepo == "aus" {
		c.UpdateRepo = ""
	}
	c.Telemetry = lookup(Telemetry) != "inaktiv"
	c.Notify = map[string]bool{}
	for _, k := range NotifyChannels {
		c.Notify[k] = lookup(k) != "inaktiv"
	}
	ai, aiErrs := parseAI(str)
	errs = append(errs, aiErrs...)
	ai.normalize()
	errs = append(errs, ai.check()...)
	c.AI, c.ImagesEnabled = ai, len(ai.Active(AreaImage)) > 0
	if _, ok := whisper.ByID(c.WhisperModel); !ok {
		errs = append(errs, i18n.T("WHISPER_MODEL ist kein bekanntes Modell"))
	}
	limit := str("RESEARCH_LIMIT_USD", "0.50")
	f, err := strconv.ParseFloat(limit, 64)
	if err != nil || f <= 0 {
		errs = append(errs, i18n.T("RESEARCH_LIMIT_USD ist keine positive Zahl"))
	}
	c.ResearchLimitUSD = f
	if v, err := strconv.ParseFloat(str("USER_DAILY_LIMIT_USD", "2"), 64); err != nil || v < 0 {
		errs = append(errs, i18n.T("USER_DAILY_LIMIT_USD ist keine Zahl ab 0"))
	} else {
		c.UserDailyLimitUSD = v
	}
	if v, err := strconv.ParseFloat(str("DECISION_MIN_CONFIDENCE", "0.7"), 64); err != nil || v < 0 || v > 1 {
		errs = append(errs, i18n.T("DECISION_MIN_CONFIDENCE ist keine Zahl von 0 bis 1"))
	} else {
		c.DecisionMinConf = v
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		switch {
		case err != nil || u.Host == "" || !hostRe.MatchString(u.Host):
			errs = append(errs, i18n.T("Öffentliche Adresse: keine gültige Adresse (erlaubt: Buchstaben, Ziffern, Punkt, Bindestrich, :Port)"))
		case u.Scheme != "https" && u.Hostname() != "localhost":
			errs = append(errs, i18n.T("Öffentliche Adresse: muss mit https:// beginnen"))
		case u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil:
			errs = append(errs, i18n.T("Öffentliche Adresse: nur https://host, ohne Pfad, ? oder #"))
		}
	}
	// Joplin: inaktiv → aus, auch wenn Werte stehen; aktiv ohne Server → noch nicht eingerichtet (kein Fehler).
	if !c.JoplinEnabled {
		c.JoplinURL = ""
	} else if c.JoplinURL != "" && (c.JoplinEmail == "" || c.JoplinPassword == "") {
		errs = append(errs, i18n.T("Joplin: Konto (E-Mail) und Passwort fehlen"))
	}
	if c.TrustedProxy != "" && net.ParseIP(c.TrustedProxy) == nil {
		errs = append(errs, i18n.T("Reverse-Proxy: keine IP-Adresse"))
	}
	if !headerRe.MatchString(c.HookTokenHeader) {
		errs = append(errs, i18n.T("Header für den Token: nur Buchstaben, Ziffern und Bindestriche"))
	}
	if c.VAPIDSubject != "" && !strings.HasPrefix(c.VAPIDSubject, "mailto:") && !strings.HasPrefix(c.VAPIDSubject, "https://") {
		errs = append(errs, i18n.T("Kontakt für Web-Push: mailto:… oder https://…"))
	}
	if _, err := time.Parse("15:04", c.AutoUpdateTime); err != nil || len(c.AutoUpdateTime) != 5 {
		errs = append(errs, i18n.T("Uhrzeit automatisches Update: HH:MM, z. B. 04:00"))
	}
	if _, err := time.Parse("15:04", c.BackupTime); err != nil || len(c.BackupTime) != 5 {
		errs = append(errs, i18n.T("Uhrzeit Backup: HH:MM, z. B. 03:00"))
	}
	switch c.BackupSchedule {
	case "täglich", "wöchentlich", "monatlich", "aus":
	default:
		errs = append(errs, i18n.T("Backup-Plan: täglich, wöchentlich, monatlich oder aus"))
	}
	if c.UpdateRepo != "" && !repoRe.MatchString(c.UpdateRepo) {
		errs = append(errs, i18n.T("Update-Quelle: owner/repo oder aus"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("Konfiguration: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

// intLimits: erlaubter Bereich je Ganzzahl-Einstellung (Vorgabe 0 … 1 000 000). Grenzen verhindern Werte,
// mit denen der Dienst nicht sinnvoll läuft (0 Versuche, 0 MB Aufnahme) oder überläuft.
var intLimits = map[string][2]int{
	"HOOK_MAX_MB":                {1, 2048},
	"HOOK_RATE_PER_MIN":          {1, 10_000},
	"RESEARCH_MAX_OUTPUT_TOKENS": {256, 200_000},
	"TRASH_RETENTION_DAYS":       {1, 36_500},
	"BACKUP_RETENTION_DAYS":      {1, 36_500},
	"EMPTY_RETENTION_HOURS":      {0, 87_600},
	"RING_CAPACITY_HOURS":        {1, 1000},
	"PLANNING_AUTOCLOSE_DAYS":    {0, 3650},
	"MAX_ATTEMPTS":               {1, 50},
	"MIN_FREE_MB":                {0, 10_000_000},
	"LIST_DONE_ACTIVE_MINUTES":   {0, 525_600},
	"VISIT_GAP_MINUTES":          {1, 10_080},
	"NOTE_CONTEXT_MINUTES":       {0, 1440},
	"HISTORY_MIN_ENTRIES":        {1, 500},
	"MEMORY_TRAIN_ENTRIES":       {1, 500},
	"MEMORY_REJECTED":            {0, 500},
}

var (
	hostRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)
	headerRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	modelRe  = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,100}$`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// Host liefert den Hostnamen der öffentlichen Adresse (WebAuthn RP-ID).
func (c *Config) Host() string {
	u, _ := url.Parse(c.PublicURL)
	return u.Hostname()
}
