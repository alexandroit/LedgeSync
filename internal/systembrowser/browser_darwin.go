//go:build darwin

package systembrowser

import "context"

func launch(ctx context.Context, raw string) error { return command(ctx, "/usr/bin/open", raw) }
