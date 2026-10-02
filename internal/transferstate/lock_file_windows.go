package transferstate

import (
	"errors"

	"golang.org/x/sys/windows"
)

func processLockContended(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
