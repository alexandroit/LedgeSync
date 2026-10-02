//go:build !windows

package main

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"time"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

func launchBrowser(ctx context.Context, target string) error {
	program := "/usr/bin/xdg-open"
	if runtime.GOOS == "darwin" {
		program = "/usr/bin/open"
	} else if runtime.GOOS != "linux" {
		return driveauth.ErrBrowser
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, target)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.WaitDelay = time.Second
	if command.Run() != nil {
		return driveauth.ErrBrowser
	}
	return nil
}
