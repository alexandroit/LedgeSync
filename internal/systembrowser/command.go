//go:build darwin || linux

package systembrowser

import (
	"context"
	"io"
	"os/exec"
	"time"
)

// command accepts a fixed/discovered OS launcher and typed arguments only. The
// user-controlled URL is one argument; it is never interpreted as shell source.
func command(ctx context.Context, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 250 * time.Millisecond
	return cmd.Run()
}
