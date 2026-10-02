//go:build !windows

package transferstate

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

const noFollowFlag = unix.O_NOFOLLOW

func privateMode(st os.FileInfo, directory bool) bool {
	if st.Mode().Perm()&0077 != 0 {
		return false
	}
	info, ok := st.Sys().(*syscall.Stat_t)
	return ok && info.Uid == uint32(os.Geteuid()) && (directory || info.Nlink == 1)
}

func lockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlockFile(f *os.File)     { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
