// Package openai spricht die Transkriptions- und die Responses-API von OpenAI, mit Chat=true die
// Chat-Completions-API kompatibler Dienste (Ollama, LM Studio, Gemini, Groq, Mistral, OpenRouter …).
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
)

type Client struct {
	Key  string
	Base string
	HTTP *http.Client
	// Chat: Respond über /chat/completions statt /responses (Dienst, Lokal).
	Chat bool
	// Websuche bei Request.WebSearch: ProviderSearch = die des Anbieters (OpenAI web_search, OpenRouter Web-Plugin,
	// Modelle, die selbst suchen); SearchURL = SearXNG als eigenes Werkzeug. Beides geht, keins = ohne Websuche.
	Plugin         bool // Websuche als OpenRouter-Web-Plugin (sonst an der Adresse erkannt)
	ProviderSearch bool
	SearchURL      string
}

// label: Name des Gegenübers in Fehlermeldungen.
func (c *Client) label() string {
	if c.Chat {
		return i18n.T("KI-Dienst")
	}
	return "OpenAI"
}

func New(key, base string) *Client {
	return &Client{Key: key, Base: base, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

type APIError struct {
	Label   string
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return i18n.Tf("%s lehnt den API-Schlüssel ab (HTTP 401)", e.Label)
	case 429:
		return i18n.Tf("%s: Limit erreicht (HTTP 429, %s)", e.Label, e.Code)
	}
	return i18n.Tf("%s-Fehler HTTP %d: %s", e.Label, e.Status, e.Message)
}

func (c *Client) send(req *http.Request, out any) error {
	if c.Key != "" { // lokale Dienste (Ollama) brauchen keinen Schlüssel
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return i18n.Errorf("%s nicht erreichbar: %w", c.label(), err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return i18n.Errorf("%s-Antwort unvollständig: %w", c.label(), err)
	}
	if resp.StatusCode != 200 {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Code    any    `json:"code"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		json.Unmarshal(data, &e)
		code := fmt.Sprint(e.Error.Code)
		if e.Error.Code == nil {
			code = e.Error.Type
		}
		return &APIError{Label: c.label(), Status: resp.StatusCode, Code: code, Message: e.Error.Message}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return i18n.Errorf("%s-Antwort nicht lesbar: %w", c.label(), err)
	}
	return nil
}

// Transcribe schickt eine Aufnahme (name: Dateiname mit Endung) zur Transkription und liefert den Text und die
// abgerechnete Dauer in Sekunden (-1, wenn die Antwort keine Dauer nennt).
func (c *Client) Transcribe(ctx context.Context, model, name string, audio []byte, mime, language string) (string, float64, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("model", model)
	w.WriteField("response_format", "json")
	if language != "" {
		// Ältere Modelle kennen nur `language`, neuere erwarten `languages[]`.
		if strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "whisper") {
			w.WriteField("language", language)
		} else {
			w.WriteField("languages[]", language)
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(name)))
	h.Set("Content-Type", mime)
	part, _ := w.CreatePart(h)
	part.Write(audio)
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/audio/transcriptions", &body)
	if err != nil {
		return "", -1, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var out struct {
		Text  string `json:"text"`
		Usage *struct {
			Type    string  `json:"type"`
			Seconds float64 `json:"seconds"`
		} `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return "", -1, err
	}
	seconds := -1.0
	if out.Usage != nil && out.Usage.Type == "duration" {
		seconds = out.Usage.Seconds
	}
	return strings.TrimSpace(out.Text), seconds, nil
}

// Image erzeugt ein Bild (Images-API) und liefert es als WebP. quality: low | medium | high; size z. B. 1536x1024.
func (c *Client) Image(ctx context.Context, model, prompt, size, quality string) ([]byte, error) {
	if c.Chat {
		return nil, i18n.Errorf("Bilder erzeugen gibt es nur mit OpenAI")
	}
	body, _ := json.Marshal(map[string]any{"model": model, "prompt": prompt, "n": 1, "size": size, "quality": quality,
		"output_format": "webp", "output_compression": 80})
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 || out.Data[0].B64 == "" {
		return nil, i18n.Errorf("OpenAI hat kein Bild geliefert")
	}
	img, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil {
		return nil, i18n.Errorf("OpenAI-Bild nicht lesbar: %w", err)
	}
	return img, nil
}

type Request struct {
	Model           string
	Instructions    string
	Input           string
	MaxOutputTokens int
	ReasoningEffort string
	WebSearch       bool
	SearchContext   string // low | medium | high
	MaxToolCalls    int
	SchemaName      string
	Schema          map[string]any
	// Area: Bereich der KI (AreaTitle, AreaDictation, AreaResearch); der Router wählt danach den Anbieter.
	Area string
	// Functions: eigene Werkzeuge, die die KI aufrufen darf; Call führt einen Aufruf aus (Ergebnis als Text an die KI).
	// Höchstens MaxFunctionRounds Runden mit Aufrufen, danach muss sie antworten.
	Functions         []Function
	Call              func(ctx context.Context, name, args string) string
	MaxFunctionRounds int
}

// Function beschreibt ein eigenes Werkzeug (Responses-API, type function, strict).
type Function struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Citation struct{ Title, URL string }

type Result struct {
	Text         string
	Citations    []Citation
	InputTokens  int
	OutputTokens int
	Searches     int
	Calls        int      // ausgeführte eigene Werkzeugaufrufe
	Incomplete   string   // Grund, wenn die Antwort abgeschnitten wurde
	Cost         *float64 // USD nach den Preisen des Eintrags (Router); nil = nicht berechnet
	Provider     string   // Name des Eintrags, der geantwortet hat (Router)
}

// Respond ruft die Responses-API auf. Mit Functions läuft eine Schleife: ruft die KI ein Werkzeug, wird es ausgeführt
// und das Ergebnis samt bisheriger Ausgabe (store=false, Denkschritte verschlüsselt) zurückgegeben, bis sie antwortet.
func (c *Client) Respond(ctx context.Context, r Request) (*Result, error) {
	if c.Chat {
		return c.respondChat(ctx, r)
	}
	var found *hitList
	if r.WebSearch && c.SearchURL != "" {
		found = &hitList{}
		r = c.withSearx(r, found)
	}
	if r.WebSearch && !c.ProviderSearch {
		if c.SearchURL == "" {
			r.Instructions += noSearchNote
		}
		r.WebSearch = false
	}
	res, err := c.respondResponses(ctx, r)
	if err == nil && found != nil {
		res.Citations = found.cited(res.Text, res.Citations)
	}
	return res, err
}

func (c *Client) respondResponses(ctx context.Context, r Request) (*Result, error) {
	body := map[string]any{"model": r.Model, "input": r.Input, "store": false}
	if r.Instructions != "" {
		body["instructions"] = r.Instructions
	}
	if r.MaxOutputTokens > 0 {
		body["max_output_tokens"] = r.MaxOutputTokens
	}
	if r.ReasoningEffort != "" {
		body["reasoning"] = map[string]any{"effort": r.ReasoningEffort}
	}
	var tools []any
	if r.WebSearch {
		tool := map[string]any{"type": "web_search"}
		if r.SearchContext != "" {
			tool["search_context_size"] = r.SearchContext
		}
		tools = append(tools, tool)
		if r.MaxToolCalls > 0 {
			body["max_tool_calls"] = r.MaxToolCalls
		}
	}
	useFunctions := len(r.Functions) > 0 && r.Call != nil
	var input []any
	if useFunctions {
		for _, f := range r.Functions {
			tools = append(tools, map[string]any{"type": "function", "name": f.Name, "description": f.Description, "parameters": f.Parameters, "strict": true})
		}
		input = []any{map[string]any{"role": "user", "content": r.Input}}
		body["include"] = []string{"reasoning.encrypted_content"}
		if r.MaxToolCalls > 0 {
			body["max_tool_calls"] = r.MaxToolCalls + max(1, r.MaxFunctionRounds)*3
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if r.Schema != nil {
		body["text"] = map[string]any{"format": map[string]any{
			"type": "json_schema", "name": r.SchemaName, "schema": r.Schema, "strict": true}}
	}
	total := &Result{}
	for round := 0; ; round++ {
		if useFunctions {
			body["input"] = input
			if round >= max(1, r.MaxFunctionRounds) {
				body["tool_choice"] = "none" // Runden aufgebraucht: jetzt antworten
			}
		}
		res, items, calls, err := c.respondOnce(ctx, body)
		if err != nil {
			return nil, err
		}
		total.InputTokens += res.InputTokens
		total.OutputTokens += res.OutputTokens
		total.Searches += res.Searches
		total.Citations = append(total.Citations, res.Citations...)
		if !useFunctions || len(calls) == 0 {
			if res.Text == "" {
				return nil, i18n.Errorf("OpenAI hat keinen Text geliefert (%s)", res.Incomplete)
			}
			total.Text, total.Incomplete = res.Text, res.Incomplete
			return total, nil
		}
		input = append(input, items...)
		for _, fc := range calls {
			total.Calls++
			input = append(input, map[string]any{"type": "function_call_output", "call_id": fc.CallID, "output": r.Call(ctx, fc.Name, fc.Arguments)})
		}
	}
}

type functionCall struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	CallID    string `json:"call_id"`
}

// respondOnce: ein Aufruf der Responses-API. items = rohe Ausgabe (für die nächste Runde), calls = Werkzeugaufrufe.
func (c *Client) respondOnce(ctx context.Context, body map[string]any) (*Result, []any, []functionCall, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/responses", bytes.NewReader(data))
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var out struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []json.RawMessage `json:"output"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		ToolUsage *struct {
			WebSearch *struct {
				NumRequests int `json:"num_requests"`
			} `json:"web_search"`
		} `json:"tool_usage"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, nil, nil, err
	}
	res := &Result{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}
	if out.Status == "incomplete" {
		res.Incomplete = "unbekannt"
		if out.IncompleteDetails != nil {
			res.Incomplete = out.IncompleteDetails.Reason
		}
	}
	counted := 0
	seen := map[string]bool{}
	var text strings.Builder
	var items []any
	var calls []functionCall
	for _, raw := range out.Output {
		var item struct {
			Type   string `json:"type"`
			Action *struct {
				Type string `json:"type"`
			} `json:"action"`
			Content []struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Refusal     string `json:"refusal"`
				Annotations []struct {
					Type  string `json:"type"`
					URL   string `json:"url"`
					Title string `json:"title"`
				} `json:"annotations"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		items = append(items, raw)
		switch item.Type {
		case "function_call":
			var fc functionCall
			if json.Unmarshal(raw, &fc) == nil && fc.CallID != "" {
				calls = append(calls, fc)
			}
		case "web_search_call":
			if item.Action == nil || item.Action.Type == "search" {
				counted++
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "refusal" {
					return nil, nil, nil, i18n.Errorf("OpenAI hat die Anfrage abgelehnt: %s", part.Refusal)
				}
				if part.Type != "output_text" {
					continue
				}
				text.WriteString(part.Text)
				for _, a := range part.Annotations {
					if a.Type == "url_citation" && a.URL != "" && !seen[a.URL] {
						seen[a.URL] = true
						res.Citations = append(res.Citations, Citation{Title: a.Title, URL: a.URL})
					}
				}
			}
		}
	}
	// Abgerechnet wird nach dem größeren der beiden Zähler: lieber zu hoch als zu niedrig.
	res.Searches = counted
	if out.ToolUsage != nil && out.ToolUsage.WebSearch != nil && out.ToolUsage.WebSearch.NumRequests > counted {
		res.Searches = out.ToolUsage.WebSearch.NumRequests
	}
	res.Text = strings.TrimSpace(text.String())
	if res.Text == "" && len(calls) == 0 {
		return nil, nil, nil, i18n.Errorf("OpenAI hat keinen Text geliefert (Status %s, %s)", out.Status, res.Incomplete)
	}
	return res, items, calls, nil
}
