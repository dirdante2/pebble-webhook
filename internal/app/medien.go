package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"pebble-webhook/internal/i18n"
	"pebble-webhook/internal/openai"
	"pebble-webhook/internal/schluessel"
	"pebble-webhook/internal/store"
)

// Medien in Auswertungen und Planungen: die KI beschreibt sie (Diagramm mit Daten, Bild zum Erzeugen oder aus dem
// Netz, Trailer zu Film/Serie), der Worker löst sie danach auf (Bild erzeugen/laden und verschlüsselt ablegen,
// YouTube-Video suchen). Im Text stehen sie als Marker [[medium:N]] an ihrer Stelle; die Web-App zeigt sie dort.
// Scheitert ein Medium, steht der Grund in fehler (sichtbar), das übrige Ergebnis bleibt.

// Medium: von der KI geliefert (nr bis suche), vom Server ergänzt (bild bis fehler).
type Medium struct {
	Nr           int    `json:"nr"`
	Art          string `json:"art"` // diagramm | bild_erzeugen | bild_extern | trailer
	Titel        string `json:"titel"`
	Unterschrift string `json:"unterschrift,omitempty"`
	Diagramm     *Chart `json:"diagramm,omitempty"`
	Prompt       string `json:"prompt,omitempty"` // bild_erzeugen
	URL          string `json:"url,omitempty"`    // bild_extern: direkte Adresse der Bilddatei
	Suche        string `json:"suche,omitempty"`  // trailer: Titel und Jahr

	Bild   string `json:"bild,omitempty"`   // Name der Bilddatei (siehe imageNameRe)
	Quelle string `json:"quelle,omitempty"` // Herkunft eines externen Bildes bzw. Video-Seite
	Video  string `json:"video,omitempty"`  // YouTube-Id
	Fehler string `json:"fehler,omitempty"`
}

// Chart: Daten eines Diagramms; gezeichnet wird in der Web-App.
type Chart struct {
	Typ            string        `json:"typ"` // balken | balken_quer | gestapelt | linie | kreis | ablauf
	Beschriftungen []string      `json:"beschriftungen"`
	Reihen         []ChartSeries `json:"reihen"`
	Einheit        string        `json:"einheit"`
}

type ChartSeries struct {
	Name  string    `json:"name"`
	Werte []float64 `json:"werte"`
}

// Grenzen je Auswertung bzw. Runde (Kosten, Übersicht).
const (
	maxMedia          = 6 // ohne Trailer
	maxImages         = 4 // davon höchstens maxGenerated erzeugt
	maxGenerated      = 2
	maxTrailers       = 5 // je Film/Serie einer
	maxChartLabels    = 12
	maxChartSeries    = 4
	maxImageBytes     = 8 << 20
	imageOrphanMinAge = 24 * time.Hour
)

var mediaSchema = map[string]any{"type": "array", "items": map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"nr", "art", "titel", "unterschrift", "diagramm", "prompt", "url", "suche"},
	"properties": map[string]any{
		"nr":           map[string]any{"type": "integer"},
		"art":          map[string]any{"type": "string", "enum": []string{"diagramm", "bild_erzeugen", "bild_extern", "trailer"}},
		"titel":        map[string]any{"type": "string"},
		"unterschrift": map[string]any{"type": "string"},
		"diagramm": map[string]any{"type": "object", "additionalProperties": false,
			"required": []string{"typ", "beschriftungen", "reihen", "einheit"},
			"properties": map[string]any{
				"typ":            map[string]any{"type": "string", "enum": []string{"balken", "balken_quer", "gestapelt", "linie", "kreis", "ablauf"}},
				"beschriftungen": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"reihen": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false,
					"required": []string{"name", "werte"}, "properties": map[string]any{
						"name":  map[string]any{"type": "string"},
						"werte": map[string]any{"type": "array", "items": map[string]any{"type": "number"}}}}},
				"einheit": map[string]any{"type": "string"}}},
		"prompt": map[string]any{"type": "string"},
		"url":    map[string]any{"type": "string"},
		"suche":  map[string]any{"type": "string"},
	}}}

