package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"

	"pebble-webhook/internal/whisper"
)

// Field beschreibt eine Einstellung, die in der Datenbank liegt und über die Web-Oberfläche gepflegt wird
// (Einrichtungsassistent, Status → Einstellungen). Default füllt das Formular vor und gilt, wenn nichts gesetzt ist.
type Field struct {
	Key      string            `json:"key"`
	Group    string            `json:"gruppe"`
	Label    string            `json:"name"`
	Help     string            `json:"hilfe,omitempty"`
	Kind     string            `json:"art"` // text | secret | int | num | choice | multi (Komma-Liste aus Choices, „keine“ = nichts) | url
	Default  string            `json:"vorgabe"`
	Required bool              `json:"pflicht,omitempty"`
	Choices  []string          `json:"auswahl,omitempty"`
	Generate bool              `json:"erzeugen,omitempty"`      // Geheimnis, das der Assistent selbst erzeugt
	Personal bool              `json:"persoenlich,omitempty"`   // je Nutzer (in seiner Datenbank), sonst für das ganze System
	Labels   map[string]string `json:"bezeichnungen,omitempty"` // Anzeige je Auswahlwert (sonst der Wert selbst)
	// Hidden: nicht in Formularen (KI_ZUGAENGE/KI_BEREICHE haben eigene Seiten; frühere KI-Einzelwerte bleiben nur
	// als Quelle für die Übernahme in Zugänge und Bereiche). Setup: trotzdem im Einrichtungsassistenten.
	Hidden bool `json:"-"`
	Setup  bool `json:"-"`
}

// triggerLabels: Anzeige der Aktionen einer Geste.
var triggerLabels = map[string]string{"diktat": "Notiz", "planung": "Planung (Recherche mit Antwort)"}

// providerLabels: Anzeige der Anbieter eines KI-Bereichs.
var providerLabels = map[string]string{"openai": "OpenAI", "dienst": "Dienst", "lokal": "Lokal"}

// personal: Einstellungen, die jeder Nutzer selbst setzt (überlagern den Wert des Systems).
var personal = map[string]bool{"LANGUAGE": true, "TRIGGER_SINGLE": true, "TRIGGER_DOUBLE": true, "RING_CAPACITY_HOURS": true,
	"JOPLIN_ENABLED": true, "JOPLIN_URL": true, "JOPLIN_EMAIL": true, "JOPLIN_PASSWORD": true, "JOPLIN_NOTEBOOK": true,
	"LIST_DONE_ACTIVE_MINUTES": true, "VISIT_GAP_MINUTES": true, "NOTE_CONTEXT_MINUTES": true, "HISTORY_MIN_ENTRIES": true,
	"MEMORY_TRAIN_ENTRIES": true, "MEMORY_REJECTED": true,
	NotifyUpdates: true, NotifyQuestions: true, NotifyErrors: true, NotifySaved: true, NotifyAppointments: true, NotifyRing: true}

// Kanäle der Benachrichtigungen (Web-Push); je Nutzer einzeln an oder aus.
const (
	NotifyUpdates      = "NOTIFY_UPDATES"
	NotifyQuestions    = "NOTIFY_QUESTIONS"
	NotifyErrors       = "NOTIFY_ERRORS"
	NotifySaved        = "NOTIFY_SAVED"
	NotifyAppointments = "NOTIFY_APPOINTMENTS"
	NotifyRing         = "NOTIFY_RING"
)

// Telemetry: anonyme Nutzungsstatistik an den Entwickler (für die ganze Installation, nur Admins schalten; Vorgabe an).
const Telemetry = "TELEMETRY"

// NotifyChannels: alle Kanäle in der Reihenfolge der Oberfläche.
var NotifyChannels = []string{NotifyQuestions, NotifyErrors, NotifySaved, NotifyAppointments, NotifyRing, NotifyUpdates}

func init() {
	for i := range Fields {
		Fields[i].Personal = personal[Fields[i].Key]
	}
}

