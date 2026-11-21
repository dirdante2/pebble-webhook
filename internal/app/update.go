package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	pebblewebhook "pebble-webhook"

	"pebble-webhook/internal/i18n"
)

// Update-Prüfung: neuere Versionen aus den GitHub-Releases (UPDATE_REPO) samt Änderungsliste (Release-Text =
// Abschnitt aus CHANGELOG.md). Installiert wird über den Updater des Installers: die App schreibt eine Anfrage
// nach DATA_DIR/update-request, die Path-Unit pebble-webhook-update startet install.sh --apply-request als root.

const updaterPath = "/usr/local/lib/pebble-webhook/install.sh"

// githubAPI ist in Tests ersetzbar.
var githubAPI = "https://api.github.com"

type release struct {
	Version string `json:"version"`
	Channel string `json:"kanal"` // stable | beta | nightly
	Date    string `json:"datum"`
	Notes   string `json:"aenderungen"`
	URL     string `json:"url"`

	Published string `json:"veroeffentlicht,omitempty"` // Zeitpunkt (RFC 3339), zum Sortieren
}

type updateCache struct {
	mu       sync.Mutex
	at       time.Time
	repo     string
	releases []release // neueste zuerst
	err      string
	channel  string // aktiver Kanal; leer = aus der Einstellung
}

// updateInterval: so oft prüft der Dienst im Hintergrund auf neue Versionen.
const updateInterval = 4 * time.Hour

