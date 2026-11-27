package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Nutzerverwaltung (nur Admin): einladen, Rolle setzen, sperren, löschen; je Nutzer nur Zahlen (Kosten, Speicher,
// letzte Anmeldung) – nie Inhalte.

const (
	inviteTTL      = 7 * 24 * time.Hour
	adminInviteTTL = 24 * time.Hour // Admin-Einladungen kürzer: ein verlorener Link wiegt schwerer
)

func (s *System) userView(u *store.User) map[string]any {
	m := map[string]any{"id": u.ID, "name": u.Name, "rolle": u.Role, "angelegt": u.CreatedAt, "zuletzt": u.LastLoginAt}
	if s.needsPasskey(u.ID) {
		m["passkey_fehlt"] = true // neue Adresse: neu einladen (Code)
	}
	a, reason := s.app(u.ID)
	if a == nil {
		m["gesperrt"] = reason
	} else {
		start := time.Now().AddDate(0, 0, -30).UnixMilli()
		if jobs, err := a.DB.Since(start); err == nil {
			var usd float64
			for _, j := range jobs {
				for _, p := range []*float64{j.CostTranscribe, j.CostAux, j.CostUSD} {
					if p != nil {
						usd += *p
					}
				}
			}
			m["kosten_30_tage"] = usd
		}
	}
	db := UserDB(s.Cfg.DataDir, u.ID)
	var bytes int64
	for _, suf := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(db + suf); err == nil {
			bytes += fi.Size()
		}
	}
	m["daten_bytes"] = bytes
	m["audio_bytes"] = dirSize(s.userMediaRoot(u.ID))
	return m
}

// storageFullPercent: ab dieser Belegung zeigt jede Seite jedem Nutzer eine Karte (die vollste Ablage).
const storageFullPercent = 90

// fullestStorage: die am stärksten belegte Ablage (Daten, Datenbanken, Medien, Backups, Schlüssel) ab 90 %; nil = alles gut.
// Je Dateisystem nur einmal (Ablagen ohne eigenen Mount teilen sich eins). Ohne Pfade: sehen auch Nutzer ohne Admin-Rolle.
func (s *System) fullestStorage() map[string]any {
	type place struct{ name, path string }
	places := []place{{"Datenbanken", filepath.Join(s.Cfg.DataDir, "users")}, {"Medien", s.Cfg.MediaDir},
		{"Backups", s.Cfg.BackupDir}, {"Programm und Daten", s.Cfg.DataDir}, {"Schlüssel", s.Cfg.KeyDir}}
	var best map[string]any
	bestPct := -1.0
	seen := map[string]bool{}
	for _, p := range places {
		free, total, err := diskSpace(p.path)
		if err != nil || total == 0 {
			continue
		}
		key := strconv.FormatUint(total, 10) + "/" + strconv.FormatUint(free, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		pct := float64(total-free) / float64(total) * 100
		if pct >= storageFullPercent && pct > bestPct {
			bestPct = pct
			best = map[string]any{"name": p.name, "prozent": int(pct), "frei": free, "gesamt": total}
		}
	}
	return best
}

// storageByUser: Speicher je Nutzer (Datenbank-Ordner users/<id>, Medien MEDIA_DIR/<id>) für die Statusseite des Admins,
// größter zuerst. Ordner ohne Nutzer in system.db (Reste, Tests) erscheinen mit ihrer Id und ohne Namen.
func (s *System) storageByUser() []map[string]any {
	names := map[int64]string{}
	if us, err := s.DB.Users(); err == nil {
		for _, u := range us {
			names[u.ID] = u.Name
		}
	}
	ids := map[int64]bool{}
	for id := range names {
		ids[id] = true
	}
	for _, root := range []string{filepath.Join(s.Cfg.DataDir, "users"), s.Cfg.MediaDir} {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if id, err := strconv.ParseInt(e.Name(), 10, 64); err == nil && id > 0 && e.IsDir() {
				ids[id] = true
			}
		}
	}
	out := []map[string]any{}
	for id := range ids {
		m := map[string]any{"id": id, "daten": dirSize(UserDir(s.Cfg.DataDir, id)), "medien": dirSize(s.userMediaRoot(id))}
		if n, ok := names[id]; ok {
			m["name"] = n
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i]["daten"].(int64) + out[i]["medien"].(int64)
		tj := out[j]["daten"].(int64) + out[j]["medien"].(int64)
		if ti != tj {
			return ti > tj
		}
		return out[i]["id"].(int64) < out[j]["id"].(int64)
	})
	return out
}

// userMediaRoot: Medien (Aufnahmen und Bilder) eines Nutzers.
func (s *System) userMediaRoot(uid int64) string {
	return strings.TrimRight(s.Cfg.MediaDir, "/") + "/" + strconv.FormatInt(uid, 10)
}

