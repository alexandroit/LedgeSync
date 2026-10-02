package desktop

import (
	"context"
	"errors"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

type transferService interface {
	SetDestination(context.Context, string, string) (*transfer.Destination, error)
	CurrentDestination() *transfer.Destination
	Preview(context.Context, string, bool) (transfer.Plan, error)
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

var errTransferUnavailable = errors.New("TRANSFER_UNAVAILABLE: Google Drive uploads are unavailable in this build")
var errTransferBusy = errors.New("TRANSFER_BUSY: wait for the current Drive operation or cancel it")

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
		return nil, err
	}
	defer a.finish()
	account, err := a.connectedAccount(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := chooser.ChooseFolder(ctx, account)
	if errors.Is(err, driveauth.ErrCanceled) || errors.Is(err, driveauth.ErrDenied) || errors.Is(err, context.Canceled) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if selected.AccountReference != account {
		return nil, driveauth.ErrIdentity
	}
	destination, err := a.transfer.SetDestination(ctx, selected.ID, account)
	if err == nil && a.showAfterConnect != nil && a.connectionContext.Err() == nil {
		a.showAfterConnect()
	}
	return destination, err
}

func (a *App) UseMyDrive() (*transfer.Destination, error) {
	if a.transfer == nil {
		return nil, errTransferUnavailable
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer a.finish()
	account, err := a.connectedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return a.transfer.SetDestination(ctx, "root", account)
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
		return transfer.Plan{}, err
	}
	defer a.finish()
	a.mu.Lock()
	source, isConfig := a.path, a.isConfig
	a.mu.Unlock()
	if source == "" {
		return transfer.Plan{}, errors.New("ROOT_REQUIRED: choose a local folder first")
	}
	return a.transfer.Preview(ctx, source, isConfig)
}

func (a *App) StartDriveUpload(planDigest string) (transfer.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.transfer == nil {
		return transfer.Status{}, errTransferUnavailable
	}
	if a.lifecycle || a.cancel != nil || a.transfer.Busy() {
		return transfer.Status{}, errTransferBusy
	}
	if a.connectionContext.Err() != nil {
		return transfer.Status{}, context.Canceled
	}
	return a.transfer.Start(a.connectionContext, planDigest)
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
	if status.State != "succeeded" || !validResultID(status.RemoteFolderID) {
		return errors.New("UPLOAD_RESULT_REQUIRED: a verified uploaded folder is not available")
	}
	if err := a.openDriveFolder("https://drive.google.com/drive/folders/" + status.RemoteFolderID); err != nil {
		return errors.New("BROWSER_UNAVAILABLE: the uploaded folder could not be opened in your browser")
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
}

// Reserve the desktop lifecycle boundary before draining. New previews, Picker
// actions and starts cannot race between cancellation and credential cleanup.
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
	if a.transfer != nil {
		a.transfer.CancelAndWait()
		a.transfer.Invalidate()
	}
	if pending != nil {
		<-pending
	}
	return func() { a.mu.Lock(); a.lifecycle = false; a.mu.Unlock() }, nil
}
