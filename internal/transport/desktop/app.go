// Package desktop exposes shared preview, authorization and approved transfer services.
package desktop

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

type previewService interface {
	PreviewRoot(context.Context, string) (app.Preview, error)
	Preview(context.Context, string) (app.Preview, error)
	Scan(context.Context, config.Config) (app.Preview, error)
}

// Picker is supplied by the native shell; the browser cannot choose arbitrary roots.
type Picker func() (string, error)

// selection is the native-selected source: a folder with the default policy,
// a configuration file, or a saved sync pair's inline policy.
type selection struct {
	path     string
	isConfig bool
	inline   *config.Config
}

func (s selection) source() transfer.Source {
	switch {
	case s.inline != nil:
		c := *s.inline
		return transfer.Source{Root: s.path, Config: &c}
	case s.isConfig:
		return transfer.Source{ConfigPath: s.path}
	default:
		return transfer.Source{Root: s.path}
	}
}

// App binds read-only previews, explicit Google Drive authorization, approved
// transfers, saved sync pairs and opt-in automatic copies.
type App struct {
	transfer          transferService
	automation        transferService
	automatic         *transfer.Service
	projects          *projects.Store
	scheduler         *projects.Scheduler
	openDriveFolder   func(string) error
	google            GoogleDriveService
	showAfterConnect  func()
	connectionContext context.Context
	closeConnections  context.CancelFunc
	service           previewService
	folderPicker      Picker
	configPicker      Picker
	mu                sync.Mutex
	cancel            context.CancelFunc
	actionDone        chan struct{}
	lifecycle         bool
	automationBusy    bool
	selected          selection
	restoreProvider   restore.Provider
	restorePicker     Picker
	restoreRunner     *restore.Runner
	restoreCancel     context.CancelFunc
	restoreDone       chan struct{}
	projectID         string
	runProjectID      string
	lastPlan          *transfer.Plan
}

func New(service previewService, folderPicker, configPicker Picker) *App {
	return &App{service: service, folderPicker: folderPicker, configPicker: configPicker}
}

var errScanBusy = domain.Fail("SCAN_BUSY", "Wait for the current scan or cancel it.")
var errRootRequired = domain.Fail("ROOT_REQUIRED", "Choose a local folder first.")

func (a *App) begin() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lifecycle || a.automationBusy || (a.transfer != nil && a.transfer.Busy()) {
		return nil, errTransferBusy
	}
	if a.connectionContext != nil && a.connectionContext.Err() != nil {
		return nil, context.Canceled
	}
	if a.cancel != nil {
		return nil, errScanBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.actionDone = make(chan struct{})
	return ctx, nil
}

func (a *App) finish() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancel()
	a.cancel = nil
	close(a.actionDone)
	a.actionDone = nil
}

func (a *App) scanSelection(ctx context.Context, s selection) (app.Preview, error) {
	switch {
	case s.inline != nil:
		c := *s.inline
		return a.service.Scan(ctx, c)
	case s.isConfig:
		return a.service.Preview(ctx, s.path)
	default:
		return a.service.PreviewRoot(ctx, s.path)
	}
}

func (a *App) scan(ctx context.Context, s selection, projectID string) (*app.Preview, error) {
	result, err := a.scanSelection(ctx, s)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.selected = s
	a.projectID = projectID
	a.lastPlan = nil
	if a.transfer != nil {
		a.transfer.Invalidate()
	}
	a.mu.Unlock()
	return &result, nil
}

func (a *App) open(picker Picker, isConfig bool) (*app.Preview, error) {
	ctx, err := a.begin()
	if err != nil {
		return nil, connections.PublicError(err)
	}
	defer a.finish()
	if picker == nil {
		return nil, domain.Fail("PICKER_UNAVAILABLE", "The native source picker is unavailable.")
	}
	path, err := picker()
	if err != nil || path == "" {
		return nil, connections.PublicError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, connections.PublicError(err)
	}
	result, err := a.scan(ctx, a.defaultSelection(path, isConfig), "")
	return result, connections.PublicError(err)
}

// defaultSelection applies the saved defaults for new sync pairs to a newly
// chosen folder. A configuration file always keeps its own settings.
func (a *App) defaultSelection(path string, isConfig bool) selection {
	sel := selection{path: path, isConfig: isConfig}
	if isConfig || a.projects == nil {
		return sel
	}
	settings, err := a.projects.Settings()
	if err != nil || settings == projects.DefaultSettings() || settings.DefaultConflictPolicy == "" {
		return sel
	}
	policy := projects.DefaultPolicy()
	policy.ConflictPolicy, policy.MaxRetries = settings.DefaultConflictPolicy, settings.DefaultMaxRetries
	root, err := filepath.Abs(path)
	if err != nil {
		return sel
	}
	c, err := policy.Config(root)
	if err != nil {
		return sel
	}
	sel.path, sel.inline = root, &c
	return sel
}

func (a *App) OpenFolder() (*app.Preview, error)        { return a.open(a.folderPicker, false) }
func (a *App) OpenConfiguration() (*app.Preview, error) { return a.open(a.configPicker, true) }

func (a *App) Refresh() (*app.Preview, error) {
	ctx, err := a.begin()
	if err != nil {
		return nil, connections.PublicError(err)
	}
	defer a.finish()
	a.mu.Lock()
	s, project := a.selected, a.projectID
	a.mu.Unlock()
	if s.path == "" {
		return nil, errRootRequired
	}
	result, err := a.scan(ctx, s, project)
	return result, connections.PublicError(err)
}

func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}