// Versionen: X.Y.Z (stable), X.Y.Z-beta.N, X.Y.Z-nightly.N (N = Zeitstempel des Builds).
var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(beta|nightly)\.(\d+))?$`)

// minRollback: ältere Versionen lesen die Einstellungen noch aus der env-Datei.
var minRollback = version{1, 14, 0, 2, 0}

// version ist sortierbar: Basis, dann Rang (nightly < beta < stable), dann Nummer der Vorabversion.
type version [5]int

var channelRank = map[string]int{"nightly": 0, "beta": 1, "": 2}

// parseVersion zerlegt „1.15.0-beta.2“ (auch „1.13.1 (Build …)“); ok=false bei „dev“ u. ä.
func parseVersion(v string) (out version, channel string, ok bool) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return out, "", false
	}
	m := semverRe.FindStringSubmatch(f[0])
	if m == nil {
		return out, "", false
	}
	for i := range 3 {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	out[3] = channelRank[m[4]]
	out[4], _ = strconv.Atoi(m[5])
	channel = m[4]
	if channel == "" {
		channel = "stable"
	}
	return out, channel, true
}

func (a version) less(b version) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// updateChannel liefert den aktiven Kanal (in der App umschaltbar, ohne Neustart).
func (a *App) updateChannel() string {
	a.updates.mu.Lock()
	defer a.updates.mu.Unlock()
	if a.updates.channel == "" {
		a.updates.channel = a.Cfg.UpdateChannel
	}
	return a.updates.channel
}

// channelRelease ist eine Version des gewählten Kanals; installiert = sie läuft gerade.
type channelRelease struct {
	release
	Installed bool `json:"installiert"`
}

// channelList: Versionen eines Kanals, neueste Veröffentlichung zuerst (die Nummern der Kanäle hängen nicht
// zusammen – eine stable kann eine höhere Nummer haben als die laufende nightly und umgekehrt).
func (a *App) channelList(releases []release, channel string) []channelRelease {
	cur, curChannel, curOK := parseVersion(a.Version)
	current := strings.Fields(a.Version + " ")[0]
	list := []channelRelease{}
	for _, rel := range releases {
		if rel.Channel != channel {
			continue
		}
		v, _, _ := parseVersion(rel.Version)
		if v.less(minRollback) {
			continue
		}
		// Ein eigener Build ohne Kanal-Endung gilt als die Kanal-Version derselben Nummer.
		same := rel.Version == current || (curOK && curChannel == "stable" && rel.Channel != "stable" && v[0] == cur[0] && v[1] == cur[1] && v[2] == cur[2])
		list = append(list, channelRelease{release: rel, Installed: same})
	}
	sort.SliceStable(list, func(i, k int) bool { return list[i].Published > list[k].Published })
	return list
}

// channelView: Versionen des gewählten Kanals; empfohlen ist die neueste, sofern sie nicht schon läuft.
func (a *App) channelView(releases []release) (list []channelRelease, recommended string, installedChannel string) {
	_, curChannel, _ := parseVersion(a.Version)
	list = a.channelList(releases, a.updateChannel())
	if len(list) > 0 && !list[0].Installed {
		recommended = list[0].Version
	}
	return list, recommended, curChannel
}

// savedState ist ein Backup vor einem Update oder Versionswechsel: Programm und Datenbank der Version, die
// damals lief, mit ihrem Kanal (stable/beta/nightly, lokal = eigener Build).
type savedState struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Channel  string `json:"kanal"`
	At       int64  `json:"zeit"`
	Complete bool   `json:"vollstaendig"` // mit Programm (ab 1.27.4); ältere Sicherungen haben nur die Datenbank
}

var backupNameRe = regexp.MustCompile(`^pebble-([0-9A-Za-z.+-]+)-(\d{8}-\d{6})$`)

// savedStates liest DATA_DIR/backup (neueste zuerst); angelegt vom Installer vor jedem Wechsel.
func (a *App) savedStates() []savedState {
	out := []savedState{}
	entries, _ := os.ReadDir(filepath.Join(a.Cfg.DataDir, "backup"))
	for _, e := range entries {
		m := backupNameRe.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		at, err := time.ParseInLocation("20060102-150405", m[2], time.Local)
		if err != nil {
			continue
		}
		dir := filepath.Join(a.Cfg.DataDir, "backup", e.Name())
		_, errBin := os.Stat(filepath.Join(dir, "pebble-webhook"))
		_, errDB := os.Stat(filepath.Join(dir, "pebble.db"))
		channel := ""
		if b, err := os.ReadFile(filepath.Join(dir, "kanal")); err == nil {
			channel = strings.TrimSpace(string(b))
		} else if _, ch, ok := parseVersion(m[1]); ok && ch != "stable" {
			channel = ch // Vorabversion: am Namen erkennbar; ohne Endung bleibt es offen
		}
		out = append(out, savedState{Name: e.Name(), Version: m[1], Channel: channel, At: at.UnixMilli(), Complete: errBin == nil && errDB == nil})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].At > out[k].At })
	return out
}

// backupRetention: Backups älter als das löscht die tägliche Bereinigung (und der Installer beim nächsten Update).
const backupRetention = 30 * 24 * time.Hour

// cleanBackups löscht Backups älter als backupRetention; das jüngste bleibt immer.
func (a *App) cleanBackups() {
	states := a.savedStates()
	for i, st := range states {
		if i == 0 || time.Since(time.UnixMilli(st.At)) < backupRetention {
			continue
		}
		if err := os.RemoveAll(filepath.Join(a.Cfg.DataDir, "backup", st.Name)); err != nil {
			logf("Backups: %s: %v", st.Name, err)
			continue
		}
		logf("Backups: %s gelöscht (älter als 30 Tage)", st.Name)
	}
}

// handleUpdateRestore: zurück zu einem gespeicherten Stand (Programm und Datenbank) über den Updater.
func (a *App) handleUpdateRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if readJSON(r, &in) != nil || !backupNameRe.MatchString(in.Name) {
		writeErr(w, 400, "ungueltig", "Stand fehlt")
		return
	}
	if !updateInstallable() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", "Kein Updater installiert – Installation mit install.sh nötig")
		return
	}
	found := false
	for _, st := range a.savedStates() {
		found = found || (st.Name == in.Name && st.Complete)
	}
	if !found {
		writeErr(w, 404, "fehlt", "Stand nicht gefunden oder ohne Programm")
		return
	}
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-status"), []byte("requested restore "+in.Name+"\n"), 0o640)
	if err := os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-request"), []byte("restore "+in.Name+"\n"), 0o640); err != nil {
		writeErr(w, 500, "speicher", "Anfrage nicht schreibbar")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// AvailableUpdate: empfohlene Version des Kanals aus der letzten Prüfung, ohne GitHub zu fragen ("" = keine).
func (a *App) AvailableUpdate() string {
	a.updates.mu.Lock()
	releases := a.updates.releases
	a.updates.mu.Unlock()
	_, rec, _ := a.channelView(releases)
	return rec
}

// CheckUpdates läuft im Hintergrund: beim Start und dann alle updateInterval.
func (a *App) CheckUpdates(ctx context.Context) {
	if a.Cfg.UpdateRepo == "" {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.fetchReleases(ctx, false) // gespeicherten Stand laden
		a.updates.mu.Lock()
		due := time.Since(a.updates.at) >= updateInterval || a.updates.repo != a.Cfg.UpdateRepo
		a.updates.mu.Unlock()
		if !due {
			t.Reset(time.Minute * 10)
			continue
		}
		if _, err := a.fetchReleases(ctx, true); err == "" {
			if v := a.AvailableUpdate(); v != "" {
				logf("Update verfügbar: %s (Kanal %s)", v, a.updateChannel())
			}
		}
		t.Reset(updateInterval)
	}
}

// updateCacheKey: letzter Stand der Releases in der kv-Tabelle, damit ein Neustart nicht erneut GitHub fragt.
const updateCacheKey = "update-releases"

type storedReleases struct {
	At       int64     `json:"at"`
	Repo     string    `json:"repo"`
	Releases []release `json:"releases"`
}

// fetchReleases liefert die zwischengespeicherten Releases; GitHub wird nur mit force gefragt (Prüfung im
// Hintergrund alle updateInterval oder „Jetzt prüfen“) – oder einmal, wenn noch gar nichts gespeichert ist.
func (a *App) fetchReleases(ctx context.Context, force bool) ([]release, string) {
	c := &a.updates
	c.mu.Lock()
	defer c.mu.Unlock()
	repo := a.Cfg.UpdateRepo
	if c.at.IsZero() {
		if raw, err := a.DB.Get(updateCacheKey); err == nil && raw != "" {
			var st storedReleases
			if json.Unmarshal([]byte(raw), &st) == nil && st.Repo == repo {
				c.at, c.repo, c.releases = time.UnixMilli(st.At), st.Repo, st.Releases
			}
		}
	}
	if !force && c.repo == repo && !c.at.IsZero() {
		return c.releases, c.err
	}
	previous := c.releases
	if c.repo != repo {
		previous = nil
	}
	c.at, c.repo, c.releases, c.err = time.Now(), repo, previous, ""
	fresh := []release{}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", githubAPI+"/repos/"+repo+"/releases?per_page=100", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pebble-webhook/"+strings.Fields(a.Version + " ")[0])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.err = i18n.T("GitHub nicht erreichbar")
		return c.releases, c.err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		c.err = i18n.Tf("Keine Versionen gefunden (%s)", repo)
		return c.releases, c.err
	}
	if resp.StatusCode != 200 {
		c.err = i18n.Tf("GitHub antwortet %d", resp.StatusCode)
		return c.releases, c.err
	}
	var raw []struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		URL        string `json:"html_url"`
		Published  string `json:"published_at"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		c.err = i18n.T("Antwort von GitHub nicht lesbar")
		return c.releases, c.err
	}
	for _, r := range raw {
		if r.Draft {
			continue
		}
		_, channel, ok := parseVersion(r.Tag)
		if !ok {
			continue
		}
		date := r.Published
		if len(date) >= 10 {
			date = date[:10]
		}
		fresh = append(fresh, release{Version: strings.TrimPrefix(r.Tag, "v"), Channel: channel, Date: date, Notes: r.Body, URL: r.URL, Published: r.Published})
	}
	sort.SliceStable(fresh, func(i, k int) bool { return fresh[i].Published > fresh[k].Published })
	c.releases = fresh
	if b, err := json.Marshal(storedReleases{At: c.at.UnixMilli(), Repo: repo, Releases: fresh}); err == nil {
		if err := a.DB.Set(updateCacheKey, string(b)); err != nil {
			logf("Update-Cache: %v", err)
		}
	}
	return c.releases, ""
}

