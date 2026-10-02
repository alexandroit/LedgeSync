package transferstate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

func TestStorePersistsIntentAcknowledgementAndVerifiedIdentity(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(base, "journal")
	store, err := Open(directory, source)
	if err != nil {
		t.Fatal(err)
	}
	project := Project{Key: "project", SourceIdentity: "source-id", AccountReference: "account-id", DestinationID: "destination-id"}
	if err = store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	node := Node{Path: "nested/file.txt", Kind: "file", Name: "file.txt", ID: "reserved-id", ParentID: "parent-id", OperationID: "operation-id", SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32), Size: 5, Status: "intent"}
	if err = store.SaveNode(project.Key, node); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginRun("run-id", project.Key, struct{ Digest string }{"approved-plan"}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(directory, source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load(project.Key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SourceIdentity != project.SourceIdentity || loaded.AccountReference != project.AccountReference || loaded.DestinationID != project.DestinationID || loaded.Nodes[node.OperationID] != node {
		t.Fatalf("intent identity changed across restart: %+v", loaded)
	}
	node.Status = "acknowledged"
	if err = store.SaveNode(project.Key, node); err != nil {
		t.Fatal(err)
	}
	node.Status = "verified"
	if err = store.SaveNode(project.Key, node); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishRun("run-id", "succeeded", map[string]string{"state": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(project.Key)
	if err != nil || loaded.Nodes[node.OperationID].Status != "verified" {
		t.Fatalf("verified state was not durable: %+v %v", loaded, err)
	}
	var state string
	if err = store.db.QueryRow("SELECT state FROM runs WHERE id='run-id'").Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("run finalization missing: %s %v", state, err)
	}
	var events int
	if err = store.db.QueryRow("SELECT count(*) FROM run_events WHERE run_id='run-id'").Scan(&events); err != nil || events != 1 {
		t.Fatalf("run event missing: %d %v", events, err)
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{"writer.lock", "transfers.sqlite"} {
			info, e := os.Stat(filepath.Join(directory, name))
			if e != nil || info.Mode().Perm()&0077 != 0 {
				t.Fatalf("state file is not private: %s %v", name, e)
			}
		}
	}
}

func TestStoreRejectsStateInsideSourceWithoutCreatingFiles(t *testing.T) {
	source := t.TempDir()
	for _, directory := range []string{source, filepath.Join(source, "private-state"), filepath.Join(source, "nested", "journal")} {
		t.Run(filepath.Base(directory), func(t *testing.T) {
			store, err := Open(directory, source)
			if store != nil {
				store.Close()
				t.Fatal("state inside upload root accepted")
			}
			if domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
				t.Fatalf("expected source-boundary failure, got %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "writer.lock")); !os.IsNotExist(err) {
				t.Fatalf("unsafe state directory modified source: %v", err)
			}
		})
	}
}

func TestStoreRejectsSymlinkedDirectoryAndJournalFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host-specific privilege; Unix coverage exercises refusal")
	}
	for _, target := range []string{"directory", "writer.lock", "transfers.sqlite", "transfers.sqlite-journal", "transfers.sqlite-wal", "transfers.sqlite-shm"} {
		t.Run(target, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source")
			directory := filepath.Join(base, "state")
			outside := filepath.Join(base, "outside")
			for _, dir := range []string{source, directory, outside} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if target == "directory" {
				directory = filepath.Join(base, "linked-state")
				if err := os.Symlink(outside, directory); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(sentinel, filepath.Join(directory, target)); err != nil {
				t.Fatal(err)
			}
			store, err := Open(directory, source)
			if store != nil {
				store.Close()
				t.Fatal("symlinked state accepted")
			}
			if domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
				t.Fatalf("expected safe unavailable error, got %v", err)
			}
			content, err := os.ReadFile(sentinel)
			if err != nil || string(content) != "untouched" {
				t.Fatal("symlink target changed")
			}
		})
	}
}

