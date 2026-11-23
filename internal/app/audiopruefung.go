package app

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pebble-webhook/internal/store"
)

// Audiodateien ohne Eintrag: nach dem Wiederherstellen eines älteren Backups (oder von Hand kopiert) liegen in
// AUDIO_DIR Aufnahmen, die die Datenbank nicht kennt. Die Prüfung vergleicht nur Pfad und Dateiname (= Id des
// Auftrags) mit der Datenbank, ohne die Dateien zu lesen: beim Start, täglich und auf der Statusseite.
// „Importieren“ legt je Datei einen neuen Eintrag an (Eingang = Zeitpunkt der Datei), der wie eine neue
// Aufnahme transkribiert und von der KI ausgewertet wird.

type orphanAudio struct {
	Path  string `json:"datei"` // relativ zu AUDIO_DIR
	Mode  string `json:"modus"` // diktat | planung (aus dem Unterordner; ältere Dateien ohne Unterordner: diktat)
	Web   bool   `json:"web"`   // in der Web-App aufgenommen (Dateiname <id>-web), zählt nicht zum Ring
	Size  int64  `json:"groesse"`
	MTime int64  `json:"zeit"`
}

var (
	orphanMu   sync.Mutex
	orphanList []orphanAudio
	jobIDRe    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// webMark: Zusatz im Dateinamen von Aufnahmen aus der Web-App (<id>-web.<ext>). Sie zählen nicht zum Ring;
// am Zusatz erkennt der Reimport das auch ohne Datenbank.
const webMark = "-web"

// audioIDOf: Eintrags-Id aus dem Dateinamen einer Aufnahme und ob sie aus der Web-App stammt.
func audioIDOf(name string) (id string, web bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if id, ok := strings.CutSuffix(base, webMark); ok {
		return id, true
	}
	return base, false
}

// audioSub: Unterordner einer Aufnahme je Art – Planungsrunden getrennt von Notizen, damit sich eine Datei
// ohne Eintrag später wieder richtig zuordnen lässt.
func audioSub(mode string) string {
	if mode == "planung" {
		return "planung"
	}
	return "diktat"
}

// modeOfPath: Art aus dem Unterordner (relativer Pfad); ohne Unterordner (vor 1.31) diktat.
func modeOfPath(rel string) string {
	if strings.HasPrefix(filepath.ToSlash(rel), "planung/") {
		return "planung"
	}
	return "diktat"
}

// sortAudio legt jede bekannte Aufnahme in den Unterordner ihrer Art (AUDIO_DIR/<diktat|planung>/JJJJ/MM) und
// richtet den Pfad in der Datenbank nach dem Ort der Datei: verschiebt Aufnahmen aus der Zeit vor den
// Unterordnern, folgt einem geänderten Modus und repariert Pfade einer wiederhergestellten älteren Datenbank.
// Gefunden wird eine Datei über ihre Id im Dateinamen. Läuft beim Start.
func (a *App) sortAudio() {
	byID := map[string]string{}
	filepath.WalkDir(a.Cfg.AudioDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && audioMimeOf(p) != "" {
			id, _ := audioIDOf(d.Name())
			byID[id] = p
		}
		return nil
	})
	jobs, err := a.DB.AudioJobs()
	if err != nil {
		logf("Audio einsortieren: %v", err)
		return
	}
	moved, fixed := 0, 0
	for _, j := range jobs {
		cur, ok := byID[j.ID]
		if !ok {
			continue // Datei fehlt: bleibt erkennbar (Abspielen meldet es)
		}
		rel, err := filepath.Rel(a.Cfg.AudioDir, cur)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rest := filepath.ToSlash(rel)
		for _, sub := range []string{"diktat/", "planung/"} {
			rest = strings.TrimPrefix(rest, sub)
		}
		want := filepath.Join(a.Cfg.AudioDir, audioSub(j.Mode), filepath.FromSlash(rest))
		if cur != want {
			if err := os.MkdirAll(filepath.Dir(want), 0o750); err != nil {
				logf("Audio einsortieren: %v", err)
				continue
			}
			if err := os.Rename(cur, want); err != nil {
				logf("Audio einsortieren: %s: %v", rel, err)
				continue
			}
			cur = want
			moved++
		}
		if j.AudioPath != cur {
			if err := a.DB.SetAudioPath(j.ID, cur); err != nil {
				logf("Audio einsortieren: %v", err)
				continue
			}
			fixed++
		}
	}
	if moved > 0 || fixed > 0 {
		logf("Audio einsortieren: %d verschoben, %d Pfade angepasst", moved, fixed)
	}
}

