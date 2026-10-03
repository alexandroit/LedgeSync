package desktop

import (
	"context"
	"sort"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/syncer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// SyncService is the two-way sync manager used by the desktop.
type SyncService interface {
	List() []syncer.Status
	Get(id string) (syncer.Status, error)
	Add(ctx context.Context, localRoot string, parent syncer.Destination) (syncer.Status, error)
	Pause(id string) error
	Resume(id string) error
	SyncNow(id string) error
	ConfirmDeletes(id string) error
	RestoreDeletes(id string) error
	Remove(id string) error
	Activity(id string, limit int) ([]transferstate.SyncActivity, error)
	AccountChanged()
	Start()
	Stop()
}

// FolderLister lists Drive folders for the in-app location browser.
type FolderLister interface {
	ListFolders(ctx context.Context, account, parent, pageToken string) (drive.FolderPage, error)
}

// SyncOptions connects two-way sync to the desktop.
type SyncOptions struct {
	Service      SyncService
	Folders      FolderLister
	FolderPicker Picker
	OpenLocal    func(path string) error
}

var errSyncUnavailable = domain.Fail("SYNC_UNAVAILABLE", "Folder sync is unavailable in this build.")

// EnableSync adds two-way sync to an App built by NewDesktop.
func (a *App) EnableSync(o SyncOptions) {
	a.sync, a.syncFolders, a.syncPicker, a.openLocal = o.Service, o.Folders, o.FolderPicker, o.OpenLocal
}

// StartSync begins background syncing of saved folders.
func (a *App) StartSync() {
	if a.sync != nil {
		a.sync.Start()
	}
}

func (a *App) syncAccountChanged() {
	if a.sync != nil {
		a.sync.AccountChanged()
	}
}

// SyncList returns every synced folder with its live status.
func (a *App) SyncList() ([]syncer.Status, error) {
	if a.sync == nil {
		return []syncer.Status{}, nil
	}
	return a.sync.List(), nil
}

// SyncChooseFolder opens the native folder chooser and starts syncing the
// chosen folder with a folder of the same name inside parentID ("" or "root"
// for My Drive). Cancelling the chooser returns an empty status.
func (a *App) SyncChooseFolder(parentID, parentName string) (syncer.Status, error) {
	if a.sync == nil || a.syncPicker == nil {
		return syncer.Status{}, errSyncUnavailable
	}
	selected, err := a.syncPicker()
	if err != nil {
		return syncer.Status{}, connections.PublicError(err)
	}
	if selected == "" {
		return syncer.Status{Issues: []syncer.Issue{}}, nil
	}
	ctx := a.connectionContext
	if ctx == nil {
		ctx = context.Background()
	}
	status, err := a.sync.Add(ctx, selected, syncer.Destination{FolderID: parentID, Name: parentName})
	return status, connections.PublicError(err)
}

func (a *App) syncAction(id string, fn func(SyncService, string) error) (syncer.Status, error) {
	if a.sync == nil {
		return syncer.Status{}, errSyncUnavailable
	}
	if err := fn(a.sync, id); err != nil {
		return syncer.Status{}, connections.PublicError(err)
	}
	status, err := a.sync.Get(id)
	return status, connections.PublicError(err)
}

func (a *App) SyncPause(id string) (syncer.Status, error) {
	return a.syncAction(id, SyncService.Pause)
}
func (a *App) SyncResume(id string) (syncer.Status, error) {
	return a.syncAction(id, SyncService.Resume)
}
func (a *App) SyncNow(id string) (syncer.Status, error) {
	return a.syncAction(id, SyncService.SyncNow)
}
func (a *App) SyncConfirmDeletes(id string) (syncer.Status, error) {
	return a.syncAction(id, SyncService.ConfirmDeletes)
}
func (a *App) SyncRestoreDeletes(id string) (syncer.Status, error) {
	return a.syncAction(id, SyncService.RestoreDeletes)
}

// SyncRemove stops syncing a folder. No file is deleted on either side.
func (a *App) SyncRemove(id string) error {
	if a.sync == nil {
		return errSyncUnavailable
	}
	return connections.PublicError(a.sync.Remove(id))
}

// SyncActivity returns recent events of one folder, or of all folders.
func (a *App) SyncActivity(id string) ([]transferstate.SyncActivity, error) {
	if a.sync == nil {
		return []transferstate.SyncActivity{}, nil
	}
	list, err := a.sync.Activity(id, 200)
	return list, connections.PublicError(err)
}

// SyncOpenLocal shows the synced folder in the file manager.
func (a *App) SyncOpenLocal(id string) error {
	if a.sync == nil || a.openLocal == nil {
		return errSyncUnavailable
	}
	status, err := a.sync.Get(id)
	if err != nil {
		return connections.PublicError(err)
	}
	return connections.PublicError(a.openLocal(status.Pair.LocalRoot))
}

// SyncOpenDrive opens the synced Drive folder in the browser.
func (a *App) SyncOpenDrive(id string) error {
	if a.sync == nil || a.openDriveFolder == nil {
		return errSyncUnavailable
	}
	status, err := a.sync.Get(id)
	if err != nil {
		return connections.PublicError(err)
	}
	return connections.PublicError(a.openDriveFolder(status.DriveURL))
}

// DriveFolders lists the folders inside parentID ("" for My Drive), sorted by
// name, for choosing where a synced folder lives in Drive.
func (a *App) DriveFolders(parentID string) ([]drive.Folder, error) {
	if a.syncFolders == nil || a.google == nil {
		return nil, errSyncUnavailable
	}
	if parentID == "" {
		parentID = "root"
	}
	ctx := a.connectionContext
	if ctx == nil {
		ctx = context.Background()
	}
	status, err := a.google.Status(ctx)
	if err != nil || status.State != "connected" || status.Account == nil {
		return nil, domain.Fail("AUTH_REQUIRED", "Connect Google Drive first.")
	}
	folders := []drive.Folder{}
	token := ""
	for page := 0; page < 20; page++ {
		result, err := a.syncFolders.ListFolders(ctx, status.Account.Reference, parentID, token)
		if err != nil {
			return nil, connections.PublicError(err)
		}
		folders = append(folders, result.Folders...)
		if result.NextPageToken == "" {
			break
		}
		token = result.NextPageToken
	}
	sort.Slice(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	return folders, nil
}
