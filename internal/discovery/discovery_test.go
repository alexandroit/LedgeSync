package discovery

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
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
func TestLinksAreRecordedButNeverFollowed(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink privilege unavailable")
		}
		t.Fatal(err)
	}
	tree, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	entries := tree.Entries()
	if len(entries) != 1 || entries[0].Path != "escape" || entries[0].Kind != KindSymlink || entries[0].Size != 0 {
		t.Fatalf("link entry = %+v", entries)
	}
	if _, err = tree.HashFile(context.Background(), "escape"); err == nil {
		t.Fatal("a symbolic link was read")
	}
	if tree.Has("escape/secret") {
		t.Fatal("link target was traversed")
	}
}

func TestPrunedDirectoriesAreRecordedWithoutDescendants(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"node_modules/pkg/deep", "src"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("y"), 0600); err != nil {
		t.Fatal(err)
	}
	var order []string
	tree, err := ScanWith(context.Background(), root, Options{
		Directory: func(_ context.Context, tr *Tree, dir string) error {
			order = append(order, "list:"+dir)
			if dir == "." && (!tr.Has("node_modules") || !tr.Has("src")) {
				t.Fatal("directory callback ran before its listing was complete")
			}
			return nil
		},
		Prune: func(_ context.Context, _ *Tree, dir string) (bool, error) {
			order = append(order, "prune:"+dir)
			return dir == "node_modules", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if !tree.Has("node_modules") || !tree.Pruned("node_modules") || tree.Has("node_modules/pkg") || !tree.Has("src/main.go") {
		t.Fatalf("pruning result: %+v", tree.Entries())
	}
	want := "list:. prune:node_modules prune:src list:src"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("traversal order = %q, want %q", got, want)
	}
	if dirs := tree.Directories(); len(dirs) != 2 || dirs[1] != "src" {
		t.Fatalf("traversed directories = %v", dirs)
	}
}

func TestPolicySourceChecksDetectEditsAndNewSourcesOnlyWhereTraversed(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	for _, dir := range []string{"sub", "ignored"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	rules := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(rules, []byte("ignored/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := ScanWith(ctx, root, Options{Prune: func(_ context.Context, _ *Tree, dir string) (bool, error) { return dir == "ignored", nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	check := func() error {
		return tree.CheckPolicySources(ctx, []string{".gitignore"}, []string{".gitignore"}, []string{"filters/rclone.txt"})
	}
	if err = check(); err != nil {
		t.Fatalf("unchanged sources rejected: %v", err)
	}
	if err = os.WriteFile(filepath.Join(root, "ignored", ".gitignore"), []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = check(); err != nil {
		t.Fatalf("a source inside a pruned directory cannot apply: %v", err)
	}
	if err = os.WriteFile(filepath.Join(root, "sub", ".gitignore"), []byte("file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = check(); domain.ErrorCode(err) != "RULES_CHANGED" {
		t.Fatalf("new nested source accepted: %v", err)
	}
	if err = os.Remove(filepath.Join(root, "sub", ".gitignore")); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err = os.Chtimes(rules, later, later); err != nil {
		t.Fatal(err)
	}
	if err = check(); domain.ErrorCode(err) != "RULES_CHANGED" {
		t.Fatalf("edited source accepted: %v", err)
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
