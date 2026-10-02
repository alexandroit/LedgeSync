package restore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

const account = "google-drive:restore-fixture"

type accounts struct{ mu sync.Mutex }

func (a *accounts) Status(context.Context) (driveauth.Status, error) {
	return driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: account}}, nil
}

func digestTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(p)
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRestoreRecreatesVerifiedCopyInEmptyFolderOnly(t *testing.T) {
	server := drivetest.New()
	t.Cleanup(server.Close)
	client := drive.NewWithOptions(server.Authorizer(account), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	state := filepath.Join(t.TempDir(), "state")
	service := transfer.New(app.NewService(), client, &accounts{}, state)
	source := filepath.Join(t.TempDir(), "Studio")
	files := map[string]string{".gitignore": "*.tmp\n", "a.txt": "alpha", "empty.bin": "", "nested/deep/b.bin": strings.Repeat("b", 9<<20+123), "ünïcode file.md": "text", "scratch.tmp": "excluded"}
	for name, content := range files {
		p := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(source, "empty-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination := server.AddUserFolder("Backups", "root", true)
	ctx := context.Background()
	if _, err := service.SetDestination(ctx, destination, account); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Preview(ctx, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Start(ctx, plan.PlanDigest); err != nil {
		t.Fatal(err)
	}
	for service.Busy() {
		time.Sleep(5 * time.Millisecond)
	}
	if st := service.Status(); st.State != "succeeded" {
		t.Fatalf("copy = %+v", st)
	}
	key := transfer.ProjectKey(plan.SourceIdentity, account, destination)
	if _, err = restore.PrepareTarget(filepath.Join(source, "restored"), []string{source}); domain.ErrorCode(err) != "RESTORE_TARGET_INVALID" {
		t.Fatalf("restore into the source accepted: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(source, "restored")); !os.IsNotExist(statErr) {
		t.Fatal("a rejected restore target was created inside the source")
	}
	busy := t.TempDir()
	if err = os.WriteFile(filepath.Join(busy, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = restore.PrepareTarget(busy, nil); domain.ErrorCode(err) != "RESTORE_TARGET_NOT_EMPTY" {
		t.Fatalf("non-empty restore folder accepted: %v", err)
	}
	target, err := restore.PrepareTarget(filepath.Join(t.TempDir(), "Studio restored"), []string{source, state})
	if err != nil {
		t.Fatal(err)
	}
	var runner restore.Runner
	result, err := runner.Run(ctx, client, restore.Request{StateDir: state, ProjectKey: key, Account: account, Target: target})
	if err != nil || result.State != "succeeded" || result.Files != 5 {
		t.Fatalf("restore = %+v %v", result, err)
	}
	want := digestTree(t, source)
	delete(want, "scratch.tmp")
	if got := digestTree(t, target); len(got) != len(want) {
		t.Fatalf("restored tree = %v, want %v", got, want)
	} else {
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("restored %s = %s, want %s", k, got[k], v)
			}
		}
	}
	// A file trashed in Drive is reported, never silently restored or replaced.
	for _, child := range server.Children(server.Children(destination)[0].ID) {
		if child.Name == "a.txt" {
			server.Update(child.ID, func(o *drivetest.Object) { o.Trashed = true })
		}
	}
	second, err := restore.PrepareTarget(filepath.Join(t.TempDir(), "again"), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err = (&restore.Runner{}).Run(ctx, client, restore.Request{StateDir: state, ProjectKey: key, Account: account, Target: second})
	if err != nil || result.State != "partial" || len(result.Issues) != 1 || result.Issues[0].Path != "a.txt" {
		t.Fatalf("partial restore = %+v %v", result, err)
	}
}
