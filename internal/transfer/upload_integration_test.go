package transfer_test

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

const testAccount = "google-drive:transfer-fixture"

type testAccounts struct {
	mu               sync.Mutex
	reference, state string
}

func (a *testAccounts) Status(ctx context.Context) (driveauth.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return driveauth.Status{}, err
	}
	return driveauth.Status{State: a.state, Account: &driveauth.Account{Reference: a.reference}}, nil
}
func (a *testAccounts) change(reference, state string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reference, a.state = reference, state
}

type memoryDrive struct {
	mu                sync.Mutex
	folder            drive.Folder
	objects           map[string]drive.Object
	writes, generated int
	lostAckName       string
	blockName         string
	entered           chan struct{}
	enteredOnce       sync.Once
	afterWrite        func(string)
}

func newMemoryDrive() *memoryDrive {
	return &memoryDrive{folder: drive.Folder{ID: "destination", Name: "Existing parent", Parents: []string{"root"}, CanAddChildren: true}, objects: map[string]drive.Object{}}
}
func (p *memoryDrive) GetFolder(ctx context.Context, account, id string) (drive.Folder, error) {
	if err := ctx.Err(); err != nil {
		return drive.Folder{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if account != testAccount || id != p.folder.ID {
		return drive.Folder{}, domain.Fail("DRIVE_NOT_FOUND", "Folder unavailable")
	}
	f := p.folder
	f.Parents = append([]string{}, f.Parents...)
	return f, nil
}
func copyObject(o drive.Object) drive.Object {
	o.Parents = append([]string{}, o.Parents...)
	properties := map[string]string{}
	for k, v := range o.AppProperties {
		properties[k] = v
	}
	o.AppProperties = properties
	return o
}
func (p *memoryDrive) GetObject(ctx context.Context, account, id string) (drive.Object, error) {
	if err := ctx.Err(); err != nil {
		return drive.Object{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if account != testAccount {
		return drive.Object{}, domain.Fail("ACCOUNT_CHANGED", "Account unavailable")
	}
	o, ok := p.objects[id]
	if !ok {
		return drive.Object{}, domain.Fail("DRIVE_NOT_FOUND", "Object unavailable")
	}
	return copyObject(o), nil
}
func (p *memoryDrive) GenerateIDs(ctx context.Context, account string, count int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, count)
	for i := range ids {
		p.generated++
		ids[i] = fmt.Sprintf("reserved-%d", p.generated)
	}
	return ids, nil
}
func (p *memoryDrive) CreateFolder(ctx context.Context, account, id, parent, name, operation string) (drive.Object, error) {
	return p.write(ctx, drive.Object{ID: id, Name: name, Parents: []string{parent}, MimeType: "application/vnd.google-apps.folder", AppProperties: map[string]string{"ledgesyncOperation": operation}})
}
func (p *memoryDrive) Upload(ctx context.Context, account, id, parent, name, operation string, source io.ReadSeeker, size int64, expectedMD5 string) (drive.Object, error) {
	p.mu.Lock()
	blocked := name == p.blockName && p.blockName != ""
	entered := p.entered
	p.mu.Unlock()
	if blocked {
		p.enteredOnce.Do(func() { close(entered) })
		<-ctx.Done()
		return drive.Object{}, ctx.Err()
	}
	content, err := io.ReadAll(source)
	if err != nil {
		return drive.Object{}, err
	}
	sum := md5.Sum(content)
	digest := hex.EncodeToString(sum[:])
	if int64(len(content)) != size || digest != expectedMD5 {
		return drive.Object{}, domain.Fail("SOURCE_CHANGED", "Unexpected source content")
	}
	return p.write(ctx, drive.Object{ID: id, Name: name, Parents: []string{parent}, MimeType: "application/octet-stream", Size: size, MD5: digest, AppProperties: map[string]string{"ledgesyncOperation": operation}})
}
func (p *memoryDrive) write(ctx context.Context, o drive.Object) (drive.Object, error) {
	if err := ctx.Err(); err != nil {
		return drive.Object{}, err
	}
	p.mu.Lock()
	if _, exists := p.objects[o.ID]; exists {
		p.mu.Unlock()
		return drive.Object{}, domain.Fail("DRIVE_CONFLICT", "No overwrites allowed")
	}
	p.objects[o.ID] = copyObject(o)
	p.writes++
	lost := p.lostAckName == o.Name && p.lostAckName != ""
	if lost {
		p.lostAckName = ""
	}
	after := p.afterWrite
	p.mu.Unlock()
	if after != nil {
		after(o.Name)
	}
	if lost {
		return drive.Object{}, domain.Fail("UNKNOWN_REMOTE_RESULT", "Upload acknowledgement was lost")
	}
	return copyObject(o), nil
}
func (p *memoryDrive) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes, p.generated
}
func (p *memoryDrive) snapshot() []drive.Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := []drive.Object{}
	for _, o := range p.objects {
		result = append(result, copyObject(o))
	}
	return result
}

type fixture struct {
	source, state string
	api           *memoryDrive
	accounts      *testAccounts
	service       *transfer.Service
}

func fixtureFor(t *testing.T, files map[string]string, dirs ...string) *fixture {
	t.Helper()
	base := t.TempDir()
	source := filepath.Join(base, "local-project")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(source, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		writeFile(t, source, name, content)
	}
	f := &fixture{source: source, state: filepath.Join(base, "private-state"), api: newMemoryDrive(), accounts: &testAccounts{reference: testAccount, state: "connected"}}
	f.service = transfer.New(app.NewService(), f.api, f.accounts, f.state)
	if _, err := f.service.SetDestination(context.Background(), "destination", testAccount); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.service.CancelAndWait)
	return f
}
func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func previewPlan(t *testing.T, f *fixture) transfer.Plan {
	t.Helper()
	p, err := f.service.Preview(context.Background(), f.source, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func finishRun(t *testing.T, s *transfer.Service) transfer.Status {
	t.Helper()
	// Native ACL checks and synchronous journal flushes run on the hosted
	// filesystem even with a fake provider. This is a completion bound, not an
	// upload performance assertion; busy Windows runners can exceed five seconds.
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if !s.Busy() {
			return s.Status()
		}
		select {
		case <-timer.C:
			status := s.Status()
			s.Cancel()
			t.Fatalf("transfer did not finish within thirty seconds; last status: %+v", status)
		case <-ticker.C:
		}
	}
}
func runPlan(t *testing.T, f *fixture, p transfer.Plan) transfer.Status {
	t.Helper()
	if _, err := f.service.Start(context.Background(), p.PlanDigest); err != nil {
		t.Fatal(err)
	}
	return finishRun(t, f.service)
}
func requireNoRemoteWrites(t *testing.T, f *fixture) {
	t.Helper()
	writes, generated := f.api.counts()
	if writes != 0 || generated != 0 {
		t.Fatalf("unapproved or stale plan touched provider: writes=%d generated=%d", writes, generated)
	}
}

func TestUploadPreservesHierarchyEmptyDirectoriesAndIgnoreRules(t *testing.T) {
	f := fixtureFor(t, map[string]string{".gitignore": "ignored/\n*.log\n", "README.md": "readme", "src/main.txt": "main", "src/deep/nested.txt": "nested", "ignored/secret.txt": "excluded", "debug.log": "excluded"}, "empty", "src/deep")
	plan := previewPlan(t, f)
	requireNoRemoteWrites(t, f)
	if plan.FileCount != 4 || plan.FolderCount != 4 || plan.ExcludedCount < 2 {
		t.Fatalf("unexpected selection counts: %+v", plan)
	}
	if f.service.Status().State != "awaiting_approval" {
		t.Fatal("preview did not await explicit approval")
	}
	status := runPlan(t, f, plan)
	if status.State != "succeeded" || status.CompletedFiles != plan.FileCount || status.UploadedBytes != plan.TotalBytes {
		t.Fatalf("run did not verify approved snapshot: %+v", status)
	}
	objects := f.api.snapshot()
	byName := map[string]drive.Object{}
	for _, o := range objects {
		byName[o.Name] = o
		if o.Name == "secret.txt" || o.Name == "debug.log" || o.Name == "ignored" {
			t.Fatalf("excluded path uploaded: %s", o.Name)
		}
	}
	if byName["local-project"].Parents[0] != "destination" || byName["src"].Parents[0] != byName["local-project"].ID || byName["deep"].Parents[0] != byName["src"].ID || byName["nested.txt"].Parents[0] != byName["deep"].ID || byName["empty"].MimeType != "application/vnd.google-apps.folder" {
		t.Fatalf("hierarchy changed: %+v", objects)
	}
	if content, err := os.ReadFile(filepath.Join(f.source, ".gitignore")); err != nil || string(content) != "ignored/\n*.log\n" {
		t.Fatal("source rules changed")
	}
}

func TestUploadRequiresExactCurrentApproval(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first"})
	if _, err := f.service.Start(context.Background(), "invented"); domain.ErrorCode(err) != "PLAN_REQUIRED" {
		t.Fatalf("missing plan accepted: %v", err)
	}
	p := previewPlan(t, f)
	if _, err := f.service.Start(context.Background(), p.PlanDigest+"changed"); domain.ErrorCode(err) != "PLAN_REQUIRED" {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	requireNoRemoteWrites(t, f)
	if status := runPlan(t, f, p); status.State != "succeeded" {
		t.Fatalf("valid plan failed: %+v", status)
	}
	if _, err := f.service.Start(context.Background(), p.PlanDigest); domain.ErrorCode(err) != "PLAN_REQUIRED" {
		t.Fatalf("consumed plan replayed: %v", err)
	}
}

func TestUploadRejectsSourceRulesDestinationAndAccountChangesBeforeMutation(t *testing.T) {
	cases := map[string]func(*testing.T, *fixture){"file content": func(t *testing.T, f *fixture) { writeFile(t, f.source, "a.txt", "other") }, "new file": func(t *testing.T, f *fixture) { writeFile(t, f.source, "new.txt", "new") }, "ignore rules": func(t *testing.T, f *fixture) { writeFile(t, f.source, ".gitignore", "a.txt\n") }, "missing rule source": func(t *testing.T, f *fixture) {
		if err := os.Remove(filepath.Join(f.source, ".gitignore")); err != nil {
			t.Fatal(err)
		}
	}, "destination move": func(t *testing.T, f *fixture) {
		f.api.mu.Lock()
		f.api.folder.Parents = []string{"different-parent"}
		f.api.mu.Unlock()
	}, "destination read only": func(t *testing.T, f *fixture) {
		f.api.mu.Lock()
		f.api.folder.CanAddChildren = false
		f.api.mu.Unlock()
	}, "account switched": func(t *testing.T, f *fixture) { f.accounts.change("google-drive:other", "connected") }, "grant revoked": func(t *testing.T, f *fixture) { f.accounts.change(testAccount, "reconnect_required") }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixtureFor(t, map[string]string{"a.txt": "first", ".gitignore": "*.log\n"})
			p := previewPlan(t, f)
			change(t, f)
			status := runPlan(t, f, p)
			if status.State != "failed" && status.State != "needs_review" {
				t.Fatalf("stale plan accepted: %+v", status)
			}
			requireNoRemoteWrites(t, f)
		})
	}
}

