package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transfer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
	"github.com/mattn/go-isatty"
)

// The CLI owns only terminal interaction. Authorization, approvals, execution,
// native storage, and recovery are the same services used by the desktop.
type onlineAccount interface {
	Status(context.Context) (driveauth.Status, error)
	Connect(context.Context) (driveauth.Status, error)
	Disconnect(context.Context) (driveauth.Status, error)
	ChooseFolder(context.Context, string) (driveauth.SelectedFolder, error)
	Cancel()
}
type onlineTransfer interface {
	SetDestination(context.Context, string, string) (*transfer.Destination, error)
	Preview(context.Context, string, bool) (transfer.Plan, error)
	Start(context.Context, string) (transfer.Status, error)
	Status() transfer.Status
	Busy() bool
	CancelAndWait()
	Invalidate()
}
type onlineServices struct {
	account     onlineAccount
	copy        onlineTransfer
	checkSource func(string, bool) error
}
type onlineFactory func(func(string) error) (onlineServices, error)

func newOnlineServices(openURL func(string) error) (onlineServices, error) {
	auth, err := connections.NewGoogleDrive(openURL)
	if err != nil {
		return onlineServices{}, driveauth.ErrBuildConfig
	}
	directory, err := transferstate.DefaultDirectory()
	if err != nil {
		return onlineServices{}, driveauth.ErrStorage
	}
	return onlineServices{account: auth, copy: transfer.New(app.NewService(), drive.New(auth), auth, directory), checkSource: connections.ValidateSourceSelection}, nil
}

func interactiveTerminal(streams ...any) bool {
	for _, stream := range streams {
		file, ok := stream.(*os.File)
		if !ok || !(isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())) {
			return false
		}
	}
	return true
}

func onlineError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, driveauth.ErrCanceled) || errors.Is(err, context.Canceled) {
		return domain.Fail("CANCELLED", "Operation canceled.")
	}
	if _, ok := err.(*domain.Error); ok {
		return err
	}
	for _, item := range []struct {
		err  error
		code string
	}{
		{driveauth.ErrBuildConfig, "OAUTH_UNAVAILABLE"},
		{driveauth.ErrStorage, "AUTH_STORAGE_UNAVAILABLE"},
		{driveauth.ErrBusy, "AUTH_BUSY"},
		{driveauth.ErrReconnect, "AUTH_REQUIRED"},
		{driveauth.ErrClientChanged, "AUTH_CLIENT_CHANGED"},
		{driveauth.ErrIdentity, "AUTH_IDENTITY_CHANGED"},
		{driveauth.ErrScope, "AUTH_SCOPE_REQUIRED"},
		{driveauth.ErrScopeNotGranted, "AUTH_SCOPE_NOT_GRANTED"},
		{driveauth.ErrScopeUnexpected, "AUTH_SCOPE_UNEXPECTED"},
		{driveauth.ErrNoFolderSelected, "DRIVE_FOLDER_NOT_SELECTED"},
		{driveauth.ErrDenied, "AUTH_DENIED"},
		{driveauth.ErrTimeout, "AUTH_TIMEOUT"},
		{driveauth.ErrConnected, "AUTH_CONNECTED"},
		{driveauth.ErrBrowser, "AUTH_BROWSER_UNAVAILABLE"},
	} {
		if errors.Is(err, item.err) {
			return domain.Fail(item.code, "%s", item.err)
		}
	}
	return domain.Fail("AUTH_UNAVAILABLE", "Operation failed. Check the connection and native credential vault, then try again.")
}

