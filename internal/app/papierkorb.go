package app

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"pebble-webhook/internal/store"
)

// Papierkorb: Was der Nutzer löscht, liegt TRASH_RETENTION_DAYS mit deleted_at im Papierkorb und lässt sich
// unter Status → Papierkorb wiederherstellen; danach (oder auf Knopfdruck) wird es endgültig entfernt.

// errRestore: Wiederherstellen nicht möglich; der Text geht an den Nutzer.
type errRestore string

func (e errRestore) Error() string { return string(e) }

// trashUntil liefert den Zeitpunkt der Endlöschung zu einem Löschzeitpunkt (0 = nie).
func (a *App) trashUntil(deletedAt int64) int64 {
	if a.Cfg.TrashRetentionDays <= 0 {
		return 0
	}
	return deletedAt + int64(a.Cfg.TrashRetentionDays)*24*3600_000
}

func (a *App) handleTrash(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.DB.TrashedJobs(0)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	plannings, err := a.DB.TrashedPlannings(0)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, j := range jobs {
		titel := shortTitle(j.Title)
		if titel == "" {
			titel = store.Preview(j.Transcript, 80)
		}
		m := map[string]any{"typ": "eintrag", "id": j.ID, "nr": j.Nr, "modus": j.Mode, "titel": titel,
			"eingang": j.ReceivedAt, "geloescht": j.DeletedAt, "endgueltig": a.trashUntil(j.DeletedAt)}
		if j.PlanningID != "" {
			m["runde"] = j.RoundNo
			if p, err := a.DB.PlanningAny(j.PlanningID); err == nil {
				m["nr"], m["planung"] = p.Nr, shortTitle(p.Title)
			}
		}
		out = append(out, m)
	}
	for _, p := range plannings {
		rounds, _ := a.DB.AllRounds(p.ID)
		var last int64
		for _, r := range rounds {
			last = max(last, r.ReceivedAt)
		}
		out = append(out, map[string]any{"typ": "planung", "id": p.ID, "nr": p.Nr, "modus": "planung", "titel": shortTitle(p.Title),
			"runden": len(rounds), "eingang": last, "geloescht": p.DeletedAt, "endgueltig": a.trashUntil(p.DeletedAt)})
	}
	sort.SliceStable(out, func(i, k int) bool { return out[i]["geloescht"].(int64) > out[k]["geloescht"].(int64) })
	writeJSON(w, 200, map[string]any{"eintraege": out, "tage": a.Cfg.TrashRetentionDays})
}

