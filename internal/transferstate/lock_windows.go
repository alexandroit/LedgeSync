package transferstate

import (
	"golang.org/x/sys/windows"
	"os"
)

const noFollowFlag = 0

// The private per-user configuration directory inherits the user's Windows ACL.
// Unix permission bits are not meaningful on this platform.
func privateMode(st os.FileInfo, directory bool) bool { return st.Mode()&os.ModeSymlink == 0 }

func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func unlockFile(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
