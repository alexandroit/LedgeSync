package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/restore"
	"github.com/alexandroit/LedgeSync/internal/transfer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// pairServices are the shared services behind saved sync pairs. The CLI never
// implements its own planning, approval, automation or restore logic.
type pairServices struct {
	store    *projects.Store
	accounts projects.Accounts
	transfer *transfer.Service
	local    *app.Service
	restorer restore.Provider
	stateDir string
}

type pairFactory func() (pairServices, error)

func newPairServices() (pairServices, error) {
	auth, err := connections.NewGoogleDrive(func(string) error { return driveauth.ErrBrowser })
	if err != nil {
		return pairServices{}, driveauth.ErrBuildConfig
	}
	directory, err := transferstate.DefaultDirectory()
	if err != nil {
		return pairServices{}, driveauth.ErrStorage
	}
	local := app.NewService()
	provider := drive.New(auth)
	return pairServices{store: projects.NewStore(directory), accounts: auth, transfer: transfer.New(local, provider, auth, directory), local: local, restorer: provider, stateDir: directory}, nil
}

func (s pairServices) runner() projects.ServiceRunner {
	return projects.ServiceRunner{Local: s.local, Transfer: s.transfer, Accounts: s.accounts, Store: s.store}
}

func pairFlags(name string) (*flag.FlagSet, *string) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f, f.String("pair", "", "saved sync pair ID")
}

func connectedAccount(ctx context.Context, accounts projects.Accounts) (string, error) {
	status, err := accounts.Status(ctx)
	if err != nil {
		return "", err
	}
	if status.State != "connected" || status.Account == nil || status.Account.Reference == "" {
		return "", domain.Fail("AUTH_REQUIRED", "Run ledgesync auth connect in this user's session first.")
	}
	return status.Account.Reference, nil
}

// runPairs implements pairs, automatic and restore commands.
func runPairs(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, interactive bool, create pairFactory) int {
	fail := func(err error) int { return report(errOut, connections.PublicError(err)) }
	if len(args) < 2 && args[0] != "restore" {
		return report(errOut, domain.Fail("CONFIG_INVALID", "Use pairs list|add|remove, automatic enable|disable|run|watch, or restore."))
	}
	services, err := create()
	if err != nil {
		return fail(err)
	}
	switch {
	case args[0] == "pairs" && args[1] == "list":
		if len(args) != 2 {
			return report(errOut, domain.Fail("CONFIG_INVALID", "pairs list takes no arguments"))
		}
		list, err := services.store.List()
		if err != nil {
			return fail(err)
		}
		return report(errOut, output(out, list))
	case args[0] == "pairs" && args[1] == "add":
		f := flag.NewFlagSet("ledgesync pairs add", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		root := f.String("root", "", "local folder")
		destination := f.String("destination", "", "root or an authorized Drive folder ID")
		name := f.String("name", "", "pair name")
		if f.Parse(args[2:]) != nil || f.NArg() != 0 || *root == "" || *destination == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use pairs add --root DIRECTORY --destination root|FOLDER_ID [--name NAME]."))
		}
		if err = connections.ValidateSourceSelection(*root, false); err != nil {
			return fail(err)
		}
		account, err := connectedAccount(ctx, services.accounts)
		if err != nil {
			return fail(err)
		}
		d, err := services.transfer.SetDestination(ctx, *destination, account)
		if err != nil {
			return fail(err)
		}
		abs, err := filepath.Abs(*root)
		if err != nil {
			return report(errOut, domain.Fail("CONFIG_INVALID", "invalid local folder"))
		}
		if *name == "" {
			*name = filepath.Base(abs)
		}
		if existing, ok, err := services.store.FindPair(abs, account, d.ID); err == nil && ok {
			return report(errOut, output(out, existing))
		}
		p, err := services.store.Save(projects.Project{Name: *name, SourceRoot: abs, Policy: projects.DefaultPolicy(), Destination: *d})
		if err != nil {
			return fail(err)
		}
		return report(errOut, output(out, p))
	case args[0] == "pairs" && args[1] == "remove":
		f, id := pairFlags("ledgesync pairs remove")
		if f.Parse(args[2:]) != nil || f.NArg() != 0 || *id == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use pairs remove --pair ID. Drive copies and the local folder are not changed."))
		}
		if err = services.store.Delete(*id); err != nil {
			return fail(err)
		}
		return report(errOut, output(out, map[string]string{"removed": *id}))
	case args[0] == "automatic" && args[1] == "enable":
		return runAutomaticEnable(ctx, args[2:], in, out, errOut, interactive, services)
	case args[0] == "automatic" && args[1] == "disable":
		f, id := pairFlags("ledgesync automatic disable")
		if f.Parse(args[2:]) != nil || f.NArg() != 0 || *id == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use automatic disable --pair ID."))
		}
		p, err := services.store.Get(*id)
		if err != nil {
			return fail(err)
		}
		p.Automation = projects.Automation{}
		if p, err = services.store.Save(p); err != nil {
			return fail(err)
		}
		return report(errOut, output(out, p.Automation))
	case args[0] == "automatic" && args[1] == "run":
		f, id := pairFlags("ledgesync automatic run")
		if f.Parse(args[2:]) != nil || f.NArg() != 0 {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use automatic run [--pair ID]."))
		}
		return runAutomaticOnce(ctx, *id, out, errOut, services)
	case args[0] == "automatic" && args[1] == "watch":
		if len(args) != 2 {
			return report(errOut, domain.Fail("CONFIG_INVALID", "automatic watch takes no arguments"))
		}
		scheduler := projects.NewScheduler(services.store, services.runner(), func() {})
		if _, err = fmt.Fprintln(out, "Checking authorized sync pairs while this command runs. Press Ctrl+C to stop; a running copy is cancelled safely."); err != nil {
			return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
		}
		scheduler.Start()
		<-ctx.Done()
		scheduler.Stop()
		return 0
	case args[0] == "restore":
		return runRestore(ctx, args[1:], out, errOut, services)
	}
	return report(errOut, domain.Fail("CONFIG_INVALID", "Use pairs list|add|remove, automatic enable|disable|run|watch, or restore."))
}

