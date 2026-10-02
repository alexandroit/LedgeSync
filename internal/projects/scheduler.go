package projects

import (
	"context"
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

// Outcome is the result of one automatic check.
type Outcome struct {
	// Ran reports whether an authorized transfer was executed.
	Ran bool
	// Summary describes the executed transfer.
	Summary RunSummary
	// Fingerprint is the local selection fingerprint observed by the check.
	Fingerprint string
}

// Runner performs automatic checks through the shared services.
type Runner interface {
	// LocalFingerprint scans the source with the project's policy, without Drive.
	LocalFingerprint(ctx context.Context, p Project) (string, error)
	// RunAuthorized previews the project and executes it only when the preview
	// matches the project's authorization and contains only permitted work.
	RunAuthorized(ctx context.Context, p Project) (Outcome, error)
}

// pauseCodes require the user's review; the job never resumes by itself.
var pauseCodes = map[string]bool{
	"AUTH_REQUIRED": true, "ACCOUNT_CHANGED": true, "AUTH_CLIENT_CHANGED": true, "AUTH_IDENTITY_CHANGED": true,
	"AUTH_SCOPE_REQUIRED": true, "AUTH_STORAGE_UNAVAILABLE": true, "AUTH_CONFIGURATION_REQUIRED": true,
	"DESTINATION_CHANGED": true, "DESTINATION_UNAVAILABLE": true, "DESTINATION_READ_ONLY": true, "DRIVE_NOT_FOLDER": true,
	"RULES_CHANGED": true, "CONFIG_CHANGED": true, "CONFIG_INVALID": true, "RULE_SOURCE_UNAVAILABLE": true, "RULE_PARSE_ERROR": true,
	"AUTOMATION_REVIEW_REQUIRED": true, "AUTOMATION_UNAUTHORIZED": true, "STATE_INVALID": true, "CAPABILITY_UNSUPPORTED": true,
	"REMOTE_CHANGED": true, "UNKNOWN_REMOTE_RESULT": true, "DRIVE_PERMISSION_DENIED": true, "DRIVE_STORAGE_FULL": true,
	"DRIVE_QUOTA": true, "DRIVE_UNSUPPORTED": true, "PLAN_INVALID": true, "DRIVE_VERIFICATION_FAILED": true,
	"DRIVE_IDENTITY_MISMATCH": true, "STATE_INSIDE_SOURCE": true, "NODE_UNSUPPORTED": true, "PATH_UNSAFE": true,
	"SOURCE_REPLACED": true, "OAUTH_UNAVAILABLE": true,
}

// Pausing reports whether an error requires the user's review.
func Pausing(err error) bool { return pauseCodes[domain.ErrorCode(err)] }

// Scheduler runs authorized automatic copies while the application is open.
// It never runs a transfer concurrently with another one, never enables itself
// and never broadens an authorization: any change pauses the job for review.
type Scheduler struct {
	store    *Store
	runner   Runner
	now      func() time.Time
	retry    time.Duration
	onChange func()
	mu       sync.Mutex
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	active   string
}

func NewScheduler(store *Store, runner Runner, onChange func()) *Scheduler {
	return &Scheduler{store: store, runner: runner, now: time.Now, retry: 5 * time.Minute, onChange: onChange, wake: make(chan struct{}, 1)}
}

// Start begins checking due projects until Stop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	go s.loop(ctx, s.done)
}

// Stop cancels any in-progress check and waits for it to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// Nudge re-evaluates schedules after a settings or project change.
func (s *Scheduler) Nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Active returns the ID of the project being checked, if any.
func (s *Scheduler) Active() string { s.mu.Lock(); defer s.mu.Unlock(); return s.active }

func (s *Scheduler) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

func due(p Project, now time.Time) (time.Time, bool) {
	a := p.Automation
	if !a.Enabled || a.Paused || a.Authorization == nil {
		return time.Time{}, false
	}
	if a.NextRunAt == "" {
		return now, true
	}
	next, err := time.Parse(time.RFC3339, a.NextRunAt)
	if err != nil {
		return now, true
	}
	return next, true
}

