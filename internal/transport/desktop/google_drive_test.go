package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

type fakeGoogleDrive struct {
	status      driveauth.Status
	err         error
	imported    string
	inputBuffer []byte
	ctx         context.Context
	calls       []string
	cancelled   bool
}

func (g *fakeGoogleDrive) result(ctx context.Context, method string) (driveauth.Status, error) {
	g.ctx = ctx
	g.calls = append(g.calls, method)
	return g.status, g.err
}
func (g *fakeGoogleDrive) Status(ctx context.Context) (driveauth.Status, error) {
	return g.result(ctx, "status")
}
func (g *fakeGoogleDrive) ConfigureClient(ctx context.Context, data []byte) (driveauth.Status, error) {
	g.imported = string(data)
	g.inputBuffer = data
	return g.result(ctx, "configure")
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
func (g *fakeGoogleDrive) Cancel() { g.cancelled = true }

func TestGoogleBridgeImportsOnlyNativeSelectionAndClearsBuffer(t *testing.T) {
	g := &fakeGoogleDrive{status: driveauth.Status{State: "disconnected", ClientConfigured: true}}
	path := filepath.Join(t.TempDir(), "client.json")
	fixture := `{"installed":{"fixture":"synthetic-only"}}`
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	b := NewWithGoogleDrive(nil, nil, nil, func() (string, error) { return path, nil }, g, nil)
	if len(g.calls) != 0 {
		t.Fatal("construction touched vault")
	}
	status, err := b.ImportGoogleOAuthClient()
	if err != nil || status == nil || status.State != "disconnected" || g.imported != fixture {
		t.Fatal("native selection did not reach core")
	}
	for _, value := range g.inputBuffer {
		if value != 0 {
			t.Fatal("client import buffer not cleared")
		}
	}
}

func TestGoogleBridgeCancelledOrFailedPickerDoesNotConfigure(t *testing.T) {
	for _, pickErr := range []error{nil, errors.New("private/path/secret")} {
		g := &fakeGoogleDrive{}
		b := NewWithGoogleDrive(nil, nil, nil, func() (string, error) { return "", pickErr }, g, nil)
		status, err := b.ImportGoogleOAuthClient()
		if status != nil || len(g.calls) != 0 {
			t.Fatal("cancelled picker changed connection")
		}
		if pickErr == nil && err != nil {
			t.Fatal(err)
		}
		if pickErr != nil && (err == nil || strings.Contains(err.Error(), "private")) {
			t.Fatal("picker failure not redacted")
		}
	}
}

func TestGoogleBridgeStorageStatusSurvivesWailsErrorContract(t *testing.T) {
	g := &fakeGoogleDrive{status: driveauth.Status{State: "storage_unavailable", Message: driveauth.ErrStorage.Error()}, err: driveauth.ErrStorage}
	b := NewWithGoogleDrive(nil, nil, nil, nil, g, nil)
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
	b := NewWithGoogleDrive(nil, nil, nil, nil, g, func() error { return errors.New("private browser details") })
	for _, action := range []func() (driveauth.Status, error){b.ConnectGoogleDrive, b.CheckGoogleDrive, b.DisconnectGoogleDrive} {
		if _, err := action(); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(g.calls, ",") != "connect,check,disconnect" {
		t.Fatal("wrong bridge routing")
	}
	if err := b.OpenGoogleOAuthSetup(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("browser error not redacted")
	}
	b.Shutdown()
	if !g.cancelled || !errors.Is(g.ctx.Err(), context.Canceled) {
		t.Fatal("shutdown did not cancel pending OAuth")
	}
}
