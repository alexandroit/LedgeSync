package discovery

import (
	"context"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestMissingAndCancellation(t *testing.T) {
	if _, err := Scan(context.Background(), filepath.Join(t.TempDir(), "missing")); domain.ErrorCode(err) != "SOURCE_UNAVAILABLE" {
		t.Fatalf("missing source: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, t.TempDir()); domain.ErrorCode(err) != "CANCELLED" {
		t.Fatalf("cancel: %v", err)
	}
}
func TestSymlinksRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privilege unavailable")
		}
		t.Fatal(err)
	}
	if _, err := Scan(context.Background(), root); domain.ErrorCode(err) != "NODE_UNSUPPORTED" {
		t.Fatalf("symlink accepted: %v", err)
	}
}
func TestContentAndDirectoryChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "sub", "a")
	if err := os.WriteFile(p, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	hash, err := tree.HashFile(context.Background(), "sub/a")
	if err != nil || hash != domain.HashBytes([]byte("one")) {
		t.Fatalf("hash: %s %v", hash, err)
	}
	if err = os.WriteFile(p, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = tree.HashFile(context.Background(), "sub/a"); domain.ErrorCode(err) != "SCAN_INCOMPLETE" {
		t.Fatalf("changed content accepted: %v", err)
	}
	if err = tree.Revalidate(); domain.ErrorCode(err) != "SCAN_INCOMPLETE" {
		t.Fatalf("stale inventory accepted: %v", err)
	}
}
func TestRootReplacementIdentity(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "source")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	first := a.Identity()
	a.Close()
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(root, filepath.Join(base, "old")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(root, time.Now(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	b, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if first == b.Identity() {
		t.Fatal("replacement root reused old identity")
	}
}
