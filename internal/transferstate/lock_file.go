package transferstate

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrLockBusy        = errors.New("another LedgeSync process owns this operation lock")
	ErrLockUnavailable = errors.New("the protected LedgeSync operation lock is unavailable")
)

// AcquireProcessLock acquires a nonblocking kernel lock in a native-protected
// directory. It opens no database and stores no credential data. Crashes release
// kernel ownership; the empty lock file stays in place to preserve one identity
// across contenders. Callers must invoke the idempotent release on every path.
func AcquireProcessLock(directory string) (func(), error) {
	abs, err := canonicalDirectory(directory)
	if err != nil || prepareStateDirectory(abs) != nil {
		return nil, ErrLockUnavailable
	}
	if st, err := os.Lstat(abs); err != nil || !privateNode(abs, st, true) {
		return nil, ErrLockUnavailable
	}
	filename := filepath.Join(abs, "operation.lock")
	if st, err := os.Lstat(filename); err == nil {
		if !privateNode(filename, st, false) {
			return nil, ErrLockUnavailable
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrLockUnavailable
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|noFollowFlag, 0600)
	if err != nil {
		return nil, ErrLockUnavailable
	}
	if st, err := f.Stat(); err != nil || !privateNode(filename, st, false) {
		_ = f.Close()
		return nil, ErrLockUnavailable
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if processLockContended(err) {
			return nil, ErrLockBusy
		}
		return nil, ErrLockUnavailable
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockFile(f)
			_ = f.Close()
		})
	}, nil
}
