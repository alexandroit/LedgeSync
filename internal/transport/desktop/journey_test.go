package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/providers/drive/drivetest"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

// journeyGoogle is a concurrency-safe connected account with a native Picker.
type journeyGoogle struct {
	mu      sync.Mutex
	account string
	picked  string
}

func (g *journeyGoogle) Status(context.Context) (driveauth.Status, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return driveauth.Status{State: "connected", ClientConfigured: true, Account: &driveauth.Account{Reference: g.account}}, nil
}
func (g *journeyGoogle) Connect(ctx context.Context) (driveauth.Status, error)    { return g.Status(ctx) }
func (g *journeyGoogle) Check(ctx context.Context) (driveauth.Status, error)      { return g.Status(ctx) }
func (g *journeyGoogle) Disconnect(ctx context.Context) (driveauth.Status, error) { return g.Status(ctx) }
func (g *journeyGoogle) Revoke(ctx context.Context, _ string, _ bool) (driveauth.Status, error) {
	return g.Status(ctx)
}
func (g *journeyGoogle) Cancel() {}
func (g *journeyGoogle) ChooseFolder(context.Context, string) (driveauth.SelectedFolder, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return driveauth.SelectedFolder{ID: g.picked, AccountReference: g.account}, nil
}

type journey struct {
	t      *testing.T
	server *drivetest.Server
	google *journeyGoogle
	app    *App
	source string
	state  string
}

