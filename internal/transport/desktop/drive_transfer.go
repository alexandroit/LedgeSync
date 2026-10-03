package desktop

import (
	"context"
	"errors"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

type transferService interface {
	SetDestination(context.Context, string, string) (*transfer.Destination, error)
	RestoreDestination(context.Context, transfer.Destination) (*transfer.Destination, error)
	CurrentDestination() *transfer.Destination
	Preview(context.Context, string, bool) (transfer.Plan, error)
	PreviewSource(context.Context, transfer.Source) (transfer.Plan, error)
	Start(context.Context, string) (transfer.Status, error)
	Status() transfer.Status
	Busy() bool
	Invalidate()
	Cancel()
	CancelAndWait()
}

type googleFolderPicker interface {
	ChooseFolder(context.Context, string) (driveauth.SelectedFolder, error)
}

var errTransferUnavailable = domain.Fail("TRANSFER_UNAVAILABLE", "Google Drive uploads are unavailable in this build.")
var errTransferBusy = domain.Fail("TRANSFER_BUSY", "Wait for the current Drive operation, or cancel it, before starting another.")

// Options wires the desktop bridge to shared services. Transfers and Automatic
// are separate service instances over the same provider, accounts and journal,
// so automatic runs never replace the destination or approval shown in the UI.
type Options struct {
	Preview          previewService
	FolderPicker     Picker
	ConfigPicker     Picker
	Google           GoogleDriveService
	ShowAfterConnect func()
	Transfers        *transfer.Service
	Automatic        *transfer.Service
	Projects         *projects.Store
	OpenDriveFolder  func(string) error
	OnChange         func()
	// RestoreProvider and RestorePicker enable restore-to-new-location.
	RestoreProvider restore.Provider
	RestorePicker   Picker
}

// NewDesktop connects all bridges. Missing optional services leave the
// corresponding features unavailable rather than partially enabled.
func NewDesktop(o Options) *App {
	a := NewWithGoogleDriveAndTransfers(o.Preview, o.FolderPicker, o.ConfigPicker, o.Google, o.ShowAfterConnect, o.Transfers, o.OpenDriveFolder)
	a.projects = o.Projects
	a.restoreProvider, a.restorePicker = o.RestoreProvider, o.RestorePicker
	if o.Transfers != nil && o.Projects != nil {
		o.Transfers.OnFinish(a.recordManualRun)
	}
	if o.Automatic != nil && o.Projects != nil && a.transfer != nil {
		a.automation, a.automatic = o.Automatic, o.Automatic
		a.scheduler = projects.NewScheduler(o.Projects, a.automationRunner(), o.OnChange)
	}
	return a
}

// NewWithGoogleDriveAndTransfers connects both transports to the same services.
// Existing constructors remain usable for offline and authorization-only builds.
func NewWithGoogleDriveAndTransfers(service previewService, folderPicker, configPicker Picker,
	google GoogleDriveService, showAfterConnect func(), transfers *transfer.Service, openDriveFolder func(string) error) *App {
	a := NewWithGoogleDrive(service, folderPicker, configPicker, google, showAfterConnect)
	if transfers != nil {
		a.transfer = transfers
	}
	a.openDriveFolder = openDriveFolder
	return a
}

// StartAutomation begins the in-app scheduler for authorized sync pairs.
func (a *App) StartAutomation() {
	if a.scheduler != nil {
		a.scheduler.Start()
	}
}

func (a *App) connectedAccount(ctx context.Context) (string, error) {
	if a.google == nil {
		return "", errGoogleUnavailable
	}
	status, err := a.google.Status(ctx)
	if err != nil {
		return "", err
	}
	if status.State != "connected" || status.Account == nil || status.Account.Reference == "" {
		return "", driveauth.ErrReconnect
	}
	return status.Account.Reference, nil
}

func (a *App) ChooseDriveDestination() (*transfer.Destination, error) {
	if a.transfer == nil {
		return nil, errTransferUnavailable
	}
	chooser, ok := a.google.(googleFolderPicker)
	if !ok {
		return nil, errGoogleUnavailable
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, connections.PublicError(err)
	}
	defer a.finish()
	account, err := a.connectedAccount(ctx)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	selected, err := chooser.ChooseFolder(ctx, account)
	if errors.Is(err, driveauth.ErrCanceled) || errors.Is(err, driveauth.ErrDenied) || errors.Is(err, context.Canceled) {
		return nil, nil
	}
	if err != nil {
		return nil, connections.PublicError(err)
	}
	if selected.AccountReference != account {
		return nil, connections.PublicError(driveauth.ErrIdentity)
	}
	destination, err := a.transfer.SetDestination(ctx, selected.ID, account)
	if err == nil {
		a.destinationChanged()
		if a.showAfterConnect != nil && a.connectionContext.Err() == nil {
			a.showAfterConnect()
		}
	}
	return destination, connections.PublicError(err)
}

func (a *App) UseMyDrive() (*transfer.Destination, error) {
	return a.UseDriveFolder(transfer.MyDriveID)
}

// UseDriveFolder selects a Drive folder chosen in the in-app folder browser
// ("" or "root" for My Drive). Full Drive access lists every folder, so no
// browser selection is needed. The folder is validated again before use.
func (a *App) UseDriveFolder(id string) (*transfer.Destination, error) {
	if a.transfer == nil {
		return nil, errTransferUnavailable
	}
	if id == "" {
		id = transfer.MyDriveID
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, connections.PublicError(err)
	}
	defer a.finish()
	account, err := a.connectedAccount(ctx)
	if err != nil {
		return nil, connections.PublicError(err)
	}
	destination, err := a.transfer.SetDestination(ctx, id, account)
	if err == nil {
		a.destinationChanged()
	}
	return destination, connections.PublicError(err)
}

// destinationChanged detaches a previously opened sync pair whose destination
// differs; the next approved run saves the new pair.
func (a *App) destinationChanged() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastPlan = nil
	a.projectID = ""
}

