package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/syncer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

type syncAccounts struct{}

func (syncAccounts) Status(context.Context) (driveauth.Status, error) {
	return driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: "drive_cli_sync"}}, nil
}

func testSyncFactory(t *testing.T) (syncFactory, *drivetest.Server) {
	t.Helper()
	server := drivetest.New()
	server.FullScope, server.RootReadable = true, true
	t.Cleanup(server.Close)
	client := drive.NewWithOptions(server.Authorizer("drive_cli_sync"), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	dir := filepath.Join(t.TempDir(), "state")
	state, err := transferstate.OpenSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	// Every command opens a fresh manager over the same state, like separate processes.
	return func() (*syncer.Manager, func(), error) {
		m, err := syncer.NewManager(state, client, syncAccounts{}, syncer.Options{Protected: []string{dir}})
		if err != nil {
			return nil, nil, err
		}
		return m, m.Stop, nil
	}, server
}

func syncCLI(t *testing.T, factory syncFactory, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runSync(context.Background(), append([]string{"sync"}, args...), &out, &errOut, factory)
	return code, out.String(), errOut.String()
}

func TestCLISyncAddRunGuardAndControls(t *testing.T) {
	factory, server := testSyncFactory(t)
	root := filepath.Join(t.TempDir(), "Server Data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		if err := os.WriteFile(filepath.Join(root, "f"+string(rune('a'+i))+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := syncCLI(t, factory, "add", "--root", root)
	var first struct {
		Pair   string        `json:"pair"`
		Result syncer.Result `json:"result"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &first) != nil || first.Result.Uploaded != 25 {
		t.Fatalf("add: %d %s %s", code, out, errOut)
	}
	code, out, _ = syncCLI(t, factory, "list")
	var list []syncer.Status
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 1 || list[0].Pair.ID != first.Pair {
		t.Fatalf("list: %d %s", code, out)
	}
	rootID := list[0].Pair.RemoteRootID
	server.AddUserFile("from web.txt", rootID, []byte("web"))
	if code, out, errOut = syncCLI(t, factory, "run"); code != 0 {
		t.Fatalf("run: %d %s %s", code, out, errOut)
	}
	if data, err := os.ReadFile(filepath.Join(root, "from web.txt")); err != nil || string(data) != "web" {
		t.Fatalf("a file added on the website must be downloaded: %q %v", data, err)
	}
	for i := 0; i < 25; i++ {
		_ = os.Remove(filepath.Join(root, "f"+string(rune('a'+i))+".txt"))
	}
	code, out, _ = syncCLI(t, factory, "run", "--pair", first.Pair)
	if code != 5 || !strings.Contains(out, `"remoteDeletes": 25`) {
		t.Fatalf("many deletions must wait (exit 5): %d %s", code, out)
	}
	code, _, errOut = syncCLI(t, factory, "confirm-deletes", "--pair", first.Pair, "--count", "3")
	if code == 0 || !strings.Contains(errOut, "SYNC_CONFIRMATION_MISMATCH") || len(server.Children(rootID)) != 26 {
		t.Fatalf("a wrong count must delete nothing: %d %s", code, errOut)
	}
	code, out, errOut = syncCLI(t, factory, "confirm-deletes", "--pair", first.Pair, "--count", "25")
	if code != 0 || len(server.Children(rootID)) != 1 {
		t.Fatalf("confirmed deletions: %d %s %s; %d left", code, out, errOut, len(server.Children(rootID)))
	}
	for _, action := range []string{"pause", "resume"} {
		if code, out, _ = syncCLI(t, factory, action, "--pair", first.Pair); code != 0 || !strings.Contains(out, first.Pair) {
			t.Fatalf("%s: %d %s", action, code, out)
		}
	}
	if code, out, _ = syncCLI(t, factory, "activity", "--pair", first.Pair); code != 0 || !strings.Contains(out, `"kind": "upload"`) || !strings.Contains(out, `"kind": "delete_up"`) {
		t.Fatalf("activity: %d %s", code, out)
	}
	if code, out, _ = syncCLI(t, factory, "remove", "--pair", first.Pair); code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, "from web.txt")); err != nil {
		t.Fatal("removing a sync must keep local files")
	}
	if code, _, _ = syncCLI(t, factory, "bogus"); code == 0 {
		t.Fatal("unknown sync commands must fail")
	}
}