// mediaRules: Anweisung an die KI (Feld medien), für Notizen und Planungen gleich.
const mediaRules = `medien: Anschauungsmaterial, das Zusammenhänge und Vergleiche schneller verständlich macht als Text. Nutze es aktiv: Zahlen über Zeit, Kostenverläufe, Anteile oder Größenvergleiche → ein Diagramm; Frage nach Aufbau, Funktionsweise, Aussehen oder einem Ort → ein Bild; Film oder Serie → Trailer. Bei persönlichen Notizen ohne solches Thema leer. Je Medium: nr (1, 2, …), art, titel (kurz), unterschrift (ein Satz, was man sieht), dazu je nach art:
  - "diagramm": bevorzugt nutzen, wann immer es Sinn macht – Zahlenreihen, Entwicklungen, Anteile, Rangfolgen, Zusammensetzungen, Abläufe. diagramm.typ: balken (Werte vergleichen), balken_quer (Rangfolge oder lange Namen), gestapelt (Zusammensetzung je Gruppe, mehrere Reihen addieren sich), linie (Verlauf über Zeit), kreis (Anteile an einem Ganzen, nur eine Reihe), ablauf (Schritte oder Stationen in Reihenfolge, 2–8 Beschriftungen, reihen leer); beschriftungen = Achse, Teile bzw. Schritte (höchstens 12); reihen = 1–4 Reihen mit name und je einem Wert pro Beschriftung (bei ablauf leer); einheit (z. B. "€" oder "kWh"; bei Prozent das Prozentzeichen). Nur belegte oder klar geschätzte Zahlen.
  - "bild_extern": ein vorhandenes Foto oder eine Abbildung aus dem Netz (Produkt, Ort, Gebäude, Pflanze, Schema). url = direkte Adresse der Bilddatei (.jpg, .png, .webp), die du in der Websuche gefunden hast – keine Webseite, nichts erfinden.
  - "bild_erzeugen": eine schlichte erklärende Skizze oder Illustration, wenn es kein passendes Bild gibt (Aufbau, Ablauf, Prinzip). prompt = genaue Bildbeschreibung auf Englisch, schlichter Stil, möglichst ohne Text im Bild. Höchstens zwei.
  - "trailer": bei jedem Film und jeder Serie genau einer. suche = Originaltitel und Jahr (z. B. "Arrival 2016"), ohne das Wort Trailer.
  Nicht benötigte Felder leer lassen (diagramm mit leeren Listen). Höchstens 4 Bilder. Tabellen sind kein Medium: sie stehen als Markdown-Tabelle im Text.
  Platz im Text: setze den Marker [[medium:nr]] als eigene Zeile dorthin, wo das Medium hingehört; ohne Marker steht es am Ende.`

// mediaMarkerRe: Marker eines Mediums im Text.
var mediaMarkerRe = regexp.MustCompile(`\[\[medium:(\d+)\]\]`)

// stripMediaMarkers entfernt die Marker (Joplin, Suche, KI-Eingabe ohne Medien).
func stripMediaMarkers(s string) string {
	s = markerLineRe.ReplaceAllString(s, "")
	return strings.TrimSpace(markerInlineRe.ReplaceAllString(s, ""))
}

var markerLineRe = regexp.MustCompile(`(?m)^[ \t]*\[\[medium:\d+\]\][ \t]*(\n|$)`)
var markerInlineRe = regexp.MustCompile(`[ \t]*\[\[medium:\d+\]\]`)

// mediaKey: woran ein Medium über eine neue Auswertung hinweg wiedererkannt wird (dann bleibt das schon geladene Bild).
func mediaKey(m Medium) string {
	v := m.Prompt
	switch m.Art {
	case "bild_extern":
		v = m.URL
	case "trailer":
		v = m.Suche
	case "diagramm":
		return ""
	}
	v = strings.ToLower(strings.Join(strings.Fields(v), " "))
	if v == "" {
		return ""
	}
	return m.Art + "|" + v
}

