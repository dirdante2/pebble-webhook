package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"pebble-webhook/internal/i18n"
)

// searchTool ist SearXNG als eigenes Werkzeug der KI.
const searchTool = "websuche"

var searxFunction = Function{Name: searchTool, Description: "Sucht im Web. Liefert Titel, Adresse und Auszug der besten Treffer.",
	Parameters: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"suchbegriff"},
		"properties": map[string]any{"suchbegriff": map[string]any{"type": "string"}}}}

const noSearchNote = "\n\nEine Websuche steht nicht zur Verfügung: antworte aus deinem Wissen, nenne keine Quellen, die du nicht kennst, und erfinde keine Adressen."

var (
	thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>`)
	fenceRe = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*(.*?)\\s*```$")
)

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
	// ExtraContent: Gemini legt hier die Denk-Signatur ab; sie muss in der nächsten Runde unverändert zurück.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

// respondChat: wie Respond, über /chat/completions. Werkzeuge (eigene und SearXNG) laufen in einer Schleife;
// sind die Runden aufgebraucht, gehen keine Werkzeuge mehr mit und die KI muss antworten. Die Websuche des
// Anbieters: bei OpenRouter das Web-Plugin; andere Modelle (Perplexity Sonar, Groq Compound) suchen selbst –
// ihre Quellen kommen als Annotationen, citations oder search_results zurück.
func (c *Client) respondChat(ctx context.Context, r Request) (*Result, error) {
	searx := r.WebSearch && c.SearchURL != ""
	provider := r.WebSearch && c.ProviderSearch
	instructions := r.Instructions
	if r.WebSearch && !searx && !provider {
		instructions += noSearchNote
	}
	var messages []any
	if instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}
	messages = append(messages, map[string]any{"role": "user", "content": r.Input})

	functions := []Function{}
	if r.Call != nil {
		functions = append(functions, r.Functions...)
	}
	if searx {
		functions = append(functions, searxFunction)
	}
	var tools []any
	for _, f := range functions {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
			"name": f.Name, "description": f.Description, "parameters": f.Parameters, "strict": true}})
	}
	maxSearx := max(1, r.MaxToolCalls)
	maxRounds := max(1, r.MaxFunctionRounds)
	if searx {
		maxRounds += maxSearx
	}

	body := map[string]any{"model": r.Model, "stream": false}
	if p := c.zdrProvider(); p != nil {
		body["provider"] = p
	}
	// OpenRouter: Nachdenken einheitlich über „reasoning“ (aus = enabled false).
	if c.Dialect == "openrouter" {
		if e := c.effort(r.ReasoningEffort); e == "none" {
			body["reasoning"] = map[string]any{"enabled": false}
		} else if e != "" {
			body["reasoning"] = map[string]any{"effort": e}
		}
	}
	if r.MaxOutputTokens > 0 {
		body["max_tokens"] = r.MaxOutputTokens
	}
	if c.Dialect == "gemini" {
		if e := c.effort(r.ReasoningEffort); e != "" {
			body["reasoning_effort"] = e
		}
	}
	if r.Schema != nil {
		body["response_format"] = map[string]any{"type": "json_schema",
			"json_schema": map[string]any{"name": r.SchemaName, "schema": r.Schema, "strict": true}}
	}
	plugin := provider && (c.Plugin || strings.Contains(c.Base, "openrouter.ai"))
	if plugin {
		body["plugins"] = []any{map[string]any{"id": "web", "max_results": 5}}
	}

	total := &Result{}
	found := &hitList{}
	searxCalls := 0
	for round := 0; ; round++ {
		body["messages"] = messages
		delete(body, "tools")
		if len(tools) > 0 && round < maxRounds {
			body["tools"] = tools
		}
		msg, err := c.chatOnce(ctx, body)
		if err != nil {
			return nil, err
		}
		total.InputTokens += msg.usage[0]
		total.OutputTokens += msg.usage[1]
		total.ReportedCost = addCost(total.ReportedCost, msg.cost)
		// Websuche des Anbieters: je Aufruf eine Suche (OpenRouter rechnet je Anfrage ab; andere, wenn sie Quellen liefern).
		if plugin || (provider && len(msg.citations) > 0) {
			total.Searches++
		}
		total.Citations = appendCitations(total.Citations, msg.citations)
		if len(msg.ToolCalls) == 0 || round >= maxRounds {
			text := strings.TrimSpace(thinkRe.ReplaceAllString(msg.Content, ""))
			if r.Schema != nil {
				if m := fenceRe.FindStringSubmatch(text); m != nil {
					text = m[1]
				}
			}
			if msg.finish == "length" {
				total.Incomplete = "max_output_tokens"
			}
			if text == "" {
				return nil, i18n.Errorf("%s hat keinen Text geliefert (Status %s, %s)", c.label(), msg.finish, total.Incomplete)
			}
			total.Text = text
			total.Citations = found.cited(text, total.Citations)
			return total, nil
		}
		assistant := map[string]any{"role": "assistant", "content": msg.Content, "tool_calls": msg.ToolCalls}
		if len(msg.ReasoningDetails) > 0 && string(msg.ReasoningDetails) != "null" {
			assistant["reasoning_details"] = msg.ReasoningDetails
		}
		messages = append(messages, assistant)
		for _, tc := range msg.ToolCalls {
			var out string
			switch {
			case tc.Function.Name == searchTool && searx:
				if searxCalls >= maxSearx {
					out = "Suchlimit erreicht – antworte jetzt mit dem, was du hast."
					break
				}
				searxCalls++
				var hits []searchHit
				out, hits = c.searxText(ctx, tc.Function.Arguments)
				found.add(hits)
			case r.Call != nil:
				total.Calls++
				out = r.Call(ctx, tc.Function.Name, tc.Function.Arguments)
			default:
				out = "Unbekanntes Werkzeug."
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": tc.ID, "content": out})
		}
	}
}

