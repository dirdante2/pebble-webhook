package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/web"
)

// Handler baut alle Routen: Webhook, API der Web-App, statische PWA.
func (s *System) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook", s.handleHook)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("GET /api/sitzung", s.handleSession)
	mux.HandleFunc("POST /api/auth/einrichten/start", s.public(s.handleEnrollStart))
	mux.HandleFunc("POST /api/auth/einrichten/ende", s.public(s.handleEnrollFinish))
	mux.HandleFunc("POST /api/auth/code", s.public(s.handleDeviceCodeRedeem))
	mux.HandleFunc("POST /api/auth/anmelden/start", s.public(s.handleLoginStart))
	mux.HandleFunc("POST /api/auth/anmelden/ende", s.public(s.handleLoginFinish))
	mux.HandleFunc("POST /api/auth/abmelden", s.public(s.handleLogout))

	// u: Web-App des angemeldeten Nutzers; m: zusätzlich Maschinenzugang (API-Token des Nutzers) – nur Lesen,
	// Einsortieren, Abhaken; ad: Systemverwaltung (nur Admin).
	u, m, ad := s.user, s.machine, s.admin
	uf := func(f func(*App) http.HandlerFunc) http.HandlerFunc {
		return u(func(a *App, w http.ResponseWriter, r *http.Request) { f(a)(w, r) })
	}
	mux.HandleFunc("POST /api/listenpunkte/{id}/erledigt", m((*App).handleItemDone))
	// MCP: API-Token oder OAuth (Connectoren in Claude, ChatGPT …).
	mux.HandleFunc("POST /mcp", s.mcpAuth((*App).handleMCP))
	mux.HandleFunc("GET /mcp", s.mcpAuth(func(_ *App, w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) }))
	for _, p := range []string{"/mcp", "/oauth/token", "/oauth/register", "/oauth/revoke", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		mux.HandleFunc("OPTIONS "+p, s.handleOAuthOptions)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleAuthServerMeta)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.handleAuthServerMeta)
	mux.HandleFunc("POST /oauth/register", s.handleOAuthRegister)
	mux.HandleFunc("GET /oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", s.handleOAuthToken)
	mux.HandleFunc("POST /oauth/revoke", s.handleOAuthRevoke)
	mux.HandleFunc("GET /api/oauth/anfrage", u((*App).handleOAuthRequest))
	mux.HandleFunc("POST /api/oauth/freigabe", u((*App).handleOAuthDecide))
	mux.HandleFunc("GET /api/oauth/apps", u((*App).handleOAuthGrants))
	mux.HandleFunc("DELETE /api/oauth/apps/{id}", u((*App).handleOAuthGrantDelete))
	mux.HandleFunc("GET /api/verlauf", u((*App).handleHistory))
	mux.HandleFunc("POST /api/eintraege", m((*App).handleExternCreate))
	mux.HandleFunc("GET /api/eintraege/extern", m((*App).handleExternKnown))
	mux.HandleFunc("GET /api/eintraege/{id}", m((*App).handleJob))
	mux.HandleFunc("GET /api/eintraege/{id}/audio", u((*App).handleAudio))
	mux.HandleFunc("POST /api/eintraege/{id}/neustart", u((*App).handleRestart))
	mux.HandleFunc("POST /api/eintraege/{id}/umhaengen", u((*App).handleReassign))
	mux.HandleFunc("POST /api/eintraege/{id}/liste", m((*App).handleJobToList))
	mux.HandleFunc("POST /api/eintraege/{id}/antwort", u((*App).handleJobAnswer))
	mux.HandleFunc("POST /api/eintraege/{id}/anweisung", u((*App).handleJobInstruction))
	mux.HandleFunc("POST /api/planungen/{id}/liste", m((*App).handlePlanningToList))
	mux.HandleFunc("DELETE /api/eintraege/{id}", u((*App).handleDeleteJob))
	mux.HandleFunc("DELETE /api/fehler", u((*App).handleDeleteFailed))
	mux.HandleFunc("POST /api/verlauf/loeschen", u((*App).handleDeleteMany))
	mux.HandleFunc("GET /api/papierkorb", u((*App).handleTrash))
	mux.HandleFunc("POST /api/papierkorb/{typ}/{id}/wiederherstellen", u((*App).handleTrashRestore))
	mux.HandleFunc("POST /api/papierkorb/{typ}/{id}/loeschen", u((*App).handleTrashPurge))
	mux.HandleFunc("POST /api/eintraege/{id}/gelesen", uf(func(a *App) http.HandlerFunc { return a.handleMarkRead("job") }))
	mux.HandleFunc("POST /api/eintraege/{id}/erledigt", u((*App).handleJobDone))
	mux.HandleFunc("POST /api/eintraege/{id}/termin", u((*App).handleReschedule))
	mux.HandleFunc("POST /api/planungen/{id}/gelesen", uf(func(a *App) http.HandlerFunc { return a.handleMarkRead("planung") }))
	mux.HandleFunc("GET /api/listen", m((*App).handleLists))
	mux.HandleFunc("GET /api/listen/{id}", m((*App).handleList))
	mux.HandleFunc("DELETE /api/listen/{id}", u((*App).handleListDelete))
	mux.HandleFunc("PUT /api/listen/{id}", u((*App).handleListEdit))
	mux.HandleFunc("PUT /api/gruppen", u((*App).handleGroupEdit))
	mux.HandleFunc("POST /api/gruppen/loeschen", u((*App).handleGroupDelete))
	mux.HandleFunc("POST /api/listenpunkte/{id}/haken", u((*App).handleItemToggle))
	mux.HandleFunc("POST /api/listenpunkte/{id}/verschieben", u((*App).handleItemMove))
	mux.HandleFunc("DELETE /api/listenpunkte/{id}", u((*App).handleItemDelete))
	mux.HandleFunc("GET /api/planungen/offen", u((*App).handleOpenPlannings))
	mux.HandleFunc("GET /api/planungen/{id}", m((*App).handlePlanning))
	mux.HandleFunc("POST /api/planungen", u((*App).handleAnswer))
	mux.HandleFunc("POST /api/notizen", u((*App).handleWebNote))
	mux.HandleFunc("POST /api/planungen/{id}/antwort", u((*App).handleAnswer))
	mux.HandleFunc("POST /api/planungen/{id}/schliessen", u((*App).handleClose))
	mux.HandleFunc("POST /api/planungen/{id}/oeffnen", u((*App).handleReopen))
	mux.HandleFunc("GET /api/suche", u((*App).handleSearch))
	mux.HandleFunc("GET /api/status", u((*App).handleStatus))
	mux.HandleFunc("GET /api/status/system", ad(s.handleSystem))
	mux.HandleFunc("GET /api/einstellungen", ad(s.handleSettings))
	mux.HandleFunc("PUT /api/einstellungen", ad(s.handleSettingsSave))
	mux.HandleFunc("GET /api/update", ad(s.handleUpdate))
	mux.HandleFunc("POST /api/update", ad(s.handleUpdateInstall))
	mux.HandleFunc("POST /api/update/zurueck", ad(s.handleUpdateRestore))
	mux.HandleFunc("POST /api/update/auto", ad(s.handleUpdateAuto))
	mux.HandleFunc("POST /api/update/kanal", ad(s.handleUpdateChannel))
	mux.HandleFunc("POST /api/backups", ad(s.handleBackupCreate))
	mux.HandleFunc("POST /api/backups/hochladen", ad(s.handleBackupUpload))
	mux.HandleFunc("POST /api/speicher/aufraeumen", ad(s.handleCleanup))
	mux.HandleFunc("GET /api/audio-unbekannt", u((*App).handleAudioOrphans))
	mux.HandleFunc("POST /api/audio-unbekannt/import", u((*App).handleAudioImport))
	mux.HandleFunc("GET /api/backups/{name}", ad(s.handleBackupDownload))
	mux.HandleFunc("DELETE /api/backups/{name}", ad(s.handleBackupDelete))
	mux.HandleFunc("POST /api/backups/aufraeumen", ad(s.handleBackupPrune))
	mux.HandleFunc("GET /api/pebble", u((*App).handlePebbleSetup))
	mux.HandleFunc("GET /api/kosten", u((*App).handleCosts))
	mux.HandleFunc("GET /api/gedaechtnis", u((*App).handleMemory))
	mux.HandleFunc("GET /api/zaehler", u((*App).handleCounters))
	mux.HandleFunc("POST /api/version/bestaetigt", u((*App).handleVersionConfirm))
	mux.HandleFunc("GET /api/ring", u((*App).handleRing))
	mux.HandleFunc("POST /api/ring/tauschen", u((*App).handleRingSwap))
	mux.HandleFunc("PUT /api/ring/{nr}", u((*App).handleRingEdit))
	mux.HandleFunc("POST /api/ring/bestaetigt", u((*App).handleRingConfirm))
	mux.HandleFunc("PUT /api/eintraege/{id}/wortlaut", u((*App).handleTranscriptEdit))
	mux.HandleFunc("POST /api/gedaechtnis", u((*App).handleMemoryAdd))
	mux.HandleFunc("PUT /api/gedaechtnis/{id}", u((*App).handleMemoryEdit))
	mux.HandleFunc("POST /api/gedaechtnis/{id}", u((*App).handleMemoryAction))
	mux.HandleFunc("POST /api/gedaechtnis/lernen", u((*App).handleTrain))
	mux.HandleFunc("GET /api/tags", u((*App).handleTags))
	mux.HandleFunc("GET /api/darstellung", u((*App).handleDarstellung))
	mux.HandleFunc("POST /api/besuch", u((*App).handleBesuch))
	mux.HandleFunc("POST /api/joplin/pruefen", u((*App).handleJoplinCheck))
	mux.HandleFunc("GET /api/kalender", u((*App).handleCalendar))
	mux.HandleFunc("POST /api/kalender/neu", u((*App).handleCalendarNewLink))
	// Kalender-Abo (iCal): ohne Sitzung, geschützt durch den geheimen Link.
	mux.HandleFunc("GET /kalender/{datei}", s.handleCalendarFeed)
	mux.HandleFunc("PUT /api/darstellung", u((*App).handleDarstellungSave))
	mux.HandleFunc("DELETE /api/tags/{name}", u((*App).handleDeleteTag))
	mux.HandleFunc("POST /api/eintraege/{id}/tags", uf(func(a *App) http.HandlerFunc { return a.handleSetTags("job") }))
	mux.HandleFunc("POST /api/planungen/{id}/tags", uf(func(a *App) http.HandlerFunc { return a.handleSetTags("planung") }))
	mux.HandleFunc("POST /api/eintraege/{id}/tags/vorschlag", uf(func(a *App) http.HandlerFunc { return a.handleTagSuggestion("job") }))
	mux.HandleFunc("POST /api/planungen/{id}/tags/vorschlag", uf(func(a *App) http.HandlerFunc { return a.handleTagSuggestion("planung") }))
	mux.HandleFunc("GET /api/ereignisse", u((*App).handleEvents))
	mux.HandleFunc("GET /api/geraete", u((*App).handleDevices))
	mux.HandleFunc("DELETE /api/geraete/{id}", u((*App).handleDeviceDelete))
	mux.HandleFunc("POST /api/geraete/link", u((*App).handleDeviceLink))
	mux.HandleFunc("POST /api/geraete/code", u((*App).handleDeviceCode))
	mux.HandleFunc("POST /api/geraete/code/halten", u((*App).handleDeviceCodeKeep))
	mux.HandleFunc("DELETE /api/geraete/code", u((*App).handleDeviceCodeDrop))
	mux.HandleFunc("GET /api/push/schluessel", u((*App).handlePushKey))
	mux.HandleFunc("POST /api/push/abo", u((*App).handlePushSubscribe))
	mux.HandleFunc("POST /api/push/abo/entfernen", u((*App).handlePushUnsubscribe))
	mux.HandleFunc("POST /api/push/test", u((*App).handlePushTest))

	// Nutzerverwaltung (Admin) und Maschinenzugang des Nutzers.
	mux.HandleFunc("GET /api/admin/nutzer", ad(s.handleUsers))
	mux.HandleFunc("GET /api/admin/nutzer/{id}", ad(s.handleUser))
	mux.HandleFunc("POST /api/admin/nutzer/einladung", ad(s.handleInvite))
	mux.HandleFunc("PUT /api/admin/nutzer/{id}", ad(s.handleUserEdit))
	mux.HandleFunc("DELETE /api/admin/nutzer/{id}", ad(s.handleUserDelete))
	mux.HandleFunc("GET /api/einstellungen/persoenlich", u((*App).handlePersonalSettings))
	mux.HandleFunc("PUT /api/einstellungen/persoenlich", u((*App).handlePersonalSettingsSave))
	mux.HandleFunc("GET /api/zugang/api", u((*App).handleAPIToken))
	mux.HandleFunc("POST /api/zugang/api", u((*App).handleAPITokenNew))
	mux.HandleFunc("DELETE /api/zugang/api", u((*App).handleAPITokenDelete))

	// Gesperrte Daten: mit dem Wiederherstellungscode öffnen; Code nach Einrichtung/Umzug anzeigen und bestätigen.
	mux.HandleFunc("POST /api/wiederherstellen", s.signedIn(s.handleRecover))
	mux.HandleFunc("GET /api/wiederherstellungscode", u((*App).handleRecoveryCode))
	mux.HandleFunc("POST /api/wiederherstellungscode/notiert", u((*App).handleRecoveryConfirm))
	mux.HandleFunc("POST /api/wiederherstellungscode/neu", u((*App).handleRecoveryNew))

	// Hülle in der Sprache des angemeldeten Nutzers (sonst der des Systems).
	mux.HandleFunc("/", staticHandler(func(r *http.Request) string {
		if sess, ok := s.sessionOf(r); ok {
			if a, _ := s.app(sess.uid); a != nil {
				return a.Cfg.Language
			}
		}
		return i18n.Lang()
	}))

	return securityHeaders(apiCache(mux))
}

