package app

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Systemkarte auf der Statusseite: Betriebssystem, CPU, RAM, Auslastung, dieser Dienst und die
// Prozesse mit der höchsten CPU-Last. Alles aus /proc (Linux); ein nicht lesbarer Wert bleibt leer.

// startedAt: Start dieses Dienstes (Laufzeit auf der Statusseite).
var startedAt = time.Now()

// systemSample: Abstand der beiden CPU-Stichproben.
const systemSample = 300 * time.Millisecond

// systemTop: so viele Prozesse zeigt die Liste.
const systemTop = 5

// procStat: CPU-Zeit (Ticks) und Kennzahlen eines Prozesses aus /proc/[pid]/stat.
type procStat struct {
	name    string
	ticks   uint64
	threads int
	rss     int64 // Seiten
}

// parseProcStat liest eine Zeile aus /proc/[pid]/stat. Der Name steht in Klammern und darf Leerzeichen enthalten.
func parseProcStat(s string) (procStat, bool) {
	l, r := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if l < 0 || r < l {
		return procStat{}, false
	}
	f := strings.Fields(s[r+1:]) // f[0] = Feld 3 (Zustand)
	if len(f) < 22 {
		return procStat{}, false
	}
	ut, e1 := strconv.ParseUint(f[11], 10, 64)
	st, e2 := strconv.ParseUint(f[12], 10, 64)
	th, e3 := strconv.Atoi(f[17])
	rss, e4 := strconv.ParseInt(f[21], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return procStat{}, false
	}
	return procStat{name: s[l+1 : r], ticks: ut + st, threads: th, rss: rss}, true
}

// parseCPUTotal: Summe und Leerlauf (idle+iowait) der „cpu“-Zeile aus /proc/stat.
func parseCPUTotal(s string) (total, idle uint64, ok bool) {
	for line := range strings.SplitSeq(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		for i, v := range f[1:] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			if i < 8 { // guest/guest_nice sind in user/nice schon enthalten
				total += n
			}
			if i == 3 || i == 4 {
				idle += n
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

// parseKeyValues liest „Schlüssel: Wert …“-Zeilen (/proc/meminfo, /proc/cpuinfo, /proc/self/status).
func parseKeyValues(s string) map[string]string {
	m := map[string]string{}
	for line := range strings.SplitSeq(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, da := m[k]; !da {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

// kibBytes: „123 kB“ → Bytes; nicht lesbar → -1.
func kibBytes(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
	if err != nil {
		return -1
	}
	return n * 1024
}

// parseOSRelease: PRETTY_NAME aus /etc/os-release (ohne Anführungszeichen).
func parseOSRelease(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// allProcs: alle Prozesse mit lesbarem /proc/[pid]/stat.
func allProcs() map[int]procStat {
	out := map[int]procStat{}
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		pid, err := strconv.Atoi(filepath.Base(d))
		if err != nil {
			continue
		}
		if p, ok := parseProcStat(readText(d + "/stat")); ok {
			out[pid] = p
		}
	}
	return out
}

func (a *System) handleSystem(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	pageSize := int64(os.Getpagesize())
	round1 := func(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

	// System
	sys := map[string]any{"architektur": runtime.GOARCH}
	if h, err := os.Hostname(); err == nil {
		sys["hostname"] = h
	}
	if n := parseOSRelease(readText("/etc/os-release")); n != "" {
		sys["os"] = n
	}
	var u syscall.Utsname
	if syscall.Uname(&u) == nil {
		var b strings.Builder
		for _, c := range u.Release {
			if c == 0 {
				break
			}
			b.WriteByte(byte(c))
		}
		sys["kernel"] = b.String()
	}
	if c := strings.TrimSpace(readText("/run/systemd/container")); c != "" {
		sys["container"] = c
	}
	if f := strings.Fields(readText("/proc/uptime")); len(f) > 0 {
		if s, err := strconv.ParseFloat(f[0], 64); err == nil {
			sys["laufzeit_s"] = int64(s)
		}
	}
	out["system"] = sys

	// CPU: zwei Stichproben für Gesamtlast und Prozesse.
	cpu := map[string]any{"kerne": runtime.NumCPU()}
	if m := parseKeyValues(readText("/proc/cpuinfo")); m["model name"] != "" {
		cpu["modell"] = m["model name"]
	}
	if f := strings.Fields(readText("/proc/loadavg")); len(f) >= 3 {
		cpu["load"] = f[:3]
	}
	t1, i1, ok1 := parseCPUTotal(readText("/proc/stat"))
	p1 := allProcs()
	time.Sleep(systemSample)
	t2, i2, ok2 := parseCPUTotal(readText("/proc/stat"))
	p2 := allProcs()
	dt := float64(t2 - t1)
	if ok1 && ok2 && dt > 0 {
		cpu["auslastung"] = round1(100 * (dt - float64(i2-i1)) / dt)
	}
	out["cpu"] = cpu

	// RAM
	mem := parseKeyValues(readText("/proc/meminfo"))
	out["ram"] = map[string]any{"gesamt": kibBytes(mem["MemTotal"]), "verfuegbar": kibBytes(mem["MemAvailable"]),
		"swap_gesamt": kibBytes(mem["SwapTotal"]), "swap_frei": kibBytes(mem["SwapFree"])}

	// Prozesse nach CPU-Anteil an der ganzen Maschine (0–100 %).
	share := func(pid int) (float64, bool) {
		a, okA := p1[pid]
		b, okB := p2[pid]
		if !okA || !okB || dt <= 0 || b.ticks < a.ticks {
			return 0, false
		}
		return round1(100 * float64(b.ticks-a.ticks) / dt), true
	}
	type top struct {
		PID  int     `json:"pid"`
		Name string  `json:"name"`
		CPU  float64 `json:"cpu"`
		RSS  int64   `json:"rss"`
	}
	tops := []top{}
	for pid, p := range p2 {
		if c, ok := share(pid); ok {
			tops = append(tops, top{pid, p.name, c, p.rss * pageSize})
		}
	}
	sort.Slice(tops, func(i, j int) bool {
		if tops[i].CPU != tops[j].CPU {
			return tops[i].CPU > tops[j].CPU
		}
		return tops[i].RSS > tops[j].RSS
	})
	out["prozesse"] = tops[:min(systemTop, len(tops))]
	out["prozesse_anzahl"] = len(p2)

	// Dieser Dienst
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	self := os.Getpid()
	dienst := map[string]any{"pid": self, "version": a.Version, "go": runtime.Version(), "goroutinen": runtime.NumGoroutine(),
		"heap": ms.HeapAlloc, "laufzeit_s": int64(time.Since(startedAt).Seconds())}
	if c, ok := share(self); ok {
		dienst["cpu"] = c
	}
	if p, ok := p2[self]; ok {
		dienst["threads"], dienst["rss"] = p.threads, p.rss*pageSize
	}
	out["dienst"] = dienst

	writeJSON(w, 200, out)
}