func (s *Scheduler) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait := time.Minute
		settings, err := s.store.Settings()
		list, listErr := s.store.List()
		if err == nil && listErr == nil && !settings.AutomationPaused {
			for _, p := range list {
				next, ok := due(p, s.now())
				if !ok {
					continue
				}
				if !next.After(s.now()) {
					s.check(ctx, p)
					if ctx.Err() != nil {
						return
					}
					wait = 0
					break
				}
				wait = min(wait, next.Sub(s.now()))
			}
		}
		if wait <= 0 {
			wait = 10 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// CheckNow runs one check of a project immediately, outside its schedule.
func (s *Scheduler) CheckNow(ctx context.Context, id string) error {
	p, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if !p.Automation.Enabled || p.Automation.Authorization == nil {
		return domain.Fail("AUTOMATION_UNAUTHORIZED", "Enable and authorize automatic copies for this sync pair first.")
	}
	s.check(ctx, p)
	return nil
}

func (s *Scheduler) check(ctx context.Context, p Project) {
	s.mu.Lock()
	s.active = p.ID
	s.mu.Unlock()
	s.changed()
	defer func() {
		s.mu.Lock()
		s.active = ""
		s.mu.Unlock()
		s.changed()
	}()
	interval := time.Duration(max(p.Automation.IntervalSeconds, MinimumInterval)) * time.Second
	update := func(fn func(*Automation)) {
		// Reload so a concurrent edit (for example disabling automation) wins.
		current, err := s.store.Get(p.ID)
		if err != nil || !current.Automation.Enabled || current.Automation.Authorization == nil || p.Automation.Authorization == nil || *current.Automation.Authorization != *p.Automation.Authorization {
			return
		}
		current.Automation.LastCheckedAt = s.now().UTC().Format(time.RFC3339)
		fn(&current.Automation)
		_, _ = s.store.Save(current)
	}
	if p.Automation.Trigger == TriggerWatch && p.Automation.LastFingerprint != "" {
		fingerprint, err := s.runner.LocalFingerprint(ctx, p)
		if err == nil && fingerprint == p.Automation.LastFingerprint {
			update(func(a *Automation) {
				a.Waiting = ""
				a.NextRunAt = s.now().Add(interval).UTC().Format(time.RFC3339)
			})
			return
		}
		if err != nil {
			s.settle(ctx, err, interval, update)
			return
		}
	}
	outcome, err := s.runner.RunAuthorized(ctx, p)
	if err != nil {
		s.settle(ctx, err, interval, update)
		return
	}
	update(func(a *Automation) {
		a.Waiting = ""
		if outcome.Fingerprint != "" {
			a.LastFingerprint = outcome.Fingerprint
		}
		a.NextRunAt = s.now().Add(interval).UTC().Format(time.RFC3339)
		if outcome.Ran && outcome.Summary.State != "succeeded" && outcome.Summary.State != "partial" {
			a.Paused, a.PauseCode, a.PauseReason = true, outcome.Summary.ErrorCode, "The last automatic copy did not complete: "+outcome.Summary.Message
		}
	})
}

// settle classifies a failed check: review-required errors pause the job,
// transient ones (offline, unmounted source, busy) retry later.
func (s *Scheduler) settle(ctx context.Context, err error, interval time.Duration, update func(func(*Automation))) {
	if ctx.Err() != nil {
		return
	}
	if Pausing(err) {
		update(func(a *Automation) {
			a.Paused, a.PauseCode, a.Waiting = true, domain.ErrorCode(err), ""
			a.PauseReason = safeReason(err)
		})
		return
	}
	update(func(a *Automation) {
		a.Waiting = safeReason(err)
		a.NextRunAt = s.now().Add(max(interval, s.retry)).UTC().Format(time.RFC3339)
	})
}

func safeReason(err error) string {
	if e, ok := err.(*domain.Error); ok {
		return e.Message
	}
	return "The automatic check could not complete. It will be retried."
}
