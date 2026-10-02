package transferstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPrivateDescriptorRefusesForeignAccess(t *testing.T) {
	owner, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, descriptor string
		want             bool
	}{
		{"owner only", "O:" + owner.String() + "D:P(A;OICI;FA;;;" + owner.String() + ")", true},
		{"native privileged principals", "O:" + owner.String() + "D:P(A;;FA;;;" + owner.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)", true},
		{"elevated administrator owner", "O:BAD:P(A;;FA;;;" + owner.String() + ")", true},
		{"system owner", "O:SYD:P(A;;FA;;;" + owner.String() + ")", true},
		{"explicit everyone read", "O:" + owner.String() + "D:P(A;;FA;;;" + owner.String() + ")(A;;FR;;;WD)", false},
		{"inherited users read", "O:" + owner.String() + "D:P(A;;FA;;;" + owner.String() + ")(A;ID;FR;;;BU)", false},
		{"future children readable", "O:" + owner.String() + "D:P(A;;FA;;;" + owner.String() + ")(A;OICIIO;FR;;;WD)", false},
		{"foreign owner", "O:WDD:P(A;;FA;;;" + owner.String() + ")", false},
		{"other ordinary user owner", "O:S-1-5-21-101-102-103-104D:P(A;;FA;;;" + owner.String() + ")", false},
		{"null DACL", "O:" + owner.String() + "D:NO_ACCESS_CONTROL", false},
		{"missing DACL", "O:" + owner.String(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(test.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if got := privateWindowsDescriptor(sd, owner); got != test.want {
				t.Fatalf("ACL acceptance = %t, want %t", got, test.want)
			}
		})
	}
}

func setSyntheticDACL(t *testing.T, path, entries string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:P" + entries)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNewJournalBlocksBroadACLInheritance(t *testing.T) {
	base := t.TempDir()
	owner, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	ownerACE := "(A;OICI;FA;;;" + owner.String() + ")"
	setSyntheticDACL(t, base, ownerACE+"(A;OICI;FA;;;WD)")
	path := filepath.Join(base, "private-state")
	if err = prepareStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(path)
	if err != nil || !privateNode(path, st, true) {
		t.Fatalf("new state inherited foreign access: %v", err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("new state permits broad parent ACL propagation: %v", err)
	}
	file := filepath.Join(path, "synthetic.sqlite-journal")
	if err = os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err = os.Lstat(file)
	if err != nil || !privateNode(file, st, false) {
		t.Fatalf("new journal sidecar inherited foreign access: %v", err)
	}
	setSyntheticDACL(t, file, ownerACE+"(A;;FR;;;WD)")
	st, err = os.Lstat(file)
	if err != nil || privateNode(file, st, false) {
		t.Fatalf("preexisting journal granting everyone read accepted: %v", err)
	}
	setSyntheticDACL(t, path, ownerACE+"(A;;FR;;;BU)")
	st, err = os.Lstat(path)
	if err != nil || privateNode(path, st, true) {
		t.Fatalf("preexisting state granting Users read accepted: %v", err)
	}
}

func TestWindowsPrivateNodeRefusesHardlinkedJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := prepareStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "journal")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(path, "alias")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(file)
	if err != nil || privateNode(file, st, false) {
		t.Fatalf("multiply-linked journal accepted: %v", err)
	}
}

func TestWindowsPrivateNodeDoesNotFollowReparsePoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := prepareStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "original")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(path, "reparse")
	if err = os.Symlink(file, link); err != nil {
		if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			t.Skip("runner does not allow creating a synthetic Windows symbolic link")
		}
		t.Fatal(err)
	}
	// Pass the target's original FileInfo to exercise the handle reparse check,
	// as if a path changed after Lstat. Following the link would match this ID.
	if privateNode(link, st, false) {
		t.Fatal("journal security validation followed a reparse point")
	}
}