func TestStoreKernelLockCrossProcessAndRelease(t *testing.T) {
	if os.Getenv("LEDGESYNC_TRANSFER_LOCK_CHILD") == "1" {
		store, err := Open(os.Getenv("LEDGESYNC_TRANSFER_LOCK_DIR"), os.Getenv("LEDGESYNC_TRANSFER_LOCK_SOURCE"))
		if store != nil {
			store.Close()
			t.Fatal("child acquired another process writer lock")
		}
		if domain.ErrorCode(err) != "TRANSFER_BUSY" {
			t.Fatalf("unexpected lock result: %v", err)
		}
		return
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	directory := filepath.Join(base, "journal")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(directory, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStoreKernelLockCrossProcessAndRelease$", "-test.count=1")
	cmd.Env = append(os.Environ(), "LEDGESYNC_TRANSFER_LOCK_CHILD=1", "LEDGESYNC_TRANSFER_LOCK_DIR="+directory, "LEDGESYNC_TRANSFER_LOCK_SOURCE="+source)
	output, childErr := cmd.CombinedOutput()
	if childErr != nil {
		store.Close()
		t.Fatalf("cross-process writer lock failed: %v\n%s", childErr, output)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(directory, source)
	if err != nil {
		t.Fatalf("released process lock remained stuck: %v", err)
	}
	reopened.Close()
}

func TestStoreRejectsCorruptOrFutureJournalWithoutResettingIt(t *testing.T) {
	for _, kind := range []string{"corrupt payload", "future version"} {
		t.Run(kind, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "private-journal")
			store, err := Open(directory, "")
			if err != nil {
				t.Fatal(err)
			}
			project := Project{Key: "project", SourceIdentity: "source", AccountReference: "account", DestinationID: "destination"}
			if err = store.SaveProject(project); err != nil {
				t.Fatal(err)
			}
			if kind == "future version" {
				if _, err = store.db.Exec("PRAGMA user_version=999"); err != nil {
					t.Fatal(err)
				}
				store.Close()
				store, err = Open(directory, "")
				if store != nil {
					store.Close()
					t.Fatal("future schema silently accepted")
				}
				if domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
					t.Fatalf("unexpected schema failure: %v", err)
				}
				return
			}
			defer store.Close()
			_, err = store.db.Exec("INSERT INTO operations(id,project_id,payload) VALUES(?,?,?)", "operation", "project", []byte(`{"OperationID":"operation","ID":"reserved","ParentID":"parent","Path":"../outside"}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.Load("project"); domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
				t.Fatalf("unsafe corrupt path accepted: %v", err)
			}
			if strings.Contains(errText(err), "../outside") {
				t.Fatal("state failure exposed payload")
			}
			var count int
			if err = store.db.QueryRow("SELECT count(*) FROM operations").Scan(&count); err != nil || count != 1 {
				t.Fatal("corrupt state was reset instead of retained for review")
			}
		})
	}
}
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestStoreRejectsInvalidOperationMetadataOnWriteAndRead(t *testing.T) {
	valid := Node{Path: "nested/file", Kind: "file", Name: "file", ID: "reserved", ParentID: "parent", OperationID: "operation", SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32), Size: 5, Status: "verified"}
	cases := map[string]func(*Node){
		"unknown status":         func(n *Node) { n.Status = "trusted" },
		"unknown kind":           func(n *Node) { n.Kind = "shortcut" },
		"invalid identifier":     func(n *Node) { n.ID = "../outside" },
		"invalid parent":         func(n *Node) { n.ParentID = "" },
		"unsafe path":            func(n *Node) { n.Path = "../outside" },
		"unsafe name":            func(n *Node) { n.Name = "nested/file" },
		"invalid digest":         func(n *Node) { n.SHA256 = strings.Repeat("z", 64) },
		"missing checksum":       func(n *Node) { n.MD5 = "" },
		"negative size":          func(n *Node) { n.Size = -1 },
		"directory with content": func(n *Node) { n.Kind = "directory" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "journal"), "")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.SaveProject(Project{Key: "project", SourceIdentity: "source", AccountReference: "account", DestinationID: "destination"}); err != nil {
				t.Fatal(err)
			}
			node := valid
			mutate(&node)
			if err := store.SaveNode("project", node); domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
				t.Fatalf("invalid operation accepted: %v", err)
			}
			raw, err := json.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec("INSERT INTO operations(id,project_id,payload) VALUES(?,?,?)", node.OperationID, "project", raw); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load("project"); domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
				t.Fatalf("corrupt operation accepted on recovery: %v", err)
			}
		})
	}
}

func TestStoreAllowsTrustedMacSystemAliasWithoutWeakeningSourceBoundary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system alias regression")
	}
	base, err := os.MkdirTemp("/var/tmp", "ledgesync-state-alias-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(base, "journal"), "")
	if err != nil {
		t.Fatalf("root-owned /var alias rejected: %v", err)
	}
	store.Close()
	if store, err := Open(filepath.Join(base, "forbidden"), resolved); domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
		if store != nil {
			store.Close()
		}
		t.Fatalf("alias bypassed source boundary: %v", err)
	}
}

func TestStoreRefusesPreexistingPublicStatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not model Windows ACLs")
	}
	for _, target := range []string{"directory", "writer.lock", "transfers.sqlite"} {
		t.Run(target, func(t *testing.T) {
			base := t.TempDir()
			directory := filepath.Join(base, "state")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if target == "directory" {
				if err := os.Chmod(directory, 0755); err != nil {
					t.Fatal(err)
				}
			} else {
				filename := filepath.Join(directory, target)
				if err := os.WriteFile(filename, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filename, 0644); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Open(directory, "")
			if store != nil {
				store.Close()
				t.Fatal("publicly readable journal was accepted")
			}
			if domain.ErrorCode(err) != "STATE_UNAVAILABLE" {
				t.Fatalf("unexpected permission error: %v", err)
			}
		})
	}
}
