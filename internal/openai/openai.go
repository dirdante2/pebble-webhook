// Package openai spricht die Transkriptions- und die Responses-API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
)

type Client struct {
	Key  string
	Base string
	HTTP *http.Client
}

func New(key, base string) *Client {
	return &Client{Key: key, Base: base, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return i18n.T("OpenAI lehnt den API-Schlüssel ab (HTTP 401)")
	case 429:
		return i18n.Tf("OpenAI: Limit erreicht (HTTP 429, %s)", e.Code)
	}
	return i18n.Tf("OpenAI-Fehler HTTP %d: %s", e.Status, e.Message)
}

func (c *Client) send(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.Key)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return i18n.Errorf("OpenAI nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return i18n.Errorf("OpenAI-Antwort unvollständig: %w", err)
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
		return &APIError{Status: resp.StatusCode, Code: code, Message: e.Error.Message}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return i18n.Errorf("OpenAI-Antwort nicht lesbar: %w", err)
	}
	return nil
}

// Transcribe schickt eine Audiodatei zur Transkription und liefert den Text und die
// abgerechnete Dauer in Sekunden (-1, wenn die Antwort keine Dauer nennt).
func (c *Client) Transcribe(ctx context.Context, model, path, mime, language string) (string, float64, error) {
	audio, err := os.ReadFile(path)
	if err != nil {
		return "", -1, i18n.Errorf("Audio nicht lesbar: %w", err)
	}
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
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(path)))
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
}

type Citation struct{ Title, URL string }

type Result struct {
	Text         string
	Citations    []Citation
	InputTokens  int
	OutputTokens int
	Searches     int
	Incomplete   string // Grund, wenn die Antwort abgeschnitten wurde
}

// Respond ruft die Responses-API auf.
func (c *Client) Respond(ctx context.Context, r Request) (*Result, error) {
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
	if r.WebSearch {
		tool := map[string]any{"type": "web_search"}
		if r.SearchContext != "" {
			tool["search_context_size"] = r.SearchContext
		}
		body["tools"] = []any{tool}
		if r.MaxToolCalls > 0 {
			body["max_tool_calls"] = r.MaxToolCalls
		}
	}
	if r.Schema != nil {
		body["text"] = map[string]any{"format": map[string]any{
			"type": "json_schema", "name": r.SchemaName, "schema": r.Schema, "strict": true}}
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/responses", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var out struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
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
		} `json:"output"`
		Usage struct {
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
		return nil, err
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
	for _, item := range out.Output {
		switch item.Type {
		case "web_search_call":
			if item.Action == nil || item.Action.Type == "search" {
				counted++
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "refusal" {
					return nil, i18n.Errorf("OpenAI hat die Anfrage abgelehnt: %s", part.Refusal)
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
	if res.Text == "" {
		return nil, i18n.Errorf("OpenAI hat keinen Text geliefert (Status %s, %s)", out.Status, res.Incomplete)
	}
	return res, nil
}
