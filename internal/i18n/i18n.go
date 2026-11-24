// Package i18n: Sprache der Oberfläche und der KI-Texte (Deutsch oder Englisch, Einstellung LANGUAGE).
// Im Code steht Deutsch; en.json bildet deutsche Texte (auch Formatvorlagen mit %s/%d und {0}) auf
// englische ab. Dasselbe Wörterbuch geht für die Web-App in index.html mit.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed en.json
var raw []byte

var en map[string]string

var lang atomic.Value // "de" | "en"

func init() {
	if err := json.Unmarshal(raw, &en); err != nil {
		panic("i18n: en.json: " + err.Error())
	}
	lang.Store("de")
}

// Norm macht aus einer Angabe ("en", "en-US", "EN") eine bekannte Sprache; alles andere ist Deutsch.
func Norm(l string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "en") {
		return "en"
	}
	return "de"
}

// Set legt die Sprache des Dienstes fest (beim Start aus der Einstellung LANGUAGE).
func Set(l string) { lang.Store(Norm(l)) }

// Lang liefert "de" oder "en".
func Lang() string { return lang.Load().(string) }

// T übersetzt einen Text in die eingestellte Sprache; unbekannte Texte bleiben, wie sie sind.
func T(s string) string { return In(Lang(), s) }

// In übersetzt in eine bestimmte Sprache (Einrichtungsassistent, je Anfrage).
func In(l, s string) string {
	if l != "en" {
		return s
	}
	k := strings.TrimSpace(s)
	if v, ok := en[k]; ok {
		return strings.Replace(s, k, v, 1)
	}
	return s
}

// Tf übersetzt eine Formatvorlage und füllt sie.
func Tf(format string, a ...any) string { return fmt.Sprintf(T(format), a...) }

// Errorf wie fmt.Errorf, mit übersetzter Vorlage (%w bleibt erhalten).
func Errorf(format string, a ...any) error { return fmt.Errorf(T(format), a...) }

// Dictionary liefert das Wörterbuch für die Web-App (leer bei Deutsch).
func Dictionary(l string) []byte {
	if l != "en" {
		return []byte("{}")
	}
	return raw
}

var weekdaysDE = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}

// Weekday: Name des Wochentags in der eingestellten Sprache.
func Weekday(d time.Weekday) string {
	if Lang() == "en" {
		return d.String()
	}
	return weekdaysDE[d]
}

// Date: Datum im Format der Sprache (31.12.2026 bzw. 31/12/2026).
func Date(t time.Time) string {
	if Lang() == "en" {
		return t.Format("02/01/2006")
	}
	return t.Format("02.01.2006")
}

// DayMonth: Tag und Monat (31.12. bzw. 31/12).
func DayMonth(t time.Time) string {
	if Lang() == "en" {
		return t.Format("02/01")
	}
	return t.Format("02.01.")
}

// DateTime: Datum mit Uhrzeit (31.12.2026, 14:05 bzw. 31/12/2026, 14:05).
func DateTime(t time.Time) string { return Date(t) + ", " + t.Format("15:04") }

// LanguageRule: Zusatz für jede KI-Anweisung bei Englisch (leer bei Deutsch – die Anweisungen sind deutsch).
func LanguageRule() string { return LanguageRuleIn(Lang()) }

// LanguageRuleIn: wie LanguageRule für eine bestimmte Sprache (je Nutzer).
func LanguageRuleIn(l string) string {
	if Norm(l) != "en" {
		return ""
	}
	return "Sprache: Alle Texte, die du erzeugst (Titel, Notiz, Hinweise, Tags, Listen- und Gruppennamen, Beschreibungen, Fragen, Ergebnisse, Tipps), schreibst du auf Englisch – auch wenn Notiz, Gedächtnis, Listen oder Beispiele deutsch sind. Vorhandene Listen-, Gruppen- und Tag-Namen übernimmst du exakt wie geschrieben. Schlüssel und feste Werte des JSON-Formats bleiben unverändert."
}

// TranscribeLanguage: Sprachhinweis für die Transkription (ISO 639-1).
func TranscribeLanguage() string { return Lang() }
