package config

import (
	"crypto/rand"
	"encoding/hex"
)

// Field beschreibt eine Einstellung, die in der Datenbank liegt und über die Web-Oberfläche gepflegt wird
// (Einrichtungsassistent, Status → Einstellungen). Default füllt das Formular vor und gilt, wenn nichts gesetzt ist.
type Field struct {
	Key      string   `json:"key"`
	Group    string   `json:"gruppe"`
	Label    string   `json:"name"`
	Help     string   `json:"hilfe,omitempty"`
	Kind     string   `json:"art"` // text | secret | int | num | choice | url
	Default  string   `json:"vorgabe"`
	Required bool     `json:"pflicht,omitempty"`
	Choices  []string `json:"auswahl,omitempty"`
	Generate bool     `json:"erzeugen,omitempty"` // Geheimnis, das der Assistent selbst erzeugt
}

// Fields: alle Einstellungen außer den Start-Werten (LISTEN, DATA_DIR, AUDIO_DIR, BACKUP_DIR).
var Fields = []Field{
	{Key: "LANGUAGE", Group: "Sprache", Label: "Sprache", Help: "Sprache der Web-App, der Meldungen und der Texte, die die KI schreibt; auch die Transkription richtet sich danach. Bestehende Einträge bleiben, wie sie sind.", Kind: "choice", Default: "de", Choices: []string{"de", "en"}},

	{Key: "PUBLIC_URL", Group: "Adresse", Label: "Öffentliche Adresse", Help: "https-Adresse der Web-App ohne Slash am Ende; bestimmt die Passkey-Domain. Später ändern macht alle Passkeys ungültig.", Kind: "url", Required: true},
	{Key: "TRUSTED_PROXY", Group: "Adresse", Label: "Reverse-Proxy", Help: "IP des Proxys davor; nur von dort wird X-Real-IP geglaubt. Leer = keiner.", Kind: "text"},

	{Key: "HOOK_TOKEN_HEADER", Group: "Pebble-App", Label: "Header für den Token", Help: "Name des Headers, in dem die Pebble-App das Webhook-Token mitschickt.", Kind: "text", Default: "X-Webhook-Token", Required: true},
	{Key: "HOOK_TOKEN", Group: "Pebble-App", Label: "Webhook-Token", Help: "Wird in der Pebble-App als eigener Header eingetragen.", Kind: "secret", Required: true, Generate: true},
	{Key: "HOOK_SIGNING_SECRET", Group: "Pebble-App", Label: "Signatur-Secret", Help: "Signatur der Pebble-App; nicht gleich dem Token.", Kind: "secret", Required: true, Generate: true},
	{Key: "TRIGGER_SINGLE", Group: "Pebble-App", Label: "Geste „Halten“", Help: "Was eine Aufnahme mit „Halten“ auslöst: diktat = Notiz, planung = Recherche mit Antwort.", Kind: "choice", Default: "diktat", Choices: []string{"diktat", "planung"}},
	{Key: "TRIGGER_DOUBLE", Group: "Pebble-App", Label: "Geste „Klick + Halten“", Help: "Was eine Aufnahme mit „Klick + Halten“ auslöst: diktat = Notiz, planung = Recherche mit Antwort.", Kind: "choice", Default: "planung", Choices: []string{"diktat", "planung"}},
	{Key: "HOOK_MAX_MB", Group: "Pebble-App", Label: "Größte Aufnahme (MB)", Help: "Größere Aufnahmen weist der Webhook ab.", Kind: "int", Default: "25"},
	{Key: "HOOK_RATE_PER_MIN", Group: "Pebble-App", Label: "Aufrufe je Minute", Help: "Höchstens so viele Webhook-Aufrufe je Minute; mehr werden abgewiesen (Schutz vor Missbrauch).", Kind: "int", Default: "30"},

	{Key: "OPENAI_API_KEY", Group: "OpenAI", Label: "API-Schlüssel", Help: "Schlüssel für Transkription, Auswertung und Recherche bei OpenAI.", Kind: "secret", Required: true},
	{Key: "OPENAI_BASE_URL", Group: "OpenAI", Label: "API-Adresse", Help: "Adresse der OpenAI-API; nur für einen kompatiblen anderen Anbieter ändern.", Kind: "url", Default: "https://api.openai.com/v1"},
	{Key: "TRANSCRIPTION", Group: "OpenAI", Label: "Transkription", Help: "openai = Audio bei OpenAI transkribieren; pebble = Text der Pebble-App übernehmen.", Kind: "choice", Default: "openai", Choices: []string{"openai", "pebble"}},
	{Key: "OPENAI_TRANSCRIBE_MODEL", Group: "OpenAI", Label: "Modell Transkription", Help: "Modell, das die Aufnahme in Text umwandelt.", Kind: "text", Default: "gpt-transcribe"},
	{Key: "OPENAI_TITLE_MODEL", Group: "OpenAI", Label: "Modell Kurztitel und Zuordnung", Help: "Günstiges Modell für Kurztitel und die Zuordnung zu Listen.", Kind: "text", Default: "gpt-6-luna", Required: true},
	{Key: "OPENAI_PLAN_MODEL", Group: "OpenAI", Label: "Modell Recherche", Help: "Modell für Recherchen (Responses-API mit Websuche).", Kind: "text", Default: "gpt-6-luna", Required: true},
	{Key: "OPENAI_DICTATION_MODEL", Group: "OpenAI", Label: "Modell Notizen", Help: "Modell für die Auswertung von Notizen. Leer = Modell der Recherche.", Kind: "text"},
	{Key: "OPENAI_PLAN_REASONING_EFFORT", Group: "OpenAI", Label: "Denkaufwand Recherche", Help: "Wie gründlich das Modell bei Recherchen nachdenkt; höher = besser, aber teurer. standard = Vorgabe des Modells.", Kind: "choice", Default: "low", Choices: []string{"standard", "none", "low", "medium", "high"}},
	{Key: "RESEARCH_LIMIT_USD", Group: "OpenAI", Label: "Kostenlimit je Recherche (USD)", Help: "Eine Recherche bricht ab, sobald ihre Kosten diesen Betrag erreichen.", Kind: "num", Default: "0.50", Required: true},
	{Key: "RESEARCH_MAX_OUTPUT_TOKENS", Group: "OpenAI", Label: "Höchstens Ausgabe-Tokens", Help: "Obergrenze für die Länge einer Recherche-Antwort.", Kind: "int", Default: "6000"},

	{Key: "PRICE_PLAN_INPUT_PER_MTOK", Group: "Preise", Label: "Recherche: Eingabe (USD je 1 Mio. Tokens)", Help: "Fehlt ein Recherche-Preis, läuft keine Recherche.", Kind: "num", Default: "0.10"},
	{Key: "PRICE_PLAN_OUTPUT_PER_MTOK", Group: "Preise", Label: "Recherche: Ausgabe (USD je 1 Mio. Tokens)", Help: "Preis für erzeugte Tokens der Recherche; zählt für das Kostenlimit und die KI-Kosten.", Kind: "num", Default: "0.50"},
	{Key: "PRICE_SEARCH_PER_CALL", Group: "Preise", Label: "Websuche (USD je Aufruf)", Help: "Preis je Websuche der Recherche.", Kind: "num", Default: "0.01"},
	{Key: "PRICE_TITLE_INPUT_PER_MTOK", Group: "Preise", Label: "Kurztitel: Eingabe (USD je 1 Mio. Tokens)", Help: "Preis für gelesene Tokens beim Kurztitel und der Zuordnung.", Kind: "num", Default: "0.10"},
	{Key: "PRICE_TITLE_OUTPUT_PER_MTOK", Group: "Preise", Label: "Kurztitel: Ausgabe (USD je 1 Mio. Tokens)", Help: "Preis für erzeugte Tokens beim Kurztitel und der Zuordnung.", Kind: "num", Default: "0.50"},
	{Key: "PRICE_TRANSCRIBE_PER_MIN", Group: "Preise", Label: "Transkription (USD je Minute)", Help: "Preis je Minute Audio bei der Transkription.", Kind: "num", Default: "0.0045"},

	{Key: "JOPLIN_ENABLED", Group: "Joplin", Label: "Joplin", Help: "aktiv = fertige Notizen, Planungen und Listen nach Joplin übertragen; eingerichtet und getestet wird unter Status → Joplin.", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},
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
	{Key: "HISTORY_MIN_ENTRIES", Group: "Anzeige und Gedächtnis", Label: "Verlauf: mindestens Einträge", Help: "Der Verlauf zeigt die letzten 24 Stunden, aber mindestens so viele Einträge je Filter.", Kind: "int", Default: "10"},
	{Key: "MEMORY_TRAIN_ENTRIES", Group: "Anzeige und Gedächtnis", Label: "Lernlauf: jüngste Einträge", Help: "So viele der jüngsten Einträge wertet der Lernlauf des Gedächtnisses aus.", Kind: "int", Default: "30"},
	{Key: "MEMORY_REJECTED", Group: "Anzeige und Gedächtnis", Label: "Gedächtnis: gelöschte als „nicht erwünscht“", Help: "So viele gelöschte Gedächtnis-Einträge bekommt die KI als „nicht erwünscht“ mit.", Kind: "int", Default: "30"},

	{Key: "API_TOKEN", Group: "Zugang und Updates", Label: "Maschinenzugang (API-Token)", Help: "Authorization: Bearer; nur Lesen, Einsortieren, Abhaken. Leer = aus, sonst mind. 32 Zeichen.", Kind: "secret"},
	{Key: "VAPID_SUBJECT", Group: "Zugang und Updates", Label: "Kontakt für Web-Push", Help: "mailto:… oder https-Adresse; leer = öffentliche Adresse.", Kind: "text"},
	{Key: "UPDATE_CHANNEL", Group: "Zugang und Updates", Label: "Update-Kanal", Help: "stable = Langzeit-Versionen; beta = neue, noch nicht ausgereifte Funktionen; nightly = jede Änderung, auch ungetestet.", Kind: "choice", Default: "stable", Choices: []string{"stable", "beta", "nightly"}},
	{Key: "AUTO_UPDATE", Group: "Zugang und Updates", Label: "Automatisches Update", Help: "aktiv = jeden Tag zur gewählten Uhrzeit die neueste Version des Update-Kanals installieren, wenn es eine neuere gibt (vorher Backup).", Kind: "choice", Default: "aktiv", Choices: []string{"aktiv", "inaktiv"}},
	{Key: "AUTO_UPDATE_TIME", Group: "Zugang und Updates", Label: "Uhrzeit automatisches Update", Help: "Uhrzeit (HH:MM, Zeitzone des Servers), zu der das automatische Update prüft und installiert.", Kind: "text", Default: "04:00"},
	{Key: "UPDATE_REPO", Group: "Zugang und Updates", Label: "Update-Quelle (GitHub owner/repo)", Help: "aus = keine Update-Prüfung.", Kind: "text", Default: "dirdante2/pebble-webhook"},

	{Key: "BACKUP_SCHEDULE", Group: "Backup", Label: "Backup-Plan", Help: "Wann das Komplett-Backup läuft (Datenbank mit Einstellungen, Version und Kanal): täglich, wöchentlich (montags), monatlich (am 1.) oder aus.", Kind: "choice", Default: "täglich", Choices: []string{"täglich", "wöchentlich", "monatlich", "aus"}},
	{Key: "BACKUP_TIME", Group: "Backup", Label: "Uhrzeit Backup", Help: "Uhrzeit (HH:MM, Zeitzone des Servers) des geplanten Backups.", Kind: "text", Default: "03:00"},
	{Key: "BACKUP_RETENTION_DAYS", Group: "Backup", Label: "Backups behalten (Tage)", Help: "Ältere geplante und manuelle Backups werden gelöscht; Backups vor Updates bleiben 30 Tage.", Kind: "int", Default: "30"},
	{Key: "BACKUP_AUDIO", Group: "Backup", Label: "Aufnahmen sichern", Help: "aktiv = die Audiodateien kommen mit ins Backup (größer).", Kind: "choice", Default: "inaktiv", Choices: []string{"aktiv", "inaktiv"}},
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