// Authorization URLs are shown only in an explicitly requested interactive
// terminal. Callback URLs/codes, OAuth tokens, and vault values are never output.
// Strict validation prevents a faulty launcher boundary from printing arbitrary
// endpoints, terminal controls, or credential-bearing URL parameters.
func terminalBrowser(out io.Writer) func(string) error {
	return func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "accounts.google.com" || u.Path != "/o/oauth2/v2/auth" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
			return driveauth.ErrBrowser
		}
		query, err := url.ParseQuery(u.RawQuery)
		allowed := map[string]bool{"client_id": true, "redirect_uri": true, "response_type": true, "scope": true, "state": true, "code_challenge": true, "code_challenge_method": true, "access_type": true, "prompt": true, "trigger_onepick": true, "allow_folder_selection": true, "mimetypes": true}
		if err != nil {
			return driveauth.ErrBrowser
		}
		for key, values := range query {
			if !allowed[key] || len(values) != 1 || strings.ContainsAny(values[0], "\r\n\x00\x1b") {
				return driveauth.ErrBrowser
			}
		}
		redirect, err := url.Parse(query.Get("redirect_uri"))
		if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Path != "/" || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.User != nil {
			return driveauth.ErrBrowser
		}
		port, err := strconv.Atoi(redirect.Port())
		if err != nil || port < 1 || port > 65535 || redirect.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) || query.Get("response_type") != "code" || query.Get("scope") != driveauth.Scope || query.Get("code_challenge_method") != "S256" || query.Get("state") == "" || query.Get("code_challenge") == "" {
			return driveauth.ErrBrowser
		}
		_, err = fmt.Fprintf(out, "Open this Google authorization page in your browser:\n%s\n\nIf LedgeSync is running on an SSH server, first run this command on your own computer in another terminal (replace USER@SERVER):\nssh -N -L 127.0.0.1:%d:127.0.0.1:%d USER@SERVER\nThen open the page on that computer. Keep both terminals open until authorization returns. The attempt expires after three minutes.\n\n", u.String(), port, port)
		if err != nil {
			return driveauth.ErrBrowser
		}
		return nil
	}
}

func browserAction(ctx context.Context, noBrowser bool, out io.Writer) func(string) error {
	if noBrowser {
		return terminalBrowser(out)
	}
	return func(raw string) error {
		if terminalBrowser(io.Discard)(raw) != nil {
			return driveauth.ErrBrowser
		}
		if _, err := fmt.Fprintln(out, "Opening Google authorization in your system browser. For an SSH server, cancel and run again with --no-browser."); err != nil {
			return driveauth.ErrBrowser
		}
		return launchBrowser(ctx, raw)
	}
}

func readConfirmation(ctx context.Context, in io.Reader, expected string) bool {
	if ctx.Err() != nil {
		return false
	}
	answer := make(chan bool, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(in, 1024)).ReadString('\n')
		answer <- err == nil && strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") == expected
	}()
	select {
	case <-ctx.Done():
		return false
	case confirmed := <-answer:
		return confirmed && ctx.Err() == nil
	}
}

