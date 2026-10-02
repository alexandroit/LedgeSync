package transferstate

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDarwinPrivateNodeRejectsExtendedACLDespitePrivateMode(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "synthetic-node")
			var err error
			if directory {
				err = os.Mkdir(path, 0700)
			} else {
				err = os.WriteFile(path, []byte("synthetic-journal"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			st, err := os.Lstat(path)
			if err != nil || !privateNode(path, st, directory) {
				t.Fatalf("private fixture refused: %v", err)
			}
			// Only this temporary fixture is modified, never the user's settings
			// or upload source. Fixed arguments invoke the native ACL utility.
			if output, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
				t.Fatalf("could not create ACL regression fixture: %v: %s", err, output)
			}
			t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", path).Run() })
			st, err = os.Lstat(path)
			if err != nil || !privateMode(st, directory) {
				t.Fatalf("fixture did not retain the vulnerable private POSIX mode: %v", err)
			}
			if privateNode(path, st, directory) {
				t.Fatal("extended ACL bypassed private journal permissions")
			}
		})
	}
}

func TestDarwinPrivateNodeRejectsReplacedIdentityAndHardlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, filepath.Join(dir, "original")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if privateNode(path, st, false) {
		t.Fatal("replaced node identity accepted")
	}
	if err = os.Link(path, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	st, err = os.Lstat(path)
	if err != nil || privateNode(path, st, false) {
		t.Fatalf("multiply-linked journal accepted: %v", err)
	}
}

func TestDarwinACLResponseBoundsFailClosed(t *testing.T) {
	empty := make([]byte, 12)
	binary.LittleEndian.PutUint32(empty[:4], 12)
	binary.LittleEndian.PutUint32(empty[4:8], 8)
	if !emptyDarwinACL(empty) {
		t.Fatal("native no-ACL representation refused")
	}
	for name, mutate := range map[string]func([]byte){
		"truncated total": func(b []byte) { binary.LittleEndian.PutUint32(b[:4], 13) },
		"short total":     func(b []byte) { binary.LittleEndian.PutUint32(b[:4], 8) },
		"negative offset": func(b []byte) { binary.LittleEndian.PutUint32(b[4:8], ^uint32(0)) },
		"outside offset":  func(b []byte) { binary.LittleEndian.PutUint32(b[4:8], 12) },
		"truncated ACL":   func(b []byte) { binary.LittleEndian.PutUint32(b[8:12], 44) },
	} {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), empty...)
			mutate(b)
			if emptyDarwinACL(b) {
				t.Fatal("invalid security metadata accepted")
			}
		})
	}
}
