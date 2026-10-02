package desktop

import (
	"path/filepath"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

var errProjectsUnavailable = domain.Fail("PROJECTS_UNAVAILABLE", "Saved sync pairs are unavailable in this build.")

// ErrorInfo is a typed, redacted error inside a successful response.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func errorInfo(err error) *ErrorInfo {
	if err == nil {
		return nil
	}
	public := connections.PublicError(err)
	e, _ := public.(*domain.Error)
	return &ErrorInfo{Code: e.Code, Message: e.Message}
}

// ProjectSession is returned when a saved sync pair is opened.
type ProjectSession struct {
	Project          projects.Project      `json:"project"`
	Preview          *app.Preview          `json:"preview"`
	Destination      *transfer.Destination `json:"destination,omitempty"`
	DestinationError *ErrorInfo            `json:"destinationError,omitempty"`
}

// AutomationView reports the in-app scheduler and any automatic run.
type AutomationView struct {
	Available       bool             `json:"available"`
	Paused          bool             `json:"paused"`
	ActiveProjectID string           `json:"activeProjectId,omitempty"`
	Running         bool             `json:"running"`
	Transfer        *transfer.Status `json:"transfer,omitempty"`
}

func (a *App) store() (*projects.Store, error) {
	if a.projects == nil {
		return nil, errProjectsUnavailable
	}
	return a.projects, nil
}

func (a *App) ListProjects() ([]projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	list, err := s.List()
	return list, connections.PublicError(err)
}

// CurrentProject returns the saved pair that is open in the interface, if any.
func (a *App) CurrentProject() (*projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return nil, nil
	}
	a.mu.Lock()
	id := a.projectID
	a.mu.Unlock()
	if id == "" {
		return nil, nil
	}
	p, err := s.Get(id)
	if domain.ErrorCode(err) == "PROJECT_NOT_FOUND" {
		return nil, nil
	}
	if err != nil {
		return nil, connections.PublicError(err)
	}
	return &p, nil
}

func projectSelection(p projects.Project) (selection, error) {
	if p.ConfigPath != "" {
		return selection{path: p.ConfigPath, isConfig: true}, nil
	}
	if p.Policy.IsDefault() {
		return selection{path: p.SourceRoot}, nil
	}
	c, err := p.Policy.Config(p.SourceRoot)
	if err != nil {
		return selection{}, err
	}
	return selection{path: p.SourceRoot, inline: &c}, nil
}

// OpenProject selects a saved pair: it scans the local folder and validates the
// saved destination again. Nothing is approved; a fresh preview is required.
func (a *App) OpenProject(id string) (*ProjectSession, error) {
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, connections.PublicError(err)
	}
	defer a.finish()
	p, err := s.Get(id)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	sel, err := projectSelection(p)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	preview, err := a.scan(ctx, sel, p.ID)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	session := &ProjectSession{Project: p, Preview: preview}
	if a.transfer == nil {
		session.DestinationError = errorInfo(errTransferUnavailable)
		return session, nil
	}
	account, err := a.connectedAccount(ctx)
	switch {
	case err != nil:
		session.DestinationError = errorInfo(err)
	case account != p.Destination.AccountReference:
		session.DestinationError = errorInfo(domain.Fail("ACCOUNT_CHANGED", "This sync pair belongs to a different Google account. Connect that account or choose a new destination."))
	default:
		d, e := a.transfer.RestoreDestination(ctx, p.Destination)
		if e != nil {
			session.DestinationError = errorInfo(e)
		} else {
			session.Destination = d
		}
	}
	a.mu.Lock()
	a.projectID = p.ID
	a.mu.Unlock()
	return session, nil
}