func TestRepeatedUploadSkipsVerifiedObjectsAndChangedFileKeepsBoth(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first", "nested/b.txt": "stable"}, "empty")
	first := previewPlan(t, f)
	if status := runPlan(t, f, first); status.State != "succeeded" {
		t.Fatal(status)
	}
	before, _ := f.api.counts()
	repeated := previewPlan(t, f)
	for _, entry := range repeated.Entries {
		if entry.Action != "skip" {
			t.Fatalf("verified entry not skipped: %+v", entry)
		}
	}
	if status := runPlan(t, f, repeated); status.State != "succeeded" {
		t.Fatal(status)
	}
	after, _ := f.api.counts()
	if after != before {
		t.Fatalf("unchanged retry created %d duplicate objects", after-before)
	}
	writeFile(t, f.source, "a.txt", "new content")
	changed := previewPlan(t, f)
	found := false
	for _, entry := range changed.Entries {
		if entry.RelativePath == "a.txt" {
			found = entry.Action == "keep-both"
		}
	}
	if !found {
		t.Fatal("modified existing file did not require keep-both")
	}
	if status := runPlan(t, f, changed); status.State != "succeeded" {
		t.Fatal(status)
	}
	after, _ = f.api.counts()
	if after != before+1 {
		t.Fatalf("expected one new content object, got %d", after-before)
	}
	original, replacement := false, false
	for _, o := range f.api.snapshot() {
		original = original || o.Name == "a.txt"
		replacement = replacement || strings.HasPrefix(o.Name, "a.txt.ledgesync-")
	}
	if !original || !replacement {
		t.Fatal("keep-both did not preserve original and separate changed file")
	}
}

