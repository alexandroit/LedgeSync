package desktop

import (
	"context"
	"errors"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

type googleDriveService interface {
	Status(context.Context) (driveauth.Status, error)
	ConfigureClient(context.Context, []byte) (driveauth.Status, error)
	Connect(context.Context) (driveauth.Status, error)
	Check(context.Context) (driveauth.Status, error)
	Disconnect(context.Context) (driveauth.Status, error)
	Cancel()
}

// NewWithGoogleDrive exposes narrow account operations, never raw tokens or URLs.
func NewWithGoogleDrive(service previewService, folderPicker, configPicker, clientPicker Picker,
	google googleDriveService, openSetup func() error) *App {
	a := New(service, folderPicker, configPicker)
	a.google = google
	a.clientPicker = clientPicker
	a.openGoogleSetup = openSetup
	a.connectionContext, a.closeConnections = context.WithCancel(context.Background())
	return a
}

var errGoogleUnavailable = errors.New("OAUTH_UNAVAILABLE: Google Drive authorization is unavailable in this build")

func (a *App) GoogleDriveStatus() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	status, err := a.google.Status(a.connectionContext)
	// Wails rejects an error result and drops its accompanying value. A locked
	// vault is an expected display state; preserve that safe DTO for recovery.
	if errors.Is(err, driveauth.ErrStorage) && status.State == "storage_unavailable" {
		return status, nil
	}
	return status, err
}
func (a *App) ImportGoogleOAuthClient() (*driveauth.Status, error) {
	if a.google == nil || a.clientPicker == nil {
		return nil, errGoogleUnavailable
	}
	path, err := a.clientPicker()
	if err != nil {
		return nil, errors.New("OAUTH_CLIENT_PICKER: the client configuration picker could not be opened")
	}
	if path == "" {
		return nil, nil
	}
	data, err := connections.ReadClientFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	status, err := a.google.ConfigureClient(a.connectionContext, data)
	if err != nil {
		return nil, err
	}
	return &status, nil
}
func (a *App) ConnectGoogleDrive() (driveauth.Status, error) {
	if a.google == nil {
		return driveauth.Status{}, errGoogleUnavailable
	}
	return a.google.Connect(a.connectionContext)
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
func (a *App) OpenGoogleOAuthSetup() error {
	if a.openGoogleSetup == nil {
		return errGoogleUnavailable
	}
	if err := a.openGoogleSetup(); err != nil {
		return errors.New("OAUTH_BROWSER: the Google Cloud setup page could not be opened in your browser")
	}
	return nil
}
func (a *App) Shutdown() {
	a.Cancel()
	if a.closeConnections != nil {
		a.closeConnections()
	}
	a.CancelGoogleDrive()
}
