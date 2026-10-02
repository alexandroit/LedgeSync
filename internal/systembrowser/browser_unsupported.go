//go:build !darwin && !linux && !windows

package systembrowser

import "context"

func launch(context.Context, string) error { return ErrLaunch }
