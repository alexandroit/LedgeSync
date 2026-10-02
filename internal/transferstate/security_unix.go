//go:build !windows

package transferstate

import "os"

func prepareStateDirectory(path string) error { return os.MkdirAll(path, 0700) }