// normalizeMedia prüft und kürzt die Medien der KI und übernimmt aus prev, was schon aufgelöst ist.
func normalizeMedia(in, prev []Medium) []Medium {
	done := map[string]Medium{}
	for _, m := range prev {
		if k := mediaKey(m); k != "" && (m.Bild != "" || m.Video != "") {
			done[k] = m
		}
	}
	out := []Medium{}
	used := map[int]bool{}
	images, generated, trailers, others := 0, 0, 0, 0
	for _, m := range in {
		m.Art = strings.TrimSpace(m.Art)
		m.Titel = store.Preview(m.Titel, 80)
		m.Unterschrift = store.Preview(m.Unterschrift, 200)
		m.Prompt, m.URL, m.Suche = clipText(m.Prompt, 1000), strings.TrimSpace(m.URL), store.Preview(m.Suche, 100)
		m.Bild, m.Quelle, m.Video, m.Fehler = "", "", "", ""
		if m.Art != "diagramm" {
			m.Diagramm = nil
		}
		if m.Art != "trailer" && others >= maxMedia {
			continue
		}
		switch m.Art {
		case "diagramm":
			if err := checkChart(m.Diagramm); err != nil {
				m.Fehler = err.Error()
			}
		case "bild_erzeugen":
			if m.Prompt == "" || generated >= maxGenerated || images >= maxImages {
				continue
			}
			generated++
			images++
		case "bild_extern":
			if m.URL == "" || images >= maxImages {
				continue
			}
			images++
		case "trailer":
			if m.Suche == "" || trailers >= maxTrailers {
				continue
			}
			trailers++
		default:
			continue
		}
		if m.Art != "trailer" {
			others++
		}
		if m.Nr <= 0 || used[m.Nr] {
			m.Nr = 1
			for used[m.Nr] {
				m.Nr++
			}
		}
		used[m.Nr] = true
		if old, ok := done[mediaKey(m)]; ok {
			m.Bild, m.Quelle, m.Video = old.Bild, old.Quelle, old.Video
		}
		out = append(out, m)
	}
	return out
}

// checkChart: Diagramm muss zeichenbar sein (Beschriftungen und Werte passen zusammen).
func checkChart(c *Chart) error {
	if c != nil && c.Typ == "ablauf" {
		if len(c.Beschriftungen) < 2 || len(c.Beschriftungen) > 8 {
			return errors.New(i18n.T("Ablauf braucht 2 bis 8 Schritte"))
		}
		for i := range c.Beschriftungen {
			c.Beschriftungen[i] = store.Preview(c.Beschriftungen[i], 60)
		}
		c.Reihen, c.Einheit = nil, ""
		return nil
	}
	if c == nil || len(c.Beschriftungen) == 0 || len(c.Reihen) == 0 {
		return errors.New(i18n.T("Diagramm ohne Daten"))
	}
	if len(c.Beschriftungen) > maxChartLabels || len(c.Reihen) > maxChartSeries {
		return errors.New(i18n.T("Diagramm zu groß"))
	}
	if c.Typ == "kreis" && len(c.Reihen) != 1 {
		return errors.New(i18n.T("Kreisdiagramm braucht genau eine Reihe"))
	}
	for i := range c.Beschriftungen {
		c.Beschriftungen[i] = store.Preview(c.Beschriftungen[i], 40)
	}
	c.Einheit = store.Preview(c.Einheit, 12)
	for i, r := range c.Reihen {
		if len(r.Werte) != len(c.Beschriftungen) {
			return errors.New(i18n.T("Diagramm: Werte passen nicht zu den Beschriftungen"))
		}
		for _, v := range r.Werte {
			if math.IsNaN(v) || math.IsInf(v, 0) || (c.Typ == "kreis" && v < 0) {
				return errors.New(i18n.T("Diagramm: ungültiger Wert"))
			}
		}
		c.Reihen[i].Name = store.Preview(r.Name, 40)
	}
	return nil
}

// pending: Medium wartet noch auf Auflösung.
func (m Medium) pending() bool {
	return m.Fehler == "" && m.Bild == "" && m.Video == "" && m.Art != "diagramm"
}

// resolveMedia löst offene Medien auf; liefert, ob sich etwas geändert hat, und die Kosten.
func (a *App) resolveMedia(ctx context.Context, list []Medium) (changed bool, cost *float64) {
	for i := range list {
		m := &list[i]
		if !m.pending() || ctx.Err() != nil {
			continue
		}
		var err error
		switch m.Art {
		case "bild_erzeugen":
			var c *float64
			c, err = a.generateImage(ctx, m)
			addCost(&cost, c)
		case "bild_extern":
			err = a.fetchImage(ctx, m)
		case "trailer":
			var c *float64
			c, err = a.findTrailer(ctx, m)
			addCost(&cost, c)
		}
		if ctx.Err() != nil {
			return changed, cost // Abbruch (Dienst endet): beim nächsten Mal weiter
		}
		if err != nil {
			m.Fehler = store.Preview(err.Error(), 200)
			logf("Medium %d (%s): %v", m.Nr, m.Art, err)
		}
		changed = true
	}
	return changed, cost
}