// Fields: alle Einstellungen außer den Start-Werten (LISTEN, DATA_DIR, MEDIA_DIR, BACKUP_DIR).
var Fields = []Field{
	{Key: "LANGUAGE", Group: "Sprache", Label: "Sprache", Help: "Sprache der Web-App, der Meldungen und der Texte, die die KI schreibt; auch die Transkription richtet sich danach. Bestehende Einträge bleiben, wie sie sind.", Kind: "choice", Default: "de", Choices: []string{"de", "en"}},

	{Key: "PUBLIC_URL", Group: "Adresse", Label: "Öffentliche Adresse", Help: "https-Adresse der Web-App ohne Slash am Ende; bestimmt die Passkey-Domain. Später ändern macht alle Passkeys ungültig.", Kind: "url", Required: true},
	{Key: "TRUSTED_PROXY", Group: "Adresse", Label: "Reverse-Proxy", Help: "IP des Proxys davor; nur von dort wird X-Real-IP geglaubt. Leer = keiner.", Kind: "text"},

	{Key: "HOOK_TOKEN_HEADER", Group: "Pebble-App", Label: "Header für den Token", Help: "Name des Headers, in dem die Pebble-App das Webhook-Token mitschickt; fest vorgegeben.", Kind: "text", Default: "X-Webhook-Token", Required: true, Hidden: true},
	{Key: "TRIGGER_SINGLE", Group: "Pebble-App", Label: "Geste „Halten“", Help: "Was eine Aufnahme mit „Halten“ auslöst.", Kind: "choice", Default: "diktat", Choices: []string{"diktat", "planung"}, Labels: triggerLabels},
	{Key: "TRIGGER_DOUBLE", Group: "Pebble-App", Label: "Geste „Klick + Halten“", Help: "Was eine Aufnahme mit „Klick + Halten“ auslöst.", Kind: "choice", Default: "planung", Choices: []string{"diktat", "planung"}, Labels: triggerLabels},
	{Key: "RING_CAPACITY_HOURS", Group: "Pebble-App", Label: "Kapazität eines Rings (Stunden Aufnahme)", Help: "Vorgabe für neue Ringe; Pebble nennt 12–15 Stunden. Warnungen bei 60, 80 und 95 %.", Kind: "int", Default: "12"},
	{Key: "HOOK_MAX_MB", Group: "Pebble-App", Label: "Größte Aufnahme (MB)", Help: "Größere Aufnahmen weist der Webhook ab.", Kind: "int", Default: "25"},
	{Key: "HOOK_RATE_PER_MIN", Group: "Pebble-App", Label: "Aufrufe je Minute", Help: "Höchstens so viele Webhook-Aufrufe je Minute; mehr werden abgewiesen (Schutz vor Missbrauch).", Kind: "int", Default: "30"},
	{Key: "HOOK_MITSCHNITT", Group: "Pebble-App", Label: "Zustellungen mitschneiden", Help: "Nur zur Fehlersuche: jede Zustellung der Pebble-App liegt roh und unverschlüsselt in einem eigenen Ordner unter DATA_DIR/mitschnitt/<nutzer>/ (die letzten 200), mit allen Headern, Feldern und dem Audio. Ausschalten löscht die Mitschnitte bei der nächsten Zustellung.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},

	{Key: "KI_ZUGAENGE", Group: "KI-Zugänge", Label: "KI-Zugänge", Kind: "json", Hidden: true},
	{Key: "KI_BEREICHE", Group: "KI-Zugänge", Label: "KI-Bereiche", Kind: "json", Hidden: true},
	{Key: "OPENAI_ENABLED", Group: "KI-Zugänge", Label: "OpenAI", Help: "Zugang zu OpenAI nutzen.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}, Hidden: true},
	{Key: "OPENAI_API_KEY", Group: "KI-Zugänge", Label: "OpenAI: API-Schlüssel", Help: "Nur nötig, wenn ein Bereich OpenAI nutzt. Einziger Anbieter mit erzeugten Bildern.", Kind: "secret", Hidden: true, Setup: true},
	{Key: "OPENAI_BASE_URL", Group: "KI-Zugänge", Label: "OpenAI: API-Adresse", Help: "Adresse der OpenAI-API (Responses-API).", Kind: "url", Default: "https://api.openai.com/v1", Hidden: true},
	{Key: "SERVICE_ENABLED", Group: "KI-Zugänge", Label: "Dienst", Help: "Zugang zu einem OpenAI-kompatiblen Dienst nutzen.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}, Hidden: true},
	{Key: "SERVICE_BASE_URL", Group: "KI-Zugänge", Label: "Dienst: API-Adresse", Help: "Ein OpenAI-kompatibler Dienst (Chat-Completions), z. B. OpenRouter https://openrouter.ai/api/v1, Gemini https://generativelanguage.googleapis.com/v1beta/openai, Groq https://api.groq.com/openai/v1, Mistral https://api.mistral.ai/v1, Perplexity https://api.perplexity.ai. Gratis-Kontingente nutzen Daten teils zum Training.", Kind: "url", Hidden: true},
	{Key: "SERVICE_API_KEY", Group: "KI-Zugänge", Label: "Dienst: API-Schlüssel", Help: "Schlüssel des Dienstes.", Kind: "secret", Hidden: true},
	{Key: "LOCAL_ENABLED", Group: "KI-Zugänge", Label: "Lokal", Help: "Zugang zu einem lokalen Modell nutzen.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}, Hidden: true},
	{Key: "LOCAL_BASE_URL", Group: "KI-Zugänge", Label: "Lokal: API-Adresse", Help: "Ollama (http://<server>:11434/v1) oder LM Studio im eigenen Netz; ohne Schlüssel, Daten bleiben im Haus. Sucht nicht selbst im Web.", Kind: "url", Hidden: true},
	{Key: "SEARXNG_ENABLED", Group: "KI-Zugänge", Label: "SearXNG", Help: "Eigene Suchmaschine nutzen.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}, Hidden: true},
	{Key: "SEARXNG_URL", Group: "KI-Zugänge", Label: "SearXNG: Adresse", Help: "Eigene Suchmaschine für die Websuche, auch für lokale Modelle. JSON-Ausgabe muss erlaubt sein (settings.yml: formats json).", Kind: "url", Hidden: true},
	{Key: "DECISION_MIN_CONFIDENCE", Group: "Entscheidung", Label: "Mindestsicherheit", Help: "Ab dieser Sicherheit (0–1) gilt die Antwort des Entscheidungsmodells als feste Vorgabe für die Auswertung; darunter entscheidet die Auswertung selbst.", Kind: "num", Default: "0.7"},
	{Key: "USER_DAILY_LIMIT_USD", Group: "KI-Zugänge", Label: "Tageslimit KI-Kosten je Nutzer (USD)", Help: "Darüber warten neue Aufträge eines Nutzers bis zum nächsten Tag. 0 = kein Limit; Admins ausgenommen.", Kind: "num", Default: "2"},

	{Key: "TRANSCRIPTION", Group: "Transkription", Label: "Transkription", Help: "Wer die Aufnahme in Text umwandelt. Lokal = Whisper auf diesem Server, die Aufnahme verlässt ihn nicht; das Modell wird nach dem Speichern geladen. Mit Ausweichweg übernimmt der Cloud-Anbieter, wenn lokal etwas scheitert (am Eintrag vermerkt).", Kind: "choice", Default: "openai", Choices: []string{"openai", "lokal", "lokal-openai", "pebble"},
		Labels: map[string]string{"openai": "Nur Cloud-Anbieter", "lokal": "Nur lokal (Whisper)", "lokal-openai": "Lokal, Cloud-Anbieter als Ausweichweg", "pebble": "Text der Pebble-App übernehmen"}, Hidden: true},
	{Key: "TRANSCRIBE_PROVIDER", Group: "Transkription", Label: "Cloud-Anbieter", Help: "Wer in der Cloud transkribiert: OpenAI oder der Dienst (z. B. Groq mit Whisper).", Kind: "choice", Default: "openai", Choices: []string{"openai", "dienst"},
		Labels: map[string]string{"openai": "OpenAI", "dienst": "Dienst"}, Hidden: true},
	{Key: "OPENAI_TRANSCRIBE_MODEL", Group: "Transkription", Label: "Modell (Cloud)", Help: "Modell, das die Aufnahme in der Cloud in Text umwandelt (Groq z. B. whisper-large-v3-turbo).", Kind: "text", Default: "gpt-transcribe", Hidden: true},
	{Key: "PRICE_TRANSCRIBE_PER_MIN", Group: "Transkription", Label: "Preis (USD je Minute)", Help: "Preis je Minute Audio in der Cloud.", Kind: "num", Default: "0.0045", Hidden: true},
	{Key: "WHISPER_MODEL", Group: "Transkription", Label: "Whisper-Modell", Help: "Größer = genauer, aber langsamer und mehr RAM.", Kind: "choice", Default: whisper.DefaultModel, Choices: whisper.IDs()},
	{Key: "WHISPER_KEEP_MINUTES", Group: "Transkription", Label: "Modell im RAM halten (Minuten)", Help: "So lange bleibt das Whisper-Modell nach der letzten Aufnahme im RAM, damit es nicht jedes Mal neu geladen wird. 0 = dauerhaft.", Kind: "int", Default: "0"},

	{Key: "TITLE_PROVIDER", Group: "Kurztitel", Label: "Anbieter", Help: "Kurztitel, Zuordnung zu offenen Planungen, Termine nachtragen, Beschreibungen der Listen. Kleine Aufgaben: ein lokales Modell reicht.", Kind: "choice", Default: "openai", Choices: []string{"openai", "dienst", "lokal"}, Labels: providerLabels, Hidden: true},
	{Key: "OPENAI_TITLE_MODEL", Group: "Kurztitel", Label: "Modell", Help: "Modell beim gewählten Anbieter.", Kind: "text", Default: "gpt-6-luna", Hidden: true},
	{Key: "PRICE_TITLE_INPUT_PER_MTOK", Group: "Kurztitel", Label: "Preis Eingabe (USD je 1 Mio. Tokens)", Help: "Lokal: 0.", Kind: "num", Default: "0.10", Hidden: true},
	{Key: "PRICE_TITLE_OUTPUT_PER_MTOK", Group: "Kurztitel", Label: "Preis Ausgabe (USD je 1 Mio. Tokens)", Help: "Lokal: 0.", Kind: "num", Default: "0.50", Hidden: true},

	{Key: "DICTATION_PROVIDER", Group: "Auswertung & Sortierung", Label: "Anbieter", Help: "Notizen auswerten, in Gruppen und Listen einsortieren, Lernlauf des Gedächtnisses. Braucht strukturierte Ausgabe (JSON-Schema); sehr kleine lokale Modelle liefern oft unbrauchbares JSON.", Kind: "choice", Default: "openai", Choices: []string{"openai", "dienst", "lokal"}, Labels: providerLabels, Hidden: true},
	{Key: "OPENAI_DICTATION_MODEL", Group: "Auswertung & Sortierung", Label: "Modell", Help: "Modell beim gewählten Anbieter. Leer = Modell der Webrecherche.", Kind: "text", Hidden: true},
	{Key: "PRICE_DICTATION_INPUT_PER_MTOK", Group: "Auswertung & Sortierung", Label: "Preis Eingabe (USD je 1 Mio. Tokens)", Help: "Leer = wie Webrecherche. Lokal: 0.", Kind: "num", Hidden: true},
	{Key: "PRICE_DICTATION_OUTPUT_PER_MTOK", Group: "Auswertung & Sortierung", Label: "Preis Ausgabe (USD je 1 Mio. Tokens)", Help: "Leer = wie Webrecherche. Lokal: 0.", Kind: "num", Hidden: true},

	{Key: "RESEARCH_PROVIDER", Group: "Webrecherche", Label: "Anbieter", Help: "Planungen mit Antwort und Trailer-Suche.", Kind: "choice", Default: "openai", Choices: []string{"openai", "dienst", "lokal"}, Labels: providerLabels, Hidden: true},
	{Key: "OPENAI_PLAN_MODEL", Group: "Webrecherche", Label: "Modell", Help: "Modell beim gewählten Anbieter.", Kind: "text", Default: "gpt-6-luna", Hidden: true},
	{Key: "RESEARCH_WEB_SEARCH", Group: "Webrecherche", Label: "Websuche", Help: "Mehrere möglich. Anbieter = Suche von OpenAI, OpenRouter (Web-Plugin) oder einem Modell, das selbst sucht (Perplexity Sonar, Groq Compound); ein lokales Modell sucht nicht selbst. Gilt auch für kurze Suchen der Auswertung. Keine = Antwort aus dem Modellwissen.", Kind: "multi", Default: "anbieter", Choices: []string{"anbieter", "searxng"},
		Labels: map[string]string{"anbieter": "Websuche des Anbieters", "searxng": "SearXNG"}, Hidden: true},
	{Key: "OPENAI_PLAN_REASONING_EFFORT", Group: "Webrecherche", Label: "Denkaufwand", Help: "Wie gründlich das Modell nachdenkt (OpenAI, Grok, Gemini, Muse); gilt auch für die Auswertung. Höher = besser, aber teurer.", Kind: "choice", Default: "low", Choices: []string{"standard", "none", "low", "medium", "high"},
		Labels: map[string]string{"standard": "Vorgabe des Modells", "none": "keiner", "low": "niedrig", "medium": "mittel", "high": "hoch"}},
	{Key: "RESEARCH_LIMIT_USD", Group: "Webrecherche", Label: "Kostenlimit je Recherche (USD)", Help: "Eine Recherche bricht ab, sobald ihre Kosten diesen Betrag erreichen.", Kind: "num", Default: "0.50", Required: true},
	{Key: "RESEARCH_MAX_OUTPUT_TOKENS", Group: "Webrecherche", Label: "Höchstens Ausgabe-Tokens", Help: "Obergrenze für die Länge einer Recherche-Antwort.", Kind: "int", Default: "6000"},
	{Key: "PRICE_PLAN_INPUT_PER_MTOK", Group: "Webrecherche", Label: "Preis Eingabe (USD je 1 Mio. Tokens)", Help: "Fehlt ein Recherche-Preis, läuft keine Recherche. Lokal: 0.", Kind: "num", Default: "0.10", Hidden: true},
	{Key: "PRICE_PLAN_OUTPUT_PER_MTOK", Group: "Webrecherche", Label: "Preis Ausgabe (USD je 1 Mio. Tokens)", Help: "Zählt für das Kostenlimit und die KI-Kosten. Lokal: 0.", Kind: "num", Default: "0.50", Hidden: true},
	{Key: "PRICE_SEARCH_PER_CALL", Group: "Webrecherche", Label: "Preis Websuche (USD je Aufruf)", Help: "Preis je Websuche des Anbieters; SearXNG: 0.", Kind: "num", Default: "0.01", Hidden: true},
	{Key: "IMAGES_GENERATE", Group: "Webrecherche", Label: "Bilder erzeugen (nur OpenAI)", Help: "aktiv = die KI darf zu Notizen und Planungen eigene Bilder erzeugen (braucht den OpenAI-Schlüssel, kostet je Bild). Externe Bilder und Diagramme gibt es immer.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}, Hidden: true},
	{Key: "OPENAI_IMAGE_MODEL", Group: "Webrecherche", Label: "Modell Bilder", Help: "Modell, mit dem die KI erklärende Bilder erzeugt (Images-API).", Kind: "text", Default: "gpt-image-1-mini", Hidden: true},
	{Key: "PRICE_IMAGE_PER_PICTURE", Group: "Webrecherche", Label: "Preis Bild (USD je Bild)", Help: "Zählt zu den KI-Kosten des Eintrags.", Kind: "num", Default: "0.01", Hidden: true},

	{Key: "YOUTUBE_API_KEY", Group: "YouTube", Label: "YouTube-API-Schlüssel", Help: "Schlüssel der YouTube Data API v3 für Trailer zu Filmen und Serien (kostenlos, ca. 100 Suchen am Tag). Leer = die KI sucht den Trailer über die Websuche (ungenauer, kostet).", Kind: "secret"},

	{Key: "JOPLIN_ENABLED", Group: "Joplin", Label: "Joplin", Help: "Fertige Notizen, Planungen und Listen nach Joplin übertragen; eingerichtet und getestet wird unter Status → Joplin.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: "JOPLIN_URL", Group: "Joplin", Label: "Joplin-Server", Help: "Adresse deines Joplin-Servers (Sync-Ziel). Übertragen wird nur schreibend; E2EE muss aus sein.", Kind: "url"},
	{Key: "JOPLIN_EMAIL", Group: "Joplin", Label: "Joplin-Konto (E-Mail)", Help: "Anmeldung am Joplin-Server.", Kind: "text"},
	{Key: "JOPLIN_PASSWORD", Group: "Joplin", Label: "Joplin-Passwort", Help: "Passwort zum Joplin-Konto.", Kind: "secret"},
	{Key: "JOPLIN_NOTEBOOK", Group: "Joplin", Label: "Oberstes Notizbuch", Help: "Notizbuch in Joplin, unter dem Notizen, Planungen und Listen abgelegt werden.", Kind: "text", Default: "Sprachnotizen"},

	{Key: "TRASH_RETENTION_DAYS", Group: "Fristen", Label: "Papierkorb leeren nach (Tagen)", Help: "Gelöschte Einträge bleiben so lange im Papierkorb, dann werden sie endgültig entfernt.", Kind: "int", Default: "183"},
	{Key: "EMPTY_RETENTION_HOURS", Group: "Fristen", Label: "Leere Aufnahmen löschen nach (Stunden)", Help: "Aufnahmen ohne erkannte Sprache werden danach endgültig gelöscht. Fehlgeschlagene Einträge bleiben immer, bis du sie löschst. 0 = nie.", Kind: "int", Default: "1"},
	{Key: "PLANNING_AUTOCLOSE_DAYS", Group: "Fristen", Label: "Planungen schließen nach (Tagen ohne Runde)", Help: "0 = nie.", Kind: "int", Default: "3"},
	{Key: "MAX_ATTEMPTS", Group: "Fristen", Label: "Versuche je Schritt", Help: "So oft wird ein fehlgeschlagener Schritt (z. B. Transkription) wiederholt, bevor der Eintrag als Fehler gilt.", Kind: "int", Default: "6"},
	{Key: "MIN_FREE_MB", Group: "Fristen", Label: "Mindestens freier Speicher (MB)", Help: "Darunter werden Aufnahmen abgewiesen.", Kind: "int", Default: "500"},

	{Key: "LIST_DONE_ACTIVE_MINUTES", Group: "Anzeige und Gedächtnis", Label: "Erledigtes aktiv zeigen (Minuten)", Help: "So lange bleiben abgehakte Punkte bei den offenen stehen, danach unter „Vergangen“.", Kind: "int", Default: "60"},
	{Key: "VISIT_GAP_MINUTES", Group: "Anzeige und Gedächtnis", Label: "Neuer Besuch nach (Minuten Pause)", Help: "Nach so langer Pause beginnt ein neuer Besuch; was seit der letzten Aktion davor kam, gilt als neu.", Kind: "int", Default: "30"},
	{Key: "NOTE_CONTEXT_MINUTES", Group: "Anzeige und Gedächtnis", Label: "Zusammenhang mit Notizen davor (Minuten)", Help: "Notizen aus so vielen Minuten davor sieht die KI bei der Auswertung; knüpft eine neue daran an, kommt sie in dieselbe Liste. 0 = aus.", Kind: "int", Default: "30"},
	{Key: "HISTORY_MIN_ENTRIES", Group: "Anzeige und Gedächtnis", Label: "Verlauf: mindestens Einträge", Help: "Der Verlauf zeigt die letzten 24 Stunden, aber mindestens so viele Einträge je Filter.", Kind: "int", Default: "10"},
	{Key: "MEMORY_TRAIN_ENTRIES", Group: "Anzeige und Gedächtnis", Label: "Lernlauf: jüngste Einträge", Help: "So viele der jüngsten Einträge wertet der Lernlauf des Gedächtnisses aus.", Kind: "int", Default: "30"},
	{Key: "MEMORY_REJECTED", Group: "Anzeige und Gedächtnis", Label: "Gedächtnis: gelöschte als „nicht erwünscht“", Help: "So viele gelöschte Gedächtnis-Einträge bekommt die KI als „nicht erwünscht“ mit.", Kind: "int", Default: "30"},

	{Key: NotifyQuestions, Group: "Benachrichtigungen", Label: "Planungen und Rückfragen der KI", Help: "Push, wenn eine Planung fertig ist oder die KI etwas wissen will.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: NotifyErrors, Group: "Benachrichtigungen", Label: "Fehler", Help: "Push, wenn eine Notiz oder Planung endgültig fehlschlägt.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: NotifySaved, Group: "Benachrichtigungen", Label: "Gespeicherte Einträge", Help: "Push für jede fertig ausgewertete Notiz.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: NotifyAppointments, Group: "Benachrichtigungen", Label: "Termin-Erinnerungen", Help: "Push zu Terminen aus deinen Notizen.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: NotifyRing, Group: "Benachrichtigungen", Label: "Ring-Nutzung", Help: "Push bei 60, 80 und 95 % der Aufnahmezeit des Rings.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: NotifyUpdates, Group: "Benachrichtigungen", Label: "Updates", Help: "Push, wenn eine neue Version bereitsteht (nur für Admins).", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},

	{Key: Telemetry, Group: "Statistik", Label: "Anonyme Statistik senden", Help: "Es werden nur anonyme Daten zur Gesamtnutzungszeit der Ringe gesendet.", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},

	{Key: "VAPID_SUBJECT", Group: "Zugang und Updates", Label: "Kontakt für Web-Push", Help: "mailto:… oder https-Adresse; leer = öffentliche Adresse.", Kind: "text"},
	{Key: "UPDATE_CHANNEL", Group: "Zugang und Updates", Label: "Update-Kanal", Help: "stable = Langzeit-Versionen; beta = neue, noch nicht ausgereifte Funktionen; nightly = jede Änderung, auch ungetestet.", Kind: "choice", Default: "stable", Choices: []string{"stable", "beta", "nightly"}},
	{Key: "AUTO_UPDATE", Group: "Zugang und Updates", Label: "Automatisches Update", Help: "Jeden Tag zur gewählten Uhrzeit die neueste Version installieren, wenn es eine gibt (vorher Backup).", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: "AUTO_UPDATE_TIME", Group: "Zugang und Updates", Label: "Uhrzeit automatisches Update", Help: "Uhrzeit (HH:MM, Zeitzone des Servers), zu der das automatische Update prüft und installiert.", Kind: "text", Default: "04:00"},
	{Key: "UPDATE_REPO", Group: "Zugang und Updates", Label: "Update-Quelle (GitHub owner/repo)", Help: "aus = keine Update-Prüfung. Auf Servern mit Updater fest in /etc/pebble-webhook/env (UPDATE_REPO).", Kind: "text", Default: "dirdante2/pebble-webhook"},

	{Key: "BACKUP_SCHEDULE", Group: "Backup", Label: "Backup-Plan", Help: "Wann das Komplett-Backup läuft (Datenbank mit Einstellungen, Version und Kanal).", Kind: "choice", Default: "täglich", Choices: []string{"täglich", "wöchentlich", "monatlich", "aus"},
		Labels: map[string]string{"täglich": "täglich", "wöchentlich": "wöchentlich (montags)", "monatlich": "monatlich (am 1.)", "aus": "aus"}},
	{Key: "BACKUP_TIME", Group: "Backup", Label: "Uhrzeit Backup", Help: "Uhrzeit (HH:MM, Zeitzone des Servers) des geplanten Backups.", Kind: "text", Default: "03:00"},
	{Key: "BACKUP_RETENTION_DAYS", Group: "Backup", Label: "Backups behalten (Tage)", Help: "Ältere geplante und manuelle Backups werden gelöscht; Backups vor Updates bleiben 30 Tage.", Kind: "int", Default: "30"},
	{Key: "BACKUP_AUDIO", Group: "Backup", Label: "Aufnahmen sichern", Help: "Die Audiodateien kommen mit ins Backup (größer).", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},
}

// Field sucht eine Einstellung; ok=false bei unbekanntem Schlüssel.
func FieldByKey(key string) (Field, bool) {
	for _, f := range Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Private: persönliche Werte, die nie vom System geerbt werden – Zugangsdaten fremder Dienste (Joplin) und
// persönliche Geheimnisse. Sonst nutzte ein Nutzer ohne eigene Werte den Zugang eines anderen.
func (f Field) Private() bool {
	return f.Personal && (f.Kind == "secret" || strings.HasPrefix(f.Key, "JOPLIN_"))
}

// EnvLocked: Geheimnisse des Systems (API-Schlüssel), die in der Umgebung gesetzt sind (Docker, env-Datei), gelten
// immer – sie gehen der Datenbank vor und sind in der App gesperrt. Persönliche Geheimnisse (Joplin) nie.
// Ebenso die Update-Quelle: der Updater läuft als root und nimmt nur das Repo aus der env-Datei (install.sh trägt
// es dort ein); in der App ist sie dann nur zu sehen.
func EnvLocked(key string) (string, bool) {
	f, ok := FieldByKey(key)
	if !ok || f.Personal || (f.Kind != "secret" && key != "UPDATE_REPO") {
		return "", false
	}
	v := strings.TrimSpace(os.Getenv(key))
	return v, v != ""
}

// WithDefaults ergänzt eine Wertequelle um die Vorgaben der Felder.
func WithDefaults(lookup func(string) string) func(string) string {
	return func(k string) string {
		if v := lookup(k); v != "" {
			return v
		}
		if f, ok := FieldByKey(k); ok {
			return f.Default
		}
		return ""
	}
}

// NewSecret erzeugt ein zufälliges Geheimnis (64 Hex-Zeichen).
func NewSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
