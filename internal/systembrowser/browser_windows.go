//go:build windows

package systembrowser

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
)

// ShellExecute is an OS call, not a cancellable child process. At most one call
// may remain pending after our deadline; later attempts fail instead of creating
// unbounded goroutines. Its result never exposes native error strings.
var nativeLaunchSlot = make(chan struct{}, 1)

func launch(ctx context.Context, raw string) error {
	select {
	case nativeLaunchSlot <- struct{}{}:
	default:
		return ErrLaunch
	}
	result := make(chan error, 1)
	go func() {
		defer func() { <-nativeLaunchSlot }()
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		initialized := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
		// S_FALSE (1) means COM was already initialized on this thread; it still
		// requires a matching CoUninitialize. Other failures remain redacted.
		if initialized != nil && !errors.Is(initialized, syscall.Errno(1)) {
			result <- ErrLaunch
			return
		}
		defer windows.CoUninitialize()
		target, err := windows.UTF16PtrFromString(raw)
		if err == nil {
			verb, _ := windows.UTF16PtrFromString("open")
			err = windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
		}
		result <- err
	}()
	select {
	case <-ctx.Done():
		return ErrLaunch
	case err := <-result:
		return err
	}
}
