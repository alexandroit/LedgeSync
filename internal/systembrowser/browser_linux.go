//go:build linux

package systembrowser

import (
	"context"
	"os/exec"
)

func launch(ctx context.Context, raw string) error {
	executable, err := exec.LookPath("xdg-open")
	if err != nil {
		return ErrLaunch
	}
	return command(ctx, executable, raw)
}