// removeEmptyDirs löscht leere Unterordner (z. B. Monate, deren Aufnahmen nach der Frist gelöscht oder in
// die Unterordner verschoben wurden); root selbst und die Ordner diktat/planung bleiben.
func (a *App) removeEmptyDirs(root string) {
	var dirs []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- { // tiefste zuerst
		if rel, _ := filepath.Rel(root, dirs[i]); rel == "diktat" || rel == "planung" {
			continue
		}
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			os.Remove(dirs[i])
		}
	}
}

// audioMimeOf: MIME-Typ zur Dateiendung (Gegenstück zu audioExt).
func audioMimeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".webm":
		return "audio/webm"
	case ".ogg":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	}
	return ""
}

// scanAudio sucht Aufnahmen ohne Eintrag und merkt sich das Ergebnis (orphans liest es).
func (a *App) scanAudio() []orphanAudio {
	paths, ids, err := a.DB.AudioRefs()
	if err != nil {
		logf("Audio-Prüfung: %v", err)
		return a.orphans()
	}
	found := []orphanAudio{}
	recent := time.Now().Add(-time.Minute) // gerade gespeichert, Eintrag folgt gleich
	filepath.WalkDir(a.Cfg.AudioDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || audioMimeOf(p) == "" {
			return nil
		}
		base, web := audioIDOf(d.Name())
		if paths[p] || ids[base] {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.ModTime().After(recent) {
			return nil
		}
		rel, err := filepath.Rel(a.Cfg.AudioDir, p)
		if err != nil {
			return nil
		}
		found = append(found, orphanAudio{Path: filepath.ToSlash(rel), Mode: modeOfPath(rel), Web: web, Size: fi.Size(), MTime: fi.ModTime().UnixMilli()})
		return nil
	})
	sort.Slice(found, func(i, k int) bool { return found[i].MTime < found[k].MTime })
	orphanMu.Lock()
	changed := len(found) != len(orphanList)
	orphanList = found
	orphanMu.Unlock()
	if changed && len(found) > 0 {
		logf("Audio-Prüfung: %d Aufnahme(n) ohne Eintrag", len(found))
	}
	return found
}

func (a *App) orphans() []orphanAudio {
	orphanMu.Lock()
	defer orphanMu.Unlock()
	return append([]orphanAudio(nil), orphanList...)
}

// importAudio legt für eine Aufnahme ohne Eintrag einen neuen Eintrag an; die Datei bleibt, wo sie ist.
func (a *App) importAudio(o orphanAudio) error {
	path := filepath.Join(a.Cfg.AudioDir, filepath.FromSlash(o.Path))
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	base, web := audioIDOf(path)
	_, ids, err := a.DB.AudioRefs()
	if err != nil {
		return err
	}
	id := store.NewID()
	if jobIDRe.MatchString(base) && !ids[base] {
		id = base // ursprüngliche Id aus dem Dateinamen
	}
	at := fi.ModTime().UnixMilli()
	source := "import" // vom Ring
	if web {
		source = "web"
	}
	j := &store.Job{ID: id, DeliveryID: "import-" + id, Source: source, Trigger: "import", Mode: modeOfPath(o.Path),
		ReceivedAt: at, RecordedAt: at, AudioPath: path, AudioMime: audioMimeOf(path), State: store.StateNew}
	if _, err := a.DB.InsertJob(j); err != nil {
		return err
	}
	logf("Audio-Import: %s → Eintrag %s", o.Path, id)
	a.changed(j.ID)
	return nil
}

