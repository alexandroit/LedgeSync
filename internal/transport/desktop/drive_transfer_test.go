package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

type fakeTransfer struct {
	mu                                       sync.Mutex
	busy                                     bool
	destination                              *transfer.Destination
	source                                   string
	config                                   bool
	digest                                   string
	ctx                                      context.Context
	result                                   *transfer.Status
	invalidated, drained, selections, starts int
}

func (f *fakeTransfer) SetDestination(_ context.Context, id, account string) (*transfer.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.selections++
	f.destination = &transfer.Destination{ID: id, Name: "Selected folder", AccountReference: account}
	d := *f.destination
	return &d, nil
}
func (f *fakeTransfer) CurrentDestination() *transfer.Destination {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.destination == nil {
		return nil
	}
	d := *f.destination
	return &d
}
func (f *fakeTransfer) Preview(_ context.Context, source string, isConfig bool) (transfer.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.source, f.config = source, isConfig
	return transfer.Plan{PlanDigest: "approved-digest"}, nil
}
func (f *fakeTransfer) Start(ctx context.Context, digest string) (transfer.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctx, f.digest, f.busy = ctx, digest, true
	f.starts++
	return transfer.Status{State: "uploading", PlanDigest: digest}, nil
}
func (f *fakeTransfer) Status() transfer.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.result != nil {
		return *f.result
	}
	state := "idle"
	if f.busy {
		state = "uploading"
	}
	return transfer.Status{State: state, PlanDigest: f.digest}
}

func TestUploadedFolderOpenerAcceptsOnlyVerifiedBackendResult(t *testing.T) {
	f := &fakeTransfer{}
	b := NewWithGoogleDrive(nil, nil, nil, connectedGoogle(), nil)
	b.transfer = f
	opened := ""
	b.openDriveFolder = func(destination string) error { opened = destination; return nil }
	for _, state := range []string{"idle", "uploading", "failed", "needs_review"} {
		f.result = &transfer.Status{State: state, RemoteFolderID: "valid-folder"}
		if err := b.OpenUploadedDriveFolder(); err == nil || opened != "" {
			t.Fatal("unverified result opened browser")
		}
	}
	for _, id := range []string{"", "root", "../other", "folder?query=bad", "https://example.test", strings.Repeat("a", 257)} {
		f.result = &transfer.Status{State: "succeeded", RemoteFolderID: id}
		if err := b.OpenUploadedDriveFolder(); err == nil || opened != "" {
			t.Fatal("invalid provider ID opened browser")
		}
	}
	f.result = &transfer.Status{State: "succeeded", RemoteFolderID: "valid-folder"}
	if err := b.OpenUploadedDriveFolder(); err != nil || opened != "https://drive.google.com/drive/folders/valid-folder" {
		t.Fatal("verified result did not open fixed Drive destination")
	}
	b.openDriveFolder = func(string) error { return errors.New("private launcher detail") }
	if err := b.OpenUploadedDriveFolder(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("launcher error was not redacted")
	}
}
func (f *fakeTransfer) Busy() bool     { f.mu.Lock(); defer f.mu.Unlock(); return f.busy }
func (f *fakeTransfer) Invalidate()    { f.mu.Lock(); defer f.mu.Unlock(); f.invalidated++ }
func (f *fakeTransfer) Cancel()        { f.CancelAndWait() }
func (f *fakeTransfer) CancelAndWait() { f.mu.Lock(); defer f.mu.Unlock(); f.busy = false; f.drained++ }

type selectingGoogle struct {
	fakeGoogleDrive
	selectFolder func(context.Context, string) (driveauth.SelectedFolder, error)
}

func (g *selectingGoogle) ChooseFolder(ctx context.Context, account string) (driveauth.SelectedFolder, error) {
	return g.selectFolder(ctx, account)
}

func connectedGoogle() *selectingGoogle {
	account := "drive_" + strings.Repeat("a", 64)
	g := &selectingGoogle{fakeGoogleDrive: fakeGoogleDrive{status: driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: account}}}}
	g.selectFolder = func(_ context.Context, expected string) (driveauth.SelectedFolder, error) {
		return driveauth.SelectedFolder{ID: "selected-id", AccountReference: expected}, nil
	}
	return g
}