func runAutomaticOnce(ctx context.Context, only string, out, errOut io.Writer, services pairServices) int {
	settings, err := services.store.Settings()
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if settings.AutomationPaused {
		return report(errOut, domain.Fail("AUTOMATION_PAUSED", "Automatic copies are paused in Settings."))
	}
	list, err := services.store.List()
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	scheduler := projects.NewScheduler(services.store, services.runner(), func() {})
	type result struct {
		Pair       string               `json:"pair"`
		Name       string               `json:"name"`
		Automation projects.Automation  `json:"automation"`
		LastRun    *projects.RunSummary `json:"lastRun,omitempty"`
	}
	results := []result{}
	attention := false
	for _, p := range list {
		a := p.Automation
		if only != "" && p.ID != only || !a.Enabled || a.Authorization == nil || a.Paused {
			continue
		}
		if only == "" && a.NextRunAt != "" {
			if next, e := time.Parse(time.RFC3339, a.NextRunAt); e == nil && next.After(time.Now()) {
				continue
			}
		}
		if err = scheduler.CheckNow(ctx, p.ID); err != nil {
			return report(errOut, connections.PublicError(err))
		}
		updated, err := services.store.Get(p.ID)
		if err != nil {
			return report(errOut, connections.PublicError(err))
		}
		attention = attention || updated.Automation.Paused || updated.Automation.Waiting != ""
		results = append(results, result{updated.ID, updated.Name, updated.Automation, updated.LastRun})
	}
	if only != "" && len(results) == 0 {
		return report(errOut, domain.Fail("AUTOMATION_UNAUTHORIZED", "This sync pair has no active automatic copy authorization."))
	}
	if err = output(out, results); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
	}
	if attention {
		return report(errOut, domain.Fail("AUTOMATION_ATTENTION", "At least one sync pair is paused or waiting. Review the output."))
	}
	return 0
}

func runAutomaticEnable(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, interactive bool, services pairServices) int {
	if !interactive {
		return report(errOut, domain.Fail("TERMINAL_REQUIRED", "Authorizing automatic copies requires an interactive terminal to review the preview."))
	}
	f, id := pairFlags("ledgesync automatic enable")
	every := f.Int("every", 15, "minutes between checks")
	watch := f.Bool("watch", false, "check for local changes and copy only when something changed")
	if f.Parse(args) != nil || f.NArg() != 0 || *id == "" {
		return report(errOut, domain.Fail("CONFIG_INVALID", "Use automatic enable --pair ID [--every MINUTES] [--watch]."))
	}
	trigger := projects.TriggerInterval
	if *watch {
		trigger = projects.TriggerWatch
	}
	if err := projects.ValidateAutomation(trigger, *every*60); err != nil {
		return report(errOut, err)
	}
	p, err := services.store.Get(*id)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	account, err := connectedAccount(ctx, services.accounts)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if account != p.Destination.AccountReference {
		return report(errOut, domain.Fail("ACCOUNT_CHANGED", "This sync pair belongs to a different Google account."))
	}
	if _, err = services.transfer.RestoreDestination(ctx, p.Destination); err != nil {
		return report(errOut, connections.PublicError(err))
	}
	src, err := projects.Source(p)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	plan, err := services.transfer.PreviewSource(ctx, src)
	defer services.transfer.Invalidate()
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if err = output(out, plan); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The preview could not be displayed."))
	}
	if _, err = fmt.Fprintf(out, "Automatic copies will copy new and changed files of this pair every %d minutes while `ledgesync automatic watch` or `automatic run` executes. Drive files are never overwritten or deleted; any change to the account, destination, folder, configuration or ignore rules pauses them.\nTo authorize, type this preview's planDigest (%s), then press Enter:\n", *every, plan.PlanDigest); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The confirmation could not be displayed."))
	}
	if !readConfirmation(ctx, in, plan.PlanDigest) {
		return report(errOut, domain.Fail("CANCELLED", "Automatic copies were not authorized."))
	}
	auth, err := projects.NewAuthorization(plan, time.Now())
	if err != nil {
		return report(errOut, err)
	}
	p.SourceIdentity = plan.SourceIdentity
	p.Automation = projects.Automation{Enabled: true, Trigger: trigger, IntervalSeconds: *every * 60, Authorization: auth, NextRunAt: auth.ApprovedAt}
	if p, err = services.store.Save(p); err != nil {
		return report(errOut, connections.PublicError(err))
	}
	return report(errOut, output(out, p.Automation))
}

