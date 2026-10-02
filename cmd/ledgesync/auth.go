package main

import (
	"context"
	"errors"
	"io"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// This headless port deliberately has no mutation, browser or token methods.
// The GUI and CLI construct the same service with the same client/vault binding.
type authStatusService interface {
	Status(context.Context) (driveauth.Status, error)
}

func newAuthStatusService() (authStatusService, error) {
	return connections.NewGoogleDrive(nil)
}

func runAuthStatus(ctx context.Context, args []string, out, errOut io.Writer, create func() (authStatusService, error)) int {
	if len(args) != 1 || args[0] != "status" {
		return report(errOut, domain.Fail("AUTH_REQUIRED", "Use ledgesync auth status for local connection metadata. To connect, check, disconnect or revoke, open Connections in the desktop app."))
	}
	if ctx.Err() != nil {
		return report(errOut, domain.Fail("CANCELLED", "Connection status canceled."))
	}
	service, err := create()
	if err != nil || service == nil {
		return report(errOut, domain.Fail("OAUTH_UNAVAILABLE", "%s", driveauth.ErrBuildConfig))
	}
	status, err := service.Status(ctx)
	if err != nil {
		// Do not forward arbitrary implementation errors or accompanying metadata.
		if errors.Is(err, driveauth.ErrStorage) {
			return report(errOut, domain.Fail("AUTH_STORAGE_UNAVAILABLE", "%s", driveauth.ErrStorage))
		}
		if ctx.Err() != nil {
			return report(errOut, domain.Fail("CANCELLED", "Connection status canceled."))
		}
		return report(errOut, domain.Fail("AUTH_UNAVAILABLE", "Connection status could not be read."))
	}
	return report(errOut, output(out, struct {
		driveauth.Status
		OnlineVerified bool `json:"onlineVerified"`
	}{Status: status}))
}