func (a *App) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.PathValue("typ") {
	case "eintrag":
		err = a.restoreJob(r.PathValue("id"))
	case "planung":
		err = a.restorePlanning(r.PathValue("id"))
	default:
		writeErr(w, 400, "ungueltig", "Unbekannte Art")
		return
	}
	var er errRestore
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "fehlt", "Nicht im Papierkorb")
	case errors.As(err, &er):
		writeErr(w, http.StatusConflict, "nicht_moeglich", er.Error())
	case err != nil:
		logf("Wiederherstellen: %v", err)
		writeErr(w, 500, "speicher", "Wiederherstellen fehlgeschlagen")
	default:
		a.Wake()
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func (a *App) handleTrashPurge(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.PathValue("typ") {
	case "eintrag":
		err = a.purgeTrashedJob(r.PathValue("id"))
	case "planung":
		err = a.purgeTrashedPlanning(r.PathValue("id"))
	default:
		writeErr(w, 400, "ungueltig", "Unbekannte Art")
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "fehlt", "Nicht im Papierkorb")
	case err != nil:
		logf("Endgültig löschen: %v", err)
		writeErr(w, 500, "speicher", "Löschen fehlgeschlagen")
	default:
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// restoreJob holt einen Eintrag samt Listenpunkten zurück. Eine Planungsrunde bringt ihre Planung mit, falls die
// im Papierkorb liegt; die Planung steht danach wieder auf dem Stand ihrer jüngsten Runde.
func (a *App) restoreJob(id string) error {
	j, err := a.DB.JobAny(id)
	if err != nil {
		return err
	}
	if j.DeletedAt == 0 {
		return store.ErrNotFound
	}
	if j.PlanningID != "" {
		p, err := a.DB.PlanningAny(j.PlanningID)
		if errors.Is(err, store.ErrNotFound) {
			return errRestore(a.T("Die Planung zu dieser Runde gibt es nicht mehr"))
		}
		if err != nil {
			return err
		}
		if p.DeletedAt != 0 {
			if err := a.restorePlanning(p.ID); err != nil {
				return err
			}
		}
	}
	if err := a.DB.RestoreJob(j.ID); err != nil {
		return err
	}
	if j.PlanningID == "" && j.JoplinID != "" && j.JoplinPushedAt != 0 {
		a.DB.DoneJoplinDelete(j.JoplinID)
		if err := a.DB.MarkJobDirty(j.ID); err != nil {
			return err
		}
	}
	a.touchItemsOf(j.ID, "")
	if j.PlanningID != "" {
		if err := a.detach(j.PlanningID, 0); err != nil {
			return err
		}
		a.hub.send("planung:" + j.PlanningID)
	}
	a.changed(j.ID)
	logf("Papierkorb: Eintrag %s wiederhergestellt", j.ID)
	return nil
}

// restorePlanning holt eine Planung mit den zusammen mit ihr gelöschten Runden, Antworten und Listenpunkten zurück.
func (a *App) restorePlanning(id string) error {
	p, err := a.DB.PlanningAny(id)
	if err != nil {
		return err
	}
	if p.DeletedAt == 0 {
		return store.ErrNotFound
	}
	answers, err := a.DB.TrashedAnswerJobs(id)
	if err != nil {
		return err
	}
	if err := a.DB.RestorePlanning(id); err != nil {
		return err
	}
	if p, err = a.DB.Planning(id); err != nil {
		return err
	}
	if p.JoplinPushedAt != 0 {
		a.DB.DoneJoplinDelete(p.JoplinID)
		p.JoplinDirty = true
		if err := a.DB.SavePlanning(p); err != nil {
			return err
		}
	}
	a.touchItemsOf("", id)
	rounds, _ := a.DB.Rounds(id)
	for _, r := range append(rounds, answers...) {
		a.touchItemsOf(r.ID, "")
		a.changed(r.ID)
	}
	a.hub.send("planung:" + id)
	logf("Papierkorb: Planung %s wiederhergestellt", id)
	return nil
}

// touchItemsOf meldet die Listen der (sichtbaren) Punkte eines Auftrags bzw. einer Planung als geändert.
func (a *App) touchItemsOf(jobID, planningID string) {
	var items []*store.ListItem
	if jobID != "" {
		items, _ = a.DB.ItemsByJob(jobID)
	} else {
		items, _ = a.DB.ItemsByPlanning(planningID)
	}
	for _, it := range items {
		a.listChanged(it.ListID)
	}
}

// purgeTrashedJob entfernt einen Eintrag aus dem Papierkorb endgültig: Aufnahme, Listenpunkte, Zeile.
func (a *App) purgeTrashedJob(id string) error {
	j, err := a.DB.JobAny(id)
	if err != nil {
		return err
	}
	if j.DeletedAt == 0 {
		return store.ErrNotFound
	}
	if err := a.removeJob(j); err != nil {
		return err
	}
	logf("Papierkorb: Eintrag %s endgültig gelöscht", id)
	return nil
}

// purgeTrashedPlanning entfernt eine Planung aus dem Papierkorb endgültig: alle Runden, mitgelöschte Antworten, Punkte.
func (a *App) purgeTrashedPlanning(id string) error {
	p, err := a.DB.PlanningAny(id)
	if err != nil {
		return err
	}
	if p.DeletedAt == 0 {
		return store.ErrNotFound
	}
	rounds, err := a.DB.AllRounds(id)
	if err != nil {
		return err
	}
	answers, err := a.DB.TrashedAnswerJobs(id)
	if err != nil {
		return err
	}
	for _, j := range append(rounds, answers...) {
		if j.PlanningID == "" && j.DeletedAt != p.DeletedAt {
			continue // einzeln gelöschte Antwort: eigener Eintrag im Papierkorb
		}
		if err := a.removeJob(j); err != nil {
			return err
		}
	}
	if err := a.DB.DeleteItemsOf("", id); err != nil {
		return err
	}
	if err := a.DB.DeletePlanning(id); err != nil {
		return err
	}
	logf("Papierkorb: Planung %s endgültig gelöscht", id)
	return nil
}

// trashExpired löscht, was länger als TRASH_RETENTION_DAYS im Papierkorb liegt, endgültig.
func (a *App) trashExpired() {
	if a.Cfg.TrashRetentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -a.Cfg.TrashRetentionDays).UnixMilli()
	n := 0
	if ps, err := a.DB.TrashedPlannings(cutoff); err != nil {
		logf("Papierkorb: %v", err)
	} else {
		for _, p := range ps {
			if err := a.purgeTrashedPlanning(p.ID); err != nil {
				logf("Papierkorb: Planung %s: %v", p.ID, err)
				continue
			}
			n++
		}
	}
	if js, err := a.DB.TrashedJobs(cutoff); err != nil {
		logf("Papierkorb: %v", err)
	} else {
		for _, j := range js {
			if err := a.purgeTrashedJob(j.ID); err != nil {
				logf("Papierkorb: Eintrag %s: %v", j.ID, err)
				continue
			}
			n++
		}
	}
	if n > 0 {
		logf("Papierkorb: %d Einträge nach %d Tagen endgültig gelöscht", n, a.Cfg.TrashRetentionDays)
	}
}
