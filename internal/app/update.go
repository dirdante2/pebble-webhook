package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	pebblewebhook "pebble-webhook"

	"pebble-webhook/internal/config"
	"pebble-webhook/internal/i18n"
)

// Update-Prüfung: neuere Versionen aus den GitHub-Releases (UPDATE_REPO) samt Änderungsliste (Release-Text =
// Abschnitt aus CHANGELOG.md). Installiert wird über den Updater des Installers: die App schreibt eine Anfrage
// nach DATA_DIR/update-request, die Path-Unit pebble-webhook-update startet install.sh --apply-request als root.

const updaterPath = "/usr/local/lib/pebble-webhook/install.sh"

// Docker-Image: setzt PEBBLE_DOCKER=1 und PEBBLE_IMAGE (Name ohne Tag). Im Container gibt es keinen Updater –
// Updates werden nur angezeigt (samt Pull-Befehl), Installieren, Wiederherstellen und Auto-Update sind aus.
func inDocker() bool { return os.Getenv("PEBBLE_DOCKER") == "1" }

// noUpdater: Meldung, wenn aus der App nicht installiert werden kann.
func noUpdater() string {
	if inDocker() {
		return "Im Docker-Container nicht möglich – neues Image ziehen und Container neu erstellen"
	}
	return "Kein Updater installiert – Installation mit install.sh nötig"
}

// githubAPI ist in Tests ersetzbar.
var githubAPI = "https://api.github.com"

type release struct {
	Version string `json:"version"`
	Channel string `json:"kanal"` // stable | beta | nightly
	Date    string `json:"datum"`
	Notes   string `json:"aenderungen"`
	URL     string `json:"url"`

	Published string `json:"veroeffentlicht,omitempty"` // Zeitpunkt (RFC 3339), zum Sortieren
	Commit    string `json:"commit,omitempty"`          // Git-Commit des Builds (target_commitish), sonst leer
}

// shaRe: voller Commit-Hash (GitHub setzt target_commitish auf den Commit, aus dem gebaut wurde).
var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// buildCommit: Commit, aus dem die laufende Version gebaut wurde (Go stempelt ihn beim Bauen im Git-Repo ein);
// leer, wenn unbekannt oder mit nicht eingecheckten Änderungen gebaut – dann ist der Stand mit keinem Release gleich.
func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	rev, modified := "", false
	for _, st := range info.Settings {
		switch st.Key {
		case "vcs.revision":
			rev = st.Value
		case "vcs.modified":
			modified = st.Value == "true"
		}
	}
	if modified || !shaRe.MatchString(rev) {
		return ""
	}
	return rev
}

type updateCache struct {
	mu       sync.Mutex
	at       time.Time
	repo     string
	releases []release // neueste zuerst
	err      string
	channel  string // aktiver Kanal; leer = aus der Einstellung
	auto     int8   // automatisches Update: 0 = aus der Einstellung, 1 = an, 2 = aus (in der App umgeschaltet)
}

// updateInterval: so oft prüft der Dienst im Hintergrund auf neue Versionen.
const updateInterval = 4 * time.Hour

