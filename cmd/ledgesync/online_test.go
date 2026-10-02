package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/transfer"
)

const previewDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type onlineFakeAccount struct {
	status                              driveauth.Status
	selected                            driveauth.SelectedFolder
	err                                 error
	reads, connects, disconnects, picks int
}

func (a *onlineFakeAccount) Status(context.Context) (driveauth.Status, error) { return a.status, a.err }
func (a *onlineFakeAccount) Connect(context.Context) (driveauth.Status, error) {
	a.connects++
	return a.status, a.err
}
func (a *onlineFakeAccount) Disconnect(context.Context) (driveauth.Status, error) {
	a.disconnects++
	return a.status, a.err
}
func (a *onlineFakeAccount) ChooseFolder(context.Context, string) (driveauth.SelectedFolder, error) {
	a.picks++
	return a.selected, a.err
}
func (*onlineFakeAccount) Cancel() {}

type onlineFakeCopy struct {
	plan                                 transfer.Plan
	state                                transfer.Status
	previewErr, startErr                 error
	root, destination, account, approved string
	isConfig                             bool
	starts, cancels, invalidations       int
	active                               bool
	started                              chan struct{}
	completeAtBusy                       bool
}

func (c *onlineFakeCopy) SetDestination(_ context.Context, id, account string) (*transfer.Destination, error) {
	c.destination = id
	c.account = account
	return &transfer.Destination{ID: id, AccountReference: account}, nil
}
func (c *onlineFakeCopy) Preview(_ context.Context, source string, isConfig bool) (transfer.Plan, error) {
	c.root = source
	c.isConfig = isConfig
	return c.plan, c.previewErr
}
func (c *onlineFakeCopy) Start(_ context.Context, digest string) (transfer.Status, error) {
	c.starts++
	c.approved = digest
	if c.started != nil {
		close(c.started)
	}
	if !c.active && !c.completeAtBusy {
		c.state = transfer.Status{State: "succeeded", PlanDigest: digest, CompletedFiles: 1}
	}
	return c.state, c.startErr
}
func (c *onlineFakeCopy) Status() transfer.Status { return c.state }
func (c *onlineFakeCopy) Busy() bool {
	if c.completeAtBusy {
		c.completeAtBusy = false
		c.state = transfer.Status{State: "succeeded", PlanDigest: previewDigest}
		return false
	}
	return c.active
}
func (c *onlineFakeCopy) CancelAndWait() { c.cancels++; c.active = false }
func (c *onlineFakeCopy) Invalidate()    { c.invalidations++ }

func onlineFixture() (*onlineFakeAccount, *onlineFakeCopy, onlineFactory) {
	a := &onlineFakeAccount{status: driveauth.Status{State: "connected", Account: &driveauth.Account{Reference: "drive_" + previewDigest}}}
	c := &onlineFakeCopy{plan: transfer.Plan{PlanDigest: previewDigest, SourceName: "fixture", DestinationName: "My Drive", AccountReference: a.status.Account.Reference, Entries: []transfer.Entry{{RelativePath: "note.txt", Kind: "file", Size: 7, Action: "upload"}}}, state: transfer.Status{State: "uploading"}}
	return a, c, func(func(string) error) (onlineServices, error) { return onlineServices{account: a, copy: c}, nil }
}

func TestOnlineRequiresTerminalAndValidArgumentsBeforeServices(t *testing.T) {
	cases := []struct {
		args        []string
		interactive bool
		code        int
	}{
		{[]string{"copy", "--root", "fixture", "--destination", "root"}, false, 6},
		{[]string{"auth", "connect"}, false, 6},
		{[]string{"auth", "disconnect"}, false, 6},
		{[]string{"copy", "--root", "fixture"}, true, 2},
		{[]string{"copy", "--root", "fixture", "--config", "project.json", "--destination", "root"}, true, 2},
		{[]string{"copy", "--root", "fixture", "--destination", "root", "--pick-destination"}, true, 2},
		{[]string{"copy", "--root", "fixture", "--destination", "root", "--yes"}, true, 2},
		{[]string{"auth", "connect", "secret-argument"}, true, 2},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		code := runOnline(context.Background(), tc.args, strings.NewReader(previewDigest+"\n"), &out, &errOut, tc.interactive, func(func(string) error) (onlineServices, error) {
			t.Fatal("invalid input created services")
			return onlineServices{}, nil
		})
		if code != tc.code || out.Len() != 0 || strings.Contains(errOut.String(), "secret-argument") {
			t.Fatalf("code=%d output=%s %s", code, &out, &errOut)
		}
	}
}

