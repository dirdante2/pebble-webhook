package app

import (
	"encoding/json"
	"net/http"
	"sync"

	"pebble-webhook/internal/store"
)

// Zuletzt gesehen – für alle Geräte gemeinsam (kv „besuch“). Jede Aktion in der Web-App meldet sich hier
// (gedrosselt). Liegt die letzte Aktion länger als VISIT_GAP_MINUTES zurück, beginnt ein neuer Besuch: die letzte
// Aktion davor wird zu „gesehen“. Bei der Anmeldung ebenso. Neu ist, was danach eingegangen und noch nicht aufgerufen ist.

const besuchKey = "besuch"

type besuch struct {
	Zuletzt int64 `json:"zuletzt"`
	Gesehen int64 `json:"gesehen"`
}

var besuchMu sync.Mutex

func (a *App) handleBesuch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Anmeldung bool  `json:"anmeldung"`
		Gesehen   int64 `json:"gesehen"` // Stand des Geräts, nur für den allerersten Aufruf
	}
	readJSON(r, &in)
	besuchMu.Lock()
	defer besuchMu.Unlock()
	var b besuch
	if v, err := a.DB.Get(besuchKey); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	} else if v != "" {
		json.Unmarshal([]byte(v), &b)
	}
	now := store.Now()
	switch {
	case b.Gesehen == 0 && in.Gesehen > 0 && !in.Anmeldung:
		b.Gesehen = in.Gesehen // noch keine gemeinsame Grenze: den bisherigen Stand eines Geräts übernehmen
	case in.Anmeldung || now-b.Zuletzt > int64(a.Cfg.VisitGapMinutes)*60_000:
		b.Gesehen = b.Zuletzt
	}
	b.Zuletzt = now
	data, _ := json.Marshal(b)
	if err := a.DB.Set(besuchKey, string(data)); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	writeJSON(w, 200, b)
}
