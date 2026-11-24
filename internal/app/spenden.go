package app

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Spenden-Link: kommt aus .github/FUNDING.yml der Update-Quelle (Eintrag ko_fi), nicht aus dem Code. Abgeholt
// zusammen mit der Update-Prüfung, gespeichert in kv; ohne Datei oder Eintrag kein Link (Knopf fehlt dann).
const donateKey = "spenden-link"

var koFiRe = regexp.MustCompile(`(?m)^\s*ko_fi:\s*["']?([A-Za-z0-9_]+)["']?\s*(?:#.*)?$`)

// donateURL: gespeicherter Link ("" = keiner).
func (a *System) donateURL() string {
	v, _ := a.DB.Get(donateKey)
	return v
}

// refreshDonate holt FUNDING.yml der Update-Quelle. Netzfehler lassen den bisherigen Link stehen; fehlt die
// Datei (404) oder der Eintrag, wird der Link entfernt.
func (a *System) refreshDonate(ctx context.Context) {
	repo := a.Cfg.UpdateRepo
	if repo == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", githubAPI+"/repos/"+repo+"/contents/.github/FUNDING.yml", nil)
	req.Header.Set("Accept", "application/vnd.github.raw")
	req.Header.Set("User-Agent", "pebble-webhook/"+strings.Fields(a.Version + " dev")[0])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	link := ""
	switch resp.StatusCode {
	case 200:
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		if err != nil {
			return
		}
		if m := koFiRe.FindSubmatch(body); m != nil {
			link = "https://ko-fi.com/" + string(m[1])
		}
	case 404:
	default:
		return
	}
	if link != a.donateURL() {
		if err := a.DB.Set(donateKey, link); err != nil {
			logf("Spenden-Link nicht gespeichert: %v", err)
		}
	}
}
