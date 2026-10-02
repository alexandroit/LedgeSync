package transfer_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

// e2e drives the real HTTP Drive provider against the drive.file emulator.
type e2e struct {
	t        *testing.T
	server   *drivetest.Server
	accounts *testAccounts
	service  *transfer.Service
	state    string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	server := drivetest.New()
	t.Cleanup(server.Close)
	accounts := &testAccounts{reference: testAccount, state: "connected"}
	client := drive.NewWithOptions(server.Authorizer(testAccount), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	state := filepath.Join(t.TempDir(), "state")
	return &e2e{t: t, server: server, accounts: accounts, service: transfer.New(app.NewService(), client, accounts, state), state: state}
}

func writeTree(t *testing.T, root string, files map[string]string) {
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

func (h *e2e) run(source, destination string) (transfer.Plan, transfer.Status) {
	h.t.Helper()
	ctx := context.Background()
	if _, err := h.service.SetDestination(ctx, destination, testAccount); err != nil {
		h.t.Fatalf("destination %s: %v", destination, err)
	}
	plan, err := h.service.Preview(ctx, source, false)
	if err != nil {
		h.t.Fatalf("preview: %v", err)
	}
	if _, err = h.service.Start(ctx, plan.PlanDigest); err != nil {
		h.t.Fatalf("start: %v", err)
	}
	return plan, h.wait()
}

func (h *e2e) wait() transfer.Status {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for h.service.Busy() {
		if time.Now().After(deadline) {
			h.t.Fatal("transfer did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h.service.Status()
}

// remoteTree returns relative path -> SHA-256 (files) or "dir" for a Drive folder.
func (h *e2e) remoteTree(folder string) map[string]string {
	out := map[string]string{}
	var walk func(string, string)
	walk = func(id, prefix string) {
		for _, child := range h.server.Children(id) {
			p := path.Join(prefix, child.Name)
			if child.MimeType == drivetest.FolderMIME {
				out[p] = "dir"
				walk(child.ID, p)
				continue
			}
			sum := sha256.Sum256(child.Content)
			out[p] = hex.EncodeToString(sum[:])
		}
	}
	walk(folder, "")
	return out
}

func localDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func managedRoot(t *testing.T, server *drivetest.Server, parent, name string) drivetest.Object {
	t.Helper()
	var found []drivetest.Object
	for _, child := range server.Children(parent) {
		if child.Name == name && child.MimeType == drivetest.FolderMIME {
			found = append(found, child)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one managed folder %q in %s, found %d", name, parent, len(found))
	}
	return found[0]
}

func assertTree(t *testing.T, got, want map[string]string) {
	t.Helper()
	keys := func(m map[string]string) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(keys(got), "\n") != strings.Join(keys(want), "\n") {
		t.Fatalf("remote paths differ:\n got: %q\nwant: %q", keys(got), keys(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("remote %s = %s, want %s", k, got[k], v)
		}
	}
}

func fixtureFiles() map[string]string {
	big := strings.Repeat("0123456789abcdef", 45000) // 720,000 bytes: three 256 KiB chunks
	return map[string]string{
		".gitignore":                "*.log\nbuild/\nnode_modules/\n",
		"README.md":                 "# fixture\n",
		"empty.bin":                 "",
		"docs/guide.txt":            "nested\n",
		"docs/deeper/empty-dir/":    "",
		"docs/deeper/notes.txt":     "deeper\n",
		"ação e espaço/ü file.txt":  "unicode name\n",
		"assets/big.bin":            big,
		"app.log":                   "excluded\n",
		"build/output.o":            "excluded build\n",
		"node_modules/pkg/index.js": "module.exports = 1\n",
	}
}

func expectedRemote(name string, files map[string]string) map[string]string {
	want := map[string]string{}
	for p, content := range files {
		if strings.HasSuffix(p, ".log") || strings.HasPrefix(p, "build/") || strings.HasPrefix(p, "node_modules/") {
			continue
		}
		clean := strings.TrimSuffix(p, "/")
		if strings.HasSuffix(p, "/") {
			want[clean] = "dir"
		} else {
			want[clean] = localDigest(content)
		}
		for dir := path.Dir(clean); dir != "."; dir = path.Dir(dir) {
			want[dir] = "dir"
		}
	}
	return want
}

func TestE2EPickerFolderCopiesCompleteHierarchy(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "Project Alpha")
	files := fixtureFiles()
	writeTree(t, source, files)
	destination := h.server.AddUserFolder("Backups", "root", true)
	plan, status := h.run(source, destination)
	if status.State != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
	if plan.FileCount != 7 || plan.ExcludedCount == 0 {
		t.Fatalf("plan counts = files %d excluded %d", plan.FileCount, plan.ExcludedCount)
	}
	root := managedRoot(t, h.server, destination, "Project Alpha")
	assertTree(t, h.remoteTree(root.ID), expectedRemote("Project Alpha", files))
}

func TestE2EMyDriveWorksWithoutReadableRoot(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "Quarterly")
	files := map[string]string{"report.txt": "q3\n", "data/rows.csv": "a,b\n"}
	writeTree(t, source, files)
	_, status := h.run(source, "root")
	if status.State != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
	root := managedRoot(t, h.server, "root", "Quarterly")
	if root.Parents[0] != h.server.RootID {
		t.Fatalf("managed folder parent = %v", root.Parents)
	}
	assertTree(t, h.remoteTree(root.ID), expectedRemote("Quarterly", files))
}

func TestE2ESymlinksInsideIgnoredDirectoriesDoNotBlockCopy(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "web-app")
	files := map[string]string{".gitignore": "node_modules/\n.venv/\n", "src/main.js": "main\n", "node_modules/tool/cli.js": "cli\n"}
	writeTree(t, source, files)
	if err := os.MkdirAll(filepath.Join(source, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../tool/cli.js", filepath.Join(source, "node_modules", ".bin", "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(source, ".venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/usr/bin/python3", filepath.Join(source, ".venv", "bin", "python")); err != nil {
		t.Fatal(err)
	}
	destination := h.server.AddUserFolder("Code", "root", true)
	_, status := h.run(source, destination)
	if status.State != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
	root := managedRoot(t, h.server, destination, "web-app")
	assertTree(t, h.remoteTree(root.ID), map[string]string{".gitignore": localDigest(files[".gitignore"]), "src": "dir", "src/main.js": localDigest("main\n")})
}

func TestE2EExcludedFileChurnDuringUploadStillSucceeds(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "service")
	writeTree(t, source, map[string]string{".gitignore": "*.log\n", "a.txt": "a\n", "b.txt": "b\n", "server.log": "start\n"})
	destination := h.server.AddUserFolder("Ops", "root", true)
	// A running program appends to its ignored log while the copy is in progress.
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 1, drivetest.Fault{Hook: func(*http.Request) {
		f, err := os.OpenFile(filepath.Join(source, "server.log"), os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			_, _ = f.WriteString("request\n")
			_ = f.Close()
		}
		_ = os.WriteFile(filepath.Join(source, "server-2.log"), []byte("rotated\n"), 0o644)
	}})
	_, status := h.run(source, destination)
	if status.State != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
}

func TestE2ERepeatCopiesOnlyNewAndChangedFiles(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "notes")
	writeTree(t, source, map[string]string{"one.txt": "1\n", "two.txt": "2\n"})
	destination := h.server.AddUserFolder("Notes backup", "root", true)
	if _, status := h.run(source, destination); status.State != "succeeded" {
		t.Fatalf("first run = %+v", status)
	}
	before := len(h.server.Requests())
	writeTree(t, source, map[string]string{"three.txt": "3\n", "sub/four.txt": "4\n"})
	plan, status := h.run(source, destination)
	if status.State != "succeeded" {
		t.Fatalf("second run = %+v", status)
	}
	actions := map[string]string{}
	for _, e := range plan.Entries {
		actions[e.RelativePath] = e.Action
	}
	if actions["one.txt"] != "skip" || actions["three.txt"] != "upload" || actions["sub"] != "create" || actions["sub/four.txt"] != "upload" {
		t.Fatalf("second plan actions = %v", actions)
	}
	uploads := 0
	for _, r := range h.server.Requests()[before:] {
		if r == "POST /upload/drive/v3/files" {
			uploads++
		}
	}
	if uploads != 2 {
		t.Fatalf("second run started %d uploads, want 2", uploads)
	}
	root := managedRoot(t, h.server, destination, "notes")
	assertTree(t, h.remoteTree(root.ID), map[string]string{"one.txt": localDigest("1\n"), "two.txt": localDigest("2\n"), "three.txt": localDigest("3\n"), "sub": "dir", "sub/four.txt": localDigest("4\n")})
}

func planActions(plan transfer.Plan) map[string]string {
	actions := map[string]string{}
	for _, e := range plan.Entries {
		actions[e.RelativePath] = e.Action
	}
	return actions
}

func (h *e2e) preview(source, destination string) transfer.Plan {
	h.t.Helper()
	if _, err := h.service.SetDestination(context.Background(), destination, testAccount); err != nil {
		h.t.Fatalf("destination: %v", err)
	}
	plan, err := h.service.Preview(context.Background(), source, false)
	if err != nil {
		h.t.Fatalf("preview: %v", err)
	}
	return plan
}

func (h *e2e) start(plan transfer.Plan) transfer.Status {
	h.t.Helper()
	if _, err := h.service.Start(context.Background(), plan.PlanDigest); err != nil {
		h.t.Fatalf("start: %v", err)
	}
	return h.wait()
}

func TestE2EManagedFolderTrashedInDriveIsCopiedAgainWithoutTouchingIt(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "photos")
	files := map[string]string{"a.jpg": "a", "trip/b.jpg": "b"}
	writeTree(t, source, files)
	destination := h.server.AddUserFolder("Backups", "root", true)
	if _, status := h.run(source, destination); status.State != "succeeded" {
		t.Fatalf("first run = %+v", status)
	}
	old := managedRoot(t, h.server, destination, "photos")
	h.server.Update(old.ID, func(o *drivetest.Object) { o.Trashed = true })
	plan := h.preview(source, destination)
	actions := planActions(plan)
	if actions[""] != "recreate" || actions["a.jpg"] != "recreate" || actions["trip"] != "recreate" || actions["trip/b.jpg"] != "recreate" || plan.RecreatedItems != 4 {
		t.Fatalf("trashed managed folder plan = %v (%d recreated)", actions, plan.RecreatedItems)
	}
	if status := h.start(plan); status.State != "succeeded" {
		t.Fatalf("recreate run = %+v", status)
	}
	if o, _ := h.server.Get(old.ID); !o.Trashed || len(h.server.Children(old.ID)) != 2 {
		t.Fatal("the trashed folder or its contents were modified")
	}
	fresh := managedRoot(t, h.server, destination, "photos")
	if fresh.ID == old.ID {
		t.Fatal("trashed folder was reused")
	}
	assertTree(t, h.remoteTree(fresh.ID), expectedRemote("photos", files))
	again := h.preview(source, destination)
	for path, action := range planActions(again) {
		if action != "skip" {
			t.Fatalf("after recreation %q = %s, want skip", path, action)
		}
	}
}

func TestE2EFileDeletedOrRenamedInDriveIsRecreatedIndividually(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "docs")
	files := map[string]string{"keep.txt": "keep", "deleted.txt": "deleted", "renamed.txt": "renamed"}
	writeTree(t, source, files)
	destination := h.server.AddUserFolder("Docs backup", "root", true)
	if _, status := h.run(source, destination); status.State != "succeeded" {
		t.Fatalf("first run = %+v", status)
	}
	root := managedRoot(t, h.server, destination, "docs")
	var renamedID string
	for _, child := range h.server.Children(root.ID) {
		switch child.Name {
		case "deleted.txt":
			h.server.Delete(child.ID)
		case "renamed.txt":
			renamedID = child.ID
			h.server.Update(child.ID, func(o *drivetest.Object) { o.Name = "renamed by user.txt" })
		}
	}
	plan := h.preview(source, destination)
	actions := planActions(plan)
	if actions[""] != "skip" || actions["keep.txt"] != "skip" || actions["deleted.txt"] != "recreate" || actions["renamed.txt"] != "recreate" {
		t.Fatalf("plan = %v", actions)
	}
	if status := h.start(plan); status.State != "succeeded" {
		t.Fatalf("run = %+v", status)
	}
	if o, ok := h.server.Get(renamedID); !ok || o.Name != "renamed by user.txt" {
		t.Fatal("the user's renamed Drive file was changed")
	}
	got := h.remoteTree(root.ID)
	want := expectedRemote("docs", files)
	want["renamed by user.txt"] = localDigest("renamed")
	assertTree(t, got, want)
}

func TestE2ECancelledMultiChunkUploadResumesWithoutDuplicates(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "media")
	big := strings.Repeat("z", 900<<10)
	writeTree(t, source, map[string]string{"a-small.txt": "small", "b-big.bin": big})
	destination := h.server.AddUserFolder("Media", "root", true)
	plan := h.preview(source, destination)
	// Cancel the run while the second chunk of the large file is in flight.
	chunks := 0
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 100, drivetest.Fault{Hook: func(*http.Request) {
		chunks++
		if chunks == 3 {
			h.service.Cancel()
		}
	}})
	status := h.start(plan)
	if status.State != "cancelled" || status.CompletedFiles != 1 {
		t.Fatalf("cancelled run = %+v", status)
	}
	resume := h.preview(source, destination)
	actions := planActions(resume)
	if actions["a-small.txt"] != "skip" || actions["b-big.bin"] != "resume" {
		t.Fatalf("resume plan = %v", actions)
	}
	if status = h.start(resume); status.State != "succeeded" {
		t.Fatalf("resumed run = %+v", status)
	}
	root := managedRoot(t, h.server, destination, "media")
	assertTree(t, h.remoteTree(root.ID), map[string]string{"a-small.txt": localDigest("small"), "b-big.bin": localDigest(big)})
}

