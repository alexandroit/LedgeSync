package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alexandroit/LedgeSync/internal/connections"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/syncer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// syncFactory opens the two-way sync manager shared with the desktop.
type syncFactory func() (*syncer.Manager, func(), error)

func newSyncManager() (*syncer.Manager, func(), error) {
	auth, err := connections.NewGoogleDrive(func(string) error { return driveauth.ErrBrowser })
	if err != nil {
		return nil, nil, driveauth.ErrBuildConfig
	}
	directory, err := transferstate.DefaultDirectory()
	if err != nil {
		return nil, nil, driveauth.ErrStorage
	}
	state, err := transferstate.OpenSyncState(directory)
	if err != nil {
		return nil, nil, err
	}
	m, err := syncer.NewManager(state, drive.New(auth), auth, syncer.Options{
		Validate:  func(p string) error { return connections.ValidateSourceSelection(p, false) },
		Protected: []string{directory},
		Lock: func() (func(), error) {
			return transferstate.AcquireProcessLock(filepath.Join(directory, "sync-lock"))
		},
	})
	if err != nil {
		_ = state.Close()
		return nil, nil, err
	}
	return m, func() { m.Stop(); _ = state.Close() }, nil
}

const syncUsage = "Use sync list | add --root DIRECTORY [--parent FOLDER_ID] [--watch] | run [--pair ID] | watch | pause|resume|remove --pair ID | confirm-deletes --pair ID --count N | restore-deletes --pair ID | activity [--pair ID]."

func syncProgress(errOut io.Writer, name string) syncer.Progress {
	last := time.Time{}
	return func(done, total int, current string, uploads, downloads int) {
		if total == 0 || time.Since(last) < 500*time.Millisecond && done < total {
			return
		}
		last = time.Now()
		fmt.Fprintf(errOut, "%s: %d/%d · ↑%d ↓%d %s\n", name, done, total, uploads, downloads, current)
	}
}

type syncPass struct {
	Pair          string        `json:"pair"`
	Name          string        `json:"name"`
	Result        syncer.Result `json:"result"`
	State         string        `json:"state"`
	LocalDeletes  int           `json:"localDeletes,omitempty"`
	RemoteDeletes int           `json:"remoteDeletes,omitempty"`
	Message       string        `json:"message,omitempty"`
}

// pass runs one sync pass for id and describes its outcome.
func pass(ctx context.Context, m *syncer.Manager, id string, errOut io.Writer) (syncPass, error) {
	status, err := m.Get(id)
	if err != nil {
		return syncPass{}, err
	}
	result, err := m.RunOnce(ctx, id, syncProgress(errOut, status.Pair.Name))
	out := syncPass{Pair: id, Name: status.Pair.Name, Result: result, State: "synced"}
	if local, remote, _, ok := syncer.ConfirmationNeeded(err); ok {
		out.State, out.LocalDeletes, out.RemoteDeletes = "confirm_deletes", local, remote
		out.Message = fmt.Sprintf("%d deletion(s) wait for confirmation: sync confirm-deletes --pair %s --count %d, or restore-deletes --pair %s.", local+remote, id, local+remote, id)
		return out, nil
	}
	if err != nil {
		out.State, out.Message = "error", err.Error()
		return out, err
	}
	return out, nil
}

