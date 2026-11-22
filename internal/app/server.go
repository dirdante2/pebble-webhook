package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/web"
)

// Handler baut alle Routen: Webhook, API der Web-App, statische PWA.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook", a.handleHook)
	mux.HandleFunc("GET /healthz", a.handleHealth)

	mux.HandleFunc("GET /api/sitzung", a.handleSession)
	mux.HandleFunc("POST /api/auth/einrichten/start", a.public(a.handleEnrollStart))
	mux.HandleFunc("POST /api/auth/einrichten/ende", a.public(a.handleEnrollFinish))
	mux.HandleFunc("POST /api/auth/code", a.public(a.handleDeviceCodeRedeem))
	mux.HandleFunc("POST /api/auth/anmelden/start", a.public(a.handleLoginStart))
	mux.HandleFunc("POST /api/auth/anmelden/ende", a.public(a.handleLoginFinish))
	mux.HandleFunc("POST /api/auth/abmelden", a.public(a.handleLogout))

	s := a.requireSession
	// m: auch für den Maschinenzugang (Claude-Skill) – nur Lesen, Einsortieren, Abhaken.
	m := a.machine
	mux.HandleFunc("POST /api/listenpunkte/{id}/erledigt", m(a.handleItemDone))
	// MCP: API-Token oder OAuth (Connectoren in Claude, ChatGPT …).
	mux.HandleFunc("POST /mcp", a.mcpAuth(a.handleMCP))
	mux.HandleFunc("GET /mcp", a.mcpAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusMethodNotAllowed) }))
	for _, p := range []string{"/mcp", "/oauth/token", "/oauth/register", "/oauth/revoke", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		mux.HandleFunc("OPTIONS "+p, a.handleOAuthOptions)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", a.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", a.handleProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", a.handleAuthServerMeta)
	mux.HandleFunc("GET /.well-known/openid-configuration", a.handleAuthServerMeta)
	mux.HandleFunc("POST /oauth/register", a.handleOAuthRegister)
	mux.HandleFunc("GET /oauth/authorize", a.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", a.handleOAuthToken)
	mux.HandleFunc("POST /oauth/revoke", a.handleOAuthRevoke)
	mux.HandleFunc("GET /api/oauth/anfrage", s(a.handleOAuthRequest))
	mux.HandleFunc("POST /api/oauth/freigabe", s(a.handleOAuthDecide))
	mux.HandleFunc("GET /api/oauth/apps", s(a.handleOAuthGrants))
	mux.HandleFunc("DELETE /api/oauth/apps/{id}", s(a.handleOAuthGrantDelete))
	mux.HandleFunc("GET /api/verlauf", s(a.handleHistory))
	mux.HandleFunc("GET /api/eintraege/{id}", m(a.handleJob))
	mux.HandleFunc("GET /api/eintraege/{id}/audio", s(a.handleAudio))
	mux.HandleFunc("POST /api/eintraege/{id}/neustart", s(a.handleRestart))
	mux.HandleFunc("POST /api/eintraege/{id}/umhaengen", s(a.handleReassign))
	mux.HandleFunc("POST /api/eintraege/{id}/liste", m(a.handleJobToList))
	mux.HandleFunc("POST /api/eintraege/{id}/antwort", s(a.handleJobAnswer))
	mux.HandleFunc("POST /api/planungen/{id}/liste", m(a.handlePlanningToList))
	mux.HandleFunc("DELETE /api/eintraege/{id}", s(a.handleDeleteJob))
	mux.HandleFunc("DELETE /api/fehler", s(a.handleDeleteFailed))
	mux.HandleFunc("POST /api/verlauf/loeschen", s(a.handleDeleteMany))
	mux.HandleFunc("GET /api/papierkorb", s(a.handleTrash))
	mux.HandleFunc("POST /api/papierkorb/{typ}/{id}/wiederherstellen", s(a.handleTrashRestore))
	mux.HandleFunc("POST /api/papierkorb/{typ}/{id}/loeschen", s(a.handleTrashPurge))
	mux.HandleFunc("POST /api/eintraege/{id}/gelesen", s(a.handleMarkRead("job")))
	mux.HandleFunc("POST /api/eintraege/{id}/erledigt", s(a.handleJobDone))
	mux.HandleFunc("POST /api/eintraege/{id}/termin", s(a.handleReschedule))
	mux.HandleFunc("POST /api/planungen/{id}/gelesen", s(a.handleMarkRead("planung")))
	mux.HandleFunc("GET /api/listen", m(a.handleLists))
	mux.HandleFunc("GET /api/listen/{id}", m(a.handleList))
	mux.HandleFunc("DELETE /api/listen/{id}", s(a.handleListDelete))
	mux.HandleFunc("PUT /api/listen/{id}", s(a.handleListEdit))
	mux.HandleFunc("PUT /api/gruppen", s(a.handleGroupEdit))
	mux.HandleFunc("POST /api/gruppen/loeschen", s(a.handleGroupDelete))
	mux.HandleFunc("POST /api/listenpunkte/{id}/haken", s(a.handleItemToggle))
	mux.HandleFunc("POST /api/listenpunkte/{id}/verschieben", s(a.handleItemMove))
	mux.HandleFunc("DELETE /api/listenpunkte/{id}", s(a.handleItemDelete))
	mux.HandleFunc("GET /api/planungen/offen", s(a.handleOpenPlannings))
	mux.HandleFunc("GET /api/planungen/{id}", m(a.handlePlanning))
	mux.HandleFunc("POST /api/planungen", s(a.handleAnswer))
	mux.HandleFunc("POST /api/planungen/{id}/antwort", s(a.handleAnswer))
	mux.HandleFunc("POST /api/planungen/{id}/schliessen", s(a.handleClose))
	mux.HandleFunc("POST /api/planungen/{id}/oeffnen", s(a.handleReopen))
	mux.HandleFunc("GET /api/suche", s(a.handleSearch))
	mux.HandleFunc("GET /api/status", s(a.handleStatus))
	mux.HandleFunc("GET /api/einstellungen", s(a.handleSettings))
	mux.HandleFunc("PUT /api/einstellungen", s(a.handleSettingsSave))
	mux.HandleFunc("GET /api/update", s(a.handleUpdate))
	mux.HandleFunc("POST /api/update", s(a.handleUpdateInstall))
	mux.HandleFunc("POST /api/update/zurueck", s(a.handleUpdateRestore))
	mux.HandleFunc("POST /api/update/auto", s(a.handleUpdateAuto))
	mux.HandleFunc("POST /api/update/kanal", s(a.handleUpdateChannel))
	mux.HandleFunc("POST /api/backups", s(a.handleBackupCreate))
	mux.HandleFunc("GET /api/audio-unbekannt", s(a.handleAudioOrphans))
	mux.HandleFunc("POST /api/audio-unbekannt/import", s(a.handleAudioImport))
	mux.HandleFunc("GET /api/backups/{name}", s(a.handleBackupDownload))
	mux.HandleFunc("GET /api/pebble", s(a.handlePebbleSetup))
	mux.HandleFunc("GET /api/kosten", s(a.handleCosts))
	mux.HandleFunc("GET /api/gedaechtnis", s(a.handleMemory))
	mux.HandleFunc("GET /api/zaehler", s(a.handleCounters))
	mux.HandleFunc("PUT /api/eintraege/{id}/wortlaut", s(a.handleTranscriptEdit))
	mux.HandleFunc("POST /api/gedaechtnis", s(a.handleMemoryAdd))
	mux.HandleFunc("PUT /api/gedaechtnis/{id}", s(a.handleMemoryEdit))
	mux.HandleFunc("POST /api/gedaechtnis/{id}", s(a.handleMemoryAction))
	mux.HandleFunc("POST /api/gedaechtnis/lernen", s(a.handleTrain))
	mux.HandleFunc("GET /api/tags", s(a.handleTags))
	mux.HandleFunc("GET /api/darstellung", s(a.handleDarstellung))
	mux.HandleFunc("POST /api/besuch", s(a.handleBesuch))
	mux.HandleFunc("POST /api/joplin/pruefen", s(a.handleJoplinCheck))
	mux.HandleFunc("PUT /api/darstellung", s(a.handleDarstellungSave))
	mux.HandleFunc("DELETE /api/tags/{name}", s(a.handleDeleteTag))
	mux.HandleFunc("POST /api/eintraege/{id}/tags", s(a.handleSetTags("job")))
	mux.HandleFunc("POST /api/planungen/{id}/tags", s(a.handleSetTags("planung")))
	mux.HandleFunc("POST /api/eintraege/{id}/tags/vorschlag", s(a.handleTagSuggestion("job")))
	mux.HandleFunc("POST /api/planungen/{id}/tags/vorschlag", s(a.handleTagSuggestion("planung")))
	mux.HandleFunc("GET /api/ereignisse", s(a.handleEvents))
	mux.HandleFunc("GET /api/geraete", s(a.handleDevices))
	mux.HandleFunc("DELETE /api/geraete/{id}", s(a.handleDeviceDelete))
	mux.HandleFunc("POST /api/geraete/link", s(a.handleDeviceLink))
	mux.HandleFunc("POST /api/geraete/code", s(a.handleDeviceCode))
	mux.HandleFunc("POST /api/geraete/code/halten", s(a.handleDeviceCodeKeep))
	mux.HandleFunc("DELETE /api/geraete/code", s(a.handleDeviceCodeDrop))
	mux.HandleFunc("GET /api/push/schluessel", s(a.handlePushKey))
	mux.HandleFunc("POST /api/push/abo", s(a.handlePushSubscribe))
	mux.HandleFunc("POST /api/push/abo/entfernen", s(a.handlePushUnsubscribe))
	mux.HandleFunc("POST /api/push/test", s(a.handlePushTest))

	mux.HandleFunc("/", staticHandler(func(*http.Request) string { return i18n.Lang() }))

	return securityHeaders(mux)
}

// securityHeaders setzt die Sicherheits-Header aller Antworten (Dienst und Ersteinrichtung).
func securityHeaders(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' blob:; "+
			"object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("Permissions-Policy", "microphone=(self), camera=(), geolocation=()")
		mux.ServeHTTP(w, r)
	})
}

