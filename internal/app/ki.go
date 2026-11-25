package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/openai"
)

// NewAI baut je Bereich die Reihenfolge der Einträge aus Zugängen und Bereichen: OpenAI über die Responses-API,
// Dienst und Lokal über Chat-Completions; Websuche je Eintrag (Suche des Anbieters, SearXNG-Zugang).
func NewAI(ai config.AI, ledger openai.Ledger) *openai.Router {
	r := &openai.Router{Areas: map[string][]*openai.Step{}, Ledger: ledger}
	for area, list := range ai.Areas {
		for _, e := range list {
			acc := ai.Access(e.Access)
			if acc == nil || e.Off || acc.Type() == nil { // Whisper und Pebble-Text führt die App selbst; inaktive nie
				continue
			}
			t := acc.Type()
			cl := openai.New(acc.Key, acc.URL)
			cl.Chat, cl.Plugin = t.API == config.APIChat, t.Plugin
			for _, x := range e.Search {
				if x == "anbieter" {
					cl.ProviderSearch = t.Search
				} else if sx := ai.Access(x); sx != nil {
					cl.SearchURL = sx.URL
				}
			}
			r.Areas[area] = append(r.Areas[area], &openai.Step{ID: e.ID, Name: acc.Name, Client: cl, Model: e.Model,
				PriceIn: orZero(e.PriceIn), PriceOut: orZero(e.PriceOut), PriceSearch: orZero(e.PriceSearch), PriceMinute: orZero(e.PriceMinute),
				PriceImage: orZero(e.PriceImage), Limit: e.LimitMonth})
		}
	}
	return r
}

// kiLive: was das Speichern unter KI im laufenden Dienst ändert.
type kiLive struct {
	AI           config.AI
	Images       bool // Bereich Bilder hat aktive Einträge
	WhisperModel string
	WhisperKeep  int // Minuten im RAM nach der letzten Aufnahme; 0 = dauerhaft
}

// KI: aktueller Stand von Zugängen, Bereichen und Whisper.
func (s *System) KI() *kiLive { return s.ki.Load() }

// SetKI übernimmt Zugänge, Bereiche und Whisper aus einer Konfiguration.
func (s *System) SetKI(c *config.Config) {
	s.ki.Store(&kiLive{AI: c.AI, Images: c.ImagesEnabled, WhisperModel: c.WhisperModel, WhisperKeep: c.WhisperKeepMinutes})
}

// liveKeys: Einstellungen, die das Speichern unter KI ohne Neustart übernimmt.
var liveKeys = map[string]bool{"KI_ZUGAENGE": true, "KI_BEREICHE": true, "WHISPER_MODEL": true, "WHISPER_KEEP_MINUTES": true}

// applyKI übernimmt neue KI-Einstellungen im laufenden Dienst: Router tauschen, Stand setzen und – wenn Whisper
// genutzt wird – das Modell im Hintergrund laden. Bis es da ist, warten die Transkriptionen (siehe whisperLoading);
// der Webhook nimmt weiter an.
func (s *System) applyKI(c *config.Config) {
	if r, ok := s.AI.(*openai.Router); ok {
		r.Replace(NewAI(c.AI, s.DB))
	}
	s.SetKI(c)
	s.startWhisper(s.ctx())
	for _, a := range s.Apps() {
		a.Wake()
	}
}

// aiView: Zugänge ohne Schlüssel (nur „gesetzt“) und die Bereiche, dazu die Kosten je Eintrag im laufenden Monat.
func (s *System) aiView() map[string]any {
	acc := []map[string]any{}
	ki := s.KI().AI
	for _, a := range ki.Accesses {
		acc = append(acc, map[string]any{"id": a.ID, "name": a.Name, "art": a.Kind, "adresse": a.URL, "schluessel_gesetzt": a.Key != ""})
	}
	spent := map[string]float64{}
	for _, list := range ki.Areas {
		for _, e := range list {
			spent[e.ID] = s.DB.Spent(e.ID)
		}
	}
	areas := ki.Areas
	if areas == nil {
		areas = map[string][]config.AIEntry{}
	}
	return map[string]any{"zugaenge": acc, "bereiche": areas, "verbrauch": spent, "arten": config.ProviderTypes}
}

// handleAI: GET /api/ki – Zugänge, Bereiche, Monatskosten, dazu die übrigen KI-Felder und Whisper.
func (s *System) handleAI(w http.ResponseWriter, r *http.Request) {
	values, err := s.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := s.aiView()
	out["felder"], out["whisper"] = settingsView(values, false, scopeSystem), whisperInfo(s.Whisper)
	writeJSON(w, 200, out)
}

