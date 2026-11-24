package app

import (
	"context"
	"io"
	"mime/multipart"

	"pebble-webhook/internal/store"
)

// transcribeUpload: eine gesprochene Eingabe aus der Web-App (z. B. KI-Anweisung) sofort in Text umwandeln.
// Die Aufnahme bleibt nur im Speicher (nie unverschlüsselt auf der Platte); die Kosten zählen zur Notiz j.
func (a *App) transcribeUpload(ctx context.Context, j *store.Job, f multipart.File, mimeType string) (string, error) {
	audio, err := io.ReadAll(io.LimitReader(f, a.Cfg.HookMaxBytes))
	if err != nil {
		return "", err
	}
	text, seconds, err := a.AI.Transcribe(ctx, a.Cfg.TranscribeModel, "eingabe"+audioExt(mimeType), audio, mimeType, a.Cfg.Language)
	if err != nil {
		return "", err
	}
	if seconds >= 0 && a.Cfg.PriceTranscribeMin != nil {
		c := seconds / 60 * *a.Cfg.PriceTranscribeMin
		addCost(&j.CostTranscribe, &c)
	}
	return text, nil
}
