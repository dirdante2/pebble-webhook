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
	// Dialect: Anbieter-Art (openai, xai, muse, gemini, openrouter, dienst, lokal) – wo sich die APIs unterscheiden
	// (Websuche, Denkaufwand, Bilder, Transkription, gemeldete Kosten).
	Dialect string
	Name    string // Name des Zugangs für Meldungen
}

// label: Name des Gegenübers in Fehlermeldungen.
func (c *Client) label() string {
	if c.Name != "" {
		return c.Name
	}
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
// Dazu die Kosten, die der Anbieter meldet (nil = keine Angabe).
func (c *Client) Transcribe(ctx context.Context, model, name string, audio []byte, mime, language, prompt string) (string, float64, *float64, error) {
	switch c.Dialect {
	case "xai":
		return c.transcribeXAI(ctx, model, name, audio, mime, language, prompt)
	case "gemini":
		return c.transcribeChat(ctx, model, audio, mime, language, prompt)
	}
	text, seconds, cost, err := c.transcribeMultipart(ctx, model, name, audio, mime, language, prompt)
	return text, seconds, cost, err
}

// transcribeMultipart: /audio/transcriptions im OpenAI-Format (OpenAI, OpenRouter, kompatible Dienste).
func (c *Client) transcribeMultipart(ctx context.Context, model, name string, audio []byte, mime, language, prompt string) (string, float64, *float64, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("model", model)
	w.WriteField("response_format", "json")
	if language != "" {
		// Ältere OpenAI-Modelle kennen nur `language`, neuere erwarten `languages[]`; andere Dienste `language`.
		if (c.Dialect != "" && c.Dialect != "openai") || strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "whisper") {
			w.WriteField("language", language)
		} else {
			w.WriteField("languages[]", language)
		}
	}
	// Begriffe (Namen, Eigenbegriffe) als Hinweis für die Erkennung; Modelle mit Sprechertrennung kennen das Feld nicht.
	if prompt != "" && !strings.Contains(model, "diarize") {
		w.WriteField("prompt", prompt)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(name)))
	h.Set("Content-Type", mime)
	part, _ := w.CreatePart(h)
	part.Write(audio)
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/audio/transcriptions", &body)
	if err != nil {
		return "", -1, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var out struct {
		Text  string          `json:"text"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return "", -1, nil, err
	}
	var usage struct {
		Type    string  `json:"type"`
		Seconds float64 `json:"seconds"`
	}
	json.Unmarshal(out.Usage, &usage)
	seconds := -1.0
	// OpenAI: type „duration“; OpenRouter nennt die Sekunden ohne type (Tokens-Abrechnung hat type „tokens“).
	if usage.Seconds > 0 && (usage.Type == "duration" || usage.Type == "") {
		seconds = usage.Seconds
	}
	return strings.TrimSpace(out.Text), seconds, reportedCost(out.Usage), nil
}

// Image erzeugt ein Bild (Images-API) und liefert die Bilddaten (Format je Anbieter: WebP, PNG oder JPEG) und die
// Kosten, die der Anbieter meldet (nil = keine Angabe). quality: low | medium | high; size z. B. 1536x1024.
func (c *Client) Image(ctx context.Context, model, prompt, size, quality string) ([]byte, *float64, error) {
	body := map[string]any{"model": model, "prompt": prompt, "n": 1}
	path := "/images/generations"
	switch c.Dialect {
	case "openai", "":
		if c.Chat {
			return nil, nil, i18n.Errorf("%s erzeugt keine Bilder", c.label())
		}
		body["size"], body["quality"], body["output_format"], body["output_compression"] = size, quality, "webp", 80
	case "xai":
		body["response_format"], body["aspect_ratio"] = "b64_json", aspectRatio(size)
	case "muse":
		body["response_format"], body["size"], body["output_format"] = "b64_json", size, "webp"
	case "gemini":
		body["response_format"], body["size"] = "b64_json", size
		body["extra_body"] = map[string]any{"aspect_ratio": aspectRatio(size)}
	case "openrouter":
		path = "/images"
		body["response_format"], body["aspect_ratio"] = "b64_json", aspectRatio(size)
	default:
		return nil, nil, i18n.Errorf("%s erzeugt keine Bilder", c.label())
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+path, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, nil, err
	}
	if len(out.Data) == 0 || out.Data[0].B64 == "" {
		return nil, nil, i18n.Errorf("%s hat kein Bild geliefert", c.label())
	}
	b64 := out.Data[0].B64
	if i := strings.Index(b64, ";base64,"); strings.HasPrefix(b64, "data:") && i > 0 {
		b64 = b64[i+len(";base64,"):]
	}
	img, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, i18n.Errorf("Bild von %s nicht lesbar: %w", c.label(), err)
	}
	return img, reportedCost(out.Usage), nil
}

// aspectRatio: Seitenverhältnis aus einer Größe „BxH“ (Anbieter, die nur Verhältnisse kennen).
func aspectRatio(size string) string {
	switch size {
	case "1024x1024":
		return "1:1"
	case "1024x1536":
		return "2:3"
	case "1536x1024":
		return "3:2"
	}
	return "auto"
}

// reportedCost: Kosten, die ein Anbieter selbst im usage-Block meldet – OpenRouter `cost` (USD), xAI
// `cost_in_usd_ticks` (1 USD = 10^10 Ticks); nil = keine Angabe.
func reportedCost(raw json.RawMessage) *float64 {
	if len(raw) == 0 {
		return nil
	}
	var u struct {
		Cost  *float64 `json:"cost"`
		Ticks *float64 `json:"cost_in_usd_ticks"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return nil
	}
	if u.Cost != nil {
		return u.Cost
	}
	if u.Ticks != nil {
		v := *u.Ticks / 1e10
		return &v
	}
	return nil
}

