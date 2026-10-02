//go:build desktop || bindings

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/alexandroit/LedgeSync/frontend"
	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
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
	var transfers *transfer.Service
	if configured, err := connections.NewGoogleDrive(systembrowser.OpenURL); err == nil {
		google = configured
		if stateDir, err := transferstate.DefaultDirectory(); err == nil {
			transfers = transfer.New(local, drive.New(configured), configured, stateDir)
		}
	}
	bridge := desktop.NewWithGoogleDriveAndTransfers(local,
		func() (string, error) {
			return runtime.OpenDirectoryDialog(getContext(), runtime.OpenDialogOptions{Title: "Choose a local source folder"})
		},
		func() (string, error) {
			return runtime.OpenFileDialog(getContext(), runtime.OpenDialogOptions{Title: "Open LedgeSync configuration", Filters: []runtime.FileFilter{{DisplayName: "LedgeSync JSON configuration", Pattern: "*.json"}}})
		},
		google,
		func() {
			if ctx := getContext(); ctx != nil && ctx.Err() == nil {
				runtime.WindowUnminimise(ctx)
				runtime.Show(ctx)
			}
		},
		transfers,
		func(destination string) error {
			ctx := getContext()
			if ctx == nil || ctx.Err() != nil {
				return context.Canceled
			}
			runtime.BrowserOpenURL(ctx, destination)
			return nil
		},
	)
	err = wails.Run(&options.App{
		Title: "LedgeSync", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 620,
		BackgroundColour: &options.RGBA{R: 242, G: 245, B: 250, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) { mu.Lock(); windowContext = ctx; mu.Unlock() },
		OnBeforeClose: func(ctx context.Context) bool {
			return preventClose(transfers != nil && transfers.Busy(), func() (string, error) {
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
