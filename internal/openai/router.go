package openai

import (
	"context"
	"errors"
	"strings"
	"sync"

	"pebble-webhook/internal/i18n"
)

// Bereiche der KI (wie config.Area*).
const (
	AreaTranscribe = "transkription"
	AreaTitle      = "kurztitel"
	AreaDictation  = "auswertung"
	AreaResearch   = "recherche"
	AreaImage      = "bilder"
)

// Step: ein Eintrag der Reihenfolge eines Bereichs – Zugang, Modell, Preise (USD; Tokens je 1 Mio.) und Monatslimit.
type Step struct {
	ID, Name    string
	Client      *Client
	Model       string
	PriceIn     float64
	PriceOut    float64
	PriceSearch float64
	PriceMinute float64
	PriceImage  float64
	Limit       float64 // je Kalendermonat; 0 = ohne
}

// Ledger zählt die Kosten je Eintrag im laufenden Monat.
type Ledger interface {
	Spent(id string) float64
	Spend(id string, usd float64)
}

// ErrLimit: das Monatslimit eines Eintrags ist erreicht.
var ErrLimit = errors.New("Monatslimit erreicht")

// ChainError: kein Eintrag eines Bereichs hat es geschafft; nennt je Eintrag den Grund, Unwrap liefert den letzten.
type ChainError struct {
	Reasons []string
	Last    error
}

func (e *ChainError) Error() string { return strings.Join(e.Reasons, "; ") }
func (e *ChainError) Unwrap() error { return e.Last }

// Router verteilt die Aufrufe auf die Einträge der Bereiche: der erste nutzbare erledigt es; scheitert er oder ist
// sein Monatslimit erreicht, übernimmt der nächste.
type Router struct {
	Areas  map[string][]*Step
	Ledger Ledger
	mu     sync.RWMutex // Areas wird beim Speichern unter KI im laufenden Dienst ersetzt
}

// Replace übernimmt die Einträge eines neu gebauten Routers; laufende Aufrufe beenden mit den alten.
func (r *Router) Replace(n *Router) {
	r.mu.Lock()
	r.Areas = n.Areas
	r.mu.Unlock()
}

func (r *Router) steps(area string) []*Step {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Areas[area]
}

// open: Eintrag hat sein Monatslimit noch nicht erreicht.
func (r *Router) open(s *Step) bool {
	return s.Limit <= 0 || r.Ledger == nil || r.Ledger.Spent(s.ID) < s.Limit
}

func (r *Router) spend(s *Step, usd float64) {
	if r.Ledger != nil && usd > 0 {
		r.Ledger.Spend(s.ID, usd)
	}
}

// First: der erste Eintrag eines Bereichs unter seinem Limit (für Vorab-Rechnungen wie das Suchbudget); nil = keiner.
func (r *Router) First(area string) *Step {
	for _, s := range r.steps(area) {
		if r.open(s) {
			return s
		}
	}
	return nil
}

// chain ruft call für die Einträge des Bereichs der Reihe nach auf, bis einer gelingt.
func (r *Router) chain(ctx context.Context, area string, call func(*Step) error) error {
	steps := r.steps(area)
	if len(steps) == 0 {
		return i18n.Errorf("Kein KI-Anbieter für den Bereich „%s“", area)
	}
	fail := &ChainError{}
	for _, s := range steps {
		if !r.open(s) {
			fail.Reasons, fail.Last = append(fail.Reasons, s.Name+": "+i18n.T("Monatslimit erreicht")), ErrLimit
			continue
		}
		err := call(s)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		fail.Reasons, fail.Last = append(fail.Reasons, s.Name+": "+err.Error()), err
	}
	if len(steps) == 1 && !errors.Is(fail.Last, ErrLimit) {
		return fail.Last
	}
	return fail
}

func (r *Router) Respond(ctx context.Context, req Request) (*Result, error) {
	var res *Result
	err := r.chain(ctx, req.Area, func(s *Step) error {
		q := req
		q.Model = s.Model
		out, err := s.Client.Respond(ctx, q)
		if err != nil {
			return err
		}
		cost := (float64(out.InputTokens)*s.PriceIn+float64(out.OutputTokens)*s.PriceOut)/1e6 + float64(out.Searches)*s.PriceSearch
		out.Cost, out.Provider = &cost, s.Name
		r.spend(s, cost)
		res = out
		return nil
	})
	return res, err
}