func TestTransferBridgeNilConfiguredServiceStaysUnavailable(t *testing.T) {
	b := NewWithGoogleDriveAndTransfers(app.NewService(), nil, nil, nil, nil, nil, nil)
	if b.transfer != nil {
		t.Fatal("typed nil installed a transfer service")
	}
	if _, err := b.PreviewDriveUpload(); !errors.Is(err, errTransferUnavailable) {
		t.Fatal("unconfigured upload accepted")
	}
	if _, err := b.ChooseDriveDestination(); !errors.Is(err, errTransferUnavailable) {
		t.Fatal("unconfigured destination accepted")
	}
	if d, err := b.CurrentDriveDestination(); err != nil || d != nil {
		t.Fatal("unconfigured destination not empty")
	}
}

func TestTransferBridgePickerPreservesSelectionOnCancelAndAccountMismatch(t *testing.T) {
	g := connectedGoogle()
	f := &fakeTransfer{}
	shown := 0
	b := NewWithGoogleDrive(nil, nil, nil, g, func() { shown++ })
	b.transfer = f
	d, err := b.ChooseDriveDestination()
	if err != nil || d.ID != "selected-id" || d.AccountReference != g.status.Account.Reference || f.selections != 1 || shown != 1 {
		t.Fatal("selection was not bound to the current account")
	}
	for _, cancelErr := range []error{driveauth.ErrCanceled, driveauth.ErrDenied, context.Canceled} {
		g.selectFolder = func(context.Context, string) (driveauth.SelectedFolder, error) {
			return driveauth.SelectedFolder{}, cancelErr
		}
		if d, err := b.ChooseDriveDestination(); err != nil || d != nil || f.selections != 1 || f.destination.ID != "selected-id" {
			t.Fatal("canceled selection changed destination")
		}
	}
	g.selectFolder = func(context.Context, string) (driveauth.SelectedFolder, error) {
		return driveauth.SelectedFolder{ID: "wrong-id", AccountReference: "another-account"}, nil
	}
	if _, err = b.ChooseDriveDestination(); !errors.Is(err, driveauth.ErrIdentity) || f.selections != 1 {
		t.Fatal("Picker account mismatch accepted")
	}
	d, err = b.UseMyDrive()
	if err != nil || d.ID != "root" || d.AccountReference != g.status.Account.Reference {
		t.Fatal("My Drive was not routed to current account")
	}
	g.status.Account = &driveauth.Account{Reference: "another-account"}
	if d, err = b.CurrentDriveDestination(); err != nil || d != nil {
		t.Fatal("destination leaked across an account change")
	}
}

func TestTransferBridgeUsesNativeSourceAndExactApprovedDigest(t *testing.T) {
	root := t.TempDir()
	g := connectedGoogle()
	f := &fakeTransfer{}
	b := NewWithGoogleDrive(app.NewService(), func() (string, error) { return root, nil }, nil, g, nil)
	b.transfer = f
	if _, err := b.PreviewDriveUpload(); err == nil || !strings.Contains(err.Error(), "ROOT_REQUIRED") {
		t.Fatal("preview accepted arbitrary frontend path")
	}
	if _, err := b.OpenFolder(); err != nil {
		t.Fatal(err)
	}
	if f.invalidated != 1 {
		t.Fatal("source change retained stale upload approval")
	}
	plan, err := b.PreviewDriveUpload()
	if err != nil || f.source != root || f.config {
		t.Fatal("preview did not use native selected source")
	}
	status, err := b.StartDriveUpload(plan.PlanDigest)
	if err != nil || status.State != "uploading" || f.digest != plan.PlanDigest || f.ctx.Err() != nil {
		t.Fatal("start did not retain exact approval and lifecycle context")
	}
	b.CancelDriveUpload()
	if f.drained != 1 || f.Busy() {
		t.Fatal("upload cancel did not drain")
	}
	b.Shutdown()
	if f.ctx.Err() == nil {
		t.Fatal("shutdown left transfer context alive")
	}
}