func runSync(ctx context.Context, args []string, out, errOut io.Writer, create syncFactory) int {
	if len(args) < 2 {
		return report(errOut, domain.Fail("CONFIG_INVALID", "%s", syncUsage))
	}
	m, closeManager, err := create()
	if err != nil {
		return report(errOut, connections.PublicError(err))
	}
	defer closeManager()
	f := flag.NewFlagSet("ledgesync sync "+args[1], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	id := f.String("pair", "", "synced folder ID")
	root := f.String("root", "", "local folder to sync")
	parent := f.String("parent", "root", "Drive folder that holds the synced folder")
	watch := f.Bool("watch", false, "keep syncing until interrupted")
	count := f.String("count", "", "number of deletions being confirmed")
	if err = f.Parse(args[2:]); err != nil || f.NArg() != 0 {
		return report(errOut, domain.Fail("CONFIG_INVALID", "%s", syncUsage))
	}
	fail := func(err error) int { return report(errOut, connections.PublicError(err)) }
	need := func() bool { return *id != "" }
	switch args[1] {
	case "list":
		return report(errOut, output(out, m.List()))
	case "add":
		if *root == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use sync add --root DIRECTORY [--parent FOLDER_ID] [--watch]."))
		}
		status, err := m.Add(ctx, *root, syncer.Destination{FolderID: *parent})
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(errOut, "Syncing %s with Google Drive (%s).\n", status.Pair.LocalRoot, status.DriveURL)
		result, err := pass(ctx, m, status.Pair.ID, errOut)
		if e := output(out, result); e != nil {
			return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
		}
		if err != nil {
			return fail(err)
		}
		if *watch {
			return watchSync(ctx, m, out, errOut)
		}
		if result.State == "confirm_deletes" {
			return 5
		}
		return 0
	case "run":
		ids := []string{}
		if need() {
			ids = append(ids, *id)
		} else {
			for _, s := range m.List() {
				if !s.Pair.Paused {
					ids = append(ids, s.Pair.ID)
				}
			}
		}
		results := []syncPass{}
		code := 0
		for _, one := range ids {
			result, err := pass(ctx, m, one, errOut)
			results = append(results, result)
			if err != nil {
				code = 1
			} else if result.State == "confirm_deletes" && code == 0 {
				code = 5
			}
		}
		if err := output(out, results); err != nil {
			return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
		}
		return code
	case "watch":
		return watchSync(ctx, m, out, errOut)
	case "pause", "resume", "remove":
		if !need() {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use sync %s --pair ID.", args[1]))
		}
		action := map[string]func(string) error{"pause": m.Pause, "resume": m.Resume, "remove": m.Remove}[args[1]]
		if err := action(*id); err != nil {
			return fail(err)
		}
		if args[1] == "remove" {
			return report(errOut, output(out, map[string]string{"removed": *id, "note": "Files stay on this computer and in Google Drive."}))
		}
		status, err := m.Get(*id)
		if err != nil {
			return fail(err)
		}
		return report(errOut, output(out, status))
	case "confirm-deletes", "restore-deletes":
		if !need() || args[1] == "confirm-deletes" && *count == "" {
			return report(errOut, domain.Fail("CONFIG_INVALID", "Use sync confirm-deletes --pair ID --count N (the number shown by sync run), or sync restore-deletes --pair ID."))
		}
		held, err := pass(ctx, m, *id, errOut)
		if err != nil {
			return fail(err)
		}
		if held.State != "confirm_deletes" {
			return report(errOut, domain.Fail("SYNC_NOTHING_TO_CONFIRM", "There are no deletions waiting for confirmation."))
		}
		if args[1] == "confirm-deletes" {
			n, e := strconv.Atoi(*count)
			if e != nil || n != held.LocalDeletes+held.RemoteDeletes {
				return report(errOut, domain.Fail("SYNC_CONFIRMATION_MISMATCH", "%d deletion(s) are waiting, not %s. Nothing was deleted; review with sync run first.", held.LocalDeletes+held.RemoteDeletes, *count))
			}
			err = m.ConfirmDeletes(*id)
		} else {
			err = m.RestoreDeletes(*id)
		}
		if err != nil {
			return fail(err)
		}
		result, err := pass(ctx, m, *id, errOut)
		if e := output(out, result); e != nil {
			return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
		}
		if err != nil {
			return fail(err)
		}
		return 0
	case "activity":
		list, err := m.Activity(*id, 100)
		if err != nil {
			return fail(err)
		}
		return report(errOut, output(out, list))
	}
	return report(errOut, domain.Fail("CONFIG_INVALID", "%s", syncUsage))
}

// watchSync keeps every saved folder in sync until interrupted, printing a
// line whenever a folder's state changes.
func watchSync(ctx context.Context, m *syncer.Manager, out, errOut io.Writer) int {
	if _, err := fmt.Fprintln(errOut, "Syncing saved folders while this command runs. Press Ctrl+C to stop; a running pass stops safely."); err != nil {
		return report(errOut, domain.Fail("OUTPUT_UNAVAILABLE", "Output failed."))
	}
	m.Start()
	seen := map[string]string{}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for _, s := range m.List() {
			key := s.State + "|" + s.Message
			if seen[s.Pair.ID] != key {
				seen[s.Pair.ID] = key
				_ = output(out, map[string]any{"pair": s.Pair.ID, "name": s.Pair.Name, "state": s.State, "message": s.Message, "issues": len(s.Issues), "localDeletes": s.LocalDeletes, "remoteDeletes": s.RemoteDeletes})
			}
		}
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}
	}
}