// handleAudioOrphans: Aufnahmen ohne Eintrag (prüft neu).
func (a *App) handleAudioOrphans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"dateien": a.scanAudio()})
}

// handleAudioImport importiert eine Datei ({datei}) oder alle ({alle: true}).
func (a *App) handleAudioImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Datei string `json:"datei"`
		Alle  bool   `json:"alle"`
	}
	if readJSON(r, &in) != nil || (in.Datei == "" && !in.Alle) {
		writeErr(w, 400, "ungueltig", "Datei fehlt")
		return
	}
	n := 0
	for _, o := range a.scanAudio() {
		if !in.Alle && o.Path != in.Datei {
			continue
		}
		if err := a.importAudio(o); err != nil {
			logf("Audio-Import: %s: %v", o.Path, err)
			writeErr(w, 500, "speicher", "Import fehlgeschlagen")
			return
		}
		n++
	}
	if n == 0 {
		writeErr(w, 404, "fehlt", "Datei nicht gefunden")
		return
	}
	a.Wake()
	writeJSON(w, 200, map[string]any{"importiert": n, "dateien": a.scanAudio()})
}

// mp4Seconds liest die Länge einer m4a/mp4-Datei aus dem mvhd-Kasten (-1 = unbekannt).
func mp4Seconds(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var walk func(b []byte) float64
	walk = func(b []byte) float64 {
		for len(b) >= 8 {
			size := int(uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]))
			typ := string(b[4:8])
			hdr := 8
			if size == 1 && len(b) >= 16 {
				size, hdr = int(be64(b[8:16])), 16
			} else if size == 0 {
				size = len(b)
			}
			if size < hdr || size > len(b) {
				return -1
			}
			body := b[hdr:size]
			switch typ {
			case "moov":
				return walk(body)
			case "mvhd":
				if len(body) >= 32 && body[0] == 1 {
					return float64(be64(body[24:32])) / float64(be32(body[20:24]))
				}
				if len(body) >= 20 {
					if ts := be32(body[12:16]); ts > 0 {
						return float64(be32(body[16:20])) / float64(ts)
					}
				}
				return -1
			}
			b = b[size:]
		}
		return -1
	}
	return walk(b)
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func be64(b []byte) uint64 { return uint64(be32(b[:4]))<<32 | uint64(be32(b[4:8])) }

var (
	secondsMu    sync.Mutex
	secondsCache = map[string]float64{} // Pfad+Größe → Länge; einmal gelesen, im Speicher gehalten
)

// audioStats: Zahl, Durchschnittsgröße und -länge der Aufnahmen in AUDIO_DIR (Länge nur m4a/mp4).
func (a *App) audioStats() map[string]any {
	var n, withLen int
	var bytes int64
	var secs float64
	filepath.WalkDir(a.Cfg.AudioDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || audioMimeOf(p) == "" {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		n++
		bytes += fi.Size()
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".m4a" && ext != ".mp4" {
			return nil
		}
		key := p + "|" + strconv.FormatInt(fi.Size(), 10)
		secondsMu.Lock()
		s, ok := secondsCache[key]
		secondsMu.Unlock()
		if !ok {
			s = mp4Seconds(p)
			secondsMu.Lock()
			secondsCache[key] = s
			secondsMu.Unlock()
		}
		if s >= 0 {
			withLen++
			secs += s
		}
		return nil
	})
	out := map[string]any{"anzahl": n}
	if n > 0 {
		out["schnitt_groesse"] = bytes / int64(n)
	}
	if withLen > 0 {
		out["schnitt_sekunden"] = secs / float64(withLen)
		out["gesamt_sekunden"] = secs
	}
	return out
}
