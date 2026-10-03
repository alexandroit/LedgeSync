package syncer

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

const syncAccount = "drive_sync_test_account"

type fakeAccounts struct {
	mu        sync.Mutex
	reference string
}

func (f *fakeAccounts) Status(context.Context) (driveauth.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reference == "" {
		return driveauth.Status{State: "disconnected"}, nil
	}
	return driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: f.reference}}, nil
}

type harness struct {
	t      *testing.T
	server *drivetest.Server
	client *drive.Client
	state  *transferstate.SyncState
	mgr    *Manager
	dir    string
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	server := drivetest.New()
	server.FullScope, server.RootReadable = true, true
	t.Cleanup(server.Close)
	client := drive.NewWithOptions(server.Authorizer(syncAccount), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	dir := filepath.Join(t.TempDir(), "state")
	state, err := transferstate.OpenSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	opts.Protected = append(opts.Protected, dir)
	mgr, err := NewManager(state, client, &fakeAccounts{reference: syncAccount}, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	return &harness{t: t, server: server, client: client, state: state, mgr: mgr, dir: dir}
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// edit changes content and guarantees a different modification time.
func edit(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)
}

func localTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() && d.Name() == TrashDir {
			return filepath.SkipDir
		}
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		data, _ := os.ReadFile(p)
		out[rel] = string(data)
		return nil
	})
	return out
}

func (h *harness) remoteTree(rootID string) map[string]string {
	out := map[string]string{}
	var walk func(id, prefix string)
	walk = func(id, prefix string) {
		for _, c := range h.server.Children(id) {
			p := prefix + c.Name
			if c.MimeType == drivetest.FolderMIME {
				out[p+"/"] = ""
				walk(c.ID, p+"/")
				continue
			}
			out[p] = string(c.Content)
		}
	}
	walk(rootID, "")
	return out
}

func (h *harness) find(rootID, p string) drivetest.Object {
	h.t.Helper()
	id := rootID
	parts := strings.Split(p, "/")
	var found drivetest.Object
	for _, part := range parts {
		ok := false
		for _, c := range h.server.Children(id) {
			if c.Name == part {
				found, id, ok = c, c.ID, true
				break
			}
		}
		if !ok {
			h.t.Fatalf("%s not found in Drive", p)
		}
	}
	return found
}

func (h *harness) add(root string) Status {
	h.t.Helper()
	status, err := h.mgr.Add(context.Background(), root, Destination{FolderID: "root"})
	if err != nil {
		h.t.Fatal(err)
	}
	return status
}

func (h *harness) run(id string) Result {
	h.t.Helper()
	result, err := h.mgr.RunOnce(context.Background(), id, nil)
	if err != nil {
		h.t.Fatalf("sync pass failed: %v", err)
	}
	return result
}

func equalTrees(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	var problems []string
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			problems = append(problems, "missing or different: "+k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			problems = append(problems, "unexpected: "+k)
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("%s:\n%s", what, strings.Join(problems, "\n"))
	}
}

func TestSyncFirstPassMergesBothSides(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Project")
	write(t, root, map[string]string{"a.txt": "local a", "dir/b.txt": "local b", "empty/": "", "nome com espaços/ação.txt": "unicode"})
	// The Drive folder already exists with content added through the website.
	driveFolder := h.server.AddUserFolder("Project", "root", false)
	h.server.AddUserFile("from-web.pdf", driveFolder, []byte("web pdf"))
	sub := h.server.AddUserFolder("photos", driveFolder, false)
	h.server.AddUserFile("p1.jpg", sub, []byte("jpg bytes"))
	status := h.add(root)
	if status.Pair.RemoteRootID != driveFolder {
		t.Fatalf("the existing Drive folder must be reused, got %s", status.Pair.RemoteRootID)
	}
	result := h.run(status.Pair.ID)
	if result.Uploaded != 3 || result.Downloaded != 2 {
		t.Fatalf("first pass: %+v", result)
	}
	want := map[string]string{"a.txt": "local a", "dir/": "", "dir/b.txt": "local b", "empty/": "", "nome com espaços/": "", "nome com espaços/ação.txt": "unicode", "from-web.pdf": "web pdf", "photos/": "", "photos/p1.jpg": "jpg bytes"}
	equalTrees(t, "local", localTree(t, root), want)
	equalTrees(t, "Drive", h.remoteTree(driveFolder), want)
	again := h.run(status.Pair.ID)
	if again.Changed || again.Uploaded+again.Downloaded+again.DeletedLocal+again.DeletedRemote != 0 {
		t.Fatalf("a converged pair must make no changes: %+v", again)
	}
}

