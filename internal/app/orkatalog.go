package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/store"
)

// Katalog für die Modellwahl bei OpenRouter: alle passenden Modelle je Bereich mit Preisen, Kontext und Datenschutz
// (Zero Data Retention), nur aus Angaben von OpenRouter – keine Modellnamen oder Einschätzungen im Code. Die Seite
// sucht und filtert offline in diesem Katalog. Ablauf:
// 1. OpenRouter verbunden (Einrichtung oder Status → KI) bzw. Zugang angelegt → Abruf, Ablage in kv.
// 2. Fehlt der Katalog (z. B. Abruf gescheitert), ruft der Dienst beim Start bzw. auf der Seite KI höchstens alle 24 h neu ab.
// 3. „Prüfen“ am OpenRouter-Zugang ruft ausdrücklich neu ab.
// Quelle: öffentliche API ohne Schlüssel – /models (Preise, Fähigkeiten), /endpoints/zdr (ZDR-Endpunkte) und für
// Transkription und Entscheidung /models/<id>/endpoints (ZDR nur, wenn alle Endpunkte ZDR sind; dort lässt sich ZDR
// nicht je Anfrage erzwingen). Passend: nicht älter als ein Jahr, mit Preis, keine Varianten (:free, :batch), Aliase (~) oder Router;
// Text mit JSON-Schema (structured_outputs), Transkription mit Preis je Sekunde, Bilder mit Preis je Bild-Token,
// Entscheidungsmodelle (Decisions-API) mit Preis je Eingabe-Token.

const (
	orCatalogKey     = "openrouter-katalog"
	orCatalogTryKey  = "openrouter-katalog-versuch"
	orCatalogBase    = "https://openrouter.ai/api/v1"
	orCatalogBackoff = 24 * time.Hour
	orMaxAge         = 365 * 24 * time.Hour
	orCatalogVersion = 2 // erhöhen, wenn neue Felder dazukommen: ein älterer Katalog wird sofort neu abgerufen
)

// orModel: ein passendes Modell (USD; Tokens je 1 Mio., Transkription je Minute, Bilder je 1 Mio. Bild-Tokens).
type orModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	PriceIn     float64 `json:"preis_ein,omitempty"`
	PriceOut    float64 `json:"preis_aus,omitempty"`
	PriceMinute float64 `json:"preis_minute,omitempty"`
	PriceImageM float64 `json:"preis_bild_mio,omitempty"`
	Context     int     `json:"kontext,omitempty"`
	Reasoning   bool    `json:"denkt,omitempty"` // kann nachdenken (Parameter reasoning)
	ZDR         bool    `json:"zdr"`
}

// cost: Vergleichswert für die Reihenfolge (innerhalb eines Bereichs gleiche Einheit).
func (m orModel) cost() float64 { return m.PriceIn + m.PriceOut + m.PriceMinute + m.PriceImageM }

type orCatalog struct {
	Version int                  `json:"version"`
	Time    time.Time            `json:"zeit"`
	Areas   map[string][]orModel `json:"bereiche"` // je Bereich nach Preis aufsteigend
}

// orSpread: bis zu n Modelle gleichmäßig über die Preisspanne (günstigstes bis teuerstes); die Seite rechnet genauso.
func orSpread(list []orModel, n int) []orModel {
	if len(list) <= n {
		return list
	}
	var out []orModel
	for i := 0; i < n; i++ {
		k := (i*(len(list)-1) + (n-1)/2) / (n - 1)
		out = append(out, list[k])
	}
	return out
}

// defaultModel: Vorgabe beim Verbinden – das mittlere der gestreuten Modelle des Bereichs ("" = kein Katalog).
func (c *orCatalog) defaultModel(area string) string {
	if c == nil {
		return ""
	}
	s := orSpread(c.Areas[area], 5)
	if len(s) == 0 {
		return ""
	}
	return s[len(s)/2].ID
}

// defaults: Vorgabe je Bereich (für den Einrichtungsassistenten).
func (c *orCatalog) defaults() map[string]string {
	out := map[string]string{}
	for area := range c.areasOrNil() {
		if id := c.defaultModel(area); id != "" {
			out[area] = id
		}
	}
	return out
}

func (c *orCatalog) areasOrNil() map[string][]orModel {
	if c == nil {
		return nil
	}
	return c.Areas
}

// loadORCatalog: gespeicherter Katalog; old = es liegt einer in älterer Form vor (dann nil).
func loadORCatalog(db *store.System) (c *orCatalog, old bool) {
	raw, _ := db.Get(orCatalogKey)
	if raw == "" {
		return nil, false
	}
	var x orCatalog
	if json.Unmarshal([]byte(raw), &x) != nil || len(x.Areas) == 0 || x.Version != orCatalogVersion {
		return nil, true
	}
	return &x, false
}

// ensureORCatalog: ruft ab, wenn force oder kein Katalog da ist – ohne force höchstens alle 24 h.
func ensureORCatalog(ctx context.Context, db *store.System, force bool) *orCatalog {
	c, old := loadORCatalog(db)
	if !force && !old {
		if c != nil {
			return c
		}
		if last, _ := db.Get(orCatalogTryKey); last != "" {
			if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < orCatalogBackoff {
				return nil
			}
		}
	}
	db.Set(orCatalogTryKey, time.Now().UTC().Format(time.RFC3339))
	n, err := fetchORCatalog(ctx)
	if err != nil {
		logf("KI: OpenRouter-Katalog nicht abrufbar: %v", err)
		return c
	}
	raw, _ := json.Marshal(n)
	if err := db.Set(orCatalogKey, string(raw)); err != nil {
		logf("KI: OpenRouter-Katalog nicht gespeichert: %v", err)
	}
	logf("KI: OpenRouter-Katalog abgerufen (%d Text-, %d Transkriptions-, %d Bild-, %d Entscheidungsmodelle)",
		len(n.Areas[config.AreaTitle]), len(n.Areas[config.AreaTranscribe]), len(n.Areas[config.AreaImage]), len(n.Areas[config.AreaDecision]))
	return n
}