// TranscribeStep: Transkription über einen bestimmten Eintrag (die Reihenfolge samt Whisper führt die App);
// Kosten nach Minuten, nil bei unbekannter Länge.
func (r *Router) TranscribeStep(ctx context.Context, id, name string, audio []byte, mime, language string) (string, float64, *float64, error) {
	var s *Step
	for _, x := range r.steps(AreaTranscribe) {
		if x.ID == id {
			s = x
		}
	}
	if s == nil {
		return "", -1, nil, i18n.Errorf("Eintrag der Transkription fehlt")
	}
	if !r.open(s) {
		return "", -1, nil, ErrLimit
	}
	text, seconds, err := s.Client.Transcribe(ctx, s.Model, name, audio, mime, language)
	if err != nil {
		return "", -1, nil, err
	}
	if seconds < 0 {
		return text, seconds, nil, nil
	}
	cost := seconds / 60 * s.PriceMinute
	r.spend(s, cost)
	return text, seconds, &cost, nil
}

// Image erzeugt ein Bild über die Einträge des Bereichs Bilder.
func (r *Router) Image(ctx context.Context, prompt, size, quality string) ([]byte, float64, error) {
	var data []byte
	var cost float64
	err := r.chain(ctx, AreaImage, func(s *Step) error {
		d, err := s.Client.Image(ctx, s.Model, prompt, size, quality)
		if err != nil {
			return err
		}
		data, cost = d, s.PriceImage
		r.spend(s, cost)
		return nil
	})
	return data, cost, err
}

// withSearx gibt der KI SearXNG als eigenes Werkzeug (Responses-API); die Treffer zählen als Suchen.
func (c *Client) withSearx(r Request, found *hitList) Request {
	call := r.Call
	r.Functions = append(append([]Function{}, r.Functions...), searxFunction)
	r.MaxFunctionRounds = max(1, r.MaxFunctionRounds) + max(1, r.MaxToolCalls)
	r.Call = func(ctx context.Context, name, args string) string {
		if name == searchTool {
			out, hits := c.searxText(ctx, args)
			found.add(hits)
			return out
		}
		if call == nil {
			return "Unbekanntes Werkzeug."
		}
		return call(ctx, name, args)
	}
	return r
}

// searxText: Treffer als Text für die KI (Fehler als Text, damit sie ohne weitermachen kann).
func (c *Client) searxText(ctx context.Context, args string) (string, []searchHit) {
	hits, err := c.searx(ctx, args)
	if err != nil {
		return "Websuche fehlgeschlagen: " + err.Error(), nil
	}
	var b strings.Builder
	for _, h := range hits {
		b.WriteString(h.Title + "\n" + h.URL + "\n" + h.Content + "\n\n")
	}
	if b.Len() == 0 {
		return "Keine Treffer.", nil
	}
	return b.String(), hits
}

// hitList sammelt die Treffer von SearXNG; Quelle wird, was die Antwort nennt.
type hitList struct {
	mu    sync.Mutex
	order []string
	byURL map[string]Citation
}

func (l *hitList) add(hits []searchHit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byURL == nil {
		l.byURL = map[string]Citation{}
	}
	for _, h := range hits {
		if _, ok := l.byURL[h.URL]; !ok {
			l.byURL[h.URL] = Citation{Title: h.Title, URL: h.URL}
			l.order = append(l.order, h.URL)
		}
	}
}

// cited ergänzt cits um die Treffer, deren Adresse im Text steht.
func (l *hitList) cited(text string, cits []Citation) []Citation {
	seen := map[string]bool{}
	for _, c := range cits {
		seen[c.URL] = true
	}
	for _, u := range l.order {
		if !seen[u] && strings.Contains(text, u) {
			seen[u] = true
			cits = append(cits, l.byURL[u])
		}
	}
	return cits
}
