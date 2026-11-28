package app

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/openai"
	"pebble-webhook/internal/store"
	"pebble-webhook/internal/whisper"
)

// Quellen eines Transkripts aus Audio: Cloud-Eintrag, Whisper auf diesem Server, oder Cloud, weil ein Eintrag davor scheiterte.
const (
	sourceOpenAI   = "openai"
	sourceLocal    = "lokal"
	sourceFallback = "openai-ausweich"
)

// transcribeAudio wandelt eine Aufnahme nach der Reihenfolge der Transkription um: Whisper auf diesem Server oder ein
// Cloud-Eintrag; scheitert einer oder ist sein Monatslimit erreicht, übernimmt der nächste (Quelle „…-ausweich“,
// Grund im Log). Die Kosten trägt es in cost ein (lokal 0; unbekannte Länge bleibt nil).
func (a *App) transcribeAudio(ctx context.Context, name string, audio []byte, mime string, cost **float64) (text string, seconds float64, source string, err error) {
	var reasons []string
	ki := a.KI()
	prompt := a.transcribePrompt()
	for _, e := range ki.AI.Active(config.AreaTranscribe) {
		if e.Access == config.EntryPebble {
			continue
		}
		if e.Access == config.EntryWhisper {
			text, seconds, err = a.Whisper.Transcribe(ctx, ki.WhisperModel, audio, a.Cfg.Language, prompt)
			if err == nil {
				zero := 0.0
				addCost(cost, &zero)
				// Auch die lokale Transkription steht im Protokoll (Kosten 0), damit es alle Anfragen zeigt.
				u := openai.Usage{UserID: a.UserID, Ref: openai.TagOf(ctx).Ref, Area: openai.AreaTranscribe, EntryID: e.ID,
					Provider: "Whisper", Model: ki.WhisperModel, Cost: &zero}
				if seconds >= 0 {
					u.Seconds = &seconds
				}
				a.System.DB.Record(u)
				return text, seconds, sourceLocal, nil
			}
		} else {
			var c *float64
			text, seconds, c, err = a.AI.TranscribeStep(a.tagged(ctx), e.ID, name, audio, mime, a.Cfg.Language, prompt)
			if err == nil {
				addCost(cost, c)
				source = sourceOpenAI
				if len(reasons) > 0 {
					source = sourceFallback
				}
				return text, seconds, source, nil
			}
		}
		if ctx.Err() != nil {
			return "", -1, "", err
		}
		reasons = append(reasons, err.Error())
		logf("Nutzer %d: Transkription (%s) gescheitert: %v", a.UserID, e.Access, err)
	}
	switch len(reasons) {
	case 0:
		return "", -1, "", errors.New(a.T("Keine Transkription eingetragen (Status → KI → Transkription)"))
	case 1:
		return "", -1, "", err
	}
	return "", -1, "", errors.New(strings.Join(reasons, "; "))
}

// startWhisper lädt beim Start das gewählte Modell (falls es fehlt) und legt es gleich in den RAM, wenn lokal
// transkribiert wird; Keepalive nimmt es nach WHISPER_KEEP_MINUTES ohne Aufnahme wieder heraus (0 = nie).
func (s *System) startWhisper(ctx context.Context) {
	ki := s.KI()
	if !ki.AI.UsesWhisper() {
		return
	}
	s.Whisper.SetKeep(time.Duration(ki.WhisperKeep) * time.Minute)
	s.keepAlive.Do(func() { go s.Whisper.Keepalive(ctx) })
	s.Whisper.Ensure(ctx, ki.WhisperModel)
	s.Whisper.Preload(ctx, ki.WhisperModel)
}

// whisperLoading: Whisper steht in der Transkription und sein Modell wird gerade geladen – Transkriptionen warten.
func (a *App) whisperLoading() bool {
	ki := a.KI()
	return ki.AI.UsesWhisper() && a.Whisper.Loading(ki.WhisperModel)
}

// handleWhisperDelete löscht ein geladenes Modell (Admin, Bearbeitungsseite der Transkription); das gewählte nicht,
// solange lokal transkribiert wird.
func (s *System) handleWhisperDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if ki := s.KI(); ki.AI.UsesWhisper() && id == ki.WhisperModel {
		writeErr(w, 409, "gewaehlt", "Das gewählte Modell lässt sich nicht löschen – erst ein anderes wählen")
		return
	}
	if err := s.Whisper.Delete(id); err != nil {
		writeErr(w, 400, "whisper", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "whisper": whisperInfo(s.Whisper)})
}

