//go:build !windows && !darwin

package transferstate

import "os"

// On Linux, the POSIX ACL mask is represented by the group mode bits. Requiring
// those bits to be zero also prevents named users/groups from gaining access.
func privateNode(_ string, st os.FileInfo, directory bool) bool {
	return st != nil && st.Mode()&os.ModeSymlink == 0 && st.IsDir() == directory && (directory || st.Mode().IsRegular()) && privateMode(st, directory)
}