func appendCitations(dst, add []Citation) []Citation {
	seen := map[string]bool{}
	for _, c := range dst {
		seen[c.URL] = true
	}
	for _, c := range add {
		if c.URL != "" && !seen[c.URL] {
			seen[c.URL] = true
			dst = append(dst, c)
		}
	}
	return dst
}

type chatMessage struct {
	Content     string         `json:"content"`
	Refusal     string         `json:"refusal"`
	ToolCalls   []chatToolCall `json:"tool_calls"`
	// ReasoningDetails: Denkschritte (OpenRouter) – gehen in der Werkzeugschleife unverändert zurück.
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
	Annotations []struct {
		Type        string `json:"type"`
		URLCitation struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"url_citation"`
	} `json:"annotations"`

	usage     [2]int
	cost      *float64 // vom Anbieter gemeldet (OpenRouter)
	finish    string
	citations []Citation
}

// chatOnce: ein Aufruf; liefert die Nachricht samt Tokens, Grund des Endes und Quellen der Websuche des Anbieters.
func (c *Client) chatOnce(ctx context.Context, body map[string]any) (*chatMessage, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Choices []struct {
			Message      chatMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int      `json:"prompt_tokens"`
			CompletionTokens int      `json:"completion_tokens"`
			Cost             *float64 `json:"cost"`
		} `json:"usage"`
		Citations     []string `json:"citations"` // Perplexity
		SearchResults []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"search_results"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, i18n.Errorf("%s hat keinen Text geliefert (Status %s, %s)", c.label(), "leer", "")
	}
	ch := out.Choices[0]
	m := &ch.Message
	if m.Refusal != "" {
		return nil, i18n.Errorf("%s hat die Anfrage abgelehnt: %s", c.label(), m.Refusal)
	}
	m.usage = [2]int{out.Usage.PromptTokens, out.Usage.CompletionTokens}
	m.cost = out.Usage.Cost
	m.finish = ch.FinishReason
	for i := range m.ToolCalls {
		m.ToolCalls[i].Type = "function"
	}
	for _, a := range m.Annotations {
		if a.Type == "url_citation" {
			m.citations = append(m.citations, Citation{Title: a.URLCitation.Title, URL: a.URLCitation.URL})
		}
	}
	for _, s := range out.SearchResults {
		m.citations = append(m.citations, Citation{Title: s.Title, URL: s.URL})
	}
	for _, u := range out.Citations {
		m.citations = append(m.citations, Citation{Title: u, URL: u})
	}
	return m, nil
}

type searchHit struct{ Title, URL, Content string }

// searx fragt SearXNG (JSON-Ausgabe muss in dessen settings.yml erlaubt sein) und liefert die fünf besten Treffer.
func (c *Client) searx(ctx context.Context, args string) ([]searchHit, error) {
	var in struct {
		Suchbegriff string `json:"suchbegriff"`
	}
	if json.Unmarshal([]byte(args), &in) != nil || strings.TrimSpace(in.Suchbegriff) == "" {
		return nil, i18n.Errorf("kein Suchbegriff")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	q := url.Values{"q": {in.Suchbegriff}, "format": {"json"}, "language": {i18n.Lang()}}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.SearchURL, "/")+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, i18n.Errorf("SearXNG nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return nil, i18n.Errorf("SearXNG antwortet %d (JSON-Ausgabe erlaubt?)", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, i18n.Errorf("SearXNG-Antwort nicht lesbar: %w", err)
	}
	var hits []searchHit
	for _, h := range out.Results {
		if len(hits) == 5 {
			break
		}
		if strings.HasPrefix(h.URL, "http") {
			hits = append(hits, searchHit{Title: h.Title, URL: h.URL, Content: h.Content})
		}
	}
	return hits, nil
}
