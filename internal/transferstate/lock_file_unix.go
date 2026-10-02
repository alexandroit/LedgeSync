//go:build !windows

package transferstate

import (
	"errors"

	"golang.org/x/sys/unix"
)

func processLockContended(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}
