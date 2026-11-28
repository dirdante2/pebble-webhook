package config

import (
	"encoding/json"
	"log"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"pebble-webhook/internal/i18n"
)

// Bereiche der KI. Jeder Bereich hat eine Reihenfolge von Einträgen: der erste nutzbare erledigt den Aufruf;
// scheitert er oder ist sein Monatslimit erreicht, übernimmt der nächste.
const (
	AreaTranscribe = "transkription"
	AreaTitle      = "kurztitel"
	AreaDictation  = "auswertung"
	AreaResearch   = "recherche"
	AreaImage      = "bilder"
	AreaDecision   = "entscheidung" // Entscheidungsmodelle (OpenRouter Decisions-API): Art der Notiz vorab
)

// AIAreas: alle Bereiche in der Reihenfolge der Oberfläche.
var AIAreas = []string{AreaTranscribe, AreaTitle, AreaDictation, AreaResearch, AreaImage, AreaDecision}

// Besondere Einträge der Transkription (kein Zugang): Whisper auf diesem Server und der Text der Pebble-App.
const (
	EntryWhisper = "whisper"
	EntryPebble  = "pebble"
)

// AIAccess: ein Zugang mit eindeutigem Namen; mehrere gleicher Art sind möglich.
type AIAccess struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"art"`
	URL  string `json:"adresse,omitempty"`
	Key  string `json:"schluessel,omitempty"`
}

// Type: die Art des Zugangs aus der Liste der Anbieter-Arten (nil = unbekannt).
func (a *AIAccess) Type() *ProviderType { return ProviderTypeByID(a.Kind) }

// Cloud: Zugang mit Preisen und Kostenlimit (lokale und Suchmaschinen kosten nichts).
func (a *AIAccess) Cloud() bool { t := a.Type(); return t != nil && t.Cloud }

// AIEntry: ein Eintrag der Reihenfolge eines Bereichs. Preise in USD (Tokens je 1 Mio.), Limit je Kalendermonat.
type AIEntry struct {
	ID          string   `json:"id"`
	Access      string   `json:"zugang"` // ID eines Zugangs, whisper oder pebble
	Model       string   `json:"modell,omitempty"`
	PriceIn     *float64 `json:"preis_ein,omitempty"`
	PriceOut    *float64 `json:"preis_aus,omitempty"`
	PriceMinute *float64 `json:"preis_minute,omitempty"`
	PriceSearch *float64 `json:"preis_suche,omitempty"`
	PriceImage  *float64 `json:"preis_bild,omitempty"`
	LimitMonth  float64  `json:"limit_monat,omitempty"` // 0 = ohne Limit
	Search      []string `json:"websuche,omitempty"`    // Webrecherche: „anbieter“ und/oder ID eines SearXNG-Zugangs
	Off         bool     `json:"inaktiv,omitempty"`     // in der Gruppe „Inaktiv“: bleibt gespeichert, wird nicht genutzt
	// Private: nur Anbieter ohne Datenspeicherung (OpenRouter: provider.zdr, data_collection deny).
	Private bool `json:"datenschutz,omitempty"`
	// Effort: Nachdenken des Modells – "" = Vorgabe, none = aus, low, medium, high.
	Effort string `json:"aufwand,omitempty"`
}

// Active: die genutzten Einträge eines Bereichs in ihrer Reihenfolge.
func (a *AI) Active(area string) []AIEntry {
	var out []AIEntry
	for _, e := range a.Areas[area] {
		if !e.Off {
			out = append(out, e)
		}
	}
	return out
}

// AI: Zugänge und Bereiche (Einstellungen KI_ZUGAENGE und KI_BEREICHE, als JSON).
type AI struct {
	Accesses []AIAccess           `json:"zugaenge"`
	Areas    map[string][]AIEntry `json:"bereiche"`
}

// Access liefert den Zugang zu einer ID (nil = keiner).
func (a *AI) Access(id string) *AIAccess {
	for i := range a.Accesses {
		if a.Accesses[i].ID == id {
			return &a.Accesses[i]
		}
	}
	return nil
}

// UsesWhisper: die Transkription nutzt Whisper auf diesem Server.
func (a *AI) UsesWhisper() bool {
	return slices.ContainsFunc(a.Active(AreaTranscribe), func(e AIEntry) bool { return e.Access == EntryWhisper })
}

// PebbleText: der Text der Pebble-App wird übernommen (erster Eintrag der Transkription).
func (a *AI) PebbleText() bool {
	t := a.Active(AreaTranscribe)
	return len(t) > 0 && t[0].Access == EntryPebble
}

// OnlyPebble: es gibt nichts außer dem Text der Pebble-App (neu transkribieren geht dann nicht).
func (a *AI) OnlyPebble() bool {
	return !slices.ContainsFunc(a.Active(AreaTranscribe), func(e AIEntry) bool { return e.Access != EntryPebble })
}

// HasWebSearch: mindestens ein Eintrag der Webrecherche kann im Web suchen.
func (a *AI) HasWebSearch() bool {
	for _, e := range a.Active(AreaResearch) {
		if len(e.Search) > 0 {
			return true
		}
	}
	return false
}

var aiIDRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// parseAI liest KI_ZUGAENGE und KI_BEREICHE; fehlen beide, entstehen sie aus den früheren Einzel-Einstellungen.
func parseAI(str func(k, def string) string) (AI, []string) {
	accRaw, areaRaw := str("KI_ZUGAENGE", ""), str("KI_BEREICHE", "")
	if accRaw == "" && areaRaw == "" {
		return deriveAI(str), nil
	}
	ai := AI{Areas: map[string][]AIEntry{}}
	if accRaw != "" {
		if err := json.Unmarshal([]byte(accRaw), &ai.Accesses); err != nil {
			return ai, []string{i18n.T("KI-Zugänge nicht lesbar")}
		}
	}
	if areaRaw != "" {
		if err := json.Unmarshal([]byte(areaRaw), &ai.Areas); err != nil {
			return ai, []string{i18n.T("KI-Bereiche nicht lesbar")}
		}
	}
	return ai, nil
}

// deriveAI: Zugänge und Bereiche aus den Einzel-Einstellungen bis 4.3 (OPENAI_API_KEY, TITLE_PROVIDER, …).
func deriveAI(str func(k, def string) string) AI {
	ai := AI{Areas: map[string][]AIEntry{}}
	on := func(k, def string) bool { return str(k, def) == "aktiv" }
	if key := str("OPENAI_API_KEY", ""); key != "" && on("OPENAI_ENABLED", "aktiv") {
		ai.Accesses = append(ai.Accesses, AIAccess{ID: "openai", Name: "OpenAI", Kind: "openai", URL: strings.TrimRight(str("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"), Key: key})
	}
	if u := str("SERVICE_BASE_URL", ""); u != "" && on("SERVICE_ENABLED", "inaktiv") {
		ai.Accesses = append(ai.Accesses, AIAccess{ID: "dienst", Name: "Dienst", Kind: "dienst", URL: strings.TrimRight(u, "/"), Key: str("SERVICE_API_KEY", "")})
	}
	if u := str("LOCAL_BASE_URL", ""); u != "" && on("LOCAL_ENABLED", "inaktiv") {
		ai.Accesses = append(ai.Accesses, AIAccess{ID: "lokal", Name: "Lokal", Kind: "lokal", URL: strings.TrimRight(u, "/")})
	}
	if u := str("SEARXNG_URL", ""); u != "" && on("SEARXNG_ENABLED", "inaktiv") {
		ai.Accesses = append(ai.Accesses, AIAccess{ID: "searxng", Name: "SearXNG", Kind: "searxng", URL: strings.TrimRight(u, "/")})
	}
	price := func(k string) *float64 {
		f, err := strconv.ParseFloat(str(k, ""), 64)
		if err != nil || f < 0 {
			return nil
		}
		return &f
	}
	add := func(area, access string, e AIEntry) {
		a := ai.Access(access)
		if a == nil && access != EntryWhisper && access != EntryPebble {
			return
		}
		e.ID, e.Access = area+"-"+strconv.Itoa(len(ai.Areas[area])+1), access
		if a == nil || !a.Cloud() {
			e.PriceIn, e.PriceOut, e.PriceMinute, e.PriceSearch, e.PriceImage = nil, nil, nil, nil, nil
		}
		ai.Areas[area] = append(ai.Areas[area], e)
	}
	cloud := AIEntry{Model: str("OPENAI_TRANSCRIBE_MODEL", "gpt-4o-transcribe"), PriceMinute: price("PRICE_TRANSCRIBE_PER_MIN")}
	provider := str("TRANSCRIBE_PROVIDER", "openai")
	switch str("TRANSCRIPTION", "openai") {
	case "pebble":
		add(AreaTranscribe, EntryPebble, AIEntry{})
		add(AreaTranscribe, provider, cloud)
	case "lokal":
		add(AreaTranscribe, EntryWhisper, AIEntry{})
	case "lokal-openai":
		add(AreaTranscribe, EntryWhisper, AIEntry{})
		add(AreaTranscribe, provider, cloud)
	default:
		add(AreaTranscribe, provider, cloud)
	}
	add(AreaTitle, str("TITLE_PROVIDER", "openai"), AIEntry{Model: str("OPENAI_TITLE_MODEL", ""), PriceIn: price("PRICE_TITLE_INPUT_PER_MTOK"), PriceOut: price("PRICE_TITLE_OUTPUT_PER_MTOK")})
	dictIn, dictOut := price("PRICE_DICTATION_INPUT_PER_MTOK"), price("PRICE_DICTATION_OUTPUT_PER_MTOK")
	if dictIn == nil && dictOut == nil {
		dictIn, dictOut = price("PRICE_PLAN_INPUT_PER_MTOK"), price("PRICE_PLAN_OUTPUT_PER_MTOK")
	}
	// Websuche wie früher für Webrecherche und Auswertung gemeinsam (RESEARCH_WEB_SEARCH).
	search := func(provider string) []string {
		var out []string
		for _, s := range strings.Split(str("RESEARCH_WEB_SEARCH", "anbieter"), ",") {
			switch {
			case s == "anbieter" && provider != "lokal":
				out = append(out, "anbieter")
			case s == "searxng" && ai.Access("searxng") != nil:
				out = append(out, "searxng")
			}
		}
		return out
	}
	withSearch := func(e AIEntry, provider string) AIEntry {
		e.Search = search(provider)
		if slices.Contains(e.Search, "anbieter") {
			e.PriceSearch = price("PRICE_SEARCH_PER_CALL")
		}
		return e
	}
	dictation := str("DICTATION_PROVIDER", "openai")
	add(AreaDictation, dictation, withSearch(AIEntry{Model: str("OPENAI_DICTATION_MODEL", str("OPENAI_PLAN_MODEL", "")), PriceIn: dictIn, PriceOut: dictOut}, dictation))
	research := str("RESEARCH_PROVIDER", "openai")
	add(AreaResearch, research, withSearch(AIEntry{Model: str("OPENAI_PLAN_MODEL", ""), PriceIn: price("PRICE_PLAN_INPUT_PER_MTOK"), PriceOut: price("PRICE_PLAN_OUTPUT_PER_MTOK")}, research))
	if on("IMAGES_GENERATE", "aktiv") {
		add(AreaImage, "openai", AIEntry{Model: str("OPENAI_IMAGE_MODEL", "gpt-image-1-mini"), PriceImage: price("PRICE_IMAGE_PER_PICTURE")})
	}
	return ai
}

// checkAI prüft Zugänge und Bereiche: eindeutige Namen, passende Art je Bereich, Preise bei Cloud-Anbietern.
func (a *AI) check() (errs []string) {
	ids, names := map[string]bool{}, map[string]bool{}
	for _, x := range a.Accesses {
		name := strings.TrimSpace(x.Name)
		switch {
		case !aiIDRe.MatchString(x.ID) || ids[x.ID] || x.ID == EntryWhisper || x.ID == EntryPebble:
			errs = append(errs, i18n.Tf("Zugang „%s“: ungültige oder doppelte Kennung", name))
		case name == "":
			errs = append(errs, i18n.T("Ein Zugang hat keinen Namen"))
		case names[strings.ToLower(name)]:
			errs = append(errs, i18n.Tf("Zugang „%s“: Name doppelt", name))
		case x.Type() == nil:
			errs = append(errs, i18n.Tf("Zugang „%s“: unbekannte Art", name))
		case x.URL == "":
			errs = append(errs, i18n.Tf("Zugang „%s“: Adresse fehlt", name))
		case x.Type().Key == KeyRequired && x.Key == "":
			errs = append(errs, i18n.Tf("Zugang „%s“: Schlüssel fehlt", name))
		}
		if x.URL != "" {
			if u, err := url.Parse(x.URL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(x.URL, " \t\n<>\"'") {
				errs = append(errs, i18n.Tf("Zugang „%s“: keine gültige http(s)-Adresse", name))
			}
		}
		ids[x.ID], names[strings.ToLower(name)] = true, true
	}
	entryIDs := map[string]bool{}
	for area, list := range a.Areas {
		if !slices.Contains(AIAreas, area) {
			errs = append(errs, i18n.Tf("Unbekannter KI-Bereich „%s“", area))
			continue
		}
		active, seen := 0, map[string]bool{}
		for i, e := range list {
			label := i18n.Tf("%s, Eintrag %d", i18n.T(areaLabel[area]), i+1)
			if !e.Off {
				active++
			}
			if seen[e.Access] {
				errs = append(errs, label+i18n.T(": Zugang steht doppelt in diesem Bereich"))
			}
			seen[e.Access] = true
			if !aiIDRe.MatchString(e.ID) || entryIDs[e.ID] {
				errs = append(errs, label+i18n.T(": ungültige oder doppelte Kennung"))
			}
			entryIDs[e.ID] = true
			acc := a.Access(e.Access)
			kind := e.Access
			if acc != nil {
				kind = acc.Kind
			}
			fits := (acc == nil && area == AreaTranscribe && (e.Access == EntryWhisper || e.Access == EntryPebble)) ||
				(acc != nil && acc.Type() != nil && slices.Contains(acc.Type().Areas, area))
			if !fits {
				errs = append(errs, label+i18n.T(": Zugang fehlt oder passt nicht zu diesem Bereich"))
				continue
			}
			if kind == EntryPebble && !e.Off && active > 1 {
				errs = append(errs, label+i18n.T(": „Text der Pebble-App“ geht nur als erster Eintrag"))
			}
			if acc != nil && (e.Model == "" || !modelRe.MatchString(e.Model)) {
				errs = append(errs, label+i18n.T(": Modell fehlt oder ist ungültig"))
			}
			if e.LimitMonth < 0 || math.IsNaN(e.LimitMonth) {
				errs = append(errs, label+i18n.T(": Limit ist keine Zahl ab 0"))
			}
			// Ohne Preise und ohne gemeldete Kosten bleibt der Verbrauch unbekannt – ein Limit würde nie greifen.
			if acc != nil && acc.Type() != nil && e.LimitMonth > 0 && acc.Type().PricesOptional && !acc.Type().ReportsCost && !entryHasPrices(area, e) {
				errs = append(errs, label+i18n.T(": Monatslimit braucht Preise (dieser Anbieter meldet keine Kosten)"))
			}
			for _, p := range []*float64{e.PriceIn, e.PriceOut, e.PriceMinute, e.PriceSearch, e.PriceImage} {
				if p != nil && (*p < 0 || math.IsNaN(*p) || *p > 1_000_000) {
					errs = append(errs, label+i18n.T(": Preis ist keine Zahl ab 0"))
					break
				}
			}
			// Preise nötig bei Cloud-Anbietern, außer sie sind freiwillig (Kosten meldet der Anbieter oder bleiben offen).
			cloud := acc != nil && acc.Cloud() && !acc.Type().PricesOptional
			switch {
			case !cloud:
			case area == AreaTranscribe && e.PriceMinute == nil:
				errs = append(errs, label+i18n.T(": Preis je Minute fehlt"))
			case area == AreaImage && e.PriceImage == nil:
				errs = append(errs, label+i18n.T(": Preis je Bild fehlt"))
			case area != AreaTranscribe && area != AreaImage && (e.PriceIn == nil || e.PriceOut == nil):
				errs = append(errs, label+i18n.T(": Preise für Eingabe und Ausgabe fehlen"))
			}
			for _, s := range e.Search {
				switch {
				case area != AreaResearch && area != AreaDictation:
					errs = append(errs, label+i18n.T(": Websuche gibt es nur bei Auswertung und Webrecherche"))
				case s == "anbieter" && (acc == nil || acc.Type() == nil || !acc.Type().Search):
					errs = append(errs, label+i18n.T(": ein lokales Modell sucht nicht selbst im Web – SearXNG wählen oder ohne Websuche"))
				case s == "anbieter" && e.PriceSearch == nil && cloud:
					errs = append(errs, label+i18n.T(": Preis je Websuche fehlt"))
				case s != "anbieter" && (a.Access(s) == nil || a.Access(s).Type() == nil || a.Access(s).Type().API != APISearxng):
					errs = append(errs, label+i18n.T(": Websuche über einen unbekannten SearXNG-Zugang"))
				}
			}
		}
	}
	return errs
}

// areaLabel: Name eines Bereichs in Meldungen.
var areaLabel = map[string]string{AreaTranscribe: "Transkription", AreaTitle: "Kurztitel", AreaDictation: "Auswertung & Sortierung",
	AreaResearch: "Webrecherche", AreaImage: "Bilder", AreaDecision: "Entscheidung"}

// normalize: Lokale Einträge und Sondereinträge tragen keine Preise und kein Limit.
func (a *AI) normalize() {
	for i := range a.Accesses {
		x := &a.Accesses[i]
		x.Name = strings.TrimSpace(x.Name)
		x.URL = strings.TrimRight(strings.TrimSpace(x.URL), "/")
		t := x.Type()
		if t == nil {
			continue
		}
		if t.URL != "" && (x.URL == "" || t.FixedURL) {
			x.URL = t.URL
		}
		if t.Key == KeyNone {
			x.Key = ""
		}
	}
	for area, list := range a.Areas {
		for i := range list {
			e := &list[i]
			e.Model = strings.TrimSpace(e.Model)
			// OpenAI: nur Modelle der Auswahl; eines, das ein Update entfernt hat, wird durch die Vorgabe ersetzt.
			if acc := a.Access(e.Access); acc != nil && acc.Type() != nil && len(acc.Type().Models[area]) > 0 && !acc.Type().HasModel(area, e.Model) {
				def := acc.Type().Models[area][0]
				if e.Model != "" {
					log.Printf("KI: Modell %s (%s) nicht mehr in der Auswahl – ersetzt durch %s", e.Model, area, def.ID)
				}
				e.Model = def.ID
				// Katalog ohne Preise (freiwillige Preise): nichts eintragen – 0 hieße „kostenlos“ statt „unbekannt“.
				set := func(dst **float64, v float64) {
					if v > 0 || !acc.Type().PricesOptional {
						*dst = &v
					}
				}
				if area == AreaTranscribe {
					set(&e.PriceMinute, def.PriceMinute)
				} else if area == AreaImage {
					set(&e.PriceImage, def.PriceImage)
				} else {
					set(&e.PriceIn, def.PriceIn)
					set(&e.PriceOut, def.PriceOut)
				}
			}
			if acc := a.Access(e.Access); acc == nil || !acc.Cloud() {
				e.PriceIn, e.PriceOut, e.PriceMinute, e.PriceSearch, e.PriceImage, e.LimitMonth = nil, nil, nil, nil, nil, 0
			}
			if !slices.Contains(e.Search, "anbieter") {
				e.PriceSearch = nil
			}
			if !slices.Contains([]string{"none", "low", "medium", "high"}, e.Effort) || area == AreaTranscribe || area == AreaImage {
				e.Effort = ""
			}
		}
		a.Areas[area] = list
	}
}

// entryHasPrices: Eintrag trägt die Preise, die sein Bereich zum Rechnen braucht.
func entryHasPrices(area string, e AIEntry) bool {
	switch area {
	case AreaTranscribe:
		return e.PriceMinute != nil
	case AreaImage:
		return e.PriceImage != nil
	}
	return e.PriceIn != nil && e.PriceOut != nil
}