func TestE2ETransientFailuresRetryWithinBoundsAndLostAcknowledgementsReconcile(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "flaky")
	big := strings.Repeat("q", 600<<10)
	writeTree(t, source, map[string]string{"one.txt": "1", "two.bin": big})
	destination := h.server.AddUserFolder("Flaky", "root", true)
	plan := h.preview(source, destination)
	h.server.Inject(drivetest.MethodPath("POST", "/drive/v3/files"), 1, drivetest.Fault{DropAfterCommit: true})
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 1, drivetest.Fault{Status: 503})
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 1, drivetest.Fault{Status: 429, Reason: "userRateLimitExceeded", RetryAfter: "1"})
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 1, drivetest.Fault{DropAfterCommit: true})
	h.server.Inject(drivetest.MethodPath("GET", "/drive/v3/files/"), 1, drivetest.Fault{DropBeforeCommit: true})
	status := h.start(plan)
	if status.State != "succeeded" {
		t.Fatalf("run with transient failures = %+v", status)
	}
	root := managedRoot(t, h.server, destination, "flaky")
	assertTree(t, h.remoteTree(root.ID), map[string]string{"one.txt": localDigest("1"), "two.bin": localDigest(big)})
}

func TestE2EExpiredSessionNeedsReviewThenResumesSameIdentity(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "session")
	big := strings.Repeat("s", 600<<10)
	writeTree(t, source, map[string]string{"big.bin": big})
	destination := h.server.AddUserFolder("Sessions", "root", true)
	plan := h.preview(source, destination)
	h.server.Inject(drivetest.MethodPath("PUT", "/upload/drive/v3/files"), 1, drivetest.Fault{Status: 404, Reason: "notFound"})
	status := h.start(plan)
	if status.State == "succeeded" || status.ErrorCode == "" {
		t.Fatalf("expired session reported = %+v", status)
	}
	resume := h.preview(source, destination)
	if planActions(resume)["big.bin"] != "resume" {
		t.Fatalf("resume plan = %v", planActions(resume))
	}
	if status = h.start(resume); status.State != "succeeded" {
		t.Fatalf("resumed = %+v", status)
	}
	root := managedRoot(t, h.server, destination, "session")
	if children := h.server.Children(root.ID); len(children) != 1 {
		t.Fatalf("expected one copy, got %d", len(children))
	}
}