// updateInstallable: der Updater des Installers liegt bereit (Installation über install.sh).
func updateInstallable() bool {
	_, err := os.Stat(updaterPath)
	return err == nil
}

func (a *App) updateStatus() string {
	b, _ := os.ReadFile(filepath.Join(a.Cfg.DataDir, "update-status"))
	return strings.TrimSpace(string(b))
}

// changelogSection: Abschnitt „## [X.Y.Z]“ aus CHANGELOG.md (bei Vorabversionen der ihrer Basis), ohne Überschrift.
func changelogSection(changelog, ver string) string {
	v, _, ok := parseVersion(ver)
	if !ok {
		return ""
	}
	head := fmt.Sprintf("## [%d.%d.%d]", v[0], v[1], v[2])
	i := strings.Index(changelog, "\n"+head)
	if i < 0 {
		return ""
	}
	rest := changelog[i+1:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		return ""
	}
	if j := strings.Index(rest, "\n## ["); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// noteSection ist ein Abschnitt „## [Version] - Datum“ aus CHANGELOG.md oder RELEASES.md.
type noteSection struct {
	Version string `json:"version"`
	Datum   string `json:"datum"`
	Text    string `json:"text"`
}

var noteHead = regexp.MustCompile(`(?m)^## \[([^\]]+)\][ \t]*(?:[-–][ \t]*(.*))?$`)

// noteSections zerlegt eine Notizdatei in ihre Versionsabschnitte (Reihenfolge wie in der Datei, neueste oben).
func noteSections(text string) []noteSection {
	idx := noteHead.FindAllStringSubmatchIndex(text, -1)
	out := make([]noteSection, 0, len(idx))
	for i, m := range idx {
		end := len(text)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		sec := noteSection{Version: text[m[2]:m[3]], Text: strings.TrimSpace(text[m[1]:end])}
		if m[4] >= 0 {
			sec.Datum = strings.TrimSpace(text[m[4]:m[5]])
		}
		out = append(out, sec)
	}
	return out
}

// releaseNotes: Änderungen der laufenden Version und die früheren, passend zum Kanal. nightly zeigt jede Version
// (CHANGELOG.md); beta und stable die Zusammenfassungen (RELEASES.md), damit Zwischenschritte nicht stören.
func releaseNotes(current, channel string) (now string, earlier []noteSection, source string) {
	cur, _, okCur := parseVersion(current)
	pick := func(text string) {
		earlier = earlier[:0]
		for _, s := range noteSections(text) {
			v, _, ok := parseVersion(s.Version)
			if !ok || !okCur {
				continue
			}
			// Vorabversionen (1.26.1-nightly.N) gehören zum Abschnitt ihrer Nummer (## [1.26.1]).
			if s.Version == current || (v[0] == cur[0] && v[1] == cur[1] && v[2] == cur[2] && (v == cur || cur[3] < 2) && now == "") {
				if now == "" {
					now = s.Text
				}
				continue
			}
			if v.less(cur) && len(earlier) < 60 {
				earlier = append(earlier, s)
			}
		}
	}
	if channel != "nightly" {
		pick(pebblewebhook.Releases)
		if now != "" || len(earlier) > 0 {
			return now, earlier, "releases"
		}
	}
	now = ""
	pick(pebblewebhook.Changelog)
	return now, earlier, "changelog"
}

func (a *App) handleUpdate(w http.ResponseWriter, r *http.Request) {
	current := strings.Fields(a.Version + " ")[0]
	now, _, source := releaseNotes(current, a.updateChannel())
	allNotes := ""
	if a.Cfg.UpdateRepo != "" {
		file := "CHANGELOG.md"
		if source == "releases" {
			file = "RELEASES.md"
		}
		allNotes = "https://github.com/" + a.Cfg.UpdateRepo + "/blob/main/" + file
	}
	out := map[string]any{"aktuell": current, "quelle": a.Cfg.UpdateRepo, "kanal": a.updateChannel(), "kanaele": []string{"stable", "beta", "nightly"},
		"installierbar": updateInstallable(), "status": a.updateStatus(), "versionen": []channelRelease{},
		"mindestens": "1.14.0", "staende": a.savedStates(), "aktuell_aenderungen": now, "alle_aenderungen": allNotes}
	if a.Cfg.UpdateRepo == "" {
		out["fehler"] = i18n.T("Update-Prüfung ist aus (Einstellung Update-Quelle)")
		writeJSON(w, 200, out)
		return
	}
	releases, errMsg := a.fetchReleases(r.Context(), r.URL.Query().Get("pruefen") == "1")
	if errMsg != "" {
		out["fehler"] = errMsg
	}
	a.updates.mu.Lock()
	out["geprueft"] = a.updates.at.UnixMilli()
	a.updates.mu.Unlock()
	list, rec, curChannel := a.channelView(releases)
	if len(list) > 3 {
		list = list[:3] // die letzten 3 des gewählten Kanals
	}
	out["versionen"], out["aktuell_kanal"] = list, curChannel
	if rec != "" {
		out["neueste"] = rec
	}
	writeJSON(w, 200, out)
}

// handleUpdateChannel wechselt den Kanal: gespeichert in den Einstellungen, wirksam sofort (ohne Neustart).
func (a *App) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kanal string `json:"kanal"`
	}
	if readJSON(r, &in) != nil || (in.Kanal != "stable" && in.Kanal != "beta" && in.Kanal != "nightly") {
		writeErr(w, 400, "ungueltig", "Unbekannter Kanal")
		return
	}
	if err := a.DB.SaveSettings(map[string]string{"UPDATE_CHANNEL": in.Kanal}); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	a.updates.mu.Lock()
	a.updates.channel = in.Kanal
	a.updates.mu.Unlock()
	a.handleUpdate(w, r)
}

