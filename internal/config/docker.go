package config

import (
	"os"
	"strings"
	"sync"
)

// DockerMarker: liegt nur im Docker-Image (vom Dockerfile geschrieben) und enthält den Image-Namen ohne Tag.
// Bewusst eine Datei statt einer Umgebungsvariable: im Container gilt der Docker-Modus immer, er lässt sich nicht abschalten.
const DockerMarker = "/etc/pebble-webhook-docker"

var docker = sync.OnceValues(func() (bool, string) {
	b, err := os.ReadFile(DockerMarker)
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(b))
})

// InDocker: läuft im Docker-Image (kein Updater, Updates nur per neuem Image).
func InDocker() bool { d, _ := docker(); return d }

// DockerImage: Image-Name ohne Tag (für den angezeigten docker-pull-Befehl), leer außerhalb von Docker.
func DockerImage() string { _, img := docker(); return img }
