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
	"github.com/alexandroit/LedgeSync/internal/systembrowser"
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
	bridge := desktop.NewWithGoogleDrive(app.NewService(),
		func() (string, error) {
			return runtime.OpenDirectoryDialog(getContext(), runtime.OpenDialogOptions{Title: "Choose a local source folder"})
		},
		func() (string, error) {
			return runtime.OpenFileDialog(getContext(), runtime.OpenDialogOptions{Title: "Open LedgeSync configuration", Filters: []runtime.FileFilter{{DisplayName: "LedgeSync JSON configuration", Pattern: "*.json"}}})
		},
		func() (string, error) {
			return runtime.OpenFileDialog(getContext(), runtime.OpenDialogOptions{Title: "Import Google OAuth Desktop app configuration", Filters: []runtime.FileFilter{{DisplayName: "Google OAuth client JSON", Pattern: "*.json"}}})
		},
		connections.NewGoogleDrive(systembrowser.OpenURL),
		func() error {
			return systembrowser.OpenURL(systembrowser.SetupURL)
		},
	)
	err = wails.Run(&options.App{
		Title: "LedgeSync", Width: 1280, Height: 820, MinWidth: 900, MinHeight: 620,
		BackgroundColour: &options.RGBA{R: 242, G: 245, B: 250, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) { mu.Lock(); windowContext = ctx; mu.Unlock() },
		OnShutdown:       func(context.Context) { bridge.Shutdown() },
		Bind:             []interface{}{bridge},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