// Versionen: X.Y.Z-stable, X.Y.Z-beta, X.Y.Z-nightly – jede Nummer gibt es je Kanal nur einmal.
// Ältere Formen werden weiter erkannt: X.Y.Z (stable), X.Y.Z-beta.N, X.Y.Z-nightly.JJJJMMTTHHMM.
// Eigene Builds tragen X.Y.Z.A: A ist ein lokaler Build-Zähler (deploy.sh, nie in Git oder Releases).
var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?(?:-(stable|beta|nightly)(?:\.(\d+))?)?$`)

// minRollback: ältere Versionen kennen den Schema-Stand der Datenbank nicht (PRAGMA user_version, seit 2.0.0) –
// der Installer lehnt sie ab, sobald die Datenbank einen Stand trägt. Zurück dorthin nur über ein Backup.
var minRollback = version{2, 0, 0, 0, 0, 0}

// version ist sortierbar: Basis, dann Rang (nightly < beta < stable), dann Nummer der Vorabversion, dann der
// lokale Build-Zähler A.
type version [6]int

var channelRank = map[string]int{"nightly": 0, "beta": 1, "stable": 2}

// parseVersion zerlegt „1.15.0-beta.2“ (auch „1.13.1 (Build …)“, eigener Build „1.33.11.3“ → Kanal lokal); ok=false bei „dev“ u. ä.
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
	channel = m[5]
	switch {
	case channel == "" && m[4] != "":
		channel = "lokal" // X.Y.Z.A: eigener Build
	case channel == "":
		channel = "stable"
	}
	out[3] = channelRank[channel]
	out[4], _ = strconv.Atoi(m[6])
	out[5], _ = strconv.Atoi(m[4])
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
func (a *System) updateChannel() string {
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
	Newer     bool `json:"neu"`       // später veröffentlicht als die installierte Version gebaut bzw. veröffentlicht wurde
	Cached    bool `json:"vorhanden"` // liegt im lokalen Vorrat, Wechsel ohne Download
}

var buildStampRe = regexp.MustCompile(`\(Build (\d{8}-\d{4})`)

// installedTime: wann die laufende Version entstand – Veröffentlichung des passenden Releases, sonst der
// Build-Stempel („(Build 20261122-0016, …)“), der Zeitstempel einer nightly, zuletzt der Installationszeitpunkt.
// Die Nummern der Kanäle hängen nicht zusammen; „neuer“ heißt deshalb später entstanden.
func (a *System) installedTime(releases []release) time.Time {
	current := strings.Fields(a.Version + " ")[0]
	ch := a.installedChannel()
	for _, rel := range releases {
		if rel.Version == current && rel.Channel == ch {
			if t, err := time.Parse(time.RFC3339, rel.Published); err == nil {
				return t
			}
		}
	}
	if m := buildStampRe.FindStringSubmatch(a.Version); m != nil {
		if t, err := time.ParseInLocation("20060102-1504", m[1], time.Local); err == nil {
			// Eigener Build: es zählt allein die Zeit – jede später veröffentlichte Version des Kanals ist neuer,
			// gleich welche Nummer sie trägt.
			return t
		}
	}
	if m := semverRe.FindStringSubmatch(current); m != nil && m[5] == "nightly" && len(m[6]) == 12 {
		if t, err := time.Parse("200601021504", m[6]); err == nil {
			return t
		}
	}
	if fi, err := os.Stat(filepath.Join(a.Cfg.DataDir, "installed-channel")); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// channelsFor: Kanäle, deren Versionen ein Kanal anbietet – stable nur stable, beta dazu stable, nightly alle drei.
// Nicht jede Version erscheint in jedem Kanal; so bekommt z. B. beta eine stable, die neuer ist als die letzte beta.
func channelsFor(channel string) []string {
	switch channel {
	case "nightly":
		return []string{"nightly", "beta", "stable"}
	case "beta":
		return []string{"beta", "stable"}
	}
	return []string{"stable"}
}

// channelList: Versionen eines Kanals samt der Kanäle, die er mit anbietet (channelsFor), neueste Veröffentlichung
// zuerst (die Nummern der Kanäle hängen nicht zusammen – eine stable kann eine höhere Nummer haben als die
// laufende nightly und umgekehrt).
func (a *System) channelList(releases []release, channel string) []channelRelease {
	current := strings.Fields(a.Version + " ")[0]
	curChannel := a.installedChannel()
	curBase, _, curOK := parseVersion(a.Version)
	since := a.installedTime(releases)
	commit := buildCommit()
	list := []channelRelease{}
	for _, rel := range releases {
		if !slices.Contains(channelsFor(channel), rel.Channel) {
			continue
		}
		v, _, _ := parseVersion(rel.Version)
		if v.less(minRollback) {
			continue
		}
		// Installiert ist sie, wenn Nummer und Kanal stimmen (jeder Kanal kann dieselbe Nummer tragen).
		installed := rel.Version == current && rel.Channel == curChannel
		// Derselbe Commit (z. B. stable und nightly aus demselben Stand, oder ein eigener Build davon) ist
		// inhaltlich gleich: nie „neuer“, egal wann er veröffentlicht wurde.
		same := commit != "" && rel.Commit == commit
		newer := false
		if t, err := time.Parse(time.RFC3339, rel.Published); err == nil && !since.IsZero() {
			newer = !installed && !same && t.After(since.Add(time.Minute)) // eine Minute Spielraum für Build und Veröffentlichung
		}
		// Aus einem anderen Kanal als dem installierten zählt zusätzlich die Nummer: eine später veröffentlichte
		// stable aus älterem Stand (z. B. 1.34.0 nach 1.35.0-nightly) ist kein Update.
		if newer && rel.Channel != curChannel && curChannel != "lokal" && curOK { // eigener Build: allein die Zeit
			newer = curBase[0] < v[0] || curBase[0] == v[0] && (curBase[1] < v[1] || curBase[1] == v[1] && curBase[2] < v[2])
		}
		_, errCached := os.Stat(filepath.Join(a.Cfg.DataDir, "releases", rel.Version, "SHA256SUMS"))
		list = append(list, channelRelease{release: rel, Installed: installed, Newer: newer, Cached: errCached == nil})
	}
	sort.SliceStable(list, func(i, k int) bool { return list[i].Published > list[k].Published })
	return list
}

// channelView: Versionen des gewählten Kanals (mit channelsFor); empfohlen ist die neueste, sofern sie nicht schon läuft.
func (a *System) channelView(releases []release) (list []channelRelease, recommended string, installedChannel string) {
	list = a.channelList(releases, a.updateChannel())
	for _, r := range list {
		if r.Newer {
			recommended = r.Version // neueste wirklich neuere (nach Veröffentlichung) meldet ein Update
			break
		}
	}
	return list, recommended, a.installedChannel()
}

// installedChannel: Kanal der laufenden Version – vom Installer in DATA_DIR/installed-channel vermerkt
// (stable, beta, nightly; lokal = eigener Build). Fehlt die Datei, zählt die Endung der Versionsnummer.
func (a *System) installedChannel() string {
	if b, err := os.ReadFile(filepath.Join(a.Cfg.DataDir, "installed-channel")); err == nil {
		if ch := strings.TrimSpace(string(b)); ch != "" {
			return ch
		}
	}
	if _, ch, ok := parseVersion(a.Version); ok {
		return ch
	}
	return "lokal"
}

// AvailableUpdate: neueste Version des gewählten Kanals aus der letzten Prüfung, sofern sie später als die
// laufende veröffentlicht wurde (Nummer egal); ohne GitHub zu fragen ("" = keine).
func (a *System) AvailableUpdate() string {
	a.updates.mu.Lock()
	releases := a.updates.releases
	a.updates.mu.Unlock()
	_, rec, _ := a.channelView(releases)
	return rec
}

// CheckUpdates läuft im Hintergrund: beim Start und dann alle updateInterval.
func (a *System) CheckUpdates(ctx context.Context) {
	if a.Cfg.UpdateRepo == "" {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	var donateAt time.Time // Spenden-Link: beim ersten Lauf, dann alle updateInterval
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.fetchReleases(ctx, false) // gespeicherten Stand laden
		if time.Since(donateAt) >= updateInterval {
			donateAt = time.Now()
			a.refreshDonate(ctx)
		}
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
				a.notifyUpdate(v)
			}
		}
		t.Reset(updateInterval)
	}
}

// updateNotifiedKey: zuletzt per Push gemeldete Version (je Version nur einmal).
const updateNotifiedKey = "update:gemeldet"

// notifyUpdate meldet eine neue Version einmal an alle Admins mit eingeschaltetem Kanal „Updates“.
func (a *System) notifyUpdate(version string) {
	if last, _ := a.DB.Get(updateNotifiedKey); last == version {
		return
	}
	for _, app := range a.Apps() {
		if u, err := a.DB.User(app.UserID); err == nil && u.Admin() {
			app.notify(config.NotifyUpdates, app.T("Update verfügbar"), app.Tf("Version %s kann installiert werden.", shortVersion(version)), "/status/update")
		}
	}
	if err := a.DB.Set(updateNotifiedKey, version); err != nil {
		logf("Update-Meldung merken: %v", err)
	}
}

// keepPerChannel: so viele Releases je Kanal hält der Cache (die Seite zeigt 3, „Mehr“ den Rest).
const keepPerChannel = 20

// updateCacheKey: letzter Stand der Releases in der kv-Tabelle, damit ein Neustart nicht erneut GitHub fragt.
const updateCacheKey = "update-releases"

type storedReleases struct {
	At       int64     `json:"at"`
	Repo     string    `json:"repo"`
	Releases []release `json:"releases"`
}

// fetchReleases liefert die zwischengespeicherten Releases; GitHub wird nur mit force gefragt (Prüfung im
// Hintergrund alle updateInterval oder „Jetzt prüfen“) – oder einmal, wenn noch gar nichts gespeichert ist.
func (a *System) fetchReleases(ctx context.Context, force bool) ([]release, string) {
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
		Target     string `json:"target_commitish"`
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
		commit := ""
		if shaRe.MatchString(r.Target) {
			commit = r.Target
		}
		fresh = append(fresh, release{Version: strings.TrimPrefix(r.Tag, "v"), Channel: channel, Date: date, Notes: r.Body, URL: r.URL, Published: r.Published, Commit: commit})
	}
	sort.SliceStable(fresh, func(i, k int) bool { return fresh[i].Published > fresh[k].Published })
	// Gespeichert werden je Kanal nur die letzten keepPerChannel Releases; ältere sind nicht relevant.
	kept, count := []release{}, map[string]int{}
	for _, r := range fresh {
		if count[r.Channel] < keepPerChannel {
			kept = append(kept, r)
			count[r.Channel]++
		}
	}
	fresh = kept
	c.releases = fresh
	if b, err := json.Marshal(storedReleases{At: c.at.UnixMilli(), Repo: repo, Releases: fresh}); err == nil {
		if err := a.DB.Set(updateCacheKey, string(b)); err != nil {
			logf("Update-Cache: %v", err)
		}
	}
	return c.releases, ""
}

// updateInstallable: der Updater des Installers liegt bereit (Installation über install.sh; nie im Docker-Container).
func updateInstallable() bool {
	if inDocker() {
		return false
	}
	_, err := os.Stat(updaterPath)
	return err == nil
}

func (a *System) updateStatus() string {
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

// notesSince: Änderungsabschnitte nach der Version from bis einschließlich current (neueste oben), aus derselben
// Quelle wie releaseNotes. Verglichen wird nur X.Y.Z – Abschnitte gibt es nur je X.Y.Z.
func notesSince(from, current, channel string) []noteSection {
	f, _, okF := parseVersion(from)
	c, _, okC := parseVersion(current)
	if !okF || !okC {
		return nil
	}
	base := func(v version) [3]int { return [3]int{v[0], v[1], v[2]} }
	lessBase := func(a, b [3]int) bool {
		for i := range a {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return false
	}
	pick := func(text string) []noteSection {
		var out []noteSection
		seen := map[[3]int]bool{}
		for _, sec := range noteSections(text) {
			v, _, ok := parseVersion(sec.Version)
			if !ok || seen[base(v)] {
				continue
			}
			if lessBase(base(f), base(v)) && !lessBase(base(c), base(v)) {
				seen[base(v)] = true
				out = append(out, sec)
			}
		}
		return out
	}
	if channel != "nightly" {
		if out := pick(pebblewebhook.Releases); len(out) > 0 {
			return out
		}
	}
	return pick(pebblewebhook.Changelog)
}

func (a *System) handleUpdate(w http.ResponseWriter, r *http.Request) {
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
		"mindestens": "2.0.0", "backups": a.backups(), "backup": a.backupPlan(), "auto": a.autoUpdateOn(), "auto_zeit": a.Cfg.AutoUpdateTime, "auto_letzt": func() string { v, _ := a.DB.Get(autoUpdateKey); return v }(), "aktuell_aenderungen": now, "alle_aenderungen": allNotes}
	if inDocker() {
		out["docker"] = map[string]string{"image": os.Getenv("PEBBLE_IMAGE")}
	}
	if seit := r.URL.Query().Get("seit"); seit != "" {
		out["seit_aenderungen"] = notesSince(seit, current, a.updateChannel())
	}
	if a.Cfg.UpdateRepo == "" {
		out["fehler"] = i18n.T("Update-Prüfung ist aus (Einstellung Update-Quelle)")
		writeJSON(w, 200, out)
		return
	}
	pruefen := r.URL.Query().Get("pruefen") == "1"
	if pruefen {
		a.refreshDonate(r.Context()) // „Jetzt prüfen“ holt auch den Spenden-Link neu
	}
	releases, errMsg := a.fetchReleases(r.Context(), pruefen)
	if errMsg != "" {
		out["fehler"] = errMsg
	}
	a.updates.mu.Lock()
	out["geprueft"] = a.updates.at.UnixMilli()
	a.updates.mu.Unlock()
	list, rec, curChannel := a.channelView(releases)
	out["versionen"], out["aktuell_kanal"], out["kanal_umfasst"] = list, curChannel, channelsFor(a.updateChannel())
	newIn := map[string]bool{}
	for _, ch := range []string{"stable", "beta", "nightly"} {
		for _, r := range a.channelList(releases, ch) {
			newIn[ch] = newIn[ch] || r.Newer
		}
	}
	out["kanal_neu"] = newIn
	if rec != "" {
		out["neueste"] = rec
	}
	writeJSON(w, 200, out)
}

// handleUpdateAuto schaltet das automatische Update: gespeichert in den Einstellungen, wirksam sofort.
func (a *System) handleUpdateAuto(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Aktiv bool `json:"aktiv"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "ungueltig", "Angabe nicht lesbar")
		return
	}
	if in.Aktiv && inDocker() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", noUpdater())
		return
	}
	v, mode := "inaktiv", int8(2)
	if in.Aktiv {
		v, mode = "aktiv", 1
	}
	if err := a.DB.SaveSettings(map[string]string{"AUTO_UPDATE": v}); err != nil {
		writeErr(w, 500, "speicher", "Speichern fehlgeschlagen")
		return
	}
	a.updates.mu.Lock()
	a.updates.auto = mode
	a.updates.mu.Unlock()
	a.handleUpdate(w, r)
}