// staticHandler liefert die PWA: Dateien unter static/, alle übrigen GET-Pfade bekommen die Hülle index.html.
// Die Hülle gibt es je Sprache: lang-Attribut, feste Texte und (bei Englisch) das Wörterbuch für app.js.
func staticHandler(langFor func(*http.Request) string) http.HandlerFunc {
	static, _ := fs.Sub(web.Files, "static")
	files := http.FileServerFS(static)
	// Ein Proxy davor kann .js/.css zwischenspeichern und Cache-Control übergehen. Deshalb tragen
	// Skript und Stil eine Version in der Adresse, und der Service Worker liegt ohne Endung unter /sw.
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
	read("app.js")
	read("app.css")
	read("theme.js")
	sum.Write(i18n.Dictionary("en"))
	ver := []byte(hex.EncodeToString(sum.Sum(nil))[:12])
	index = bytes.ReplaceAll(index, []byte("__V__"), ver)
	worker = bytes.ReplaceAll(worker, []byte("__V__"), ver)
	indexes := map[string][]byte{"de": localizeShell(index, "de"), "en": localizeShell(index, "en")}
	manifests := map[string][]byte{"de": localizeShell(manifest, "de"), "en": localizeShell(manifest, "en")}
	serve := func(w http.ResponseWriter, contentType string, data []byte) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			writeErr(w, 404, "fehlt", "Unbekannter Pfad")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "sw" || name == "sw.js" {
			serve(w, "text/javascript; charset=utf-8", worker)
			return
		}
		if name == "manifest.webmanifest" {
			serve(w, "application/manifest+json", manifests[langFor(r)])
			return
		}
		if name != "" && name != "index.html" {
			if f, err := static.Open(name); err == nil {
				f.Close()
				files.ServeHTTP(w, r)
				return
			}
		}
		// Alle übrigen Pfade sind Seiten der Web-App: die Hülle ausliefern.
		serve(w, "text/html; charset=utf-8", indexes[langFor(r)])
	}
}

