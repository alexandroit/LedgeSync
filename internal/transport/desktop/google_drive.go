package desktop

import (
	"context"
	"errors"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// GoogleDriveService exposes account actions, never client configuration or tokens.
type GoogleDriveService interface {
	Status(context.Context) (driveauth.Status, error)
	Connect(context.Context) (driveauth.Status, error)
	Check(context.Context) (driveauth.Status, error)
	Disconnect(context.Context) (driveauth.Status, error)
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

var errGoogleUnavailable = errors.New("OAUTH_UNAVAILABLE: Google Drive authorization is unavailable in this build")

func (a *App) GoogleDriveStatus() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{State: "setup_required", Scope: driveauth.Scope, Message: "Google Drive connection is not configured in this build. Install an official LedgeSync release."}, nil
	}
	status, err := a.google.Status(a.connectionContext)
	// Wails rejects an error result and drops its accompanying value. A locked
	// vault is an expected display state; preserve that safe DTO for recovery.
	if errors.Is(err, driveauth.ErrStorage) && status.State == "storage_unavailable" {
		return status, nil
	}
	return status, err
}
func (a *App) ConnectGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	status, err := a.google.Connect(a.connectionContext)
	if err == nil && status.State == "connected" && a.connectionContext.Err() == nil && a.showAfterConnect != nil {
		a.showAfterConnect()
	}
	return status, err
}
func (a *App) CheckGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	return a.google.Check(a.connectionContext)
}
func (a *App) DisconnectGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	return a.google.Disconnect(a.connectionContext)
}
func (a *App) CancelGoogleDrive() {
	if a.google != nil {
		a.google.Cancel()
	}
}
func (a *App) Shutdown() {
	a.Cancel()
	if a.closeConnections != nil {
		a.closeConnections()
	}
	a.CancelGoogleDrive()
}
