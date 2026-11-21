// Package pebblewebhook bettet Dateien aus der Wurzel des Repos ein.
package pebblewebhook

import _ "embed"

// Changelog ist CHANGELOG.md; die Update-Seite zeigt daraus die Änderungen der laufenden Version.
//
//go:embed CHANGELOG.md
var Changelog string

// Releases ist RELEASES.md: zusammengefasste Notizen je beta/stable (nur was seit dem letzten Bestand hat).
//
//go:embed RELEASES.md
var Releases string