func runOnline(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, interactive bool, create onlineFactory) int {
	if ctx.Err() != nil {
		return report(errOut, domain.Fail("CANCELLED", "Operation canceled."))
	}
	if !interactive {
		return report(errOut, domain.Fail("TERMINAL_REQUIRED", "Google authorization and manual copies require an interactive terminal for input and output. Piped approval and unattended execution are not supported."))
	}
	if len(args) >= 2 && args[0] == "auth" && (args[1] == "connect" || args[1] == "disconnect") {
		noBrowser := false
		if args[1] == "connect" {
			authFlags := flag.NewFlagSet("ledgesync auth connect", flag.ContinueOnError)
			authFlags.SetOutput(io.Discard)
			authFlags.BoolVar(&noBrowser, "no-browser", false, "display the Google page and SSH tunnel instructions")
			if authFlags.Parse(args[2:]) != nil || authFlags.NArg() != 0 {
				return report(errOut, domain.Fail("CONFIG_INVALID", "Use auth connect [--no-browser]."))
			}
		} else if len(args) != 2 {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use auth disconnect without additional arguments."))
		}
		services, err := create(browserAction(ctx, noBrowser, out))
		if err != nil || services.account == nil {
			return report(errOut, onlineError(ctx, err))
		}
		defer services.account.Cancel()
		var status driveauth.Status
		if args[1] == "connect" {
			status, err = services.account.Connect(ctx)
		} else {
			if _, err = fmt.Fprintln(out, "Disconnect this device from Google Drive? Completed Drive files remain. Type disconnect to remove this device's saved authorization:"); err != nil {
				return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The disconnect confirmation could not be displayed."))
			}
			if !readConfirmation(ctx, in, "disconnect") {
				return report(errOut, domain.Fail("CANCELLED", "Disconnect was not confirmed. Saved authorization was preserved."))
			}
			status, err = services.account.Disconnect(ctx)
		}
		if err != nil {
			return report(errOut, onlineError(ctx, err))
		}
		return report(errOut, output(out, status))
	}
	if len(args) == 0 || args[0] != "copy" {
		return report(errOut, domain.Fail("CONFIG_INVALID", "Use auth connect, auth disconnect, or copy. No remote revoke command is provided."))
	}
	flags := flag.NewFlagSet("ledgesync copy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "source directory")
	configuration := flags.String("config", "", "project configuration")
	destination := flags.String("destination", "", "root, picker, or an already authorized Drive folder ID")
	pick := flags.Bool("pick-destination", false, "choose an existing folder in Google's browser Picker")
	noBrowser := flags.Bool("no-browser", false, "display the Google page and SSH tunnel instructions")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || (*root == "") == (*configuration == "") || (*destination != "") == *pick {
		return report(errOut, domain.Fail("CONFIG_INVALID", "Choose exactly one of --root or --config, and exactly one of --destination root|picker|ID or --pick-destination. Other arguments and automatic approval are not supported."))
	}
	if *destination == "picker" {
		*pick = true
	}
	services, err := create(browserAction(ctx, *noBrowser, out))
	if err != nil || services.account == nil || services.copy == nil {
		return report(errOut, onlineError(ctx, err))
	}
	defer services.account.Cancel()
	defer services.copy.CancelAndWait()
	defer services.copy.Invalidate()
	source := *root
	if *configuration != "" {
		source = *configuration
	}
	if services.checkSource != nil {
		if err = services.checkSource(source, *configuration != ""); err != nil {
			return report(errOut, onlineError(ctx, err))
		}
	}
	status, err := services.account.Status(ctx)
	if err != nil {
		return report(errOut, onlineError(ctx, err))
	}
	if status.State != "connected" || status.Account == nil || status.Account.Reference == "" {
		return report(errOut, domain.Fail("AUTH_REQUIRED", "Run ledgesync auth connect in this user's session before copying."))
	}
	account := status.Account.Reference
	if *pick {
		selected, pickErr := services.account.ChooseFolder(ctx, account)
		if pickErr != nil {
			return report(errOut, onlineError(ctx, pickErr))
		}
		if selected.AccountReference != account {
			return report(errOut, onlineError(ctx, driveauth.ErrIdentity))
		}
		*destination = selected.ID
	}
	if _, err = services.copy.SetDestination(ctx, *destination, account); err != nil {
		return report(errOut, onlineError(ctx, err))
	}
	plan, err := services.copy.Preview(ctx, source, *configuration != "")
	if err != nil {
		return report(errOut, onlineError(ctx, err))
	}
	if err = output(out, plan); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The full preview could not be displayed. No upload was approved."))
	}
	if _, err = fmt.Fprintf(out, "Review every entry above. To upload this exact preview, type its full planDigest (%s), then press Enter:\n", plan.PlanDigest); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The approval prompt could not be displayed."))
	}
	if plan.PlanDigest == "" || !readConfirmation(ctx, in, plan.PlanDigest) {
		return report(errOut, domain.Fail("CANCELLED", "Upload was not approved. No Drive files were created."))
	}
	if _, err = services.copy.Start(ctx, plan.PlanDigest); err != nil {
		return report(errOut, onlineError(ctx, err))
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var previous transfer.Status
	for {
		current := services.copy.Status()
		finished := !services.copy.Busy()
		if finished {
			// Completion can occur between the status and busy snapshots.
			current = services.copy.Status()
		}
		if !reflect.DeepEqual(current, previous) {
			if err = output(out, current); err != nil {
				return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Progress output failed; upload was stopped. Preview again before continuing."))
			}
			previous = current
		}
		if finished {
			switch current.State {
			case "succeeded":
				return 0
			case "cancelled":
				return report(errOut, domain.Fail("CANCELLED", "Upload stopped. Completed copies remain in Drive."))
			case "partial":
				return report(errOut, domain.Fail("PARTIAL", "Some approved files changed after the preview and were not copied. Run copy again to copy their current versions."))
			case "needs_review":
				return report(errOut, domain.Fail("CONFLICT", "Upload needs review. Create a fresh preview before continuing."))
			default:
				return report(errOut, domain.Fail("TRANSFER_FAILED", "Upload did not complete. Review the status and create a fresh preview before continuing."))
			}
		}
		select {
		case <-ctx.Done():
			services.copy.CancelAndWait()
			return report(errOut, domain.Fail("CANCELLED", "Upload stopped. Completed copies remain in Drive; preview again to continue."))
		case <-ticker.C:
		}
	}
}