func (s *System) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.DB.Users()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, u := range users {
		out = append(out, s.userView(u))
	}
	me, _ := r.Context().Value(sessionKey).(session)
	writeJSON(w, 200, map[string]any{"nutzer": out, "ich": me.uid})
}

func (s *System) handleUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.userView(u))
}

func (s *System) pathUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var u *store.User
	if err == nil {
		u, err = s.DB.User(id)
	}
	if err != nil {
		writeErr(w, 404, "fehlt", "Nutzer nicht gefunden")
		return nil, false
	}
	return u, true
}

// handleInvite: Einladungslink für einen neuen Nutzer (7 Tage, einmal nutzbar); angelegt wird er mit seinem Passkey.
func (s *System) handleInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Rolle string `json:"rolle"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Angaben nicht lesbar")
		return
	}
	name := store.Preview(strings.TrimSpace(in.Name), 40) // leer: der Eingeladene gibt ihn selbst ein
	if in.Rolle != store.RoleAdmin {
		in.Rolle = store.RoleUser
	}
	ttl := inviteTTL
	if in.Rolle == store.RoleAdmin {
		ttl = adminInviteTTL
	}
	token, err := s.DB.NewEnrollToken(store.Enroll{Purpose: store.EnrollInvite, Name: name, Role: in.Rolle}, ttl)
	if err != nil {
		writeErr(w, 500, "speicher", "Einladung nicht angelegt")
		return
	}
	writeJSON(w, 200, map[string]any{"link": strings.TrimRight(s.Cfg.PublicURL, "/") + "/einrichten#" + token,
		"id": store.Hash(token)[:16], "bis": time.Now().Add(ttl).UnixMilli()})
}

func inviteView(i store.Invite) map[string]any {
	return map[string]any{"id": i.ID, "name": i.Name, "rolle": i.Role, "bis": i.ExpiresAt}
}

// handleInvites: offene Einladungen (Nutzerseite).
func (s *System) handleInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Invites()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, i := range list {
		out = append(out, inviteView(i))
	}
	writeJSON(w, 200, map[string]any{"einladungen": out})
}

func (s *System) handleInviteGet(w http.ResponseWriter, r *http.Request) {
	i, ok := s.DB.Invite(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "fehlt", "Einladung nicht gefunden oder abgelaufen")
		return
	}
	writeJSON(w, 200, inviteView(*i))
}

// handleInviteDelete widerruft eine Einladung (Link und Code gelten danach nicht mehr).
func (s *System) handleInviteDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.DB.DeleteInvite(id) {
		writeErr(w, 404, "fehlt", "Einladung nicht gefunden oder abgelaufen")
		return
	}
	s.devCode.mu.Lock()
	delete(s.devCode.codes, inviteKey(id))
	s.devCode.mu.Unlock()
	logf("Einladung %s widerrufen", id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleUserEdit: Name, Rolle, gesperrt. Der letzte Admin bleibt Admin und entsperrt; sich selbst sperrt man nicht.
func (s *System) handleUserEdit(w http.ResponseWriter, r *http.Request) {
	s.lifeMu.Lock() // auch: zwei Admins stufen sich nicht gleichzeitig gegenseitig herab
	defer s.lifeMu.Unlock()
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Rolle    *string `json:"rolle"`
		Gesperrt *bool   `json:"gesperrt"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Angaben nicht lesbar")
		return
	}
	me, _ := r.Context().Value(sessionKey).(session)
	admins, err := s.DB.Admins()
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	lastAdmin := u.Admin() && u.LockedAt == 0 && admins <= 1
	if in.Rolle != nil && *in.Rolle != store.RoleAdmin && lastAdmin {
		writeErr(w, http.StatusConflict, "letzter_admin", "Der letzte Admin bleibt Admin")
		return
	}
	if in.Gesperrt != nil && *in.Gesperrt && schluessel.HasPending(s.Cfg.KeyDir, u.ID) {
		writeErr(w, http.StatusConflict, "umschluesselung", "Die Daten werden gerade neu verschlüsselt – danach sperren")
		return
	}
	if in.Gesperrt != nil && *in.Gesperrt && (u.ID == me.uid || lastAdmin) {
		writeErr(w, http.StatusConflict, "selbst", "Den eigenen Zugang und den letzten Admin kann man nicht sperren")
		return
	}
	if in.Name != nil {
		name := store.Preview(strings.TrimSpace(*in.Name), 40)
		if name == "" {
			writeErr(w, 400, "name", "Bitte einen Namen eingeben")
			return
		}
		if err := s.DB.SetUserName(u.ID, name); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
	}
	if in.Rolle != nil {
		if err := s.DB.SetUserRole(u.ID, *in.Rolle); err != nil {
			writeErr(w, 400, "ungueltig", "Rolle unbekannt")
			return
		}
	}
	if in.Gesperrt != nil && *in.Gesperrt != (u.LockedAt > 0) {
		if err := s.DB.SetUserLocked(u.ID, *in.Gesperrt); err != nil {
			writeErr(w, 500, "speicher", "Datenbankfehler")
			return
		}
		if *in.Gesperrt {
			s.closeUser(u.ID, lockedByAdmin)
			s.DB.DeleteUserSessions(u.ID) // gesperrt heißt abgemeldet, auch als Admin
			logf("Nutzer %d gesperrt", u.ID)
		} else {
			u.LockedAt = 0
			if _, err := s.openUser(u); err != nil {
				logf("Nutzer %d entsperrt, Daten nicht geöffnet: %v", u.ID, err)
			}
		}
	}
	u, _ = s.DB.User(u.ID)
	s.ensureSystemKey() // Rolle oder Sperre geändert: Bindung nur an aktive Admins
	writeJSON(w, 200, s.userView(u))
}