// hasOpenRouter: ein OpenRouter-Zugang ist eingerichtet.
func (s *System) hasOpenRouter() bool {
	return slices.ContainsFunc(s.KI().AI.Accesses, func(a config.AIAccess) bool { return a.Kind == "openrouter" })
}

// refreshORCatalogSoon: Abruf im Hintergrund.
func (s *System) refreshORCatalogSoon(force bool) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		ensureORCatalog(ctx, s.DB, force)
	}()
}

func orGet(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", orCatalogBase+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// orPricing: Preise eines Modells; Werte meist als Text, manche als Liste (Staffeln) – die zählen hier nicht.
type orPricing map[string]any

func (p orPricing) f(k string) float64 {
	t, _ := p[k].(string)
	v, _ := strconv.ParseFloat(t, 64)
	return v
}

// round: auf 2 gültige Stellen.
func round(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'g', 2, 64), 64)
	return f
}

type orRawModel struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Created      int64     `json:"created"`
	Context      int       `json:"context_length"`
	Pricing      orPricing `json:"pricing"`
	Params       []string  `json:"supported_parameters"`
	Architecture struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
}

// orArea: Bereich eines Modells laut seinen Fähigkeiten ("" = passt nirgends) und seine Preise.
func orArea(m orRawModel, now time.Time) (string, orModel) {
	out := orModel{ID: m.ID, Name: m.Name}
	if strings.ContainsAny(m.ID, ":~") || strings.HasPrefix(m.ID, "openrouter/") || now.Sub(time.Unix(m.Created, 0)) > orMaxAge {
		return "", out
	}
	in, op, p := m.Architecture.Input, m.Architecture.Output, m.Pricing
	switch {
	case slices.Equal(op, []string{"text"}) && slices.Contains(in, "text") && slices.Contains(m.Params, "structured_outputs") &&
		p.f("prompt") > 0 && p.f("completion") > 0:
		out.PriceIn, out.PriceOut, out.Context = round(p.f("prompt")*1e6), round(p.f("completion")*1e6), m.Context
		out.Reasoning = slices.Contains(m.Params, "reasoning")
		return config.AreaTitle, out
	case slices.Contains(op, "transcription") && p.f("prompt") > 0 && p.f("completion") == 0:
		out.PriceMinute = round(p.f("prompt") * 60) // Transkription ohne Ausgabepreis: prompt = USD je Sekunde Audio
		return config.AreaTranscribe, out
	case slices.Contains(op, "image") && p.f("image_output") > 0:
		out.PriceImageM = round(p.f("image_output") * 1e6)
		return config.AreaImage, out
	case slices.Contains(op, "decisions") && slices.Contains(in, "text") && p.f("prompt") > 0:
		out.PriceIn, out.Context = round(p.f("prompt")*1e6), m.Context // Entscheidungsmodelle rechnen nach Eingabe-Tokens ab
		return config.AreaDecision, out
	}
	return "", out
}

func fetchORCatalog(ctx context.Context) (*orCatalog, error) {
	var models struct {
		Data []orRawModel `json:"data"`
	}
	if err := orGet(ctx, "/models?output_modalities=all", &models); err != nil {
		return nil, err
	}
	var zdr struct {
		Data []struct {
			ModelID string   `json:"model_id"`
			Params  []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := orGet(ctx, "/endpoints/zdr", &zdr); err != nil {
		return nil, err
	}
	zdrCount, zdrSchema := map[string]int{}, map[string]bool{}
	for _, e := range zdr.Data {
		zdrCount[e.ModelID]++
		if slices.Contains(e.Params, "structured_outputs") {
			zdrSchema[e.ModelID] = true
		}
	}
	now := time.Now()
	areas := map[string][]orModel{}
	for _, raw := range models.Data {
		area, m := orArea(raw, now)
		switch area {
		case "":
			continue
		case config.AreaTitle:
			m.ZDR = zdrSchema[m.ID] // Datenschutz nur mit einem ZDR-Endpunkt, der JSON-Schema kann
		case config.AreaImage:
			m.ZDR = zdrCount[m.ID] > 0
		case config.AreaTranscribe, config.AreaDecision: // ZDR lässt sich hier nicht je Anfrage erzwingen
			if zdrCount[m.ID] > 0 {
				var ep struct {
					Data struct {
						Endpoints []json.RawMessage `json:"endpoints"`
					} `json:"data"`
				}
				if err := orGet(ctx, "/models/"+m.ID+"/endpoints", &ep); err != nil {
					return nil, err
				}
				m.ZDR = len(ep.Data.Endpoints) == zdrCount[m.ID]
			}
		}
		areas[area] = append(areas[area], m)
	}
	for _, list := range areas {
		sort.SliceStable(list, func(i, j int) bool { return list[i].cost() < list[j].cost() })
	}
	// Kurztitel, Auswertung und Recherche teilen dieselben Textmodelle.
	areas[config.AreaDictation], areas[config.AreaResearch] = areas[config.AreaTitle], areas[config.AreaTitle]
	return &orCatalog{Version: orCatalogVersion, Time: now.UTC(), Areas: areas}, nil
}
