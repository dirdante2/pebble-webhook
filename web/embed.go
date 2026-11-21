// Package web bettet die statische PWA in das Binary ein.
package web

import "embed"

//go:embed all:static
var Files embed.FS