func TestE2ERuleChangeDuringRunStopsBeforeFurtherMutation(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "rules")
	writeTree(t, source, map[string]string{".gitignore": "*.tmp\n", "a.txt": "a", "b.txt": "b", "secret.env": "token-like fixture"})
	destination := h.server.AddUserFolder("Rules", "root", true)
	plan := h.preview(source, destination)
	h.server.Inject(drivetest.MethodPath("POST", "/upload/drive/v3/files"), 1, drivetest.Fault{Hook: func(*http.Request) {
		_ = os.WriteFile(filepath.Join(source, ".gitignore"), []byte("*.tmp\n*.env\n"), 0o644)
		later := time.Now().Add(2 * time.Second)
		_ = os.Chtimes(filepath.Join(source, ".gitignore"), later, later)
	}})
	status := h.start(plan)
	if status.State == "succeeded" || status.ErrorCode != "RULES_CHANGED" {
		t.Fatalf("rule change accepted: %+v", status)
	}
	root := managedRoot(t, h.server, destination, "rules")
	for _, child := range h.server.Children(root.ID) {
		if child.Name == "secret.env" {
			t.Fatal("a file excluded by the changed rules was uploaded")
		}
	}
}

func TestE2EDestinationTrashedAfterPreviewBlocksMutation(t *testing.T) {
	h := newE2E(t)
	source := filepath.Join(t.TempDir(), "dest")
	writeTree(t, source, map[string]string{"a.txt": "a"})
	destination := h.server.AddUserFolder("Target", "root", true)
	plan := h.preview(source, destination)
	h.server.Update(destination, func(o *drivetest.Object) { o.Trashed = true })
	before := len(h.server.Requests())
	status := h.start(plan)
	if status.State != "failed" || status.ErrorCode != "DESTINATION_CHANGED" && status.ErrorCode != "DESTINATION_UNAVAILABLE" {
		t.Fatalf("trashed destination accepted: %+v", status)
	}
	for _, r := range h.server.Requests()[before:] {
		if strings.HasPrefix(r, "POST") || strings.HasPrefix(r, "PUT") {
			t.Fatalf("mutation after destination change: %s", r)
		}
	}
}
