// Package desktop exposes shared preview, authorization and approved transfer services.
package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/alexandroit/LedgeSync/internal/app"
)

type previewService interface {
	PreviewRoot(context.Context, string) (app.Preview, error)
	Preview(context.Context, string) (app.Preview, error)
}

// Picker is supplied by the native shell; the browser cannot choose arbitrary roots.
type Picker func() (string, error)

// App binds read-only previews and explicit Google Drive account authorization.
type App struct {
	transfer          transferService
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
	path              string
	isConfig          bool
}

func New(service previewService, folderPicker, configPicker Picker) *App {
	return &App{service: service, folderPicker: folderPicker, configPicker: configPicker}
}

func (a *App) begin() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lifecycle || (a.transfer != nil && a.transfer.Busy()) {
		return nil, errTransferBusy
	}
	if a.connectionContext != nil && a.connectionContext.Err() != nil {
		return nil, context.Canceled
	}
	if a.cancel != nil {
		return nil, errors.New("SCAN_BUSY: wait for the current scan or cancel it")
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

func (a *App) scan(ctx context.Context, path string, isConfig bool) (*app.Preview, error) {
	var result app.Preview
	var err error
	if isConfig {
		result, err = a.service.Preview(ctx, path)
	} else {
		result, err = a.service.PreviewRoot(ctx, path)
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.path, a.isConfig = path, isConfig
	if a.transfer != nil {
		a.transfer.Invalidate()
	}
	a.mu.Unlock()
	return &result, nil
}

func (a *App) open(picker Picker, isConfig bool) (*app.Preview, error) {
	ctx, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer a.finish()
	if picker == nil {
		return nil, errors.New("PICKER_UNAVAILABLE: the native source picker is unavailable")
	}
	path, err := picker()
	if err != nil || path == "" {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.scan(ctx, path, isConfig)
}

func (a *App) OpenFolder() (*app.Preview, error)        { return a.open(a.folderPicker, false) }
func (a *App) OpenConfiguration() (*app.Preview, error) { return a.open(a.configPicker, true) }

func (a *App) Refresh() (*app.Preview, error) {
	ctx, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer a.finish()
	a.mu.Lock()
	path, isConfig := a.path, a.isConfig
	a.mu.Unlock()
	if path == "" {
		return nil, errors.New("ROOT_REQUIRED: choose a local folder first")
	}
	return a.scan(ctx, path, isConfig)
}

func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}