// resolveJobMedia: nach der Auswertung bzw. Recherche die Medien des Ergebnisses auflösen und speichern.
func (a *App) resolveJobMedia(ctx context.Context, j *store.Job) error {
	if j.Result == "" {
		return nil
	}
	if j.Mode == "planung" {
		plan := parsePlan(j.Result)
		if plan == nil {
			return nil
		}
		changed, cost := a.resolveMedia(ctx, plan.Medien)
		if !changed {
			return nil
		}
		addCost(&j.CostUSD, cost)
		data, _ := json.Marshal(plan)
		j.Result = string(data)
		// Nur ein Stand, der noch der jüngste der Planung ist, wird dort geschrieben.
		if p, err := a.DB.Planning(j.PlanningID); err == nil && p.Plan != "" {
			if cur := parsePlan(p.Plan); cur != nil && sameMedia(cur.Medien, plan.Medien) {
				if err := a.DB.SavePlanResult(p.ID, j.Result, p.Title); err != nil {
					return err
				}
				a.hub.send("planung:" + p.ID)
			}
		}
		return nil
	}
	d := parseDictation(j.Result)
	if d == nil {
		return nil
	}
	changed, cost := a.resolveMedia(ctx, d.Medien)
	if !changed {
		return nil
	}
	addCost(&j.CostUSD, cost)
	data, _ := json.Marshal(d)
	j.Result = string(data)
	return nil
}

// sameMedia: gleiche Medien (Art und Schlüssel in gleicher Reihenfolge) – der Stand gehört zur selben Runde.
func sameMedia(a, b []Medium) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Nr != b[i].Nr || a[i].Art != b[i].Art || mediaKey(a[i]) != mediaKey(b[i]) {
			return false
		}
	}
	return true
}

// --- Bilder: Ablage MEDIA_DIR/<id>/bilder/JJJJ/MM/<JJJJMM>-<32 hex>.<endung>.enc, verschlüsselt wie Aufnahmen ---

var imageNameRe = regexp.MustCompile(`^(\d{4})(\d{2})-[0-9a-f]{32}\.(webp|png|jpg|gif)$`)

func imageMime(name string) string {
	switch filepath.Ext(name) {
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	case ".jpg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	}
	return ""
}

// imageKind erkennt das Format am Inhalt (kein SVG: könnte Skript enthalten); "" = kein Bild.
func imageKind(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp"
	}
	return ""
}

// imagePath: Ort einer Bilddatei; "" bei ungültigem Namen.
func (a *App) imagePath(name string) string {
	m := imageNameRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return filepath.Join(a.imageRoot(), m[1], m[2], name+schluessel.AudioExt)
}

