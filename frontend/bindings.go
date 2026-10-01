//go:build bindings

package frontend

import "io/fs"

// Wails generates bindings before it builds the frontend. The generator does
// not start a window or serve assets; the desktop build always embeds dist.
func Assets() (fs.FS, error) { return nil, nil }