// whisperInfo: Modelle mit Zustand und die Mittel des Servers – für die Auswahl in Einrichtung und Konfiguration.
func whisperInfo(m *whisper.Manager) map[string]any {
	mem := parseKeyValues(readText("/proc/meminfo"))
	return map[string]any{"modelle": m.States(), "kerne": runtime.NumCPU(), "ram_gesamt": kibBytes(mem["MemTotal"]),
		"ram_verfuegbar": kibBytes(mem["MemAvailable"]), "engine": whisper.EngineStand()}
}

// transcriptionStatus: Block „Transkription“ der Statusseite.
func (a *App) transcriptionStatus() map[string]any {
	ki := a.KI()
	out := map[string]any{"modus": pebbleMode(ki.AI), "lokal": ki.AI.UsesWhisper()}
	if !ki.AI.UsesWhisper() {
		return out
	}
	out["engine"] = whisper.EngineStand()
	out["kerne"] = runtime.NumCPU()
	for _, st := range a.Whisper.States() {
		if st.ID == ki.WhisperModel {
			out["modell"] = st
		}
	}
	if id, since := a.Whisper.InRAM(); id != "" {
		out["im_ram"], out["im_ram_seit"] = id, since
	}
	out["ram_halten_minuten"] = ki.WhisperKeep
	if msg, at := a.Whisper.LastError(); msg != "" {
		out["letzter_fehler"], out["letzter_fehler_zeit"] = msg, at
	}
	return out
}

// handleRetranscribe: POST /api/eintraege/{id}/neu-transkribieren. Wandelt die Aufnahme nach der aktuellen
// Einstellung neu in Text um und lässt die KI danach neu auswerten – wie „Wortlaut korrigieren“, nur dass der
// neue Wortlaut aus der Transkription kommt (Notiz: Auswertung, Liste, Termin; Planung: nur die letzte Runde).
func (a *App) handleRetranscribe(w http.ResponseWriter, r *http.Request) {
	j, err := a.DB.Job(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "fehlt", "Eintrag nicht gefunden")
		return
	}
	if j.Mode != "diktat" && j.Mode != "planung" {
		writeErr(w, http.StatusConflict, "modus", "Nur Notizen und Planungen lassen sich neu transkribieren")
		return
	}
	if j.State != store.StateDone && !j.Failed {
		writeErr(w, http.StatusConflict, "laeuft", "Wird noch verarbeitet – danach neu transkribieren")
		return
	}
	if j.AudioPath == "" || j.AudioDeletedAt != 0 {
		writeErr(w, http.StatusConflict, "kein_audio", "Keine Aufnahme (mehr) vorhanden")
		return
	}
	if a.KI().AI.OnlyPebble() {
		writeErr(w, http.StatusConflict, "pebble", "Die Transkription übernimmt den Text der Pebble-App – dafür neu transkribieren geht nicht")
		return
	}
	planningID := j.PlanningID
	if planningID != "" {
		rounds, err := a.DB.Rounds(planningID)
		if err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if len(rounds) > 0 && rounds[len(rounds)-1].ID != j.ID {
			writeErr(w, http.StatusConflict, "nicht_letzte", "Nur die letzte Runde einer Planung lässt sich neu transkribieren")
			return
		}
		addCost(&j.CostAux, j.CostUSD)
		j.Result, j.CostUSD, j.LimitHit = "", nil, false
	}
	// Notiz: die neue Auswertung ersetzt die Punkte der alten (Liste von Hand und Haken bleiben, siehe dropEvalItems).
	j.Transcript, j.TranscriptSource, j.State = "", "", store.StateNew
	j.Failed, j.Empty, j.FailedAt, j.Attempts, j.NextTryAt, j.Error = false, false, 0, 0, 0, ""
	if err := a.DB.SaveJob(j); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if planningID != "" {
		if err := a.detach(planningID, 0); err != nil {
			logf("Neu transkribiert: Planung %s: %v", planningID, err)
		}
		a.hub.send("planung:" + planningID)
	}
	a.changed(j.ID)
	a.Wake()
	writeJSON(w, 200, map[string]any{"ok": true})
}