// saveImage legt ein Bild verschlüsselt ab und liefert seinen Namen.
func (a *App) saveImage(data []byte) (string, error) {
	kind := imageKind(data)
	if kind == "" {
		return "", errors.New(i18n.T("keine Bilddatei (JPEG, PNG, GIF oder WebP)"))
	}
	b := make([]byte, 16)
	rand.Read(b)
	name := time.Now().Format("200601") + "-" + hex.EncodeToString(b) + "." + kind
	path := a.imagePath(name)
	if a.audioKey != nil {
		var err error
		if data, err = schluessel.SealAudio(a.audioKey, name, data); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return name, nil
}

// readImage liest und entschlüsselt ein Bild.
func (a *App) readImage(name string) ([]byte, time.Time, error) {
	path := a.imagePath(name)
	if path == "" {
		return nil, time.Time{}, fs.ErrNotExist
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	if a.audioKey == nil {
		return data, st.ModTime(), nil
	}
	data, err = schluessel.OpenAudio(a.audioKey, name, data)
	return data, st.ModTime(), err
}

// generateImage erzeugt ein Bild über die Einträge des Bereichs Bilder.
func (a *App) generateImage(ctx context.Context, m *Medium) (*float64, error) {
	if !a.KI().Images {
		return nil, errors.New(i18n.T("Bilder erzeugen ist ausgeschaltet"))
	}
	gen, ok := a.AI.(interface {
		Image(ctx context.Context, prompt, size, quality string) ([]byte, float64, error)
	})
	if !ok {
		return nil, errors.New(i18n.T("Bilder erzeugen wird nicht unterstützt"))
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	prompt := m.Prompt + "\n\nStyle: clean, simple explanatory illustration, flat colors, white background, no text or only very short labels."
	data, price, err := gen.Image(ctx, prompt, "1536x1024", "low")
	if err != nil {
		return nil, err
	}
	cost := &price
	name, err := a.saveImage(data)
	if err != nil {
		return cost, err
	}
	m.Bild = name
	return cost, nil
}

// restrictExternalImages: Fremde Bilder nur von Seiten, die die Websuche dieses Laufs als Quelle geliefert hat, und
// mit kurzer Adresse. Sonst könnte eingeschleuster Text (eine Webseite, eine fremde Notiz) das Modell dazu bringen,
// eine Bildadresse mit privaten Inhalten im Pfad oder in der Abfrage zu bauen, die der Server dann abruft.
func restrictExternalImages(list []Medium, cits []openai.Citation) {
	domains := map[string]bool{}
	for _, c := range cits {
		if u, err := url.Parse(c.URL); err == nil {
			d := baseDomain(u.Hostname())
			domains[d] = true
			if d == "wikipedia.org" || d == "wikimedia.org" {
				domains["wikipedia.org"], domains["wikimedia.org"] = true, true
			}
		}
	}
	for i := range list {
		m := &list[i]
		if m.Art != "bild_extern" || !m.pending() {
			continue
		}
		u, err := url.Parse(m.URL)
		switch {
		case err != nil || len(m.URL) > 400 || len(u.RawQuery) > 120:
			m.Fehler = i18n.T("Bildadresse zu lang")
		case !domains[baseDomain(u.Hostname())]:
			m.Fehler = i18n.T("Bild nicht von einer Quelle der Websuche")
		}
	}
}

// baseDomain: die letzten zwei Teile des Hostnamens (genügt hier: gleiche Seite oder nicht).
func baseDomain(host string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSuffix(host, ".")), ".")
	if len(parts) <= 2 {
		return strings.Join(parts, ".")
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// fetchImage lädt ein Bild aus dem Netz (nur öffentliche Adressen) und legt es ab.
func (a *App) fetchImage(ctx context.Context, m *Medium) error {
	u, err := url.Parse(m.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New(i18n.T("ungültige Bildadresse"))
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; pebble-webhook)")
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif,image/*;q=0.8")
	resp, err := publicHTTP.Do(req)
	if err != nil {
		return i18n.Errorf("Bild nicht ladbar: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return i18n.Errorf("Bild nicht ladbar (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return i18n.Errorf("Bild nicht ladbar: %v", err)
	}
	if len(data) > maxImageBytes {
		return errors.New(i18n.T("Bild zu groß (über 8 MB)"))
	}
	name, err := a.saveImage(data)
	if err != nil {
		return err
	}
	m.Bild, m.Quelle = name, u.String()
	return nil
}

// publicHTTP lädt nur von öffentlichen Adressen (Schutz vor Zugriffen ins eigene Netz über eine Bildadresse der KI).
var publicHTTP = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if port != "80" && port != "443" {
				return errors.New(i18n.T("nur Port 80 und 443"))
			}
			if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
				return errors.New(i18n.T("Adresse nicht öffentlich"))
			}
			return nil
		}}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New(i18n.T("zu viele Weiterleitungen"))
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New(i18n.T("ungültige Weiterleitung"))
		}
		return nil
	},
}

var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// nichtOeffentlich: übrige Bereiche, die nie ein öffentliches Ziel sind (dieses Netz, Benchmark, IETF, Dokumentation, NAT64).
var nichtOeffentlich = func() (out []*net.IPNet) {
	for _, c := range []string{"0.0.0.0/8", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2001:db8::/32", "100::/64"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return
}()

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || cgnat.Contains(ip) || ip.Equal(net.IPv4bcast) || inNets(ip, nichtOeffentlich))
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// cleanImages löscht Bilder, auf die keine Auswertung und kein Planungsstand mehr zeigt (auch nicht im Papierkorb).
// Junge Dateien bleiben: ihr Ergebnis wird womöglich gerade erst gespeichert.
func (a *App) cleanImages() {
	texts, err := a.DB.MediaTexts()
	if err != nil {
		logf("Bilder aufräumen: %v", err)
		return
	}
	refs := map[string]bool{}
	for _, t := range texts {
		for _, m := range imageRefRe.FindAllStringSubmatch(t, -1) {
			refs[m[1]] = true
		}
	}
	removed := 0
	filepath.WalkDir(a.imageRoot(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		name := strings.TrimSuffix(d.Name(), schluessel.AudioExt)
		if refs[name] {
			return nil
		}
		if fi, err := d.Info(); err != nil || time.Since(fi.ModTime()) < imageOrphanMinAge {
			return nil
		}
		if os.Remove(p) == nil {
			removed++
		}
		return nil
	})
	if removed > 0 {
		logf("Bilder aufräumen: %d Bilder ohne Eintrag gelöscht", removed)
	}
}

var imageRefRe = regexp.MustCompile(`"bild":"([^"]+)"`)

func (a *App) handleImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	data, mod, err := a.readImage(name)
	if err != nil {
		writeErr(w, 404, "fehlt", "Bild nicht gefunden")
		return
	}
	w.Header().Set("Content-Type", imageMime(name))
	// Ein Bild ändert sich nie (neuer Inhalt = neuer Name); offline hält die Web-App es höchstens 7 Tage.
	w.Header().Set("Cache-Control", "private, max-age=604800, immutable")
	http.ServeContent(w, r, "", mod, bytes.NewReader(data))
}

// --- Trailer: YouTube Data API v3 (mit Schlüssel), sonst KI-Websuche mit Prüfung über oEmbed ---

var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var youtubeURLRe = regexp.MustCompile(`(?:youtube\.com/(?:watch\?(?:[^#\s]*&)?v=|embed/|shorts/)|youtu\.be/)([A-Za-z0-9_-]{11})`)

var trailerWordRe = regexp.MustCompile(`(?i)trailer|teaser`)
var germanWordRe = regexp.MustCompile(`(?i)deutsch|german|\(de\)|\bdt\.|\bger\b`)

func (a *App) findTrailer(ctx context.Context, m *Medium) (*float64, error) {
	lang := a.Cfg.Language
	var apiErr error
	if a.Cfg.YouTubeKey != "" {
		id, title, err := a.youtubeTrailer(ctx, m.Suche, lang)
		if err == nil {
			m.Video, m.Quelle = id, "https://www.youtube.com/watch?v="+id
			if m.Titel == "" {
				m.Titel = store.Preview(title, 80)
			}
			return nil, nil
		}
		apiErr = err
		logf("Trailer: YouTube-API: %v – versuche KI-Websuche", err) // Suchbegriff (Inhalt) nicht ins Log
	}
	cost, id, err := a.aiTrailer(ctx, m.Suche, lang)
	if err != nil {
		if apiErr != nil {
			return cost, fmt.Errorf("%v; %v", apiErr, err)
		}
		return cost, err
	}
	m.Video, m.Quelle = id, "https://www.youtube.com/watch?v="+id
	return cost, nil
}

// youtubeTrailer sucht in der App-Sprache (z. B. „… trailer deutsch“), sonst einen englischen Trailer.
func (a *App) youtubeTrailer(ctx context.Context, query, lang string) (id, title string, err error) {
	type try struct{ q, lang string }
	tries := []try{{query + " official trailer", "en"}}
	if lang == "de" {
		tries = append([]try{{query + " trailer deutsch", "de"}}, tries...)
	}
	for i, t := range tries {
		items, err := a.youtubeSearch(ctx, t.q, t.lang)
		if err != nil {
			return "", "", err
		}
		localized := t.lang != "en" && i < len(tries)-1
		for _, it := range items {
			if !trailerWordRe.MatchString(it.title) || !matchesTitle(query, it.title) {
				continue
			}
			if localized && !germanWordRe.MatchString(it.title) {
				continue
			}
			return it.id, it.title, nil
		}
	}
	return "", "", errors.New(i18n.T("kein passender Trailer auf YouTube gefunden"))
}

type ytItem struct{ id, title string }

func (a *App) youtubeSearch(ctx context.Context, q, lang string) ([]ytItem, error) {
	v := url.Values{"part": {"snippet"}, "type": {"video"}, "videoEmbeddable": {"true"}, "maxResults": {"8"},
		"q": {q}, "relevanceLanguage": {lang}, "safeSearch": {"none"}, "key": {a.Cfg.YouTubeKey}}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/youtube/v3/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, i18n.Errorf("YouTube nicht erreichbar: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
		Error *struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, i18n.Errorf("YouTube-Antwort nicht lesbar (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != 200 || out.Error != nil {
		reason := ""
		if out.Error != nil {
			reason = out.Error.Message
			if len(out.Error.Errors) > 0 {
				reason = out.Error.Errors[0].Reason
			}
		}
		return nil, i18n.Errorf("YouTube-API HTTP %d: %s", resp.StatusCode, reason)
	}
	var items []ytItem
	for _, it := range out.Items {
		if youtubeIDRe.MatchString(it.ID.VideoID) {
			items = append(items, ytItem{it.ID.VideoID, htmlUnescape(it.Snippet.Title)})
		}
	}
	return items, nil
}

// matchesTitle: der Videotitel enthält das erste markante Wort des gesuchten Titels (gegen fremde Filme).
func matchesTitle(query, title string) bool {
	t := strings.ToLower(title)
	for _, w := range strings.Fields(strings.ToLower(query)) {
		w = strings.Trim(w, ".,:;!?()[]\"'„“")
		if len([]rune(w)) < 3 || (len(w) == 4 && w[0] >= '1' && w[0] <= '2') {
			continue // kurze Wörter und Jahreszahlen
		}
		if w == "the" || w == "der" || w == "die" || w == "das" {
			continue
		}
		return strings.Contains(t, w)
	}
	return true
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&quot;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">").Replace(s)
}

const trailerInstructions = `Finde mit der Websuche den offiziellen Trailer zum genannten Film oder zur Serie auf YouTube. Sprache: %s; gibt es keinen in dieser Sprache, den englischen. Antworte nur im vorgegebenen JSON-Format: url = vollständige YouTube-Adresse des Videos (youtube.com/watch?v=… oder youtu.be/…), leer, wenn du keinen sicher findest. Nichts erfinden.`

var trailerSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"url"},
	"properties": map[string]any{"url": map[string]any{"type": "string"}}}

func (a *App) aiTrailer(ctx context.Context, query, lang string) (*float64, string, error) {
	if !a.KI().AI.HasWebSearch() {
		return nil, "", errors.New(i18n.T("kein YouTube-Schlüssel und keine Websuche"))
	}
	language := "Deutsch"
	if lang == "en" {
		language = "Englisch"
	}
	res, err := a.AI.Respond(ctx, openai.Request{Area: openai.AreaResearch, Instructions: fmt.Sprintf(trailerInstructions, language),
		Input: query, MaxOutputTokens: 1500, ReasoningEffort: "low", WebSearch: true, SearchContext: "low", MaxToolCalls: 2,
		SchemaName: "trailer", Schema: trailerSchema})
	if err != nil {
		return nil, "", err
	}
	cost := res.Cost
	var out struct {
		URL string `json:"url"`
	}
	json.Unmarshal([]byte(res.Text), &out)
	m := youtubeURLRe.FindStringSubmatch(out.URL)
	if m == nil {
		return cost, "", errors.New(i18n.T("kein Trailer gefunden"))
	}
	if err := youtubeExists(ctx, m[1]); err != nil {
		return cost, "", err
	}
	return cost, m[1], nil
}

// youtubeExists prüft über oEmbed, dass es das Video gibt und es eingebettet werden darf.
func youtubeExists(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.youtube.com/oembed?format=json&url="+
		url.QueryEscape("https://www.youtube.com/watch?v="+id), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return i18n.Errorf("YouTube nicht erreichbar: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return i18n.Errorf("Trailer-Video nicht abrufbar (HTTP %d)", resp.StatusCode)
	}
	return nil
}
