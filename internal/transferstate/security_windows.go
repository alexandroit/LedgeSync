package transferstate

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

// New directories have a protected owner-only DACL from the moment they are
// created. The inheritable ACE also protects SQLite's own journal sidecars.
// Existing directories are never silently repaired; privateNode rejects them.
func prepareStateDirectory(path string) error {
	owner, err := currentUserSID()
	if err != nil {
		return failure()
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + owner.String() + "D:P(A;OICI;FA;;;" + owner.String() + ")")
	if err != nil {
		return failure()
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	return createPrivateDirectories(path, &attributes)
}

func createPrivateDirectories(path string, attributes *windows.SecurityAttributes) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return failure()
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure()
	}
	parent := filepath.Dir(path)
	if parent == path {
		return failure()
	}
	if err := createPrivateDirectories(parent, attributes); err != nil {
		return err
	}
	native, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return failure()
	}
	if err = windows.CreateDirectory(native, attributes); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return failure()
	}
	return nil
}

func privateNode(path string, st os.FileInfo, directory bool) bool {
	if st == nil || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != directory || (!directory && !st.Mode().IsRegular()) {
		return false
	}
	owner, err := currentUserSID()
	if err != nil {
		return false
	}
	native, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(native, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(handle), path)
	defer f.Close()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return false
	}
	current, err := f.Stat()
	if err != nil || !os.SameFile(st, current) {
		return false
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	return err == nil && privateWindowsDescriptor(sd, owner)
}

func privateWindowsDescriptor(sd *windows.SECURITY_DESCRIPTOR, current *windows.SID) bool {
	if sd == nil || !sd.IsValid() || current == nil || !current.IsValid() {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !trustedWindowsPrincipal(owner, current) {
		return false
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false // A null DACL grants everyone full access.
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, i, &ace) != nil || ace == nil || ace.Header.AceSize < uint16(unsafe.Sizeof(windows.ACCESS_ALLOWED_ACE{})) {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue // Denials cannot grant access, including inherited denials.
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false // Do not guess at object/callback/conditional ACE layouts.
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if int(ace.Header.AceSize) < 8+sid.Len() || !sid.IsValid() {
			return false
		}
		// SYSTEM and administrators are the native privileged principals; both
		// can already take ownership, like root on Unix. Other grants (including
		// inherit-only grants to future SQLite files) are rejected outright.
		if !trustedWindowsPrincipal(sid, current) {
			return false
		}
	}
	return true
}

func trustedWindowsPrincipal(sid, current *windows.SID) bool {
	// Elevated Windows tokens can create files owned by Administrators rather
	// than their user SID. These privileged owners already have takeover powers;
	// privacy still requires that the DACL grant only these same principals.
	return sid.IsValid() && (sid.Equals(current) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}