func TestCopyRequiresExactCurrentPreviewAndPreservesReadOnlyPreview(t *testing.T) {
	for _, answer := range []string{"yes\n", previewDigest + "changed\n", "\n", previewDigest, ""} {
		_, copy, create := onlineFixture()
		var out, errOut bytes.Buffer
		code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(answer), &out, &errOut, true, create)
		if code != 130 || copy.starts != 0 || copy.destination != "root" || copy.invalidations != 1 || !strings.Contains(out.String(), "note.txt") {
			t.Fatalf("unsafe approval path: %d %s", code, &errOut)
		}
	}
	_, copy, create := onlineFixture()
	var out, errOut bytes.Buffer
	code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create)
	if code != 0 || copy.starts != 1 || copy.approved != previewDigest || copy.root != "fixture" || copy.isConfig || !strings.Contains(out.String(), `"state": "succeeded"`) {
		t.Fatalf("approved copy failed: %d %s", code, &errOut)
	}
}

func TestCopyPickerBindingAndConfigUseSharedServices(t *testing.T) {
	for _, matching := range []bool{true, false} {
		a, c, create := onlineFixture()
		a.selected = driveauth.SelectedFolder{ID: "selected-parent", AccountReference: a.status.Account.Reference}
		if !matching {
			a.selected.AccountReference = "other-account"
		}
		var out, errOut bytes.Buffer
		code := runOnline(context.Background(), []string{"copy", "--config", "project.json", "--pick-destination"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create)
		if a.picks != 1 {
			t.Fatal("picker was not requested")
		}
		if matching && (code != 0 || c.destination != "selected-parent" || c.root != "project.json" || !c.isConfig || c.account != a.status.Account.Reference) {
			t.Fatalf("config/picker mismatch %d %s", code, &errOut)
		}
		if !matching && (code != 6 || c.starts != 0 || c.destination != "") {
			t.Fatal("different picker account accepted")
		}
	}
}

type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, errors.New("private-output-error") }

func TestCopyFailedPreviewOutputAndExpiredApprovalNeverStartMutation(t *testing.T) {
	for _, mode := range []string{"preview", "output", "expired", "disconnected", "vault"} {
		a, c, create := onlineFixture()
		var out, errOut bytes.Buffer
		var writer io.Writer = &out
		switch mode {
		case "preview":
			c.previewErr = domain.Fail("SOURCE_CHANGED", "Source changed.")
		case "output":
			writer = brokenOutput{}
		case "expired":
			c.startErr = domain.Fail("PLAN_EXPIRED", "Preview expired.")
		case "disconnected":
			a.status.State = "disconnected"
		case "vault":
			a.err = errors.New("synthetic-secret-marker")
		}
		code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(previewDigest+"\n"), writer, &errOut, true, create)
		if code == 0 || (mode != "expired" && c.starts != 0) || strings.Contains(errOut.String(), "synthetic-secret-marker") || strings.Contains(errOut.String(), "private-output-error") {
			t.Fatalf("unsafe %s result: %d %s", mode, code, &errOut)
		}
	}
}

func TestCopyDrainsOnInterruptAndHandlesCompletionBetweenSnapshots(t *testing.T) {
	_, c, create := onlineFixture()
	c.active = true
	c.started = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-c.started; cancel() }()
	var out, errOut bytes.Buffer
	if code := runOnline(ctx, []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create); code != 130 || c.cancels == 0 || c.active {
		t.Fatalf("interrupt did not drain: %d", code)
	}
	_, c, create = onlineFixture()
	c.completeAtBusy = true
	out.Reset()
	errOut.Reset()
	if code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create); code != 0 {
		t.Fatalf("terminal completion misreported: %d %s", code, &errOut)
	}
}

