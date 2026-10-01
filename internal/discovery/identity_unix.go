//go:build darwin || linux

package discovery

import (
	"fmt"
	"os"
	"syscall"
)

func rootIdentity(_ string, info os.FileInfo) (string, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("filesystem identity unavailable")
	}
	return fmt.Sprintf("device:%d:inode:%d", s.Dev, s.Ino), nil
}