// handleAISave: PUT /api/ki – Zugänge und Bereiche (ganz) und einzelne Felder; ein leerer Schlüssel behält den
// gespeicherten des Zugangs. Prüft wie die übrigen Einstellungen und startet den Dienst neu.
func (s *System) handleAISave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Zugaenge []config.AIAccess           `json:"zugaenge"`
		Bereiche map[string][]config.AIEntry `json:"bereiche"`
		Werte    map[string]string           `json:"werte"`
	}
	if readJSON(r, &in) != nil || in.Zugaenge == nil || in.Bereiche == nil {
		writeErr(w, 400, "ungueltig", "Werte nicht lesbar")
		return
	}
	for i, a := range in.Zugaenge {
		if a.Key == "" {
			if old := s.KI().AI.Access(a.ID); old != nil && old.Kind == a.Kind {
				in.Zugaenge[i].Key = old.Key
			}
		}
	}
	old, err := s.DB.Settings()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	merged := mergeSettings(old, in.Werte, scopeSystem)
	acc, _ := json.Marshal(in.Zugaenge)
	areas, _ := json.Marshal(in.Bereiche)
	merged["KI_ZUGAENGE"], merged["KI_BEREICHE"] = string(acc), string(areas)
	boot := config.Bootstrap{Listen: s.Cfg.Listen, DataDir: s.Cfg.DataDir, MediaDir: s.Cfg.MediaDir, BackupDir: s.Cfg.BackupDir, KeyDir: s.Cfg.KeyDir}
	cfg, err := config.FromValues(boot, func(k string) string { return merged[k] })
	if err != nil {
		writeErr(w, 400, "ungueltig", strings.TrimPrefix(err.Error(), "Konfiguration: "))
		return
	}
	if err := s.DB.SaveSettings(merged); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	// Nur KI und Whisper geändert: ohne Neustart übernehmen (der Webhook nimmt ununterbrochen an); sonst wie bisher.
	restart := false
	for k, v := range merged {
		if !liveKeys[k] && old[k] != v {
			restart = true
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "neustart": restart})
	if restart {
		restartSoon()
		return
	}
	s.applyKI(cfg)
}

// handleAICheck prüft einen gespeicherten Zugang (per ID) und liefert seine Modelle; die Seite merkt sie sich im
// Browser für die Auswahl in den Bereichen.
func (s *System) handleAICheck(w http.ResponseWriter, r *http.Request) {
	a := s.KI().AI.Access(r.PathValue("zugang"))
	var err error
	switch {
	case a == nil:
		err = i18n.Errorf("nicht gespeichert – erst speichern, dann prüfen")
	case a.Type() != nil && a.Type().API == config.APISearxng:
		err = checkSearx(r.Context(), a.URL)
	default:
		var models []string
		if models, err = listModels(r.Context(), a.URL, a.Key); err == nil {
			writeJSON(w, 200, map[string]any{"ok": true, "modelle": models})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"ok": err == nil, "fehler": errText(err)})
}

// listModels ruft GET /models ab (OpenAI-Format; Ollama und LM Studio ohne Schlüssel).
func listModels(ctx context.Context, base, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, i18n.Errorf("Antwort %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, i18n.Errorf("Modell-Liste nicht lesbar: %w", err)
	}
	models := []string{}
	for _, m := range out.Data {
		models = append(models, m.ID)
	}
	sort.Strings(models)
	return models, nil
}

// checkSearx stellt eine Probesuche an SearXNG (JSON-Ausgabe muss erlaubt sein).
func checkSearx(ctx context.Context, base string) error {
	if base == "" {
		return i18n.Errorf("nicht eingetragen")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/search?"+url.Values{"q": {"pebble"}, "format": {"json"}}.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return i18n.Errorf("SearXNG antwortet %d (JSON-Ausgabe erlaubt?)", resp.StatusCode)
	}
	var out struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return i18n.Errorf("SearXNG-Antwort nicht lesbar: %w", err)
	}
	return nil
}

// orZero: Preis eines Eintrags (lokale Einträge haben keinen).
func orZero(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// ctx: Lebensdauer des Dienstes (vor dem Start der Worker: ohne Ende).
func (s *System) ctx() context.Context {
	s.appsMu.RLock()
	defer s.appsMu.RUnlock()
	if s.runCtx != nil {
		return s.runCtx
	}
	return context.Background()
}
