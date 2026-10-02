package desktop

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

type fakeGoogleDrive struct {
	status                   driveauth.Status
	err                      error
	ctx                      context.Context
	calls                    []string
	cancelled                bool
	confirmed                bool
	expectedAccountReference string
}

func (g *fakeGoogleDrive) result(ctx context.Context, method string) (driveauth.Status, error) {
	g.ctx = ctx
	g.calls = append(g.calls, method)
	return g.status, g.err
}
func (g *fakeGoogleDrive) Status(ctx context.Context) (driveauth.Status, error) {
	return g.result(ctx, "status")
}
func (g *fakeGoogleDrive) Connect(ctx context.Context) (driveauth.Status, error) {
	return g.result(ctx, "connect")
}
func (g *fakeGoogleDrive) Check(ctx context.Context) (driveauth.Status, error) {
	return g.result(ctx, "check")
}
func (g *fakeGoogleDrive) Disconnect(ctx context.Context) (driveauth.Status, error) {
	return g.result(ctx, "disconnect")
}
func (g *fakeGoogleDrive) Revoke(ctx context.Context, expectedAccountReference string, confirmed bool) (driveauth.Status, error) {
	g.confirmed = confirmed
	g.expectedAccountReference = expectedAccountReference
	return g.result(ctx, "revoke")
}
func (g *fakeGoogleDrive) Cancel() { g.cancelled = true }

func TestGoogleBridgeOffersNoClientImport(t *testing.T) {
	typ := reflect.TypeOf(&App{})
	for _, name := range []string{"ImportGoogleOAuthClient", "OpenGoogleOAuthSetup", "ConfigureClient"} {
		if _, ok := typ.MethodByName(name); ok {
			t.Fatal("client configuration exposed to frontend")
		}
	}
	b := NewWithGoogleDrive(nil, nil, nil, nil, nil)
	status, err := b.GoogleDriveStatus()
	if err != nil || status.State != "setup_required" || status.ClientConfigured {
		t.Fatal("unconfigured build not represented safely")
	}
	if _, err = b.ConnectGoogleDrive(); !errors.Is(err, errGoogleUnavailable) {
		t.Fatal("unconfigured build accepted connect")
	}
	if _, err = b.RevokeGoogleDrive("google-drive:fixture-account", true); !errors.Is(err, errGoogleUnavailable) {
		t.Fatal("unconfigured build accepted revocation")
	}
}

func TestGoogleBridgeRequiresExplicitRevocationConfirmation(t *testing.T) {
	g := &fakeGoogleDrive{status: driveauth.Status{State: "disconnected", Scope: driveauth.Scope}}
	b := NewWithGoogleDrive(nil, nil, nil, g, func() { t.Fatal("revocation activated the window") })
	if _, err := b.RevokeGoogleDrive("google-drive:fixture-account", false); !errors.Is(err, driveauth.ErrRevokeConfirmation) {
		t.Fatal("missing explicit confirmation was accepted")
	}
	if len(g.calls) != 0 || g.cancelled {
		t.Fatal("unconfirmed revocation reached the service")
	}
	status, err := b.RevokeGoogleDrive("google-drive:fixture-account", true)
	if err != nil || status.State != "disconnected" || !g.confirmed || g.expectedAccountReference != "google-drive:fixture-account" || strings.Join(g.calls, ",") != "revoke" {
		t.Fatal("confirmed revocation was not routed safely")
	}
	if g.ctx == nil || g.ctx.Err() != nil {
		t.Fatal("revocation did not receive the app lifecycle context")
	}
	b.Shutdown()
	if !errors.Is(g.ctx.Err(), context.Canceled) {
		t.Fatal("shutdown did not cancel revocation context")
	}
}

func TestGoogleBridgePreservesRevocationCleanupState(t *testing.T) {
	g := &fakeGoogleDrive{
		status: driveauth.Status{State: "revoked_local_cleanup_required", Scope: driveauth.Scope},
		err:    driveauth.ErrRevokedCleanup,
	}
	b := NewWithGoogleDrive(nil, nil, nil, g, nil)
	status, err := b.RevokeGoogleDrive("google-drive:fixture-account", true)
	if !errors.Is(err, driveauth.ErrRevokedCleanup) || status.State != "revoked_local_cleanup_required" {
		t.Fatal("revoked access with failed local cleanup was misrepresented")
	}
	// The frontend reconciles through this safe DTO after Wails drops a rejected
	// mutation result. A persisted cleanup state must not become Connected.
	g.err = nil
	status, err = b.GoogleDriveStatus()
	if err != nil || status.State != "revoked_local_cleanup_required" {
		t.Fatal("cleanup state was lost during status reconciliation")
	}
}

func TestGoogleBridgeShowsAppOnlyAfterSuccessfulConnection(t *testing.T) {
	for _, tc := range []struct {
		state string
		err   error
		want  int
	}{
		{"connected", nil, 1}, {"connected", driveauth.ErrStorage, 0},
		{"disconnected", nil, 0}, {"client_changed", driveauth.ErrClientChanged, 0},
	} {
		g := &fakeGoogleDrive{status: driveauth.Status{State: tc.state}, err: tc.err}
		shown := 0
		b := NewWithGoogleDrive(nil, nil, nil, g, func() { shown++ })
		if len(g.calls) != 0 {
			t.Fatal("construction touched vault")
		}
		b.ConnectGoogleDrive()
		if shown != tc.want {
			t.Fatal("wrong native window activation")
		}
	}
	g := &fakeGoogleDrive{status: driveauth.Status{State: "connected"}}
	b := NewWithGoogleDrive(nil, nil, nil, g, func() { t.Fatal("window shown after shutdown") })
	b.Shutdown()
	b.ConnectGoogleDrive()
}

func TestGoogleBridgeStorageStatusSurvivesWailsErrorContract(t *testing.T) {
	g := &fakeGoogleDrive{status: driveauth.Status{State: "storage_unavailable", Message: driveauth.ErrStorage.Error()}, err: driveauth.ErrStorage}
	b := NewWithGoogleDrive(nil, nil, nil, g, nil)
	status, err := b.GoogleDriveStatus()
	if err != nil || status.State != "storage_unavailable" {
		t.Fatal("expected vault recovery state was lost")
	}
	g.err = driveauth.ErrCanceled
	if _, err = b.GoogleDriveStatus(); !errors.Is(err, driveauth.ErrCanceled) {
		t.Fatal("unexpected error swallowed")
	}
}

func TestGoogleBridgeOperationsAndShutdown(t *testing.T) {
	g := &fakeGoogleDrive{status: driveauth.Status{State: "connected"}}
	b := NewWithGoogleDrive(nil, nil, nil, g, nil)
	for _, action := range []func() (driveauth.Status, error){b.ConnectGoogleDrive, b.CheckGoogleDrive, b.DisconnectGoogleDrive} {
		if _, err := action(); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(g.calls, ",") != "connect,check,disconnect" {
		t.Fatal("wrong bridge routing")
	}
	b.Shutdown()
	if !g.cancelled || !errors.Is(g.ctx.Err(), context.Canceled) {
		t.Fatal("shutdown did not cancel pending OAuth")
	}
}
