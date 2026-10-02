// Package systembrowser opens only LedgeSync's fixed Google authorization/setup
// destinations. It does not expose a generic URL, shell or file-opening service.
package systembrowser

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
)

const launchTimeout = 5 * time.Second
const SetupURL = "https://console.cloud.google.com/apis/library/drive.googleapis.com"

var ErrURL = errors.New("The browser destination is not an allowed Google authorization page.")
var ErrLaunch = errors.New("The system browser could not be opened. Check your default browser and try again.")

func allowed(raw string) bool {
	if len(raw) > 8192 || strings.Contains(raw, "#") {
		return false
	}
	if raw == SetupURL {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "accounts.google.com" && u.User == nil &&
		u.Path == "/o/oauth2/v2/auth" && u.RawPath == "" && u.Fragment == "" && u.RawFragment == "" &&
		u.RawQuery != "" && !u.OmitHost && u.Opaque == ""
}

// OpenURL returns within the launch deadline; raw URLs, subprocess output and OS
// errors are never logged or returned. Authorization itself has its own deadline.
func OpenURL(raw string) error {
	ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
	defer cancel()
	return open(ctx, raw, launch)
}
func open(ctx context.Context, raw string, launcher func(context.Context, string) error) error {
	if !allowed(raw) {
		return ErrURL
	}
	if ctx.Err() != nil {
		return ErrLaunch
	}
	if err := launcher(ctx, raw); err != nil {
		return ErrLaunch
	}
	return nil
}
