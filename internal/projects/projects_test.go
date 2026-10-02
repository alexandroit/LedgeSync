package projects

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(filepath.Join(t.TempDir(), "state"))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testProject(t *testing.T, s *Store) Project {
	t.Helper()
	p, err := s.Save(Project{Name: "Pair", SourceRoot: filepath.Join(t.TempDir(), "src"), Policy: DefaultPolicy(), Destination: transfer.Destination{ID: "dest", Name: "Dest", AccountReference: "acct"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStoreValidatesAndRoundTrips(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Save(Project{Name: "", SourceRoot: "/x"}); domain.ErrorCode(err) != "CONFIG_INVALID" {
		t.Fatalf("empty name accepted: %v", err)
	}
	p := testProject(t, s)
	bad := p
	bad.Policy.Composition = "anything"
	if _, err := s.Save(bad); domain.ErrorCode(err) != "CONFIG_INVALID" {
		t.Fatalf("invalid policy accepted: %v", err)
	}
	enabled := p
	enabled.Automation = Automation{Enabled: true, Trigger: TriggerInterval, IntervalSeconds: 600}
	if _, err := s.Save(enabled); domain.ErrorCode(err) != "AUTOMATION_UNAUTHORIZED" {
		t.Fatalf("automation without authorization accepted: %v", err)
	}
	found, ok, err := s.FindPair(p.SourceRoot, "acct", "dest")
	if err != nil || !ok || found.ID != p.ID {
		t.Fatalf("pair lookup failed: %v %v", ok, err)
	}
	if !DefaultPolicy().IsDefault() {
		t.Fatal("default policy not recognized")
	}
	if err = s.RecordRun(RunSummary{RunID: "run1", ProjectID: p.ID, State: "succeeded", FinishedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.LastRun == nil || got.LastRun.RunID != "run1" {
		t.Fatal("last run not recorded")
	}
	if _, err = s.SaveSettings(Settings{DefaultConflictPolicy: "overwrite"}); domain.ErrorCode(err) != "CONFIG_INVALID" {
		t.Fatal("unsupported conflict policy accepted")
	}
}

type fakeRunner struct {
	mu          sync.Mutex
	err         error
	outcome     Outcome
	fingerprint string
	runs        int
}

func (f *fakeRunner) LocalFingerprint(context.Context, Project) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fingerprint, nil
}
func (f *fakeRunner) RunAuthorized(context.Context, Project) (Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs++
	return f.outcome, f.err
}

func authorized(t *testing.T, s *Store, trigger string) Project {
	t.Helper()
	p := testProject(t, s)
	p.Automation = Automation{Enabled: true, Trigger: trigger, IntervalSeconds: 60, Authorization: &Authorization{AccountReference: "acct", DestinationID: "dest"}}
	p, err := s.Save(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSchedulerPausesForReviewAndWaitsForTransientConditions(t *testing.T) {
	cases := map[string]struct {
		err   error
		pause bool
	}{
		"rules changed":     {domain.Fail("AUTOMATION_REVIEW_REQUIRED", "Review."), true},
		"account changed":   {domain.Fail("ACCOUNT_CHANGED", "Other account."), true},
		"volume unmounted":  {domain.Fail("SOURCE_UNAVAILABLE", "Missing."), false},
		"offline":           {domain.Fail("DRIVE_NETWORK", "Offline."), false},
		"manual upload now": {domain.Fail("TRANSFER_BUSY", "Busy."), false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			p := authorized(t, s, TriggerInterval)
			runner := &fakeRunner{err: c.err}
			sch := NewScheduler(s, runner, nil)
			if err := sch.CheckNow(context.Background(), p.ID); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Get(p.ID)
			if got.Automation.Paused != c.pause || (!c.pause && got.Automation.Waiting == "") {
				t.Fatalf("automation = %+v", got.Automation)
			}
			if !c.pause {
				next, _ := time.Parse(time.RFC3339, got.Automation.NextRunAt)
				if time.Until(next) < 4*time.Minute {
					t.Fatal("transient failure retried too soon")
				}
			}
		})
	}
}

func TestSchedulerWatchSkipsUnchangedSourceAndEditsWin(t *testing.T) {
	s := newTestStore(t)
	p := authorized(t, s, TriggerWatch)
	runner := &fakeRunner{fingerprint: "f1", outcome: Outcome{Ran: true, Fingerprint: "f1", Summary: RunSummary{State: "succeeded"}}}
	sch := NewScheduler(s, runner, nil)
	if err := sch.CheckNow(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if runner.runs != 1 {
		t.Fatal("first watch check did not run")
	}
	if err := sch.CheckNow(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if runner.runs != 1 {
		t.Fatal("unchanged source ran a transfer")
	}
	runner.fingerprint = "f2"
	if err := sch.CheckNow(context.Background(), p.ID); err != nil || runner.runs != 2 {
		t.Fatal("changed source did not run")
	}
	// Disabling while a check is due prevents the scheduler from re-enabling.
	got, _ := s.Get(p.ID)
	got.Automation = Automation{Trigger: TriggerWatch, IntervalSeconds: 60}
	if _, err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	if err := sch.CheckNow(context.Background(), p.ID); domain.ErrorCode(err) != "AUTOMATION_UNAUTHORIZED" {
		t.Fatalf("disabled job was checked: %v", err)
	}
}

func TestSchedulerLoopHonorsGlobalPause(t *testing.T) {
	s := newTestStore(t)
	p := authorized(t, s, TriggerInterval)
	if _, err := s.SaveSettings(Settings{DefaultConflictPolicy: "keep-both", DefaultMaxRetries: 6, AutomationPaused: true}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{outcome: Outcome{}}
	sch := NewScheduler(s, runner, nil)
	sch.Start()
	time.Sleep(150 * time.Millisecond)
	sch.Stop()
	if runner.runs != 0 {
		t.Fatal("globally paused automation ran")
	}
	if _, err := s.SaveSettings(DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	sch.Start()
	deadline := time.Now().Add(2 * time.Second)
	for {
		runner.mu.Lock()
		runs := runner.runs
		runner.mu.Unlock()
		if runs > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("due job never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sch.Stop()
	_ = p
}
