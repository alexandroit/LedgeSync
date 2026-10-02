package transferstate

import (
	"encoding/binary"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func privateNode(path string, st os.FileInfo, directory bool) bool {
	if st == nil || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != directory || (!directory && !st.Mode().IsRegular()) || !privateMode(st, directory) {
		return false
	}
	// Query the ACL on the opened node, not a second pathname lookup. macOS
	// extended ACLs can grant access even when POSIX permission bits are 0600.
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(st, current) || current.IsDir() != directory || !privateMode(current, directory) {
		return false
	}
	attributes := unix.Attrlist{Bitmapcount: 5, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	// Darwin's documented maximum is 128 ACEs, each 24 bytes, plus the
	// kauth_filesec header and packed attribute reference. Oversize fails closed.
	var buffer [4096]byte
	_, _, errno := unix.Syscall6(unix.SYS_FGETATTRLIST, f.Fd(), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0, 0)
	return errno == 0 && emptyDarwinACL(buffer[:])
}

// ATTR_CMN_EXTENDED_SECURITY returns a packed uint32 length, attrreference_t
// (int32 offset, uint32 length), and kauth_filesec. Layouts are defined in the
// native sys/attr.h and sys/kauth.h and identical on supported Darwin targets.
func emptyDarwinACL(buffer []byte) bool {
	if len(buffer) < 12 {
		return false
	}
	total := uint64(binary.LittleEndian.Uint32(buffer[:4]))
	offset := int64(int32(binary.LittleEndian.Uint32(buffer[4:8]))) + 4
	size := uint64(binary.LittleEndian.Uint32(buffer[8:12]))
	if total < 12 || total > uint64(len(buffer)) || offset < 12 || uint64(offset) > total || size > total-uint64(offset) {
		return false
	}
	if size == 0 {
		return true
	}
	if size < 44 {
		return false
	}
	security := buffer[offset : uint64(offset)+size]
	if binary.LittleEndian.Uint32(security[:4]) != 0x012cc16d {
		return false
	}
	count := binary.LittleEndian.Uint32(security[36:40])
	return size == 44 && (count == 0 || count == ^uint32(0))
}
