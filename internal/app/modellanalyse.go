package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/store"
)

// Bewertungen der OpenRouter-Modelle (Intelligenz, Tempo, Elo, Fehlerquote …) von Artificial Analysis, bereitgestellt
// vom Empfänger der Statistik (pebble-admin, GET <TelemetryURL>/modelle). Nur solange die Installation Statistik
// sendet: der Empfänger gibt die Daten nur an Apps ab, deren App-ID er aus Statistik-Berichten kennt. Anmeldung:
// `Authorization: PebbleApp <kennung>.<zeit>.<signatur>` – kennung = hex(SHA-256("pebble-modelle:" + App-ID)),
// signatur = hex(HMAC-SHA-256(Schlüssel App-ID, "pebble-modelle\n" + Unix-Sekunden)); die App-ID selbst geht nie mit.
// Ablauf:
// 1. Seite KI (GET /api/ki, nur mit OpenRouter-Zugang und Statistik an) → liegt ein Stand jünger als 24 h in kv, gilt der.
// 2. Sonst Abruf (höchstens einmal je Stunde, auch Fehlversuche zählen); scheitert er, bleibt der alte Stand.
// 3. Ablage in kv: je OpenRouter-ID und Art (text, entscheidung, bild, transkription) die Werte, dazu Quelle und Stand.
// 4. Die Seiten der Bereiche zeigen die Werte an den Modellen des Katalogs; fehlende Werte = nicht gemessen.
// Statistik aus (oder im Build nicht vorhanden) → kein Abruf und keine Anzeige.

const (
	modelAnalysisKey    = "modell-analyse"
	modelAnalysisTryKey = "modell-analyse-versuch"
	modelAnalysisMaxAge = 24 * time.Hour
	modelAnalysisRetry  = time.Hour
)

// modelAnalysis: Werte je OpenRouter-ID und Art, wie der Empfänger sie liefert (Schlüssel siehe admin/modelle.go).
type modelAnalysis struct {
	Source    string                                           `json:"quelle"`
	SourceURL string                                           `json:"quelle_url"`
	Fetched   time.Time                                        `json:"stand"` // Stand der Quelle
	Got       time.Time                                        `json:"abgerufen"`
	Models    map[string]map[string]map[string]json.RawMessage `json:"modelle"`
}

// modelsAuth: Anmeldung beim Empfänger (siehe oben).
func modelsAuth(appID string, ts int64) string {
	k := sha256.Sum256([]byte("pebble-modelle:" + appID))
	m := hmac.New(sha256.New, []byte(appID))
	m.Write([]byte("pebble-modelle\n" + strconv.FormatInt(ts, 10)))
	return "PebbleApp " + hex.EncodeToString(k[:]) + "." + strconv.FormatInt(ts, 10) + "." + hex.EncodeToString(m.Sum(nil))
}

func loadModelAnalysis(db *store.System) *modelAnalysis {
	raw, _ := db.Get(modelAnalysisKey)
	if raw == "" {
		return nil
	}
	var a modelAnalysis
	if json.Unmarshal([]byte(raw), &a) != nil || len(a.Models) == 0 {
		return nil
	}
	return &a
}

// ensureModelAnalysis: gespeicherter Stand, bei Bedarf vorher neu abgerufen (nil = keiner da oder Statistik aus).
func (s *System) ensureModelAnalysis(ctx context.Context) *modelAnalysis {
	if !telemetryAvailable() || !s.telemetryOn() {
		return nil
	}
	appID, err := s.appID()
	if err != nil || appID == "" {
		return nil
	}
	db := s.DB
	a := loadModelAnalysis(db)
	if a != nil && time.Since(a.Got) < modelAnalysisMaxAge {
		return a
	}
	if last, _ := db.Get(modelAnalysisTryKey); last != "" {
		if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < modelAnalysisRetry {
			return a
		}
	}
	db.Set(modelAnalysisTryKey, time.Now().UTC().Format(time.RFC3339))
	n, err := fetchModelAnalysis(ctx, s.Version, appID)
	if err != nil {
		logf("KI: Modell-Bewertungen nicht abrufbar: %v", err)
		return a
	}
	raw, _ := json.Marshal(n)
	if err := db.Set(modelAnalysisKey, string(raw)); err != nil {
		logf("KI: Modell-Bewertungen nicht gespeichert: %v", err)
	}
	return n
}

func fetchModelAnalysis(ctx context.Context, version, appID string) (*modelAnalysis, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TelemetryURL+"/modelle", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", modelsAuth(appID, time.Now().Unix()))
	req.Header.Set("User-Agent", "pebble-webhook/"+shortVersion(version))
	resp, err := telemetryHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var in struct {
		Source    string    `json:"quelle"`
		SourceURL string    `json:"quelle_url"`
		Fetched   time.Time `json:"stand"`
		Models    []struct {
			Kind         string                     `json:"art"`
			OpenRouterID string                     `json:"openrouter_id"`
			Values       map[string]json.RawMessage `json:"werte"`
		} `json:"modelle"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&in); err != nil {
		return nil, err
	}
	out := &modelAnalysis{Source: in.Source, SourceURL: in.SourceURL, Fetched: in.Fetched, Got: time.Now().UTC(),
		Models: map[string]map[string]map[string]json.RawMessage{}}
	for _, m := range in.Models {
		id, _, _ := strings.Cut(m.OpenRouterID, ":") // Varianten (:free …) zählen zum Grundmodell
		if id == "" || m.Kind == "" || len(m.Values) == 0 {
			continue
		}
		if out.Models[id] == nil {
			out.Models[id] = map[string]map[string]json.RawMessage{}
		}
		if _, da := out.Models[id][m.Kind]; !da { // die Quelle sortiert je Art bestes zuerst
			out.Models[id][m.Kind] = m.Values
		}
	}
	if len(out.Models) == 0 {
		return nil, fmt.Errorf("keine zugeordneten Modelle")
	}
	return out, nil
}
