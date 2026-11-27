package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/store"
)

// Eingänge von außen (Pebble-App, Kalender-Abo, MCP/OAuth, Rücksprung von KI-Anbietern) liegen versioniert unter
// /api/v1/…. Die Einrichtung nennt nur die aktuellen Adressen. Gibt es eine neue Version (/api/v2/…), bleibt die
// vorige gültig: sie kommt mit Serve in legacyRoutes, läuft weiter, und jeder Aufruf wird gezählt – die Web-App
// zeigt dann „Adresse geändert“ mit der neuen Adresse. Die Adressen vor v1 gibt es nicht mehr (Nutzer 27.11.2026).

const (
	pathHook              = "/api/v1/pebble/hook"
	pathCalendar          = "/api/v1/calendar"
	pathMCP               = "/api/v1/mcp"
	pathOAuth             = "/api/v1/oauth"
	pathConnectOpenRouter = "/api/v1/ai/connect/openrouter"
)

// legacyRoute: eine frühere Adresse. Who ermittelt (wenn möglich) den Nutzer, dem der Aufruf gilt (0 = unbekannt:
// Hinweis an die Admins).
type legacyRoute struct {
	Pattern string // Methode und Pfad wie im ServeMux
	Name    string // wofür (Anzeige)
	New     string // neue Adresse (Anzeige, relativ zur öffentlichen Adresse)
	Serve   http.HandlerFunc
	Who     func(s *System, r *http.Request) int64
}

func (s *System) legacyRoutes() []legacyRoute { return legacyList(s) }

// legacyList: die früheren, noch gültigen Versionen (in Tests ersetzbar).
var legacyList = func(s *System) []legacyRoute {
	// Beispiel für eine künftige v2 des Webhooks (v1 läuft weiter und warnt):
	//   {Pattern: "POST /api/v1/pebble/hook", Name: "Pebble-App (Webhook)", New: "/api/v2/pebble/hook",
	//    Serve: s.handleHook, Who: legacyHookUser}
	return nil
}

// legacyHookUser / legacyCalendarUser: Nutzer eines Aufrufs über sein Token (für Hinweise an genau ihn).
func legacyHookUser(s *System, r *http.Request) int64 {
	if t, ok := s.DB.TokenUser(store.TokenHook, r.Header.Get(s.Cfg.HookTokenHeader)); ok {
		return t.UserID
	}
	return 0
}

func legacyCalendarUser(s *System, r *http.Request) int64 {
	tok, _ := strings.CutSuffix(r.PathValue("datei"), ".ics")
	if t, ok := s.DB.TokenUser(store.TokenICal, tok); ok {
		return t.UserID
	}
	return 0
}

func (s *System) registerLegacy(mux *http.ServeMux) {
	for _, lr := range s.legacyRoutes() {
		mux.HandleFunc(lr.Pattern, s.legacyHandler(lr))
	}
}

func (s *System) legacyHandler(lr legacyRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := int64(0)
		if lr.Who != nil {
			uid = lr.Who(s, r)
		}
		s.recordLegacyHit(uid, lr)
		if lr.Serve != nil {
			lr.Serve(w, r)
			return
		}
		logf("Alte Adresse aufgerufen: %s (jetzt %s)", lr.Pattern, lr.New)
		writeErr(w, http.StatusGone, "adresse_geaendert", "Diese Adresse gilt nicht mehr. Neue Adresse: "+strings.TrimRight(s.Cfg.PublicURL, "/")+lr.New)
	}
}

// legacyHit: Aufrufe einer alten Adresse je Nutzer (kv „altadressen“, Schlüssel „<uid>|<Muster>“).
type legacyHit struct {
	UID     int64  `json:"uid"`
	Pattern string `json:"alt"`
	Name    string `json:"name"`
	New     string `json:"neu"`
	Last    int64  `json:"zuletzt"`
	Count   int    `json:"anzahl"`
}

const legacyKV = "altadressen"

var legacyMu sync.Mutex

func (s *System) loadLegacyHits() map[string]legacyHit {
	m := map[string]legacyHit{}
	if v, err := s.DB.Get(legacyKV); err == nil && v != "" {
		json.Unmarshal([]byte(v), &m)
	}
	return m
}

func (s *System) recordLegacyHit(uid int64, lr legacyRoute) {
	legacyMu.Lock()
	defer legacyMu.Unlock()
	m := s.loadLegacyHits()
	key := strconv.FormatInt(uid, 10) + "|" + lr.Pattern
	h := m[key]
	h.UID, h.Pattern, h.Name, h.New, h.Last = uid, lr.Pattern, lr.Name, lr.New, time.Now().UnixMilli()
	h.Count++
	m[key] = h
	b, _ := json.Marshal(m)
	s.DB.Set(legacyKV, string(b))
}

// legacyWindow: so lange nach dem letzten Aufruf bleibt der Hinweis stehen (danach gilt die alte Adresse als ungenutzt).
const legacyWindow = 30 * 24 * time.Hour

// legacyHitsFor: Hinweise für einen Nutzer – seine eigenen, Admins dazu die ohne erkennbaren Nutzer.
func (a *App) legacyHitsFor() []legacyHit {
	admin := false
	if u, err := a.System.DB.User(a.UserID); err == nil {
		admin = u.Admin()
	}
	legacyMu.Lock()
	m := a.System.loadLegacyHits()
	legacyMu.Unlock()
	cut := time.Now().Add(-legacyWindow).UnixMilli()
	known := map[string]bool{}
	for _, lr := range a.System.legacyRoutes() {
		known[lr.Pattern] = true
	}
	out := []legacyHit{}
	for _, h := range m {
		if known[h.Pattern] && h.Last >= cut && (h.UID == a.UserID || (h.UID == 0 && admin)) {
			h.New = strings.TrimRight(a.Cfg.PublicURL, "/") + h.New
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

func (a *App) handleLegacyHits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"alte_adressen": a.legacyHitsFor()})
}

// handleLegacyHitsOK: POST /api/alte-adressen/ok {alt} – Hinweis ausblenden (kommt wieder, wenn die alte Adresse
// erneut aufgerufen wird).
func (a *App) handleLegacyHitsOK(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Alt string `json:"alt"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Werte nicht lesbar")
		return
	}
	admin := false
	if u, err := a.System.DB.User(a.UserID); err == nil {
		admin = u.Admin()
	}
	legacyMu.Lock()
	defer legacyMu.Unlock()
	m := a.System.loadLegacyHits()
	for k, h := range m {
		if h.Pattern == in.Alt && (h.UID == a.UserID || (h.UID == 0 && admin)) {
			delete(m, k)
		}
	}
	b, _ := json.Marshal(m)
	a.System.DB.Set(legacyKV, string(b))
	writeJSON(w, 200, map[string]any{"ok": true})
}

// serveShell liefert die Hülle der Web-App für eine Seite unter einer API-Adresse (der statische Handler gibt für
// /api/… sonst 404).
func serveShell(shell http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/seite"
		shell(w, r2)
	}
}