// ensureProjectLocked saves the approved source and destination as a sync pair
// and returns its ID. Callers hold a.mu. Failures never block an approved run.
func (a *App) ensureProjectLocked(plan transfer.Plan) string {
	if a.projects == nil || a.transfer == nil {
		return ""
	}
	d := a.transfer.CurrentDestination()
	if d == nil || d.ID != plan.DestinationID || d.AccountReference != plan.AccountReference {
		return ""
	}
	remember := func(p projects.Project) string {
		if plan.SourceIdentity != "" && p.SourceIdentity != plan.SourceIdentity {
			p.SourceIdentity = plan.SourceIdentity
			if saved, err := a.projects.Save(p); err == nil {
				p = saved
			}
		}
		a.projectID = p.ID
		return p.ID
	}
	if a.projectID != "" {
		if p, err := a.projects.Get(a.projectID); err == nil && p.Destination.ID == d.ID && p.Destination.AccountReference == d.AccountReference {
			return remember(p)
		}
	}
	p, err := a.pairFromSelection(a.selected, *d, plan.SourceName)
	if err != nil {
		return ""
	}
	if existing, ok, err := a.projects.FindPair(p.SourceRoot, d.AccountReference, d.ID); err == nil && ok {
		return remember(existing)
	}
	p.SourceIdentity = plan.SourceIdentity
	saved, err := a.projects.Save(p)
	if err != nil {
		return ""
	}
	a.projectID = saved.ID
	return saved.ID
}

func (a *App) pairFromSelection(sel selection, d transfer.Destination, name string) (projects.Project, error) {
	p := projects.Project{Name: name, Destination: d, Policy: projects.DefaultPolicy()}
	switch {
	case sel.isConfig:
		c, err := config.Load(sel.path)
		if err != nil {
			return projects.Project{}, err
		}
		p.ConfigPath, p.SourceRoot = sel.path, c.Source.Root
	case sel.inline != nil:
		p.SourceRoot = sel.path
		p.Policy = projects.Policy{Composition: sel.inline.Filters.Composition, ConflictPolicy: sel.inline.Sync.ConflictPolicy, MaxRetries: sel.inline.Sync.MaxRetries, Groups: sel.inline.Filters.Groups}
	default:
		root, err := filepath.Abs(sel.path)
		if err != nil {
			return projects.Project{}, err
		}
		p.SourceRoot = root
	}
	if p.Name == "" {
		p.Name = filepath.Base(p.SourceRoot)
	}
	return p, nil
}

// SaveCurrentProject saves the open source and destination as a named pair.
func (a *App) SaveCurrentProject(name string) (projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return projects.Project{}, err
	}
	if a.transfer == nil {
		return projects.Project{}, errTransferUnavailable
	}
	a.mu.Lock()
	sel, id := a.selected, a.projectID
	a.mu.Unlock()
	if sel.path == "" {
		return projects.Project{}, errRootRequired
	}
	d := a.transfer.CurrentDestination()
	if d == nil {
		return projects.Project{}, domain.Fail("DESTINATION_REQUIRED", "Choose a Drive destination first.")
	}
	if id != "" {
		p, err := s.Get(id)
		if err == nil && p.Destination.ID == d.ID {
			p.Name = name
			saved, err := s.Save(p)
			return saved, connections.PublicError(err)
		}
	}
	p, err := a.pairFromSelection(sel, *d, name)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	if existing, ok, err := s.FindPair(p.SourceRoot, d.AccountReference, d.ID); err == nil && ok {
		p = existing
		p.Name = name
	}
	saved, err := s.Save(p)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	a.mu.Lock()
	a.projectID = saved.ID
	a.mu.Unlock()
	return saved, nil
}