func TestTransferBusyBlocksSourceAccountAndDestinationMutations(t *testing.T) {
	g := connectedGoogle()
	f := &fakeTransfer{busy: true}
	b := NewWithGoogleDrive(nil, func() (string, error) { t.Fatal("source picker opened while uploading"); return "", nil }, nil, g, nil)
	b.transfer = f
	checks := []func() error{
		func() error { _, e := b.OpenFolder(); return e }, func() error { _, e := b.OpenConfiguration(); return e }, func() error { _, e := b.Refresh(); return e },
		func() error { _, e := b.ConnectGoogleDrive(); return e }, func() error { _, e := b.CheckGoogleDrive(); return e }, func() error { _, e := b.ChooseDriveDestination(); return e },
		func() error { _, e := b.UseMyDrive(); return e }, func() error { _, e := b.PreviewDriveUpload(); return e }, func() error { _, e := b.StartDriveUpload("digest"); return e },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, errTransferBusy) {
			t.Fatalf("busy action accepted: %v", err)
		}
	}
	if len(g.calls) != 0 || f.selections != 0 || f.starts != 0 {
		t.Fatal("blocked action reached services")
	}
	if _, err := b.RevokeGoogleDrive(g.status.Account.Reference, false); !errors.Is(err, driveauth.ErrRevokeConfirmation) || f.drained != 0 || g.cancelled {
		t.Fatal("unconfirmed revocation canceled active work")
	}
}

type lifecycleGoogle struct {
	fakeGoogleDrive
	transfer *fakeTransfer
	t        *testing.T
}

func (g *lifecycleGoogle) checkDrained() {
	g.transfer.mu.Lock()
	defer g.transfer.mu.Unlock()
	if g.transfer.busy || g.transfer.drained == 0 || g.transfer.invalidated == 0 {
		g.t.Error("credentials changed before transfer drain and invalidation")
	}
}
func (g *lifecycleGoogle) Disconnect(ctx context.Context) (driveauth.Status, error) {
	g.checkDrained()
	return g.result(ctx, "disconnect")
}
func (g *lifecycleGoogle) Revoke(ctx context.Context, account string, confirmed bool) (driveauth.Status, error) {
	g.checkDrained()
	return g.fakeGoogleDrive.Revoke(ctx, account, confirmed)
}

func TestTransferDrainPrecedesDisconnectAndConfirmedRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		f := &fakeTransfer{busy: true}
		g := &lifecycleGoogle{transfer: f, t: t}
		g.status = driveauth.Status{State: "disconnected"}
		b := NewWithGoogleDrive(nil, nil, nil, g, nil)
		b.transfer = f
		var err error
		if revoke {
			_, err = b.RevokeGoogleDrive("drive_"+strings.Repeat("a", 64), true)
		} else {
			_, err = b.DisconnectGoogleDrive()
		}
		if err != nil || f.drained != 1 || f.busy {
			t.Fatal("lifecycle failed to drain upload")
		}
	}
}

func TestDestinationCancellationReachesNativePicker(t *testing.T) {
	g := connectedGoogle()
	f := &fakeTransfer{}
	started := make(chan struct{})
	g.selectFolder = func(ctx context.Context, _ string) (driveauth.SelectedFolder, error) {
		close(started)
		<-ctx.Done()
		return driveauth.SelectedFolder{}, driveauth.ErrCanceled
	}
	b := NewWithGoogleDrive(nil, nil, nil, g, nil)
	b.transfer = f
	done := make(chan error, 1)
	go func() { _, err := b.ChooseDriveDestination(); done <- err }()
	<-started
	if _, err := b.StartDriveUpload("old-plan"); !errors.Is(err, errTransferBusy) {
		t.Fatal("upload started during Picker")
	}
	b.CancelGoogleDrive()
	if err := <-done; err != nil || f.selections != 0 {
		t.Fatal("Picker cancellation did not preserve destination")
	}
}
