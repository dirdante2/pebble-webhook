package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"pebble-webhook/internal/i18n"
)

// AreaDecision: Entscheidungsmodelle (wie config.AreaDecision).
const AreaDecision = "entscheidung"

// Entscheidungen über die Decisions-API von OpenRouter (POST /api/alpha/decisions): statt Text liefert das Modell zu
// einem Zustand (state) je Frage eine typisierte Antwort mit Wahrscheinlichkeiten. Ablauf:
// 1. Aufrufer stellt Fragen (choice: Kriterien je Antwort; noul: ja/nein) zum Text der Notiz.
// 2. Router nimmt den ersten Eintrag des Bereichs Entscheidung, scheitert er, den nächsten.
// 3. Antworten mit choice und confidence; refusal oder fehlende Antwort = nicht beantwortet.

// DecisionQuestion: eine Frage. Criteria: choice = Antwort → Beschreibung; noul = {"true": …, "false": …}.
type DecisionQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// DecisionAnswer: Antwort auf eine Frage (Type "refusal" = abgelehnt).
type DecisionAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence"`
}

// Decide stellt die Fragen zum Zustand (nur OpenRouter).
func (c *Client) Decide(ctx context.Context, model, state string, questions map[string]DecisionQuestion) (map[string]DecisionAnswer, int, *float64, error) {
	if c.Dialect != "openrouter" || !strings.HasSuffix(c.Base, "/v1") {
		return nil, 0, nil, i18n.Errorf("%s kann keine Entscheidungsmodelle", c.label())
	}
	data, _ := json.Marshal(map[string]any{"model": model, "state": state, "questions": questions})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.Base, "/v1")+"/alpha/decisions", bytes.NewReader(data))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Answers map[string]DecisionAnswer `json:"answers"`
		Usage   json.RawMessage           `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, 0, nil, err
	}
	var u struct {
		In int `json:"input_tokens"`
	}
	json.Unmarshal(out.Usage, &u)
	return out.Answers, u.In, reportedCost(out.Usage), nil
}

// Decide: Entscheidung über die Einträge des Bereichs Entscheidung; Kosten meldet der Anbieter (nil = unbekannt).
func (r *Router) Decide(ctx context.Context, state string, questions map[string]DecisionQuestion) (map[string]DecisionAnswer, *float64, error) {
	var answers map[string]DecisionAnswer
	var cost *float64
	err := r.chain(ctx, AreaDecision, func(s *Step) error {
		a, in, reported, err := s.Client.Decide(ctx, s.Model, state, questions)
		if err != nil {
			return err
		}
		answers, cost = a, s.costOf(reported, float64(in)*s.PriceIn/1e6)
		r.record(ctx, s, Usage{Area: AreaDecision, TokensIn: &in, Cost: cost})
		return nil
	})
	return answers, cost, err
}