// addCost zählt gemeldete Kosten zusammen (nil bleibt nil, solange nichts gemeldet ist).
func addCost(sum, add *float64) *float64 {
	if add == nil {
		return sum
	}
	v := *add
	if sum != nil {
		v += *sum
	}
	return &v
}

// effort: Denkaufwand in der Sprache des Anbieters („standard“/leer = Vorgabe des Modells).
func (c *Client) effort(e string) string {
	if e == "" || e == "standard" {
		return ""
	}
	switch c.Dialect {
	case "muse":
		if e == "none" {
			return "minimal"
		}
	case "xai", "gemini":
		if e == "none" || e == "minimal" {
			return "low" // Grok 4.7 und Gemini 4 denken immer
		}
	}
	return e
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
	ReportedCost *float64 // USD, wie der Anbieter sie meldet (OpenRouter, xAI); nil = keine Angabe
	Provider     string   // Name des Eintrags, der geantwortet hat (Router)
	Model        string   // sein Modell (Router)
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
	if e := c.effort(r.ReasoningEffort); e != "" {
		body["reasoning"] = map[string]any{"effort": e}
	}
	var tools []any
	if r.WebSearch {
		tool := map[string]any{"type": "web_search"}
		if r.SearchContext != "" && c.Dialect != "xai" { // xAI kennt keine Suchtiefe
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
		total.ReportedCost = addCost(total.ReportedCost, res.ReportedCost)
		total.Citations = append(total.Citations, res.Citations...)
		if !useFunctions || len(calls) == 0 {
			if res.Text == "" {
				return nil, i18n.Errorf("%s hat keinen Text geliefert (%s)", c.label(), res.Incomplete)
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
		Output    []json.RawMessage `json:"output"`
		Usage     json.RawMessage   `json:"usage"`
		ToolUsage *struct {
			WebSearch *struct {
				NumRequests int `json:"num_requests"`
			} `json:"web_search"`
		} `json:"tool_usage"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, nil, nil, err
	}
	var usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		ServerTools  *struct {
			WebSearchCalls int `json:"web_search_calls"`
		} `json:"server_side_tool_usage_details"` // xAI
	}
	json.Unmarshal(out.Usage, &usage)
	res := &Result{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, ReportedCost: reportedCost(out.Usage)}
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
					return nil, nil, nil, i18n.Errorf("%s hat die Anfrage abgelehnt: %s", c.label(), part.Refusal)
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
	if out.ToolUsage != nil && out.ToolUsage.WebSearch != nil && out.ToolUsage.WebSearch.NumRequests > res.Searches {
		res.Searches = out.ToolUsage.WebSearch.NumRequests
	}
	if usage.ServerTools != nil && usage.ServerTools.WebSearchCalls > res.Searches {
		res.Searches = usage.ServerTools.WebSearchCalls
	}
	res.Text = strings.TrimSpace(text.String())
	if res.Text == "" && len(calls) == 0 {
		return nil, nil, nil, i18n.Errorf("%s hat keinen Text geliefert (Status %s, %s)", c.label(), out.Status, res.Incomplete)
	}
	return res, items, calls, nil
}
