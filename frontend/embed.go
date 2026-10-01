//go:build desktop && !bindings

// Package frontend contains the local, bundled desktop interface.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundled embed.FS

func Assets() (fs.FS, error) { return fs.Sub(bundled, "dist") }