func newJourney(t *testing.T) *journey {
	t.Helper()
	server := drivetest.New()
	t.Cleanup(server.Close)
	account := "google-drive:journey"
	google := &journeyGoogle{account: account}
	client := drive.NewWithOptions(server.Authorizer(account), drive.Options{ChunkSize: 256 << 10, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	state := filepath.Join(t.TempDir(), "state")
	local := app.NewService()
	source := filepath.Join(t.TempDir(), "Client Work")
	a := NewDesktop(Options{
		Preview:      local,
		FolderPicker: func() (string, error) { return source, nil },
		Google:       google,
		Transfers:    transfer.New(local, client, google, state),
		Automatic:    transfer.New(local, client, google, state),
		Projects:     projects.NewStore(state),
	})
	t.Cleanup(a.Shutdown)
	return &journey{t: t, server: server, google: google, app: a, source: source, state: state}
}

func (j *journey) write(files map[string]string) {
	j.t.Helper()
	for name, content := range files {
		p := filepath.Join(j.source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			j.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			j.t.Fatal(err)
		}
	}
}

func (j *journey) upload() transfer.Status {
	j.t.Helper()
	plan, err := j.app.PreviewDriveUpload()
	if err != nil {
		j.t.Fatalf("preview: %v", err)
	}
	if _, err = j.app.StartDriveUpload(plan.PlanDigest); err != nil {
		j.t.Fatalf("start: %v", err)
	}
	return j.waitManual()
}

func (j *journey) waitManual() transfer.Status {
	j.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, _ := j.app.DriveTransferStatus()
		if st.State != "uploading" && st.State != "verifying" {
			// History is recorded by the finish observer right after the state changes.
			time.Sleep(20 * time.Millisecond)
			return st
		}
		if time.Now().After(deadline) {
			j.t.Fatalf("upload did not finish: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDesktopJourneyMyDriveFirstCopyRepeatAndReopen(t *testing.T) {
	j := newJourney(t)
	j.write(map[string]string{".gitignore": "*.log\n", "brief.md": "brief", "assets/logo.svg": "<svg/>", "debug.log": "ignored"})
	if _, err := j.app.OpenFolder(); err != nil {
		t.Fatal(err)
	}
	destination, err := j.app.UseMyDrive()
	if err != nil || destination.ID != "root" || destination.Name != "My Drive" || !destination.MyDrive {
		t.Fatalf("My Drive destination = %+v, %v", destination, err)
	}
	if st := j.upload(); st.State != "succeeded" || st.CompletedFiles != 3 || st.RemoteFolderID == "" {
		t.Fatalf("first copy = %+v", st)
	}
	list, err := j.app.ListProjects()
	if err != nil || len(list) != 1 || list[0].Name != "Client Work" || list[0].LastRun == nil || list[0].LastRun.State != "succeeded" {
		t.Fatalf("approved run was not saved as a sync pair with history: %+v %v", list, err)
	}
	j.write(map[string]string{"notes/new.txt": "added later"})
	plan, err := j.app.PreviewDriveUpload()
	if err != nil {
		t.Fatal(err)
	}
	if plan.NewFiles != 1 || plan.UnchangedFiles != 3 {
		t.Fatalf("repeat plan = new %d unchanged %d", plan.NewFiles, plan.UnchangedFiles)
	}
	if _, err = j.app.StartDriveUpload(plan.PlanDigest); err != nil {
		t.Fatal(err)
	}
	if st := j.waitManual(); st.State != "succeeded" {
		t.Fatalf("repeat = %+v", st)
	}
	history, err := j.app.ProjectHistory(list[0].ID)
	if err != nil || len(history) != 2 || history[0].Trigger != "manual" {
		t.Fatalf("history = %+v %v", history, err)
	}
	// A restarted application reopens the saved pair without any approval.
	restartedLocal := app.NewService()
	restarted := NewDesktop(Options{Preview: restartedLocal, Google: j.google, Transfers: transfer.New(restartedLocal, drive.NewWithOptions(j.server.Authorizer(j.google.account), drive.Options{}), j.google, j.state), Projects: projects.NewStore(j.state)})
	t.Cleanup(restarted.Shutdown)
	session, err := restarted.OpenProject(list[0].ID)
	if err != nil || session.Destination == nil || session.DestinationError != nil || session.Preview == nil {
		t.Fatalf("reopen = %+v %v", session, err)
	}
	if _, err = restarted.StartDriveUpload("not-approved"); domain.ErrorCode(err) != "PLAN_REQUIRED" {
		t.Fatalf("reopened pair carried an approval: %v", err)
	}
	again, err := restarted.PreviewDriveUpload()
	if err != nil || again.NewFiles != 0 || again.UnchangedFiles != 4 {
		t.Fatalf("reopened preview = %+v %v", again, err)
	}
}

func TestDesktopJourneyPickerAutomationRunsAndPausesOnRuleChange(t *testing.T) {
	j := newJourney(t)
	j.google.picked = j.server.AddUserFolder("Shared backups", "root", true)
	j.write(map[string]string{".gitignore": "tmp/\n", "a.txt": "a"})
	if _, err := j.app.OpenFolder(); err != nil {
		t.Fatal(err)
	}
	if d, err := j.app.ChooseDriveDestination(); err != nil || d.ID != j.google.picked {
		t.Fatalf("picker destination = %+v %v", d, err)
	}
	if st := j.upload(); st.State != "succeeded" {
		t.Fatalf("first copy = %+v", st)
	}
	plan, err := j.app.PreviewDriveUpload()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.app.AuthorizeAutomation("", projects.TriggerInterval, 59, plan.PlanDigest); domain.ErrorCode(err) != "CONFIG_INVALID" {
		t.Fatalf("interval below one minute accepted: %v", err)
	}
	p, err := j.app.AuthorizeAutomation("", projects.TriggerWatch, 60, plan.PlanDigest)
	if err != nil || !p.Automation.Enabled || p.Automation.Authorization == nil || p.Automation.Authorization.RulesDigest != plan.RulesDigest {
		t.Fatalf("authorization = %+v %v", p, err)
	}
	j.write(map[string]string{"b.txt": "added while automatic copies are on", "tmp/scratch": "ignored"})
	runner := automationRunner{j.app}
	outcome, err := runner.RunAuthorized(context.Background(), p)
	if err != nil || !outcome.Ran || outcome.Summary.State != "succeeded" || outcome.Summary.CompletedFiles != 3 {
		t.Fatalf("automatic run = %+v %v", outcome, err)
	}
	root := j.server.Children(j.google.picked)
	if len(root) != 1 {
		t.Fatalf("managed folders = %d", len(root))
	}
	names := []string{}
	for _, child := range j.server.Children(root[0].ID) {
		names = append(names, child.Name)
	}
	if strings.Join(names, ",") != ".gitignore,a.txt,b.txt" {
		t.Fatalf("automatic copy contents = %v", names)
	}
	// Nothing changed: no transfer runs.
	if outcome, err = runner.RunAuthorized(context.Background(), p); err != nil || outcome.Ran {
		t.Fatalf("unchanged automatic check ran a transfer: %+v %v", outcome, err)
	}
	// Changing ignore rules never broadens an authorization.
	j.write(map[string]string{".gitignore": "tmp/\n*.txt\n"})
	if _, err = runner.RunAuthorized(context.Background(), p); domain.ErrorCode(err) != "AUTOMATION_REVIEW_REQUIRED" || !projects.Pausing(err) {
		t.Fatalf("rule change did not require review: %v", err)
	}
	// The scheduler records the pause on the saved pair.
	p.Automation.NextRunAt = ""
	if _, err = j.app.projects.Save(p); err != nil {
		t.Fatal(err)
	}
	if err = j.app.scheduler.CheckNow(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := j.app.projects.Get(p.ID)
	if err != nil || !paused.Automation.Paused || paused.Automation.PauseCode != "AUTOMATION_REVIEW_REQUIRED" {
		t.Fatalf("scheduler did not pause: %+v %v", paused.Automation, err)
	}
}

func TestDesktopErrorsAreTypedAndRedacted(t *testing.T) {
	j := newJourney(t)
	j.write(map[string]string{"a.txt": "a"})
	if _, err := j.app.PreviewDriveUpload(); domain.ErrorCode(err) != "ROOT_REQUIRED" {
		t.Fatalf("missing source = %v", err)
	}
	if _, err := j.app.OpenFolder(); err != nil {
		t.Fatal(err)
	}
	if _, err := j.app.PreviewDriveUpload(); domain.ErrorCode(err) != "DESTINATION_REQUIRED" {
		t.Fatalf("missing destination = %v", err)
	}
	j.google.picked = "missingFolderId"
	_, err := j.app.ChooseDriveDestination()
	if domain.ErrorCode(err) != "DRIVE_NOT_FOUND" {
		t.Fatalf("unavailable picked folder = %v", err)
	}
	home, _ := os.UserHomeDir()
	for _, private := range []string{"upload_id", "Bearer", "ya29"} {
		if strings.Contains(err.Error(), private) || len(home) > 1 && strings.Contains(err.Error(), home) {
			t.Fatalf("error leaked %q: %v", private, err)
		}
	}
}