func (a *App) CurrentDriveDestination() (*transfer.Destination, error) {
	if a.transfer == nil {
		return nil, nil
	}
	destination := a.transfer.CurrentDestination()
	if destination == nil {
		return nil, nil
	}
	account, err := a.connectedAccount(a.connectionContext)
	if err != nil || account != destination.AccountReference {
		return nil, nil
	}
	return destination, nil
}

func (a *App) PreviewDriveUpload() (transfer.Plan, error) {
	if a.transfer == nil {
		return transfer.Plan{}, errTransferUnavailable
	}
	ctx, err := a.begin()
	if err != nil {
		return transfer.Plan{}, connections.PublicError(err)
	}
	defer a.finish()
	a.mu.Lock()
	s := a.selected
	a.mu.Unlock()
	if s.path == "" {
		return transfer.Plan{}, errRootRequired
	}
	var plan transfer.Plan
	if s.inline == nil {
		plan, err = a.transfer.Preview(ctx, s.path, s.isConfig)
	} else {
		plan, err = a.transfer.PreviewSource(ctx, s.source())
	}
	if err != nil {
		return transfer.Plan{}, connections.PublicError(err)
	}
	a.mu.Lock()
	p := plan
	a.lastPlan = &p
	a.mu.Unlock()
	return plan, nil
}

func (a *App) StartDriveUpload(planDigest string) (transfer.Status, error) {
	a.mu.Lock()
	if a.transfer == nil {
		a.mu.Unlock()
		return transfer.Status{}, errTransferUnavailable
	}
	if a.lifecycle || a.cancel != nil || a.automationBusy || a.transfer.Busy() {
		a.mu.Unlock()
		return transfer.Status{}, errTransferBusy
	}
	if a.connectionContext.Err() != nil {
		a.mu.Unlock()
		return transfer.Status{}, connections.PublicError(context.Canceled)
	}
	plan := a.lastPlan
	status, err := a.transfer.Start(a.connectionContext, planDigest)
	if err == nil && plan != nil && plan.PlanDigest == planDigest {
		a.runProjectID = a.ensureProjectLocked(*plan)
	}
	a.mu.Unlock()
	return status, connections.PublicError(err)
}

func (a *App) DriveTransferStatus() (transfer.Status, error) {
	if a.transfer == nil {
		return transfer.Status{State: "idle", Message: errTransferUnavailable.Error()}, nil
	}
	return a.transfer.Status(), nil
}

func (a *App) CancelDriveUpload() {
	if a.transfer != nil {
		a.transfer.CancelAndWait()
	}
}

// OpenUploadedDriveFolder opens only the verified result returned by the shared
// executor. The frontend cannot supply a URL, account token or arbitrary ID.
func (a *App) OpenUploadedDriveFolder() error {
	if a.transfer == nil || a.openDriveFolder == nil {
		return errTransferUnavailable
	}
	status := a.transfer.Status()
	if (status.State != "succeeded" && status.State != "partial") || !validResultID(status.RemoteFolderID) {
		return domain.Fail("UPLOAD_RESULT_REQUIRED", "A verified uploaded folder is not available.")
	}
	return a.openFolder(status.RemoteFolderID)
}

func (a *App) openFolder(id string) error {
	if a.openDriveFolder == nil || !validResultID(id) {
		return domain.Fail("UPLOAD_RESULT_REQUIRED", "A verified uploaded folder is not available.")
	}
	if err := a.openDriveFolder("https://drive.google.com/drive/folders/" + id); err != nil {
		return domain.Fail("BROWSER_UNAVAILABLE", "The uploaded folder could not be opened in your browser.")
	}
	return nil
}

func validResultID(id string) bool {
	if len(id) == 0 || len(id) > 256 || id == "root" {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (a *App) invalidateTransfer() {
	if a.transfer != nil {
		a.transfer.Invalidate()
	}
	a.mu.Lock()
	a.lastPlan = nil
	a.mu.Unlock()
}

// Reserve the desktop lifecycle boundary before draining. New previews, Picker
// actions, starts and automatic runs cannot race between cancellation and
// credential cleanup.
func (a *App) beginLifecycle() (func(), error) {
	a.mu.Lock()
	if a.lifecycle {
		a.mu.Unlock()
		return nil, errTransferBusy
	}
	a.lifecycle = true
	if a.cancel != nil {
		a.cancel()
	}
	pending := a.actionDone
	a.mu.Unlock()
	if a.google != nil {
		a.google.Cancel()
	}
	a.CancelRestore()
	if a.automation != nil {
		a.automation.CancelAndWait()
		a.automation.Invalidate()
	}
	if a.transfer != nil {
		a.transfer.CancelAndWait()
		a.transfer.Invalidate()
	}
	if pending != nil {
		<-pending
	}
	a.waitAutomationIdle()
	return func() { a.mu.Lock(); a.lifecycle = false; a.mu.Unlock() }, nil
}