func TestAuthActionsRemainExplicitAndDisconnectRequiresVisibleConfirmation(t *testing.T) {
	for _, tc := range []struct {
		command, answer string
		broken          bool
		calls, code     int
	}{
		{"connect", "", false, 1, 0}, {"disconnect", "disconnect\n", false, 1, 0},
		{"disconnect", "yes\n", false, 0, 130}, {"disconnect", "disconnect\n", true, 0, 6},
	} {
		a, _, create := onlineFixture()
		var out, errOut bytes.Buffer
		var writer io.Writer = &out
		if tc.broken {
			writer = brokenOutput{}
		}
		code := runOnline(context.Background(), []string{"auth", tc.command}, strings.NewReader(tc.answer), writer, &errOut, true, create)
		if code != tc.code || a.connects+a.disconnects != tc.calls {
			t.Fatalf("%s result=%d %s", tc.command, code, &errOut)
		}
	}
}

func authorizationURL() string {
	q := url.Values{"client_id": {"fake.apps.googleusercontent.com"}, "redirect_uri": {"http://127.0.0.1:54321/"}, "response_type": {"code"}, "scope": {driveauth.Scope}, "state": {"synthetic-state"}, "code_challenge": {"synthetic-challenge"}, "code_challenge_method": {"S256"}}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}
func TestTerminalBrowserOnlyShowsGoogleAuthorizationAndLoopbackTunnel(t *testing.T) {
	var out bytes.Buffer
	if terminalBrowser(&out)(authorizationURL()) != nil || !strings.Contains(out.String(), "ssh -N -L 127.0.0.1:54321:127.0.0.1:54321 USER@SERVER") {
		t.Fatal("valid authorization instructions unavailable")
	}
	for _, bad := range []string{
		strings.Replace(authorizationURL(), "accounts.google.com", "evil.example", 1),
		authorizationURL() + "&access_token=synthetic-token", authorizationURL() + "&code=synthetic-code",
		authorizationURL() + "&state=duplicate", strings.Replace(authorizationURL(), "127.0.0.1", "0.0.0.0", 1),
		strings.Replace(authorizationURL(), "http%3A", "https%3A", 1), authorizationURL() + "#fragment",
	} {
		out.Reset()
		if terminalBrowser(&out)(bad) == nil || out.Len() != 0 {
			t.Fatal("unsafe authorization URL reached terminal")
		}
	}
}

func TestConfirmationCancellationDoesNotWaitForTerminalInput(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if readConfirmation(ctx, reader, previewDigest) || time.Since(start) > time.Second {
		t.Fatal("canceled confirmation blocked")
	}
}

func TestCopyChecksSourceBeforeAnyAccountLookup(t *testing.T) {
	a, c, _ := onlineFixture()
	create := func(func(string) error) (onlineServices, error) {
		return onlineServices{account: a, copy: c, checkSource: func(string, bool) error { return domain.Fail("STATE_INSIDE_SOURCE", "State is inside source.") }}, nil
	}
	var out, errOut bytes.Buffer
	code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "root"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create)
	if code != 6 || a.reads != 0 || c.starts != 0 || c.destination != "" || !strings.Contains(errOut.String(), "STATE_INSIDE_SOURCE") {
		t.Fatalf("source guard ignored: %d %s", code, &errOut)
	}
}

func TestDestinationPickerAliasAndManualBrowserFlag(t *testing.T) {
	a, c, create := onlineFixture()
	a.selected = driveauth.SelectedFolder{ID: "picked-parent", AccountReference: a.status.Account.Reference}
	var out, errOut bytes.Buffer
	code := runOnline(context.Background(), []string{"copy", "--root", "fixture", "--destination", "picker", "--no-browser"}, strings.NewReader(previewDigest+"\n"), &out, &errOut, true, create)
	if code != 0 || a.picks != 1 || c.destination != "picked-parent" {
		t.Fatalf("picker alias failed: %d %s", code, &errOut)
	}
	out.Reset()
	errOut.Reset()
	called := false
	create = func(openURL func(string) error) (onlineServices, error) {
		called = true
		if err := openURL(authorizationURL()); err != nil {
			t.Fatal(err)
		}
		return onlineServices{account: a, copy: c}, nil
	}
	code = runOnline(context.Background(), []string{"auth", "connect", "--no-browser"}, strings.NewReader(""), &out, &errOut, true, create)
	if code != 0 || !called || !strings.Contains(out.String(), "ssh -N -L") {
		t.Fatalf("manual browser flag failed: %d %s", code, &errOut)
	}
}
