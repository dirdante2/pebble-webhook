package config

import "slices"

// Anbieter-Arten: was ein Zugang kann und wie er angesprochen wird. Neue Anbieter (z. B. OpenRouter, Groq, Gemini,
// Ollama) kommen als weiterer Eintrag in ProviderTypes dazu – mit fester oder vorgeschlagener Adresse, Schlüsselpflicht,
// Bereichen, eigener Websuche und optional einer festen Modellauswahl samt Preisen. Oberfläche, Prüfung und Router
// lesen alles von hier.

// Schnittstellen.
const (
	APIResponses = "responses" // OpenAI Responses-API (Websuche, Bilder, Transkription)
	APIChat      = "chat"      // OpenAI-kompatible Chat-Completions (Transkription über /audio/transcriptions)
	APISearxng   = "searxng"   // Suchmaschine, kein Modell
)

// Schlüssel eines Zugangs.
const (
	KeyRequired = "pflicht"
	KeyOptional = "optional"
	KeyNone     = "kein"
)

// CatalogModel: ein Modell der festen Auswahl mit ungefähren Preisen (USD; Tokens je 1 Mio.).
type CatalogModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	PriceIn     float64 `json:"preis_ein,omitempty"`
	PriceOut    float64 `json:"preis_aus,omitempty"`
	PriceMinute float64 `json:"preis_minute,omitempty"`
	PriceSearch float64 `json:"preis_suche,omitempty"`
	PriceImage  float64 `json:"preis_bild,omitempty"`
}

// ProviderType: eine Anbieter-Art.
type ProviderType struct {
	ID       string                    `json:"id"`
	Name     string                    `json:"name"`
	Text     string                    `json:"text"`              // kurz, was er ist
	API      string                    `json:"api"`               // APIResponses | APIChat | APISearxng
	URL      string                    `json:"adresse"`           // Vorgabe der Adresse (leer = einzutragen)
	FixedURL bool                      `json:"adresse_fest"`      // Adresse steht fest, nicht änderbar
	Key      string                    `json:"schluessel"`        // KeyRequired | KeyOptional | KeyNone
	Cloud    bool                      `json:"cloud"`             // kostet: Preise und Monatslimit je Eintrag
	Areas    []string                  `json:"bereiche"`          // Bereiche, die er bedienen kann
	Search   bool                      `json:"suche"`             // eigene Websuche („Suche des Anbieters“)
	Plugin   bool                      `json:"-"`                 // Websuche als OpenRouter-Web-Plugin
	Models   map[string][]CatalogModel `json:"modelle,omitempty"` // feste Auswahl je Bereich (erstes = Vorgabe); leer = frei
}

// HasModel: Modell steht in der festen Auswahl des Bereichs.
func (t *ProviderType) HasModel(area, id string) bool {
	return slices.ContainsFunc(t.Models[area], func(m CatalogModel) bool { return m.ID == id })
}

var textAreas = []string{AreaTitle, AreaDictation, AreaResearch}

// ProviderTypes: alle Anbieter-Arten in der Reihenfolge der Auswahl. Modelle und Preise: Stand 11/2026, gerundet;
// fällt ein Modell weg, ersetzt normalize gespeicherte Einträge durch die Vorgabe.
var ProviderTypes = func() []ProviderType {
	text := []CatalogModel{
		{ID: "gpt-6-luna", Name: "GPT-6 Luna (klein, schnell)", PriceIn: 0.10, PriceOut: 0.50, PriceSearch: 0.01},
		{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra (mittel)", PriceIn: 1, PriceOut: 5, PriceSearch: 0.01},
		{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol (groß, gründlich)", PriceIn: 2, PriceOut: 10, PriceSearch: 0.01},
	}
	big := []CatalogModel{text[2], text[1], text[0]}
	return []ProviderType{
		{ID: "openai", Name: "OpenAI", Text: "OpenAI mit eigener Websuche und Bildern.", API: APIResponses, URL: "https://api.openai.com/v1",
			Key: KeyRequired, Cloud: true, Areas: []string{AreaTranscribe, AreaTitle, AreaDictation, AreaResearch, AreaImage}, Search: true,
			Models: map[string][]CatalogModel{
				AreaTranscribe: {
					{ID: "gpt-transcribe", Name: "GPT Transcribe", PriceMinute: 0.0045},
					{ID: "gpt-4o-transcribe", Name: "GPT-4o Transcribe", PriceMinute: 0.006},
					{ID: "gpt-4o-mini-transcribe", Name: "GPT-4o mini Transcribe (günstig)", PriceMinute: 0.003},
				},
				AreaTitle: text, AreaDictation: big, AreaResearch: big,
				AreaImage: {
					{ID: "gpt-image-1-mini", Name: "GPT Image 1 mini (günstig)", PriceImage: 0.01},
					{ID: "gpt-image-2", Name: "GPT Image 2", PriceImage: 0.02},
					{ID: "gpt-image-1.5", Name: "GPT Image 1.5", PriceImage: 0.04},
				},
			}},
		{ID: "dienst", Name: "Dienst (OpenAI-kompatibel)", Text: "Beliebiger OpenAI-kompatibler Dienst, z. B. OpenRouter, Groq, Gemini, Mistral, Perplexity.",
			API: APIChat, Key: KeyOptional, Cloud: true, Areas: append([]string{AreaTranscribe}, textAreas...), Search: true},
		{ID: "lokal", Name: "Lokal (Ollama, LM Studio)", Text: "Eigenes Modell im Netz, ohne Kosten; sucht nicht selbst im Web.",
			API: APIChat, URL: "", Key: KeyNone, Areas: textAreas},
		{ID: "searxng", Name: "SearXNG (Websuche)", Text: "Eigene Suchmaschine für die Websuche, auch für lokale Modelle.",
			API: APISearxng, Key: KeyNone},
	}
}()

// ProviderTypeByID liefert die Art zu einer ID (nil = unbekannt).
func ProviderTypeByID(id string) *ProviderType {
	for i := range ProviderTypes {
		if ProviderTypes[i].ID == id {
			return &ProviderTypes[i]
		}
	}
	return nil
}