// securityHeaders setzt die Sicherheits-Header aller Antworten (Dienst und Ersteinrichtung).
func securityHeaders(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' blob:; frame-src https://ko-fi.com; "+
			"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("Permissions-Policy", "microphone=(self), camera=(), geolocation=()")
		mux.ServeHTTP(w, r)
	})
}

// staticHandler liefert die PWA: Dateien unter static/, alle übrigen GET-Pfade bekommen die Hülle index.html.
// Die Hülle gibt es je Sprache: lang-Attribut, feste Texte und (bei Englisch) das Wörterbuch für app.js.
// Skript und Stil tragen die Version in der Adresse (?v=) und gelten dann ein Jahr unverändert; alles andere
// prüft der Browser mit ETag nach (304 ohne Inhalt). Textdateien liegen fertig gepackt (gzip) bereit.
func staticHandler(langFor func(*http.Request) string) http.HandlerFunc {
	static, _ := fs.Sub(web.Files, "static")
	sum := sha256.New()
	read := func(name string) []byte {
		data, err := fs.ReadFile(static, name)
		if err != nil {
			panic(err)
		}
		sum.Write(data)
		return data
	}
	index, worker, manifest := read("index.html"), read("sw.js"), read("manifest.webmanifest")
	assets := map[string]*staticFile{}
	for _, name := range []string{"app.js", "app.css", "theme.js", "icon.svg", "icon-192.png", "icon-512.png", "kofi-cup.png"} {
		assets[name] = newStaticFile(name, read(name))
	}
	sum.Write(i18n.Dictionary("en"))
	ver := hex.EncodeToString(sum.Sum(nil))[:12]
	index = bytes.ReplaceAll(index, []byte("__V__"), []byte(ver))
	sw := newStaticFile("sw.js", bytes.ReplaceAll(worker, []byte("__V__"), []byte(ver)))
	// Manifest für alle Sprachen gleich: Englisch als Basis, Deutsch unter "translations" (wählt der Browser nach Gerätesprache).
	mani := newStaticFile("manifest.webmanifest", manifest)
	indexes := map[string]*staticFile{}
	for _, lang := range []string{"de", "en"} {
		indexes[lang] = newStaticFile("index.html", localizeShell(index, lang))
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			writeErr(w, 404, "fehlt", "Unbekannter Pfad")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case name == "sw" || name == "sw.js":
			sw.serve(w, r, false)
		case name == "manifest.webmanifest":
			mani.serve(w, r, false)
		case assets[name] != nil:
			assets[name].serve(w, r, r.URL.Query().Get("v") == ver)
		default:
			// Alle übrigen Pfade sind Seiten der Web-App: die Hülle ausliefern.
			indexes[langFor(r)].serve(w, r, false)
		}
	}
}