// shellTexts: feste Texte in index.html und im Manifest, die je Sprache ersetzt werden.
var shellTexts = []string{"Offline – gezeigt wird der zuletzt geladene Stand. Aktionen werden vorgemerkt.",
	"Notizen und Planungen aus Pebble-Aufnahmen", "Alle Einträge durchsuchen …", "Menü öffnen", "Volltextsuche",
	"Nach Tags filtern", "Suche löschen", "Sprachnotizen", "Menü", "Lädt …"}

func localizeShell(src []byte, lang string) []byte {
	s := string(src)
	s = strings.ReplaceAll(s, `lang="__LANG__"`, `lang="`+lang+`"`)
	s = strings.ReplaceAll(s, `"lang": "de"`, `"lang": "`+lang+`"`)
	for _, t := range shellTexts {
		u := i18n.In(lang, t)
		s = strings.ReplaceAll(s, `"`+t+`"`, `"`+u+`"`)
		s = strings.ReplaceAll(s, ">"+t+"<", ">"+u+"<")
	}
	// Wörterbuch zuletzt, damit die Ersetzungen oben es nicht treffen.
	words := strings.ReplaceAll(string(i18n.Dictionary(lang)), "</", `<\/`)
	return []byte(strings.ReplaceAll(s, "__WOERTER__", words))
}
