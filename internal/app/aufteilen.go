package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"pebble-webhook/internal/store"
)

// Aufteilen (Nutzervorgabe 26.11.2026): Ein Film, ein Buch o. Ä. ist immer eine eigene Notiz. Nennt eine Notiz mehrere
// verschiedene Dinge, fragt die KI nach; bejaht der Nutzer, wird je Punkt eine eigene Notiz ausgewertet und die
// ursprüngliche geht in den Papierkorb. Aufnahmen vom Ring werden nie aufgeteilt.

// splitMark steht in der Zustellungskennung abgeteilter Notizen („<original>:teil:<n>“).
const splitMark = ":teil:"

func isSplitPart(j *store.Job) bool { return strings.Contains(j.DeliveryID, splitMark) }

// fromRing: Aufnahme vom Ring (Webhook oder Nachtrag mit Ring-Kennung bzw. gezählter Aufnahme).
func (a *App) fromRing(j *store.Job) bool {
	if j.Source == "pebble" || ringKennung(j.DeliveryID) != "" {
		return true
	}
	ok, err := a.DB.HasRecording(j.ID)
	if err != nil {
		logf("Auftrag %s: Ring-Aufnahme: %v", j.ID, err)
		return true // im Zweifel nicht aufteilen
	}
	return ok
}

// splitJob legt je Punkt von d eine neue Notiz mit demselben Wortlaut an, beschränkt per Anweisung auf diesen Punkt,
// und legt j mit der Auswertung d in den Papierkorb (von dort wiederherstellbar).
func (a *App) splitJob(j *store.Job, d *Dictation) error {
	now := store.Now()
	var made []string
	for i, p := range d.Punkte {
		sub := Dictation{
			Anweisungen:     []string{fmt.Sprintf("Diese Notiz ist nur der Teil „%s“ des Diktats; alles andere darin nicht auswerten.", p.Text)},
			AnweisungenZeit: []int64{now},
		}
		data, _ := json.Marshal(sub)
		n := &store.Job{ID: store.NewID(), DeliveryID: fmt.Sprintf("%s%s%d", j.DeliveryID, splitMark, i+1), Source: j.Source,
			Trigger: j.Trigger, Mode: "diktat", IsTest: j.IsTest, RecordedAt: j.RecordedAt, ReceivedAt: now,
			Transcript: j.Transcript, TranscriptSource: j.TranscriptSource, State: store.StateTranscribed, Result: string(data)}
		created, err := a.DB.InsertJob(n)
		if err != nil {
			return err
		}
		if created {
			made = append(made, n.ID)
		}
	}
	data, _ := json.Marshal(d)
	j.Result = string(data)
	if err := a.trashJob(j, now); err != nil {
		return err
	}
	j.DeletedAt = now
	logf("Auftrag %s: in %d Notizen aufgeteilt", j.ID, len(made))
	for _, id := range made {
		a.changed(id)
	}
	a.changed(j.ID)
	a.Wake()
	return nil
}