// staticFile: eingebettete Datei mit Typ, ETag und (bei Text) gepackter Fassung.
type staticFile struct {
	data, gz    []byte
	ctype, etag string
}

func newStaticFile(name string, data []byte) *staticFile {
	h := sha256.Sum256(data)
	f := &staticFile{data: data, ctype: mime.TypeByExtension(path.Ext(name)), etag: `"` + hex.EncodeToString(h[:8]) + `"`}
	switch path.Ext(name) {
	case ".html":
		f.ctype = "text/html; charset=utf-8"
	case ".js":
		f.ctype = "text/javascript; charset=utf-8"
	case ".webmanifest":
		f.ctype = "application/manifest+json"
	}
	if strings.HasPrefix(f.ctype, "text/") || strings.Contains(f.ctype, "json") || strings.Contains(f.ctype, "svg") {
		f.gz = gzipBytes(data)
	}
	return f
}

func (f *staticFile) serve(w http.ResponseWriter, r *http.Request, immutable bool) {
	h := w.Header()
	h.Set("Content-Type", f.ctype)
	h.Set("ETag", f.etag)
	h.Set("Vary", "Accept-Encoding")
	if immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if etagMatch(r, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeMaybeGzip(w, r, f.data, f.gz)
}

// shellTexts: feste Texte in index.html, die je Sprache ersetzt werden.
var shellTexts = []string{"Offline – gezeigt wird der zuletzt geladene Stand. Aktionen werden vorgemerkt.",
	"Alle Einträge durchsuchen …", "Menü öffnen", "Volltextsuche",
	"Nach Tags filtern", "Suche löschen", "Sprachnotizen", "Menü", "Lädt …"}

func localizeShell(src []byte, lang string) []byte {
	s := string(src)
	s = strings.ReplaceAll(s, `lang="__LANG__"`, `lang="`+lang+`"`)
	for _, t := range shellTexts {
		u := i18n.In(lang, t)
		s = strings.ReplaceAll(s, `"`+t+`"`, `"`+u+`"`)
		s = strings.ReplaceAll(s, ">"+t+"<", ">"+u+"<")
	}
	// Wörterbuch zuletzt, damit die Ersetzungen oben es nicht treffen.
	words := strings.ReplaceAll(string(i18n.Dictionary(lang)), "</", `<\/`)
	return []byte(strings.ReplaceAll(s, "__WOERTER__", words))
}
