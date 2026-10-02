package main

import (
	"context"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"golang.org/x/sys/windows"
)

func launchBrowser(ctx context.Context, target string) error {
	if ctx.Err() != nil {
		return driveauth.ErrCanceled
	}
	value, err := windows.UTF16PtrFromString(target)
	if err != nil || windows.ShellExecute(0, nil, value, nil, nil, windows.SW_SHOWNORMAL) != nil {
		return driveauth.ErrBrowser
	}
	return nil
}