func (a *App) handleUpdateInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string `json:"version"`
	}
	if readJSON(r, &in) != nil || !semverRe.MatchString(in.Version) {
		writeErr(w, 400, "ungueltig", "Version fehlt")
		return
	}
	if v, _, _ := parseVersion(in.Version); v.less(minRollback) {
		writeErr(w, http.StatusConflict, "zu_alt", "Versionen vor 1.14.0 lesen die Einstellungen nicht aus der Datenbank")
		return
	}
	if a.Cfg.UpdateRepo == "" || !updateInstallable() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", "Kein Updater installiert – Installation mit install.sh nötig")
		return
	}
	releases, _ := a.fetchReleases(r.Context(), false)
	known := false
	for _, rel := range releases {
		known = known || rel.Version == strings.TrimPrefix(in.Version, "v")
	}
	if !known {
		writeErr(w, 404, "fehlt", "Version nicht gefunden")
		return
	}
	line := a.Cfg.UpdateRepo + " " + strings.TrimPrefix(in.Version, "v") + "\n"
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-status"), []byte("requested "+strings.TrimPrefix(in.Version, "v")+"\n"), 0o640)
	if err := os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-request"), []byte(line), 0o640); err != nil {
		writeErr(w, 500, "speicher", "Anfrage nicht schreibbar")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
