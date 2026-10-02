package desktop

import (
	"context"
	"errors"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// GoogleDriveService exposes account actions, never client configuration or tokens.
type GoogleDriveService interface {
	Status(context.Context) (driveauth.Status, error)
	Connect(context.Context) (driveauth.Status, error)
	Check(context.Context) (driveauth.Status, error)
	Disconnect(context.Context) (driveauth.Status, error)
	Revoke(context.Context, string, bool) (driveauth.Status, error)
	Cancel()
}

// NewWithGoogleDrive exposes narrow account operations, never raw tokens or URLs.
func NewWithGoogleDrive(service previewService, folderPicker, configPicker Picker,
	google GoogleDriveService, showAfterConnect func()) *App {
	a := New(service, folderPicker, configPicker)
	a.google = google
	a.showAfterConnect = showAfterConnect
	a.connectionContext, a.closeConnections = context.WithCancel(context.Background())
	return a
}

var errGoogleUnavailable = domain.Fail("OAUTH_UNAVAILABLE", "Google Drive authorization is unavailable in this build.")

func (a *App) GoogleDriveStatus() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{State: "setup_required", Scope: driveauth.Scope, Message: "Google Drive connection is not configured in this build. Install an official LedgeSync release."}, nil
	}
	status, err := a.google.Status(a.connectionContext)
	// Wails rejects an error result and drops its accompanying value. A locked
	// vault or another process owning authorization is an expected display state;
	// preserve the safe DTO rather than leaving stale connected controls enabled.
	if errors.Is(err, driveauth.ErrStorage) && status.State == "storage_unavailable" || errors.Is(err, driveauth.ErrBusy) && status.State == "busy" {
		return status, nil
	}
	return status, connections.PublicError(err)
}
func (a *App) ConnectGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	if _, err := a.begin(); err != nil {
		return driveauth.Status{}, connections.PublicError(err)
	}
	defer a.finish()
	status, err := a.google.Connect(a.connectionContext)
	if err == nil {
		a.invalidateTransfer()
	}
	if err == nil && status.State == "connected" && a.connectionContext.Err() == nil && a.showAfterConnect != nil {
		a.showAfterConnect()
	}
	return status, connections.PublicError(err)
}
func (a *App) CheckGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	if _, err := a.begin(); err != nil {
		return driveauth.Status{}, connections.PublicError(err)
	}
	defer a.finish()
	status, err := a.google.Check(a.connectionContext)
	if status.State != "connected" {
		a.invalidateTransfer()
	}
	return status, connections.PublicError(err)
}
func (a *App) DisconnectGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	done, err := a.beginLifecycle()
	if err != nil {
		return driveauth.Status{}, err
	}
	defer done()
	status, err := a.google.Disconnect(a.connectionContext)
	return status, connections.PublicError(err)
}
func (a *App) RevokeGoogleDrive(expectedAccountReference string, confirmed bool) (driveauth.Status, error) {
	// Requiring confirmation at both boundaries prevents an accidental binding
	// call from reaching any operation with effects on the shared Google project.
	if !confirmed {
		return driveauth.Status{}, driveauth.ErrRevokeConfirmation
	}
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	done, err := a.beginLifecycle()
	if err != nil {
		return driveauth.Status{}, err
	}
	defer done()
	status, err := a.google.Revoke(a.connectionContext, expectedAccountReference, true)
	return status, connections.PublicError(err)
}
func (a *App) CancelGoogleDrive() {
	a.Cancel()
	if a.google != nil {
		a.google.Cancel()
	}
}
func (a *App) Shutdown() {
	if a.scheduler != nil {
		a.scheduler.Stop()
	}
	a.mu.Lock()
	a.lifecycle = true
	if a.cancel != nil {
		a.cancel()
	}
	pending := a.actionDone
	a.mu.Unlock()
	if a.closeConnections != nil {
		a.closeConnections()
	}
	a.CancelGoogleDrive()
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
}
