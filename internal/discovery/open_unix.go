//go:build !windows

package discovery

import "golang.org/x/sys/unix"

// A path concurrently replaced by a FIFO must not block before the f.Stat
// regular-file check. The final component must not become a followed symlink.
const safeReadFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW
