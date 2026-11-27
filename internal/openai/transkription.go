package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"pebble-webhook/internal/i18n"
)

// transcribeXAI: Sprache zu Text bei xAI (POST /stt, multipart; die Datei muss das letzte Feld sein). Begriffe gehen
// als keyterm mit; die Antwort nennt die Dauer.
func (c *Client) transcribeXAI(ctx context.Context, model, name string, audio []byte, mime, language, prompt string) (string, float64, *float64, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("model", model)
	if language != "" {
		w.WriteField("language", language)
	}
	for i, k := range keyterms(prompt) {
		if i == 100 {
			break
		}
		w.WriteField("keyterm", k)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(name)))
	h.Set("Content-Type", mime)
	part, _ := w.CreatePart(h)
	part.Write(audio)
	w.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/stt", &body)
	if err != nil {
		return "", -1, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var out struct {
		Text     string          `json:"text"`
		Duration *float64        `json:"duration"`
		Usage    json.RawMessage `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return "", -1, nil, err
	}
	seconds := -1.0
	if out.Duration != nil {
		seconds = *out.Duration
	}
	return strings.TrimSpace(out.Text), seconds, reportedCost(out.Usage), nil
}

// keyterms: Begriffe aus dem Hinweis der Transkription (mit Komma getrennt).
func keyterms(prompt string) []string {
	var out []string
	for _, k := range strings.Split(prompt, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// transcribeChat: Transkription über ein Chat-Modell mit Audio-Eingabe (Gemini hat keinen eigenen Endpunkt in der
// OpenAI-kompatiblen API). Die Dauer nennt die Antwort nicht (-1).
func (c *Client) transcribeChat(ctx context.Context, model string, audio []byte, mime, language, prompt string) (string, float64, *float64, error) {
	if len(audio) > 15<<20 {
		return "", -1, nil, i18n.Errorf("%s: Aufnahme zu groß für die Transkription (höchstens 15 MB)", c.label())
	}
	instr := "Transkribiere die Aufnahme wörtlich. Gib nur den gesprochenen Text aus, ohne Kommentar, ohne Zeitmarken und ohne Anführungszeichen. Ist nichts gesprochen, gib nichts aus."
	if language != "" {
		instr += " Sprache: " + language + "."
	}
	if prompt != "" {
		instr += " Schreibweisen dieser Begriffe übernehmen: " + prompt + "."
	}
	body := map[string]any{"model": model, "stream": false, "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": instr},
		map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": base64.StdEncoding.EncodeToString(audio), "format": audioFormat(mime)}},
	}}}}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.Base+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", -1, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := c.send(req, &out); err != nil {
		return "", -1, nil, err
	}
	if len(out.Choices) == 0 {
		return "", -1, nil, i18n.Errorf("%s hat keinen Text geliefert (Status %s, %s)", c.label(), "leer", "")
	}
	return strings.TrimSpace(thinkRe.ReplaceAllString(out.Choices[0].Message.Content, "")), -1, reportedCost(out.Usage), nil
}

// audioFormat: Format-Angabe für input_audio aus dem MIME-Typ der Aufnahme.
func audioFormat(mime string) string {
	m := strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch m {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "video/mp4":
		return "m4a"
	case "audio/aac":
		return "aac"
	case "audio/ogg", "audio/opus":
		return "ogg"
	case "audio/webm", "video/webm":
		return "webm"
	case "audio/flac", "audio/x-flac":
		return "flac"
	}
	if i := strings.LastIndex(m, "/"); i >= 0 {
		return m[i+1:]
	}
	return m
}
