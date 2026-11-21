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

// channelAllows: es zählt nur der aktive Kanal.
func channelAllows(setting, channel string) bool {
	return setting == channel
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

// newerReleases: Versionen des aktiven Kanals, die neuer sind als die laufende (neueste zuerst).
func (a *App) newerReleases(releases []release) (newer, older []release) {
	channel := a.updateChannel()
	cur, _, curOK := parseVersion(a.Version)
	newer, older = []release{}, []release{}
	for _, rel := range releases {
		if !channelAllows(channel, rel.Channel) {
			continue
		}
		v, _, _ := parseVersion(rel.Version)
		switch {
		case !curOK || cur.less(v):
			newer = append(newer, rel)
		case v.less(cur) && !v.less(minRollback):
			older = append(older, rel)
		}
	}
	return newer, older
}

// AvailableUpdate: neueste Version des Kanals aus der letzten Prüfung, ohne GitHub zu fragen ("" = keine).
func (a *App) AvailableUpdate() string {
	a.updates.mu.Lock()
	releases := a.updates.releases
	a.updates.mu.Unlock()
	if newer, _ := a.newerReleases(releases); len(newer) > 0 {
		return newer[0].Version
	}
	return ""
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
		if _, err := a.fetchReleases(ctx, true); err == "" {
			if v := a.AvailableUpdate(); v != "" {
				logf("Update verfügbar: %s (Kanal %s)", v, a.updateChannel())
			}
		}
		t.Reset(updateInterval)
	}
}

// fetchReleases holt die Releases (höchstens einmal je Stunde, mit force sofort).
func (a *App) fetchReleases(ctx context.Context, force bool) ([]release, string) {
	c := &a.updates
	c.mu.Lock()
	defer c.mu.Unlock()
	repo := a.Cfg.UpdateRepo
	if !force && c.repo == repo && time.Since(c.at) < updateInterval {
		return c.releases, c.err
	}
	c.at, c.repo, c.releases, c.err = time.Now(), repo, nil, ""
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", githubAPI+"/repos/"+repo+"/releases?per_page=100", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pebble-webhook/"+strings.Fields(a.Version + " ")[0])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.err = i18n.T("GitHub nicht erreichbar")
		return nil, c.err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		c.err = i18n.Tf("Keine Versionen gefunden (%s)", repo)
		return nil, c.err
	}
	if resp.StatusCode != 200 {
		c.err = i18n.Tf("GitHub antwortet %d", resp.StatusCode)
		return nil, c.err
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
		return nil, c.err
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
		c.releases = append(c.releases, release{Version: strings.TrimPrefix(r.Tag, "v"), Channel: channel, Date: date, Notes: r.Body, URL: r.URL})
	}
	sort.SliceStable(c.releases, func(i, k int) bool {
		vi, _, _ := parseVersion(c.releases[i].Version)
		vk, _, _ := parseVersion(c.releases[k].Version)
		return vk.less(vi)
	})
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
			if s.Version == current || (v == cur && now == "") {
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
	now, earlier, source := releaseNotes(current, a.updateChannel())
	out := map[string]any{"aktuell": current, "quelle": a.Cfg.UpdateRepo, "kanal": a.updateChannel(), "kanaele": []string{"stable", "beta", "nightly"},
		"installierbar": updateInstallable(), "status": a.updateStatus(), "neuer": []release{}, "aeltere": []release{},
		"mindestens": "1.14.0", "aktuell_aenderungen": now, "fruehere": earlier, "notizen": source}
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
	newer, older := a.newerReleases(releases)
	out["neuer"], out["aeltere"] = newer, older
	if len(newer) > 0 {
		out["neueste"] = newer[0].Version
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