func TestSyncSendsLocalChangesAndDeletionsToDrive(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Work")
	write(t, root, map[string]string{"a.txt": "v1", "dir/b.txt": "b", "keep.txt": "k"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	before := h.find(rootID, "a.txt")
	edit(t, root, "a.txt", "version two")
	if err := os.Remove(filepath.Join(root, "dir", "b.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, map[string]string{"new/c.txt": "c"})
	result := h.run(status.Pair.ID)
	if result.Uploaded != 2 || result.DeletedRemote != 1 {
		t.Fatalf("local changes: %+v", result)
	}
	after := h.find(rootID, "a.txt")
	if after.ID != before.ID || string(after.Content) != "version two" {
		t.Fatalf("an edit must update the same Drive file as a new revision: %s %q", after.ID, after.Content)
	}
	trashed, _ := h.server.Get(h.findTrashed(rootID, "dir", "b.txt"))
	if !trashed.Trashed {
		t.Fatal("a local deletion must move the Drive file to the trash")
	}
	equalTrees(t, "Drive", h.remoteTree(rootID), map[string]string{"a.txt": "version two", "dir/": "", "keep.txt": "k", "new/": "", "new/c.txt": "c"})
}

// findTrashed returns the ID of a (possibly trashed) child by name.
func (h *harness) findTrashed(rootID, dir, name string) string {
	parent := h.find(rootID, dir).ID
	for _, o := range h.server.All() {
		if o.Name == name && len(o.Parents) == 1 && o.Parents[0] == parent {
			return o.ID
		}
	}
	h.t.Fatalf("%s/%s does not exist", dir, name)
	return ""
}

func TestSyncReceivesDriveChangesIncludingWebUploads(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Shared")
	write(t, root, map[string]string{"a.txt": "a", "old.txt": "old", "dir/x.txt": "x"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	h.server.SetContent(h.find(rootID, "a.txt").ID, []byte("edited in Drive"))
	h.server.AddUserFile("web.txt", rootID, []byte("uploaded on the website"))
	folder := h.server.AddUserFolder("from phone", rootID, false)
	h.server.AddUserFile("scan.pdf", folder, []byte("pdf"))
	h.server.Update(h.find(rootID, "old.txt").ID, func(o *drivetest.Object) { o.Trashed = true })
	result := h.run(status.Pair.ID)
	if result.Downloaded != 3 || result.DeletedLocal != 1 {
		t.Fatalf("Drive changes: %+v", result)
	}
	equalTrees(t, "local", localTree(t, root), map[string]string{"a.txt": "edited in Drive", "web.txt": "uploaded on the website", "from phone/": "", "from phone/scan.pdf": "pdf", "dir/": "", "dir/x.txt": "x"})
	trashed, err := os.ReadFile(filepath.Join(root, TrashDir))
	_ = trashed
	if err == nil {
		t.Fatal("the local trash must be a folder")
	}
	found := false
	_ = filepath.WalkDir(filepath.Join(root, TrashDir), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == "old.txt" {
			data, _ := os.ReadFile(p)
			found = string(data) == "old"
		}
		return nil
	})
	if !found {
		t.Fatal("a file deleted in Drive must be recoverable from the local trash")
	}
}

func TestSyncKeepsBothVersionsWhenBothSidesChanged(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Docs")
	write(t, root, map[string]string{"report.txt": "base"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	edit(t, root, "report.txt", "local edit")
	h.server.SetContent(h.find(rootID, "report.txt").ID, []byte("drive edit"))
	result := h.run(status.Pair.ID)
	if result.Conflicts != 1 {
		t.Fatalf("conflict: %+v", result)
	}
	h.run(status.Pair.ID) // uploads the kept local version
	local := localTree(t, root)
	if local["report.txt"] != "drive edit" {
		t.Fatalf("the Drive version keeps the name: %v", local)
	}
	var kept string
	for name, content := range local {
		if strings.HasPrefix(name, "report (conflict ") && strings.HasSuffix(name, ").txt") && content == "local edit" {
			kept = name
		}
	}
	if kept == "" {
		t.Fatalf("the local version must be kept as a conflict copy: %v", local)
	}
	if remote := h.remoteTree(rootID); remote[kept] != "local edit" || remote["report.txt"] != "drive edit" {
		t.Fatalf("both versions must reach Drive: %v", remote)
	}
}

func TestSyncPrefersEditsOverDeletions(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Edits")
	write(t, root, map[string]string{"del-here.txt": "1", "del-there.txt": "2"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	_ = os.Remove(filepath.Join(root, "del-here.txt"))
	h.server.SetContent(h.find(rootID, "del-here.txt").ID, []byte("edited in Drive"))
	edit(t, root, "del-there.txt", "edited here")
	h.server.Update(h.find(rootID, "del-there.txt").ID, func(o *drivetest.Object) { o.Trashed = true })
	h.run(status.Pair.ID)
	want := map[string]string{"del-here.txt": "edited in Drive", "del-there.txt": "edited here"}
	equalTrees(t, "local", localTree(t, root), want)
	equalTrees(t, "Drive", h.remoteTree(rootID), want)
}

func TestSyncMassDeletionWaitsForConfirmation(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Many")
	files := map[string]string{}
	for i := 0; i < 30; i++ {
		files["f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt"] = "x"
	}
	write(t, root, files)
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(root, e.Name()))
		}
	}
	_, err := h.mgr.RunOnce(context.Background(), status.Pair.ID, nil)
	local, remote, _, ok := ConfirmationNeeded(err)
	if !ok || local != 0 || remote != 30 {
		t.Fatalf("emptying the folder must wait for confirmation: %v %d %d", err, local, remote)
	}
	if len(h.server.Children(rootID)) != 30 {
		t.Fatal("nothing may be deleted in Drive before confirmation")
	}
	current, _ := h.mgr.Get(status.Pair.ID)
	if current.State != "confirm_deletes" || current.RemoteDeletes != 30 {
		t.Fatalf("status: %+v", current)
	}
	if err = h.mgr.ConfirmDeletes(status.Pair.ID); err != nil {
		t.Fatal(err)
	}
	result := h.run(status.Pair.ID)
	if result.DeletedRemote != 30 || len(h.server.Children(rootID)) != 0 {
		t.Fatalf("confirmed deletions: %+v", result)
	}
}

func TestSyncHonoursIgnoreRulesAndNeverSyncsItsOwnFiles(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Code")
	write(t, root, map[string]string{".gitignore": "*.log\nbuild/\n", "main.go": "package main", "app.log": "log", "build/out.bin": "bin", ".DS_Store": "mac", TrashDir + "/x/y.txt": "trash"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	h.server.AddUserFile("debug.log", rootID, []byte("remote log"))
	h.run(status.Pair.ID)
	equalTrees(t, "Drive", h.remoteTree(rootID), map[string]string{".gitignore": "*.log\nbuild/\n", "main.go": "package main", "debug.log": "remote log"})
	if _, err := os.Stat(filepath.Join(root, "debug.log")); err == nil {
		t.Fatal("a Drive file matching the ignore rules must not be downloaded")
	}
}

func TestSyncReportsItemsItCannotSync(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Mixed")
	write(t, root, map[string]string{"a.txt": "a"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	doc := h.server.AddUserFile("Budget", rootID, nil)
	h.server.Update(doc, func(o *drivetest.Object) { o.MimeType = "application/vnd.google-apps.spreadsheet" })
	h.server.AddUserFile("same.txt", rootID, []byte("1"))
	h.server.AddUserFile("same.txt", rootID, []byte("2"))
	result := h.run(status.Pair.ID)
	codes := map[string]string{}
	for _, issue := range result.Issues {
		codes[issue.Path] = issue.Code
	}
	if codes["Budget"] != "GOOGLE_FILE_SKIPPED" || codes["same.txt"] != "DUPLICATE_NAME" {
		t.Fatalf("issues: %+v", result.Issues)
	}
	if _, err := os.Stat(filepath.Join(root, "same.txt")); err == nil {
		t.Fatal("ambiguous Drive names must not be downloaded")
	}
}

func TestSyncWaitsWhenTheLocalFolderIsMissingAndPausesWhenDriveFolderIsTrashed(t *testing.T) {
	h := newHarness(t, Options{})
	parent := t.TempDir()
	root := filepath.Join(parent, "Disk")
	write(t, root, map[string]string{"a.txt": "a"})
	status := h.add(root)
	h.run(status.Pair.ID)
	rootID := status.Pair.RemoteRootID
	if err := os.Rename(root, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	_, err := h.mgr.RunOnce(context.Background(), status.Pair.ID, nil)
	if domain.ErrorCode(err) != "SYNC_FOLDER_UNAVAILABLE" || len(h.server.Children(rootID)) != 1 {
		t.Fatalf("an unavailable folder must never delete Drive files: %v", err)
	}
	if err = os.Rename(filepath.Join(parent, "moved"), root); err != nil {
		t.Fatal(err)
	}
	h.server.Update(rootID, func(o *drivetest.Object) { o.Trashed = true })
	_, err = h.mgr.RunOnce(context.Background(), status.Pair.ID, nil)
	current, _ := h.mgr.Get(status.Pair.ID)
	if domain.ErrorCode(err) != "SYNC_REMOTE_MISSING" || !current.Pair.Paused {
		t.Fatalf("a trashed Drive folder must pause the sync: %v %+v", err, current)
	}
	if _, err = os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal("local files must stay when the Drive folder is trashed")
	}
}

func TestSyncLostUploadAcknowledgementDoesNotDuplicate(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Lost")
	write(t, root, map[string]string{"big.bin": strings.Repeat("z", 300<<10)})
	status := h.add(root)
	h.server.Inject(func(r *http.Request) bool {
		return r.Method == http.MethodPut && strings.HasPrefix(r.Header.Get("Content-Range"), "bytes 262144-")
	}, 1, drivetest.Fault{DropAfterCommit: true})
	_, _ = h.mgr.RunOnce(context.Background(), status.Pair.ID, nil)
	h.run(status.Pair.ID)
	h.run(status.Pair.ID)
	if n := len(h.server.Children(status.Pair.RemoteRootID)); n != 1 {
		t.Fatalf("a lost acknowledgement must not create duplicates: %d items", n)
	}
}

func TestManagerSyncsInTheBackgroundWithoutCommands(t *testing.T) {
	h := newHarness(t, Options{Tick: 20 * time.Millisecond, PollLocal: 20 * time.Millisecond, PollRemote: 20 * time.Millisecond})
	root := filepath.Join(t.TempDir(), "Live")
	write(t, root, map[string]string{"start.txt": "s"})
	status := h.add(root)
	h.mgr.Start()
	rootID := status.Pair.RemoteRootID
	waitFor(t, "initial upload", func() bool { return h.remoteTree(rootID)["start.txt"] == "s" })
	write(t, root, map[string]string{"added later.txt": "local"})
	waitFor(t, "a new local file reaches Drive", func() bool { return h.remoteTree(rootID)["added later.txt"] == "local" })
	h.server.AddUserFile("from web.txt", rootID, []byte("web"))
	waitFor(t, "a file added on the website reaches this computer", func() bool { return localTree(t, root)["from web.txt"] == "web" })
	waitFor(t, "synced status", func() bool { s, _ := h.mgr.Get(status.Pair.ID); return s.State == "synced" })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestSyncReconcilesAnUploadWhoseResultWasLostAcrossPasses(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Intent")
	write(t, root, map[string]string{"report.pdf": "pdf bytes"})
	status := h.add(root)
	// The upload completes in Drive, but every read of the new file fails, so
	// the pass cannot confirm it and keeps the reserved-ID intent.
	var mu sync.Mutex
	uploaded := false
	h.server.Inject(func(r *http.Request) bool {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			uploaded = true
			return false
		}
		return uploaded && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/drive/v3/files/gen") &&
			!strings.HasSuffix(r.URL.Path, status.Pair.RemoteRootID) && r.URL.Query().Get("alt") == ""
	}, 4, drivetest.Fault{Status: http.StatusServiceUnavailable})
	first, err := h.mgr.RunOnce(context.Background(), status.Pair.ID, nil)
	if err != nil || first.Uploaded != 0 || len(first.Issues) == 0 {
		t.Fatalf("the unconfirmed upload must be reported, not recorded: %+v %v", first, err)
	}
	intents, _ := h.state.Intents(status.Pair.ID)
	if len(intents) != 1 {
		t.Fatalf("the reserved ID must stay journaled: %v", intents)
	}
	second := h.run(status.Pair.ID)
	if second.Uploaded != 0 || len(h.server.Children(status.Pair.RemoteRootID)) != 1 {
		t.Fatalf("the next pass must adopt the existing file, not upload again: %+v", second)
	}
	if intents, _ = h.state.Intents(status.Pair.ID); len(intents) != 0 {
		t.Fatalf("the intent must be cleared: %v", intents)
	}
	if again := h.run(status.Pair.ID); again.Changed {
		t.Fatalf("converged: %+v", again)
	}
}

func TestSyncRestoreInsteadOfMassDeletionCopiesFilesBack(t *testing.T) {
	h := newHarness(t, Options{})
	root := filepath.Join(t.TempDir(), "Restore")
	files := map[string]string{}
	for i := 0; i < 25; i++ {
		files["r"+string(rune('a'+i))+".txt"] = "keep me"
	}
	write(t, root, files)
	status := h.add(root)
	h.run(status.Pair.ID)
	for name := range files {
		_ = os.Remove(filepath.Join(root, name))
	}
	if _, err := h.mgr.RunOnce(context.Background(), status.Pair.ID, nil); err == nil {
		t.Fatal("the guard must hold the deletions")
	}
	if err := h.mgr.RestoreDeletes(status.Pair.ID); err != nil {
		t.Fatal(err)
	}
	result := h.run(status.Pair.ID)
	if result.Downloaded != 25 || result.DeletedRemote != 0 || len(h.server.Children(status.Pair.RemoteRootID)) != 25 {
		t.Fatalf("restore must copy the files back from Drive: %+v", result)
	}
	equalTrees(t, "local", localTree(t, root), files)
}