func (a *App) RenameProject(id, name string) (projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return projects.Project{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	p.Name = name
	saved, err := s.Save(p)
	return saved, connections.PublicError(err)
}

// ForgetProject removes a saved pair from this computer. Drive copies, the
// transfer journal and the local folder are not changed.
func (a *App) ForgetProject(id string) error {
	s, err := a.store()
	if err != nil {
		return err
	}
	if a.scheduler != nil && a.scheduler.Active() == id {
		return errTransferBusy
	}
	if err = s.Delete(id); err != nil {
		return connections.PublicError(err)
	}
	a.mu.Lock()
	if a.projectID == id {
		a.projectID = ""
	}
	a.mu.Unlock()
	if a.scheduler != nil {
		a.scheduler.Nudge()
	}
	return nil
}

func (a *App) DefaultPolicy() projects.Policy { return projects.DefaultPolicy() }

// ProjectPolicyLimits reports the bounds enforced for automation and retries.
func (a *App) ProjectPolicyLimits() map[string]int {
	return map[string]int{"minimumInterval": projects.MinimumInterval, "maximumInterval": projects.MaximumInterval, "maxRetries": 20}
}

// UpdateProjectPolicy saves a validated policy. A changed policy invalidates any
// approval and automatic authorization; the open pair is scanned again.
func (a *App) UpdateProjectPolicy(id string, policy projects.Policy) (*ProjectSession, error) {
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	if err = policy.Validate(); err != nil {
		return nil, connections.PublicError(err)
	}
	p, err := s.Get(id)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	if p.ConfigPath != "" {
		return nil, domain.Fail("CONFIG_FILE_MANAGED", "This sync pair uses a configuration file. Edit that file to change its policy.")
	}
	p.Policy = policy
	if p.Automation.Enabled {
		p.Automation.Paused, p.Automation.PauseCode = true, "AUTOMATION_REVIEW_REQUIRED"
		p.Automation.PauseReason = "The policy changed. Preview this sync pair and authorize automatic copies again."
	}
	if _, err = s.Save(p); err != nil {
		return nil, connections.PublicError(err)
	}
	a.invalidateTransfer()
	a.mu.Lock()
	open := a.projectID == id
	a.mu.Unlock()
	if !open {
		return &ProjectSession{Project: p}, nil
	}
	return a.OpenProject(id)
}

func (a *App) ProjectHistory(id string) ([]projects.RunSummary, error) {
	s, err := a.store()
	if err != nil {
		return nil, err
	}
	list, err := s.History(id, 200)
	return list, connections.PublicError(err)
}

func (a *App) ClearHistory() error {
	s, err := a.store()
	if err != nil {
		return err
	}
	return connections.PublicError(s.ClearHistory())
}

func (a *App) GetSettings() (projects.Settings, error) {
	s, err := a.store()
	if err != nil {
		return projects.Settings{}, err
	}
	settings, err := s.Settings()
	return settings, connections.PublicError(err)
}

func (a *App) SaveSettings(v projects.Settings) (projects.Settings, error) {
	s, err := a.store()
	if err != nil {
		return projects.Settings{}, err
	}
	saved, err := s.SaveSettings(v)
	if err != nil {
		return projects.Settings{}, connections.PublicError(err)
	}
	if saved.AutomationPaused && a.automation != nil {
		a.automation.Cancel()
	}
	if a.scheduler != nil {
		a.scheduler.Nudge()
	}
	return saved, nil
}

// OpenProjectDriveFolder opens the managed folder of the pair's last verified run.
func (a *App) OpenProjectDriveFolder(id string) error {
	s, err := a.store()
	if err != nil {
		return err
	}
	p, err := s.Get(id)
	if err != nil {
		return connections.PublicError(err)
	}
	if p.LastRun == nil || (p.LastRun.State != "succeeded" && p.LastRun.State != "partial") {
		return domain.Fail("UPLOAD_RESULT_REQUIRED", "This sync pair has no verified copy yet.")
	}
	return a.openFolder(p.LastRun.RemoteFolderID)
}

// AuthorizeAutomation enables automatic copies for the open pair, bound to the
// exact preview the user reviewed: account, destination, source identity,
// configuration, rules and conflict policy. Only create-only work runs.
func (a *App) AuthorizeAutomation(id, trigger string, intervalSeconds int, planDigest string) (projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return projects.Project{}, err
	}
	if a.scheduler == nil {
		return projects.Project{}, domain.Fail("AUTOMATION_UNAVAILABLE", "Automatic copies are unavailable in this build.")
	}
	if err = projects.ValidateAutomation(trigger, intervalSeconds); err != nil {
		return projects.Project{}, err
	}
	a.mu.Lock()
	plan := a.lastPlan
	if plan != nil && plan.PlanDigest == planDigest && id == "" {
		id = a.ensureProjectLocked(*plan)
	}
	current := a.projectID
	a.mu.Unlock()
	if plan == nil || plan.PlanDigest != planDigest || id == "" || current != id {
		return projects.Project{}, domain.Fail("PLAN_REQUIRED", "Preview this sync pair, review it, then enable automatic copies.")
	}
	auth, err := projects.NewAuthorization(*plan, time.Now())
	if err != nil {
		return projects.Project{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	if p.Destination.ID != plan.DestinationID || p.Destination.AccountReference != plan.AccountReference {
		return projects.Project{}, domain.Fail("PLAN_REQUIRED", "This preview belongs to a different destination.")
	}
	p.Automation = projects.Automation{Enabled: true, Trigger: trigger, IntervalSeconds: intervalSeconds, NextRunAt: auth.ApprovedAt, Authorization: auth}
	saved, err := s.Save(p)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	a.scheduler.Nudge()
	return saved, nil
}

func (a *App) DisableAutomation(id string) (projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return projects.Project{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	if a.scheduler != nil && a.scheduler.Active() == id && a.automation != nil {
		a.automation.Cancel()
	}
	p.Automation = projects.Automation{Trigger: p.Automation.Trigger, IntervalSeconds: p.Automation.IntervalSeconds}
	saved, err := s.Save(p)
	if a.scheduler != nil {
		a.scheduler.Nudge()
	}
	return saved, connections.PublicError(err)
}

// ResumeAutomation clears a pause. A cause that persists pauses the job again.
func (a *App) ResumeAutomation(id string) (projects.Project, error) {
	s, err := a.store()
	if err != nil {
		return projects.Project{}, err
	}
	p, err := s.Get(id)
	if err != nil {
		return projects.Project{}, connections.PublicError(err)
	}
	if !p.Automation.Enabled || p.Automation.Authorization == nil {
		return projects.Project{}, domain.Fail("AUTOMATION_UNAUTHORIZED", "Enable and authorize automatic copies first.")
	}
	p.Automation.Paused, p.Automation.PauseCode, p.Automation.PauseReason, p.Automation.Waiting = false, "", "", ""
	p.Automation.NextRunAt = time.Now().UTC().Format(time.RFC3339)
	saved, err := s.Save(p)
	if a.scheduler != nil {
		a.scheduler.Nudge()
	}
	return saved, connections.PublicError(err)
}

// CheckProjectNow runs one authorized automatic check in the background.
func (a *App) CheckProjectNow(id string) error {
	if a.scheduler == nil {
		return domain.Fail("AUTOMATION_UNAVAILABLE", "Automatic copies are unavailable in this build.")
	}
	s, err := a.store()
	if err != nil {
		return err
	}
	p, err := s.Get(id)
	if err != nil {
		return connections.PublicError(err)
	}
	if !p.Automation.Enabled || p.Automation.Authorization == nil || p.Automation.Paused {
		return domain.Fail("AUTOMATION_UNAUTHORIZED", "This sync pair has no active automatic copy authorization.")
	}
	p.Automation.NextRunAt = time.Now().UTC().Format(time.RFC3339)
	if _, err = s.Save(p); err != nil {
		return connections.PublicError(err)
	}
	a.scheduler.Nudge()
	return nil
}

func (a *App) AutomationStatus() AutomationView {
	v := AutomationView{Available: a.scheduler != nil && a.projects != nil}
	if !v.Available {
		return v
	}
	if settings, err := a.projects.Settings(); err == nil {
		v.Paused = settings.AutomationPaused
	}
	v.ActiveProjectID = a.scheduler.Active()
	a.mu.Lock()
	v.Running = a.automationBusy
	a.mu.Unlock()
	if v.Running && a.automation != nil {
		st := a.automation.Status()
		v.Transfer = &st
	}
	return v
}

// CancelAutomaticCopy stops the automatic run in progress. Completed copies remain.
func (a *App) CancelAutomaticCopy() {
	if a.automation != nil {
		a.automation.Cancel()
	}
}

func (a *App) waitAutomationIdle() {
	for {
		a.mu.Lock()
		busy := a.automationBusy
		a.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// recordManualRun stores the outcome of an approved manual run in history.
func (a *App) recordManualRun(st transfer.Status) {
	a.mu.Lock()
	id := a.runProjectID
	a.runProjectID = ""
	a.mu.Unlock()
	if id == "" || a.projects == nil || st.RunID == "" {
		return
	}
	p, err := a.projects.Get(id)
	if err != nil {
		return
	}
	_ = a.projects.RecordRun(projects.Summarize(p, "manual", st))
}

// automationRunner adapts the shared runner to the desktop's exclusivity gate:
// manual actions and automatic runs never overlap.
func (a *App) automationRunner() projects.ServiceRunner {
	return projects.ServiceRunner{Local: a.service, Transfer: a.automatic, Accounts: a.google, Store: a.projects, Gate: a.beginAutomatic}
}

func (a *App) beginAutomatic() (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lifecycle || a.cancel != nil || a.automationBusy || (a.transfer != nil && a.transfer.Busy()) || a.automation.Busy() {
		return nil, errTransferBusy
	}
	a.automationBusy = true
	return func() { a.mu.Lock(); a.automationBusy = false; a.mu.Unlock() }, nil
}