func runRestore(ctx context.Context, args []string, out, errOut io.Writer, services pairServices) int {
	f, id := pairFlags("ledgesync restore")
	to := f.String("to", "", "new empty folder for the restored copy")
	if f.Parse(args) != nil || f.NArg() != 0 || *id == "" || *to == "" {
		return report(errOut, domain.Fail("CONFIG_INVALID", "Use restore --pair ID --to EMPTY_DIRECTORY. Existing files are never overwritten."))
	}
	p, err := services.store.Get(*id)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if p.SourceIdentity == "" {
		return report(errOut, domain.Fail("RESTORE_UNAVAILABLE", "This sync pair has no recorded copy yet."))
	}
	account, err := connectedAccount(ctx, services.accounts)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if account != p.Destination.AccountReference {
		return report(errOut, domain.Fail("ACCOUNT_CHANGED", "This copy belongs to a different Google account."))
	}
	target, err := restore.PrepareTarget(*to, []string{p.SourceRoot, services.stateDir})
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	runner := &restore.Runner{}
	result, err := runner.Run(ctx, services.restorer, restore.Request{StateDir: services.stateDir, ProjectKey: transfer.ProjectKey(p.SourceIdentity, account, p.Destination.ID), Account: account, Target: target})
	if e := output(out, result); e != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
	}
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if result.State == "partial" {
		return report(errOut, domain.Fail("PARTIAL", "Some recorded items are missing or changed in Drive and were not restored."))
	}
	return 0
}

// runPairCopy previews and, after the exact digest is typed, copies a saved pair.
func runPairCopy(ctx context.Context, id string, in io.Reader, out, errOut io.Writer, interactive bool, create pairFactory) int {
	if !interactive {
		return report(errOut, domain.Fail("TERMINAL_REQUIRED", "Manual copies require an interactive terminal for approval."))
	}
	services, err := create()
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	p, err := services.store.Get(id)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	account, err := connectedAccount(ctx, services.accounts)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	if account != p.Destination.AccountReference {
		return report(errOut, domain.Fail("ACCOUNT_CHANGED", "This sync pair belongs to a different Google account."))
	}
	if _, err = services.transfer.RestoreDestination(ctx, p.Destination); err != nil {
		return report(errOut, connections.PublicError(err))
	}
	src, err := projects.Source(p)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	plan, err := services.transfer.PreviewSource(ctx, src)
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	defer services.transfer.CancelAndWait()
	defer services.transfer.Invalidate()
	if err = output(out, plan); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The full preview could not be displayed. No upload was approved."))
	}
	if _, err = fmt.Fprintf(out, "Review every entry above. To upload this exact preview, type its full planDigest (%s), then press Enter:\n", plan.PlanDigest); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "The approval prompt could not be displayed."))
	}
	if !readConfirmation(ctx, in, plan.PlanDigest) {
		return report(errOut, domain.Fail("CANCELLED", "Upload was not approved. No Drive files were created."))
	}
	if _, err = services.transfer.Start(ctx, plan.PlanDigest); err != nil {
		return report(errOut, connections.PublicError(err))
	}
	p.SourceIdentity = plan.SourceIdentity
	_, _ = services.store.Save(p)
	for services.transfer.Busy() {
		select {
		case <-ctx.Done():
			services.transfer.CancelAndWait()
		case <-time.After(250 * time.Millisecond):
		}
	}
	final := services.transfer.Status()
	_ = services.store.RecordRun(projects.Summarize(p, "manual", final))
	if err = output(out, final); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
	}
	switch final.State {
	case "succeeded":
		return 0
	case "partial":
		return report(errOut, domain.Fail("PARTIAL", "Some approved files changed after the preview and were not copied."))
	case "cancelled":
		return report(errOut, domain.Fail("CANCELLED", "Upload stopped. Completed copies remain in Drive."))
	default:
		return report(errOut, domain.Fail("TRANSFER_FAILED", "%s", final.Message))
	}
}
