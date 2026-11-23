package app

import (
	"net/http"
	"regexp"
	"strings"

	"pebble-webhook/internal/store"
)

// Notizen aus fremden Quellen (heute nur Joplin-Rückmeldungen): ein Werkzeug legt sie per Token
// als Text an, die normale Auswertung sortiert sie ein. Die fremde Kennung steckt in der
// delivery_id ("<quelle>-<id>"), damit dieselbe Meldung nie zweimal ankommt.

var externSources = map[string]bool{"joplin": true}

var externIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const externMaxText = 4000

func externDelivery(source, id string) string { return source + "-" + id }

// externOf liefert Quelle und fremde Kennung eines Eintrags, sonst leer.
func externOf(j *store.Job) (string, string) {
	if !externSources[j.Source] {
		return "", ""
	}
	return j.Source, strings.TrimPrefix(j.DeliveryID, j.Source+"-")
}

// handleExternCreate: POST /api/eintraege mit {quelle, extern_id, text}.
func (a *App) handleExternCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Quelle   string `json:"quelle"`
		ExternID string `json:"extern_id"`
		Text     string `json:"text"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Anfrage nicht lesbar")
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	switch {
	case !externSources[in.Quelle]:
		writeErr(w, 400, "ungueltig", "Unbekannte Quelle")
		return
	case !externIDRe.MatchString(in.ExternID):
		writeErr(w, 400, "ungueltig", "Kennung fehlt oder ist ungültig")
		return
	case in.Text == "":
		writeErr(w, 400, "leer", "Text fehlt")
		return
	case len([]rune(in.Text)) > externMaxText:
		writeErr(w, http.StatusRequestEntityTooLarge, "gross", "Text zu lang")
		return
	}
	delivery := externDelivery(in.Quelle, in.ExternID)
	if old, err := a.DB.JobByDelivery(delivery); err == nil {
		writeJSON(w, 200, map[string]any{"id": old.ID, "nr": old.Nr, "bekannt": true})
		return
	}
	now := store.Now()
	j := &store.Job{ID: store.NewID(), DeliveryID: delivery, Source: in.Quelle, Trigger: in.Quelle, Mode: "diktat",
		ReceivedAt: now, RecordedAt: now, State: store.StateNew, Transcript: in.Text}
	created, err := a.DB.InsertJob(j)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if !created {
		old, _ := a.DB.JobByDelivery(delivery)
		writeJSON(w, 200, map[string]any{"id": old.ID, "nr": old.Nr, "bekannt": true})
		return
	}
	a.changed(j.ID)
	a.Wake()
	var nr int64
	if saved, err := a.DB.Job(j.ID); err == nil {
		nr = saved.Nr
	}
	writeJSON(w, 201, map[string]any{"id": j.ID, "nr": nr, "bekannt": false})
}

// handleExternKnown: GET /api/eintraege/extern?quelle=joplin → alle schon angelegten fremden
// Kennungen, auch gelöschte (sonst käme eine gelöschte Meldung beim nächsten Lauf zurück).
func (a *App) handleExternKnown(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("quelle")
	if !externSources[source] {
		writeErr(w, 400, "ungueltig", "Unbekannte Quelle")
		return
	}
	ids, err := a.DB.DeliveriesWithPrefix(source + "-")
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"quelle": source, "ids": ids})
}
