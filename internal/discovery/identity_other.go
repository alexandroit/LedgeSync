//go:build !darwin && !linux && !windows

package discovery

import (
	"fmt"
	"os"
)

func rootIdentity(_ string, _ os.FileInfo) (string, error) {
	return "", fmt.Errorf("filesystem identity is unsupported on this platform")
}