func TestLostWriteAcknowledgementReconcilesReservedIDWithoutDuplicate(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first", "b.txt": "second"})
	f.api.lostAckName = "a.txt"
	p := previewPlan(t, f)
	status := runPlan(t, f, p)
	if status.State != "needs_review" {
		t.Fatalf("ambiguous upload was not retained for review: %+v", status)
	}
	before, _ := f.api.counts()
	if before != 2 {
		t.Fatalf("expected root and remotely written file, got %d", before)
	}
	recovered := previewPlan(t, f)
	for _, entry := range recovered.Entries {
		if entry.RelativePath == "a.txt" && entry.Action != "skip" {
			t.Fatalf("remote acknowledgement was not reconciled: %+v", entry)
		}
	}
	if status = runPlan(t, f, recovered); status.State != "succeeded" {
		t.Fatal(status)
	}
	after, _ := f.api.counts()
	if after != 3 {
		t.Fatalf("retry created duplicate objects: %d", after)
	}
	count := 0
	for _, o := range f.api.snapshot() {
		if o.Name == "a.txt" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one recovered a.txt, got %d", count)
	}
}

func TestCancellationWaitsForProviderAndPreservesAlreadyCreatedFolder(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first"})
	f.api.blockName = "a.txt"
	f.api.entered = make(chan struct{})
	p := previewPlan(t, f)
	if _, err := f.service.Start(context.Background(), p.PlanDigest); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.api.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upload never reached cancellable provider")
	}
	if status := f.service.Status(); status.State == "succeeded" || status.CompletedFiles != 0 {
		t.Fatal(status)
	}
	f.service.Cancel()
	status := finishRun(t, f.service)
	if status.State != "cancelled" || status.CompletedFiles != 0 {
		t.Fatalf("cancellation misreported completion: %+v", status)
	}
	objects := f.api.snapshot()
	if len(objects) != 1 || objects[0].MimeType != "application/vnd.google-apps.folder" {
		t.Fatalf("completed root was deleted or partial file invented: %+v", objects)
	}
	f.api.mu.Lock()
	f.api.blockName = ""
	f.api.mu.Unlock()
	resume := previewPlan(t, f)
	if status = runPlan(t, f, resume); status.State != "succeeded" {
		t.Fatal(status)
	}
	writes, _ := f.api.counts()
	if writes != 2 {
		t.Fatalf("resume duplicated the original folder: %d writes", writes)
	}
}

