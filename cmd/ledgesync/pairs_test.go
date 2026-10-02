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

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

const cliAccount = "google-drive:cli-pairs"

type cliAccounts struct{}

func (cliAccounts) Status(context.Context) (driveauth.Status, error) {
	return driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: cliAccount}}, nil
}

func cliPairFixture(t *testing.T) (pairFactory, *drivetest.Server, string) {
	t.Helper()
	server := drivetest.New()
	t.Cleanup(server.Close)
	client := drive.NewWithOptions(server.Authorizer(cliAccount), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	state := filepath.Join(t.TempDir(), "state")
	local := app.NewService()
	store := projects.NewStore(state)
	t.Cleanup(func() { _ = store.Close() })
	services := pairServices{store: store, accounts: cliAccounts{}, transfer: transfer.New(local, client, cliAccounts{}, state), local: local, restorer: client, stateDir: state}
	return func() (pairServices, error) { return services, nil }, server, state
}

// digestAnswer feeds the plan digest printed by a preview back as approval.
type digestAnswer struct{ out *bytes.Buffer }

func (d digestAnswer) Read(p []byte) (int, error) {
	text := d.out.String()
	i := strings.Index(text, "\"planDigest\": \"")
	if i < 0 {
		return 0, nil
	}
	digest := text[i+15 : i+15+64]
	return copy(p, digest+"\n"), nil
}

func TestCLIPairsCopyAutomaticAndRestoreShareServices(t *testing.T) {
	create, server, _ := cliPairFixture(t)
	source := filepath.Join(t.TempDir(), "server-data")
	writeTreeFiles(t, source, map[string]string{".gitignore": "*.cache\n", "db/dump.sql": "insert", "x.cache": "skip"})
	destination := server.AddUserFolder("Server backups", "root", true)
	ctx := context.Background()
	var out, errOut bytes.Buffer
	if code := runPairs(ctx, []string{"pairs", "add", "--root", source, "--destination", destination}, nil, &out, &errOut, false, create); code != 0 {
		t.Fatalf("pairs add = %d %s", code, errOut.String())
	}
	var pair projects.Project
	if err := json.Unmarshal(out.Bytes(), &pair); err != nil || pair.ID == "" {
		t.Fatalf("pair output %q: %v", out.String(), err)
	}
	out.Reset()
	if code := runPairCopy(ctx, pair.ID, nil, &out, &errOut, false, create); code != 6 || !strings.Contains(errOut.String(), "TERMINAL_REQUIRED") {
		t.Fatalf("non-interactive copy = %d %s", code, errOut.String())
	}
	errOut.Reset()
	if code := runPairCopy(ctx, pair.ID, digestAnswer{&out}, &out, &errOut, true, create); code != 0 {
		t.Fatalf("copy --pair = %d %s", code, errOut.String())
	}
	out.Reset()
	if code := runPairs(ctx, []string{"automatic", "enable", "--pair", pair.ID, "--every", "5"}, digestAnswer{&out}, &out, &errOut, true, create); code != 0 {
		t.Fatalf("automatic enable = %d %s", code, errOut.String())
	}
	writeTreeFiles(t, source, map[string]string{"db/new.sql": "added later"})
	out.Reset()
	if code := runPairs(ctx, []string{"automatic", "run", "--pair", pair.ID}, nil, &out, &errOut, false, create); code != 0 {
		t.Fatalf("automatic run = %d %s %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `"state": "succeeded"`) || !strings.Contains(out.String(), `"trigger": "automatic"`) {
		t.Fatalf("automatic run output: %s", out.String())
	}
	target := filepath.Join(t.TempDir(), "restored")
	out.Reset()
	if code := runPairs(ctx, []string{"restore", "--pair", pair.ID, "--to", target}, nil, &out, &errOut, false, create); code != 0 {
		t.Fatalf("restore = %d %s", code, errOut.String())
	}
	for name, want := range map[string]string{"db/dump.sql": "insert", "db/new.sql": "added later", ".gitignore": "*.cache\n"} {
		got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("restored %s = %q %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "x.cache")); !os.IsNotExist(err) {
		t.Fatal("an excluded file was restored")
	}
	errOut.Reset()
	if code := runPairs(ctx, []string{"restore", "--pair", pair.ID, "--to", target}, nil, &out, &errOut, false, create); code == 0 || !strings.Contains(errOut.String(), "RESTORE_TARGET_NOT_EMPTY") {
		t.Fatalf("restore into a non-empty folder = %d %s", code, errOut.String())
	}
}

func writeTreeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