// handleUpdateChannel wechselt den Kanal: gespeichert in den Einstellungen, wirksam sofort (ohne Neustart).
func (a *System) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
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

func (a *System) handleUpdateInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string `json:"version"`
	}
	if readJSON(r, &in) != nil || !semverRe.MatchString(in.Version) || semverRe.FindStringSubmatch(in.Version)[4] != "" {
		writeErr(w, 400, "ungueltig", "Version fehlt")
		return
	}
	if v, _, _ := parseVersion(in.Version); v.less(minRollback) {
		writeErr(w, http.StatusConflict, "zu_alt", "Versionen vor 2.0.0 kennen den Datenbank-Stand nicht – zurück nur über ein Backup")
		return
	}
	if a.Cfg.UpdateRepo == "" || !updateInstallable() {
		writeErr(w, http.StatusConflict, "nicht_moeglich", noUpdater())
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
	if err := a.requestInstall(in.Version); err != nil {
		writeErr(w, 500, "speicher", "Anfrage nicht schreibbar")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// requestInstall beauftragt den Updater (Path-Unit) mit einer Version.
func (a *System) requestInstall(version string) error {
	v := strings.TrimPrefix(version, "v")
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-status"), []byte("requested "+v+"\n"), 0o640)
	return os.WriteFile(filepath.Join(a.Cfg.DataDir, "update-request"), []byte(a.Cfg.UpdateRepo+" "+v+"\n"), 0o640)
}

// autoUpdateOn: automatisches Update an (Einstellung AUTO_UPDATE, in der App umschaltbar ohne Neustart).
func (a *System) autoUpdateOn() bool {
	if inDocker() {
		return false
	}
	a.updates.mu.Lock()
	defer a.updates.mu.Unlock()
	switch a.updates.auto {
	case 1:
		return true
	case 2:
		return false
	}
	return a.Cfg.AutoUpdate
}

const autoUpdateKey = "auto-update-letzt" // „2026-11-22 1.27.12 …“: letzter Lauf und sein Ergebnis

// AutoUpdate läuft im Hintergrund: einmal am Tag zur Uhrzeit AUTO_UPDATE_TIME die neueste Version des
// gewählten Kanals installieren, wenn sie neuer ist als die laufende (der Installer sichert vorher).
func (a *System) AutoUpdate(ctx context.Context) {
	if inDocker() {
		return // kein Updater im Container
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if !a.autoUpdateOn() || a.Cfg.UpdateRepo == "" || now.Format("15:04") != a.Cfg.AutoUpdateTime {
				continue
			}
			today := now.Format("2006-01-02")
			if last, _ := a.DB.Get(autoUpdateKey); strings.HasPrefix(last, today) {
				continue
			}
			a.DB.Set(autoUpdateKey, today+" "+a.runAutoUpdate(ctx))
		}
	}
}

func (a *System) runAutoUpdate(ctx context.Context) string {
	releases, errMsg := a.fetchReleases(ctx, true)
	if errMsg != "" {
		logf("Automatisches Update: %s", errMsg)
		return "fehler " + errMsg
	}
	_, rec, _ := a.channelView(releases)
	switch {
	case rec == "":
		return "aktuell"
	case !updateInstallable():
		logf("Automatisches Update: %s verfügbar, aber kein Updater installiert", rec)
		return "fehler kein Updater"
	case strings.HasPrefix(a.updateStatus(), "requested") || strings.HasPrefix(a.updateStatus(), "running"):
		return "läuft schon"
	}
	if err := a.requestInstall(rec); err != nil {
		logf("Automatisches Update: %v", err)
		return "fehler " + err.Error()
	}
	logf("Automatisches Update: installiere %s (Kanal %s)", rec, a.updateChannel())
	return "installiert " + rec
}