func TestCompetingServiceCannotPreviewOrWriteWhileJournalLocked(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first"})
	f.api.blockName = "a.txt"
	f.api.entered = make(chan struct{})
	other := transfer.New(app.NewService(), f.api, f.accounts, f.state)
	t.Cleanup(other.CancelAndWait)
	if _, err := other.SetDestination(context.Background(), "destination", testAccount); err != nil {
		t.Fatal(err)
	}
	p := previewPlan(t, f)
	if _, err := f.service.Start(context.Background(), p.PlanDigest); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.api.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upload not blocked")
	}
	if _, err := other.Preview(context.Background(), f.source, false); domain.ErrorCode(err) != "TRANSFER_BUSY" {
		t.Fatalf("second writer was not rejected: %v", err)
	}
	f.service.Cancel()
	if status := finishRun(t, f.service); status.State != "cancelled" {
		t.Fatal(status)
	}
}

func TestRemoteIdentityChangeAfterPreviewPreventsAnyFurtherMutation(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first"})
	if status := runPlan(t, f, previewPlan(t, f)); status.State != "succeeded" {
		t.Fatal(status)
	}
	p := previewPlan(t, f)
	before, _ := f.api.counts()
	f.api.mu.Lock()
	for id, o := range f.api.objects {
		if o.Name == "a.txt" {
			o.MD5 = "changed-checksum"
			f.api.objects[id] = o
		}
	}
	f.api.mu.Unlock()
	status := runPlan(t, f, p)
	if status.State != "needs_review" {
		t.Fatalf("remote change accepted: %+v", status)
	}
	after, _ := f.api.counts()
	if after != before {
		t.Fatal("remote conflict triggered a write")
	}
}

