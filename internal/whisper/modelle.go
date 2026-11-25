// Package whisper: lokale Transkription mit whisper.cpp. Die Engine (whisper-cli, ffmpeg) steckt im Binary,
// nachgeladen wird nur das Sprachmodell (DATA_DIR/modelle/whisper).
package whisper

// Model: ein Sprachmodell zum Nachladen. RAM und Tempo sind Richtwerte, gemessen auf 4 Kernen (Zen 4, AVX-512);
// die App ersetzt das Tempo nach den ersten eigenen Läufen durch den gemessenen Wert.
type Model struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	File    string  `json:"datei"`
	SHA256  string  `json:"-"`
	Bytes   int64   `json:"bytes"`
	RAMMB   int     `json:"ram_mb"`  // Spitze beim Umwandeln
	Cores   int     `json:"kerne"`   // empfohlen
	Factor  float64 `json:"faktor"`  // Sekunden Rechenzeit je Sekunde Aufnahme (4 Kerne)
	Quality string  `json:"deutsch"` // Güte bei deutscher Sprache
}

// Models in der Reihenfolge der Auswahl (klein → groß).
var Models = []Model{
	{ID: "base", Name: "base", File: "ggml-base-q5_1.bin", SHA256: "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898",
		Bytes: 59707625, RAMMB: 250, Cores: 2, Factor: 0.08, Quality: "mäßig"},
	{ID: "small", Name: "small", File: "ggml-small-q5_1.bin", SHA256: "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb",
		Bytes: 190085487, RAMMB: 520, Cores: 2, Factor: 0.18, Quality: "gut"},
	{ID: "medium", Name: "medium", File: "ggml-medium-q5_0.bin", SHA256: "19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f",
		Bytes: 539212467, RAMMB: 1200, Cores: 4, Factor: 0.53, Quality: "sehr gut"},
	{ID: "turbo", Name: "large-v3-turbo", File: "ggml-large-v3-turbo-q5_0.bin", SHA256: "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2",
		Bytes: 574041195, RAMMB: 900, Cores: 4, Factor: 0.64, Quality: "am besten"},
}

// DefaultModel: Vorgabe der Einstellung WHISPER_MODEL.
const DefaultModel = "small"

// IDs: Auswahl für die Einstellung.
func IDs() []string {
	ids := make([]string, len(Models))
	for i, m := range Models {
		ids[i] = m.ID
	}
	return ids
}

// ByID sucht ein Modell; ok=false bei unbekannter Id.
func ByID(id string) (Model, bool) {
	for _, m := range Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

const downloadBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