// handleUserDelete löscht einen Nutzer unwiderruflich: Daten, Aufnahmen, Schlüssel, Anmeldung. Backups behalten seine
// verschlüsselte Datenbank – ohne Schlüssel und Code bleibt sie unlesbar.
func (s *System) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"bestaetigung"`
	}
	readJSON(r, &in)
	if strings.TrimSpace(in.Name) != u.Name || u.Name == "" {
		writeErr(w, 400, "bestaetigung", "Zum Löschen den Namen des Nutzers eingeben")
		return
	}
	me, _ := r.Context().Value(sessionKey).(session)
	admins, _ := s.DB.Admins()
	if u.ID == me.uid || (u.Admin() && u.LockedAt == 0 && admins <= 1) {
		writeErr(w, http.StatusConflict, "selbst", "Den eigenen Zugang und den letzten Admin kann man nicht löschen")
		return
	}
	if schluessel.HasPending(s.Cfg.KeyDir, u.ID) {
		writeErr(w, http.StatusConflict, "umschluesselung", "Die Daten werden gerade neu verschlüsselt – danach löschen")
		return
	}
	s.closeUser(u.ID, "")
	if err := s.DB.DeleteUser(u.ID); err != nil {
		writeErr(w, 500, "speicher", "Nutzer nicht gelöscht")
		return
	}
	s.appsMu.Lock()
	delete(s.locked, u.ID)
	s.appsMu.Unlock()
	if err := s.oauthDropUser(u.ID); err != nil {
		logf("Nutzer %d gelöscht, OAuth-Freigaben nicht entfernt: %v", u.ID, err)
	}
	var errs []error
	for _, p := range []string{UserDir(s.Cfg.DataDir, u.ID), s.userMediaRoot(u.ID)} {
		errs = append(errs, os.RemoveAll(p))
	}
	if err := os.Remove(schluessel.Path(s.Cfg.KeyDir, u.ID)); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		logf("Nutzer %d gelöscht, Dateien nicht vollständig entfernt: %v", u.ID, err)
	}
	logf("Nutzer %d („%s“) gelöscht", u.ID, u.Name)
	s.ensureSystemKey() // war er an den Gelöschten gebunden: an einen anderen Admin
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- Maschinenzugang (API-Token) des Nutzers: neu erzeugen (nur jetzt sichtbar) oder abschalten ---

func (a *App) handleAPIToken(w http.ResponseWriter, r *http.Request) {
	toks, err := a.System.DB.Tokens(a.UserID, store.TokenAPI)
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	out := []map[string]any{}
	for _, t := range toks {
		out = append(out, map[string]any{"id": t.ID, "name": t.Label, "angelegt": t.CreatedAt, "zuletzt": t.LastUsedAt})
	}
	writeJSON(w, 200, map[string]any{"tokens": out})
}

// handleAPITokenNew legt ein weiteres API-Token an (je App oder Skript eines, mit eigenem Namen); der Klartext
// steht nur in dieser Antwort.
func (a *App) handleAPITokenNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	readJSON(r, &in)
	name := store.Preview(strings.TrimSpace(in.Name), 40)
	if name == "" {
		writeErr(w, 400, "name", "Bitte einen Namen für das Token eingeben (z. B. die App)")
		return
	}
	token := store.NewID() + store.NewID()
	t := store.UserToken{ID: store.NewID(), UserID: a.UserID, Kind: store.TokenAPI, Label: name}
	if err := a.System.DB.AddToken(t, store.Hash(token)); err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	writeJSON(w, 200, map[string]any{"id": t.ID, "name": name, "token": token})
}

func (a *App) handleAPITokenDelete(w http.ResponseWriter, r *http.Request) {
	ok, err := a.System.DB.DeleteToken(a.UserID, store.TokenAPI, r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "speicher", "Datenbankfehler")
		return
	}
	if !ok {
		writeErr(w, 404, "fehlt", "Token nicht gefunden")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