func TestSourceEditedDuringUploadCannotBeReportedAsVerifiedSnapshot(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "first", "z.txt": "stable"})
	f.api.afterWrite = func(name string) {
		if name == "a.txt" {
			if err := os.WriteFile(filepath.Join(f.source, "z.txt"), []byte("edited"), 0600); err != nil {
				panic(err)
			}
		}
	}
	p := previewPlan(t, f)
	status := runPlan(t, f, p)
	if status.State != "needs_review" && status.State != "failed" {
		t.Fatalf("changed source reported successful: %+v", status)
	}
	for _, o := range f.api.snapshot() {
		if o.Name == "z.txt" {
			t.Fatal("unapproved changed source was uploaded")
		}
	}
}

func TestMissingIgnoreSourceSurvivesApplicationRestartAndDestinationChange(t *testing.T) {
	for _, changeDestination := range []bool{false, true} {
		t.Run(fmt.Sprintf("different_destination_%t", changeDestination), func(t *testing.T) {
			f := fixtureFor(t, map[string]string{".gitignore": "private.txt\n", "private.txt": "must remain excluded", "public.txt": "included"})
			if status := runPlan(t, f, previewPlan(t, f)); status.State != "succeeded" {
				t.Fatal(status)
			}
			before, _ := f.api.counts()
			if err := os.Remove(filepath.Join(f.source, ".gitignore")); err != nil {
				t.Fatal(err)
			}
			// Both application services are recreated so the previous in-memory observed
			// source list is unavailable. The journal must retain that boundary.
			restarted := transfer.New(app.NewService(), f.api, f.accounts, f.state)
			t.Cleanup(restarted.CancelAndWait)
			destinationID := "destination"
			if changeDestination {
				destinationID = "another-destination"
				f.api.mu.Lock()
				f.api.folder.ID = destinationID
				f.api.mu.Unlock()
			}
			if _, err := restarted.SetDestination(context.Background(), destinationID, testAccount); err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.Preview(context.Background(), f.source, false); domain.ErrorCode(err) != "RULE_SOURCE_UNAVAILABLE" {
				t.Fatalf("restart lost the ignore-source baseline: %v", err)
			}
			after, _ := f.api.counts()
			if after != before {
				t.Fatal("missing policy caused an upload")
			}
			// An explicitly edited project configuration may establish a new baseline;
			// the resulting newly included file still needs a fresh approved plan.
			configuration := config.Default(f.source)
			configuration.Filters.Groups[0].Enabled = false
			encoded, err := json.Marshal(configuration)
			if err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(filepath.Dir(f.state), "explicit-policy-change.json")
			if err = os.WriteFile(filename, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			changed, err := restarted.Preview(context.Background(), filename, true)
			if err != nil {
				t.Fatalf("explicit configuration change could not establish a new baseline: %v", err)
			}
			included := false
			for _, entry := range changed.Entries {
				included = included || entry.RelativePath == "private.txt"
			}
			if !included || changed.ExcludedCount != 0 {
				t.Fatalf("changed policy did not produce a reviewable new selection: %+v", changed)
			}
			after, _ = f.api.counts()
			if after != before {
				t.Fatal("new configuration implicitly approved writes")
			}
		})
	}
}

// The third inventory is the executor's final source snapshot, after all remote
// checks and node acknowledgements but before durable run finalization.
type finalScanPreviewer struct {
	inner      *app.Service
	scans      int
	afterFinal func() error
}

func (p *finalScanPreviewer) PreviewRoot(ctx context.Context, source string) (app.Preview, error) {
	result, err := p.inner.PreviewRoot(ctx, source)
	if err != nil {
		return result, err
	}
	p.scans++
	if p.scans == 3 {
		err = p.afterFinal()
	}
	return result, err
}
func (p *finalScanPreviewer) Preview(ctx context.Context, source string) (app.Preview, error) {
	return p.inner.Preview(ctx, source)
}

func TestDurableRunFinalizationFailureNeverReportsSucceeded(t *testing.T) {
	f := fixtureFor(t, map[string]string{"a.txt": "approved fixture"})
	// This is a separate SQLite connection inside the dedicated fake fixture. It
	// does not bypass the application lock in production or use personal state.
	filename := filepath.Join(f.state, "transfers.sqlite")
	uriPath := filepath.ToSlash(filename)
	if filepath.VolumeName(filename) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	var journal *sql.DB
	var blocker *sql.Conn
	var setupErr error
	locked := make(chan struct{})
	setupFailed := make(chan error, 1)
	reportSetupFailure := func(stage string, err error) error {
		if err != nil {
			select {
			case setupFailed <- fmt.Errorf("%s: %w", stage, err):
			default:
			}
		}
		return err
	}
	local := &finalScanPreviewer{inner: app.NewService(), afterFinal: func() error {
		if setupErr != nil {
			return setupErr
		}
		var err error
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		blocker, err = journal.Conn(ctx)
		if err != nil {
			return reportSetupFailure("open finalization blocker", err)
		}
		if _, err = blocker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return reportSetupFailure("lock finalization transaction", err)
		}
		close(locked)
		return nil
	}}
	f.service = transfer.New(local, f.api, f.accounts, f.state)
	t.Cleanup(f.service.CancelAndWait)
	if _, err := f.service.SetDestination(context.Background(), "destination", testAccount); err != nil {
		t.Fatal(err)
	}
	f.api.afterWrite = func(name string) {
		if name != "a.txt" {
			return
		}
		journal, setupErr = sql.Open("sqlite", uri.String())
		if setupErr != nil {
			reportSetupFailure("open fixture journal", setupErr)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, setupErr = journal.ExecContext(ctx, "CREATE TRIGGER reject_final_run_event BEFORE INSERT ON run_events BEGIN SELECT RAISE(ABORT, 'synthetic finalization failure'); END")
		reportSetupFailure("install finalization failure trigger", setupErr)
	}
	defer func() {
		if blocker != nil {
			_, _ = blocker.ExecContext(context.Background(), "ROLLBACK")
			blocker.Close()
		}
		if journal != nil {
			journal.Close()
		}
	}()
	p := previewPlan(t, f)
	if _, err := f.service.Start(context.Background(), p.PlanDigest); err != nil {
		t.Fatal(err)
	}
	select {
	case <-locked:
	case err := <-setupFailed:
		f.service.CancelAndWait()
		t.Fatalf("finalization fixture setup failed: %v", err)
	case <-time.After(5 * time.Second):
		f.service.CancelAndWait()
		t.Fatal("executor never reached controlled finalization window")
	}
	// A writer lock holds FinishRun long enough to catch optimistic success;
	// counters alone are not proof that the run's terminal event was persisted.
	deadline := time.NewTimer(100 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	observing := true
	for observing {
		status := f.service.Status()
		if status.State == "succeeded" {
			t.Fatal("success became visible before durable finalization")
		}
		select {
		case <-deadline.C:
			observing = false
		case <-ticker.C:
		}
	}
	if _, err := blocker.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	blocker.Close()
	blocker = nil
	status := finishRun(t, f.service)
	if status.State != "needs_review" || status.CompletedFiles != p.FileCount || status.UploadedBytes != p.TotalBytes {
		t.Fatalf("run commit failure was not separated from verified content: %+v", status)
	}
	if strings.Contains(status.Message, "synthetic finalization failure") {
		t.Fatal("SQLite diagnostic leaked into transfer status")
	}
}

type noAccount struct{}

func (noAccount) Status(context.Context) (driveauth.Status, error) {
	return driveauth.Status{State: "connected", ClientConfigured: true}, nil
}
func TestConnectedStatusWithoutAccountCannotSelectDestination(t *testing.T) {
	s := transfer.New(app.NewService(), newMemoryDrive(), noAccount{}, filepath.Join(t.TempDir(), "state"))
	if _, err := s.SetDestination(context.Background(), "destination", ""); domain.ErrorCode(err) != "AUTH_REQUIRED" {
		t.Fatalf("nil account accepted or wrong diagnostic: %v", err)
	}
	if s.Busy() {
		t.Fatal("invalid account leaked the operation lock")
	}
}
