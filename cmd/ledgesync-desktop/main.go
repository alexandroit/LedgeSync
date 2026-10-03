//go:build desktop || bindings

package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"

	"github.com/alexandroit/LedgeSync/frontend"
	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/syncer"
	"github.com/alexandroit/LedgeSync/internal/systembrowser"
	"github.com/alexandroit/LedgeSync/internal/transfer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
	"github.com/alexandroit/LedgeSync/internal/transport/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	assets, err := frontend.Assets()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var mu sync.RWMutex
	var windowContext context.Context
	getContext := func() context.Context { mu.RLock(); defer mu.RUnlock(); return windowContext }
	local := app.NewService()
	var google desktop.GoogleDriveService
	var transfers, automatic *transfer.Service
	var saved *projects.Store
	var restorer restore.Provider
	var syncManager *syncer.Manager
	var folders desktop.FolderLister
	notifySync := func() {
		if ctx := getContext(); ctx != nil && ctx.Err() == nil {
			runtime.EventsEmit(ctx, "ledgesync:sync")
		}
	}
	if configured, err := connections.NewGoogleDrive(systembrowser.OpenURL); err == nil {
		google = configured
		if stateDir, err := transferstate.DefaultDirectory(); err == nil {
			provider := drive.New(configured)
			restorer = provider
			transfers = transfer.New(local, provider, configured, stateDir)
			// Automatic runs use their own service instance so they never replace
			// the destination or approval the user is reviewing.
			automatic = transfer.New(local, provider, configured, stateDir)
			saved = projects.NewStore(stateDir)
			folders = provider
			if syncState, err := transferstate.OpenSyncState(stateDir); err == nil {
				syncManager, err = syncer.NewManager(syncState, provider, configured, syncer.Options{
					OnChange:  notifySync,
					Validate:  func(p string) error { return connections.ValidateSourceSelection(p, false) },
					Protected: []string{stateDir},
				})
				if err != nil {
					syncManager = nil
				}
			}
		}
	}
	notify := func() {
		if ctx := getContext(); ctx != nil && ctx.Err() == nil {
			runtime.EventsEmit(ctx, "ledgesync:automation")
		}
	}
	bridge := desktop.NewDesktop(desktop.Options{Preview: local,
		FolderPicker: func() (string, error) {
			selected, err := runtime.OpenDirectoryDialog(getContext(), runtime.OpenDialogOptions{Title: "Choose a local source folder"})
			if err == nil && selected != "" {
				err = connections.ValidateSourceSelection(selected, false)
			}
			return selected, err
		},
		ConfigPicker: func() (string, error) {
			selected, err := runtime.OpenFileDialog(getContext(), runtime.OpenDialogOptions{Title: "Open LedgeSync configuration", Filters: []runtime.FileFilter{{DisplayName: "LedgeSync JSON configuration", Pattern: "*.json"}}})
			if err == nil && selected != "" {
				err = connections.ValidateSourceSelection(selected, true)
			}
			return selected, err
		},
		Google: google,
		ShowAfterConnect: func() {
			if ctx := getContext(); ctx != nil && ctx.Err() == nil {
				runtime.WindowUnminimise(ctx)
				runtime.Show(ctx)
			}
		},
		Transfers: transfers,
		Automatic: automatic,
		Projects:  saved,
		OpenDriveFolder: func(destination string) error {
			ctx := getContext()
			if ctx == nil || ctx.Err() != nil {
				return context.Canceled
			}
			runtime.BrowserOpenURL(ctx, destination)
			return nil
		},
		OnChange:        notify,
		RestoreProvider: restorer,
		RestorePicker: func() (string, error) {
			return runtime.OpenDirectoryDialog(getContext(), runtime.OpenDialogOptions{Title: "Choose an empty folder for the restored copy", CanCreateDirectories: true})
		},
	})
	if syncManager != nil {
		bridge.EnableSync(desktop.SyncOptions{
			Service: syncManager,
			Folders: folders,
			FolderPicker: func() (string, error) {
				selected, err := runtime.OpenDirectoryDialog(getContext(), runtime.OpenDialogOptions{Title: "Choose a folder to sync with Google Drive", CanCreateDirectories: true})
				if err == nil && selected != "" {
					err = connections.ValidateSourceSelection(selected, false)
				}
				return selected, err
			},
			OpenLocal: func(path string) error {
				ctx := getContext()
				if ctx == nil || ctx.Err() != nil {
					return context.Canceled
				}
				runtime.BrowserOpenURL(ctx, (&url.URL{Scheme: "file", Path: path}).String())
				return nil
			},
		})
	}
	err = wails.Run(&options.App{
		Title: "LedgeSync", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 620,
		BackgroundColour: &options.RGBA{R: 242, G: 245, B: 250, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			mu.Lock()
			windowContext = ctx
			mu.Unlock()
			// Only sync pairs the user explicitly authorized are checked, and only
			// while LedgeSync is open. Installation never enables automation.
			bridge.StartAutomation()
			// Saved folders resume syncing as soon as LedgeSync opens.
			bridge.StartSync()
		},
		OnBeforeClose: func(ctx context.Context) bool {
			if syncManager != nil && syncActive(syncManager.List()) {
				choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
					Type: runtime.QuestionDialog, Title: "Keep syncing?",
					Message: "Folders sync only while LedgeSync is open. Minimize LedgeSync to keep syncing, or quit to stop until you open it again. A pass in progress stops safely.",
					Buttons: []string{"Minimize", "Quit"}, DefaultButton: "Minimize", CancelButton: "Minimize",
				})
				if err == nil && choice != "Quit" {
					runtime.WindowMinimise(ctx)
					return true
				}
			}
			return preventClose(transfers != nil && transfers.Busy() || automatic != nil && automatic.Busy(), func() (string, error) {
				return runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
					Type: runtime.QuestionDialog, Title: "Upload in progress",
					Message: "A Drive operation is still running. Stop it and close LedgeSync? Completed files stay in Drive; you can preview again to continue.",
					Buttons: []string{"Keep Open", "Stop and Close"}, DefaultButton: "Keep Open", CancelButton: "Keep Open",
				})
			}, bridge.Shutdown)
		},
		OnShutdown: func(context.Context) { bridge.Shutdown() },
		Bind:       []interface{}{bridge},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// syncActive reports whether any saved folder is still syncing (not paused).
func syncActive(list []syncer.Status) bool {
	for _, s := range list {
		if !s.Pair.Paused {
			return true
		}
	}
	return false
}
