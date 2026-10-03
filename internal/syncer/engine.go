package syncer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// Engine runs sync cycles. It is safe for one cycle per pair at a time.
type Engine struct {
	State  *transferstate.SyncState
	Remote Remote
	Now    func() time.Time
}

// Snapshot is a remote listing that a later cycle may reuse while Drive
// reports no relevant changes and the ignore rules are unchanged.
type Snapshot struct {
	items map[string]remoteItem
	rules string
	taken time.Time
}

// Progress reports the operation being applied.
type Progress func(done, total int, current string, uploads, downloads int)

// errConfirm carries the counts of a cycle held by the mass-deletion guard.
type errConfirm struct {
	local, remote int
	digest        string
	paths         []string // the deleted paths whose base entries a restore forgets
}

func (e *errConfirm) Error() string { return "SYNC_DELETE_CONFIRMATION" }

// ConfirmationNeeded reports whether err is the mass-deletion guard and its counts.
func ConfirmationNeeded(err error) (local, remote int, digest string, ok bool) {
	var c *errConfirm
	if errors.As(err, &c) {
		return c.local, c.remote, c.digest, true
	}
	return 0, 0, "", false
}

// fatal errors stop a cycle; anything else is reported for the item only.
func fatal(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	code := domain.ErrorCode(err)
	return strings.HasPrefix(code, "AUTH_") || code == "ACCOUNT_CHANGED" || code == "CANCELLED" || code == "DRIVE_NETWORK" ||
		code == "DRIVE_STORAGE_FULL" || code == "DRIVE_QUOTA" || code == "DRIVE_RATE_LIMIT" || code == "SYNC_FOLDER_UNAVAILABLE" || code == "SYNC_REMOTE_MISSING"
}

func newOperation() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "sync-" + hex.EncodeToString(b[:])
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Cycle performs one complete two-way pass for pair.
func (e *Engine) Cycle(ctx context.Context, pair Pair, account string, cached *Snapshot, progress Progress) (Result, *Snapshot, error) {
	result := Result{Issues: []Issue{}}
	if pair.AccountReference != account {
		return result, nil, domain.Fail("ACCOUNT_CHANGED", "This folder syncs with a different Google account. Connect that account or remove the sync.")
	}
	local, err := openLocal(pair.LocalRoot, e.now)
	if err != nil {
		return result, nil, err
	}
	defer local.Close()
	if _, err = e.Remote.GetFolder(ctx, account, pair.RemoteRootID); err != nil {
		if code := domain.ErrorCode(err); code == "DRIVE_NOT_FOUND" || code == "DRIVE_NOT_FOLDER" {
			return result, nil, domain.Fail("SYNC_REMOTE_MISSING", "The Drive folder of this sync is missing or in the trash. Restore it in Drive or remove the sync.")
		}
		return result, nil, err
	}
	local.maintain()
	base, err := e.State.Entries(pair.ID)
	if err != nil {
		return result, nil, err
	}
	scan, err := scanLocal(ctx, pair, base)
	if err != nil {
		return result, nil, err
	}
	rules := rulesKey(scan)
	var remote map[string]remoteItem
	var remoteIssues []Issue
	if cached != nil && cached.rules == rules {
		remote = map[string]remoteItem{}
		for p, r := range cached.items {
			if skip, err := scan.excluded(p, r.Kind); err != nil {
				return result, nil, err
			} else if !skip {
				remote[p] = r
			}
		}
	} else {
		remote, remoteIssues, err = scanRemote(ctx, e.Remote, account, pair.RemoteRootID, scan.excluded)
		if err != nil {
			return result, nil, err
		}
	}
	if err = e.reconcileIntents(ctx, pair, account, remote); err != nil {
		return result, nil, err
	}
	pl := decide(base, scan.items, remote)
	result.Issues = append(append(append(result.Issues, scan.issues...), remoteIssues...), pl.issues...)
	if pl.needsConfirmation() {
		digest := pl.deleteDigest()
		if pair.ConfirmedDeletes != digest {
			var paths []string
			for _, o := range pl.ops {
				if o.kind == opTrashLocal || o.kind == opTrashRemote {
					paths = append(paths, o.path)
				}
			}
			return result, &Snapshot{remote, rules, e.now()}, &errConfirm{pl.localDeletes, pl.remoteDeletes, digest, paths}
		}
	}
	x := &executor{e: e, ctx: ctx, pair: pair, account: account, local: local, remote: remote, base: base, result: &result, progress: progress}
	x.dirIDs = map[string]string{"": pair.RemoteRootID}
	for p, r := range remote {
		if r.Kind == "dir" {
			x.dirIDs[p] = r.ID
		}
	}
	err = x.run(pl)
	snapshot := &Snapshot{x.remote, rules, e.now()}
	if x.remoteMutated {
		snapshot = nil // our own changes are re-read from Drive next time
	}
	return result, snapshot, err
}

func rulesKey(scan *localScan) string { return scan.rules }

// reconcileIntents finds creations whose response was lost. The reserved ID is
// read directly, so an item that already exists is adopted, never duplicated.
func (e *Engine) reconcileIntents(ctx context.Context, pair Pair, account string, remote map[string]remoteItem) error {
	intents, err := e.State.Intents(pair.ID)
	if err != nil {
		return err
	}
	for p, in := range intents {
		o, err := e.Remote.GetObject(ctx, account, in.RemoteID)
		switch {
		case domain.ErrorCode(err) == "DRIVE_NOT_FOUND":
			_ = e.State.DeleteIntent(pair.ID, p)
		case err != nil:
			return err
		case o.Trashed || o.AppProperties["ledgesyncOperation"] != in.Operation:
			_ = e.State.DeleteIntent(pair.ID, p)
		default:
			parent := ""
			if len(o.Parents) == 1 {
				parent = o.Parents[0]
			}
			if _, listed := remote[p]; !listed {
				remote[p] = remoteFrom(o, parent)
			}
			_ = e.State.DeleteIntent(pair.ID, p)
		}
	}
	return nil
}

type executor struct {
	e             *Engine
	ctx           context.Context
	pair          Pair
	account       string
	local         *localFS
	remote        map[string]remoteItem
	base          map[string]transferstate.SyncEntry
	dirIDs        map[string]string
	result        *Result
	progress      Progress
	ids           []string
	done, total   int
	uploads       int
	downloads     int
	remoteMutated bool
}

func (x *executor) issue(p string, err error) {
	code := domain.ErrorCode(err)
	message := err.Error()
	var safe *domain.Error
	if !errors.As(err, &safe) {
		code, message = "SYNC_ITEM_FAILED", "This item could not be synced. It will be retried."
	}
	x.result.Issues = append(x.result.Issues, Issue{p, code, message})
}

func (x *executor) activity(kind, p, detail string) {
	_ = x.e.State.AddActivity(x.pair.ID, transferstate.SyncActivity{At: x.e.now().UTC().Format(time.RFC3339), Kind: kind, Path: p, Detail: detail})
}

func (x *executor) step(p string) {
	if x.progress != nil {
		x.progress(x.done, x.total, p, x.uploads, x.downloads)
	}
}

func (x *executor) reserve(n int) error {
	for len(x.ids) < n {
		batch := min(n-len(x.ids), 1000)
		ids, err := x.e.Remote.GenerateIDs(x.ctx, x.account, batch)
		if err != nil {
			return err
		}
		x.ids = append(x.ids, ids...)
	}
	return nil
}

func (x *executor) nextID() string {
	id := x.ids[0]
	x.ids = x.ids[1:]
	return id
}

func (x *executor) put(entry transferstate.SyncEntry) error {
	if err := x.e.State.PutEntry(x.pair.ID, entry); err != nil {
		return err
	}
	x.base[entry.Path] = entry
	return nil
}

func fileEntry(p string, l localItem, r remoteItem) transferstate.SyncEntry {
	return transferstate.SyncEntry{Path: p, Kind: "file", LocalSize: l.Size, LocalMtime: l.Mtime, LocalMD5: l.MD5,
		RemoteID: r.ID, RemoteMD5: r.MD5, RemoteVersion: r.Version, RemoteModified: r.Modified}
}

func nameOf(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

func (x *executor) run(pl *plan) error {
	groups := map[opKind][]op{}
	creations := 0
	for _, o := range pl.ops {
		groups[o.kind] = append(groups[o.kind], o)
		switch o.kind {
		case opMkdirRemote, opUpload:
			creations++
		}
		switch o.kind {
		case opAdopt, opForget:
		default:
			x.total++
		}
		switch o.kind {
		case opUpload, opUpdateRemote:
			x.uploads++
		case opDownload, opConflict:
			x.downloads++
		}
	}
	byDepth := func(list []op, deepestFirst bool) []op {
		sort.SliceStable(list, func(i, j int) bool {
			if deepestFirst {
				return depth(list[i].path) > depth(list[j].path)
			}
			return depth(list[i].path) < depth(list[j].path)
		})
		return list
	}
	for _, o := range groups[opForget] {
		if err := x.e.State.DeleteEntry(x.pair.ID, o.path); err != nil {
			return err
		}
		delete(x.base, o.path)
	}
	for _, o := range groups[opAdopt] {
		entry := transferstate.SyncEntry{Path: o.path, Kind: "dir", RemoteID: o.remote.ID}
		if !o.dir {
			entry = fileEntry(o.path, o.local, o.remote)
		}
		if err := x.put(entry); err != nil {
			return err
		}
	}
	if creations > 0 {
		if err := x.reserve(creations); err != nil {
			return err
		}
	}
	steps := []struct {
		kind  opKind
		deep  bool
		apply func(op) error
	}{
		{opMkdirRemote, false, x.mkdirRemote},
		{opMkdirLocal, false, x.mkdirLocal},
		{opConflict, false, x.conflict},
		{opUpload, false, x.upload},
		{opUpdateRemote, false, x.updateRemote},
		{opDownload, false, x.download},
		{opTrashRemote, true, x.trashRemote},
		{opTrashLocal, true, x.trashLocal},
	}
	failedDirs := map[string]bool{}
	for _, s := range steps {
		for _, o := range byDepth(groups[s.kind], s.deep) {
			if err := x.ctx.Err(); err != nil {
				return err
			}
			if blocked(o.path, failedDirs) {
				x.issue(o.path, domain.Fail("SYNC_PARENT_UNAVAILABLE", "Its folder could not be synced; it will be retried."))
				x.done++
				continue
			}
			x.step(o.path)
			err := s.apply(o)
			x.done++
			if err == nil {
				continue
			}
			if fatal(err) {
				return err
			}
			if o.dir {
				failedDirs[o.path] = true
			}
			x.issue(o.path, err)
		}
	}
	x.step("")
	return nil
}

func blocked(p string, failed map[string]bool) bool {
	for d := parentOf(p); ; d = parentOf(d) {
		if failed[d] {
			return true
		}
		if d == "" {
			return false
		}
	}
}

func (x *executor) parentID(p string) (string, error) {
	id, ok := x.dirIDs[parentOf(p)]
	if !ok {
		return "", domain.Fail("SYNC_PARENT_UNAVAILABLE", "The Drive folder for %s is not available yet.", p)
	}
	return id, nil
}

func (x *executor) mkdirRemote(o op) error {
	parent, err := x.parentID(o.path)
	if err != nil {
		return err
	}
	id, operation := x.nextID(), newOperation()
	if err = x.e.State.PutIntent(x.pair.ID, transferstate.SyncIntent{Path: o.path, Kind: "dir", RemoteID: id, Operation: operation}); err != nil {
		return err
	}
	created, err := x.e.Remote.CreateFolder(x.ctx, x.account, id, parent, nameOf(o.path), operation)
	if err != nil {
		return err
	}
	x.remoteMutated = true
	x.dirIDs[o.path] = created.ID
	x.remote[o.path] = remoteFrom(created, parent)
	if err = x.put(transferstate.SyncEntry{Path: o.path, Kind: "dir", RemoteID: created.ID}); err != nil {
		return err
	}
	_ = x.e.State.DeleteIntent(x.pair.ID, o.path)
	x.result.FoldersMade++
	x.result.Changed = true
	x.activity("folder_up", o.path, "")
	return nil
}

func (x *executor) mkdirLocal(o op) error {
	if err := x.local.mkdir(o.path); err != nil {
		return err
	}
	x.dirIDs[o.path] = o.remote.ID
	if err := x.put(transferstate.SyncEntry{Path: o.path, Kind: "dir", RemoteID: o.remote.ID}); err != nil {
		return err
	}
	x.result.FoldersMade++
	x.result.Changed = true
	x.activity("folder_down", o.path, "")
	return nil
}

func (x *executor) upload(o op) error {
	parent, err := x.parentID(o.path)
	if err != nil {
		return err
	}
	f, err := x.local.open(o.path, o.local)
	if err != nil {
		return err
	}
	defer f.Close()
	id, operation := x.nextID(), newOperation()
	if err = x.e.State.PutIntent(x.pair.ID, transferstate.SyncIntent{Path: o.path, Kind: "file", RemoteID: id, Operation: operation}); err != nil {
		return err
	}
	created, err := x.e.Remote.Upload(x.ctx, x.account, id, parent, nameOf(o.path), operation, f, o.local.Size, o.local.MD5)
	if domain.ErrorCode(err) == "DRIVE_VERIFICATION_FAILED" {
		// The file changed while it was sent. Record the Drive item with an
		// unknown local state so the next pass uploads the current content.
		x.remoteMutated = true
		if current, e := x.e.Remote.GetObject(x.ctx, x.account, id); e == nil && !current.Trashed {
			_ = x.put(fileEntry(o.path, localItem{}, remoteFrom(current, parent)))
		}
		_ = x.e.State.DeleteIntent(x.pair.ID, o.path)
		return domain.Fail("LOCAL_CHANGED", "%s changed while it was uploaded; it will be uploaded again.", o.path)
	}
	if err != nil {
		return err
	}
	x.remoteMutated = true
	r := remoteFrom(created, parent)
	x.remote[o.path] = r
	if err = x.put(fileEntry(o.path, o.local, r)); err != nil {
		return err
	}
	_ = x.e.State.DeleteIntent(x.pair.ID, o.path)
	x.result.Uploaded++
	x.result.Changed = true
	x.activity("upload", o.path, "")
	return nil
}

func (x *executor) updateRemote(o op) error {
	current, err := x.e.Remote.GetObject(x.ctx, x.account, o.remote.ID)
	if err != nil {
		return err
	}
	if current.Version != o.remote.Version && current.MD5 != o.base.RemoteMD5 {
		return domain.Fail("REMOTE_CHANGED", "%s changed in Drive during this pass; it will be compared again.", o.path)
	}
	f, err := x.local.open(o.path, o.local)
	if err != nil {
		return err
	}
	defer f.Close()
	updated, err := x.e.Remote.UpdateContent(x.ctx, x.account, o.remote.ID, f, o.local.Size, o.local.MD5)
	if domain.ErrorCode(err) == "DRIVE_VERIFICATION_FAILED" {
		x.remoteMutated = true
		if now, e := x.e.Remote.GetObject(x.ctx, x.account, o.remote.ID); e == nil {
			entry := o.base
			entry.RemoteMD5, entry.RemoteVersion, entry.RemoteModified = now.MD5, now.Version, now.ModifiedTime
			_ = x.put(entry)
		}
		return domain.Fail("LOCAL_CHANGED", "%s changed while it was uploaded; it will be uploaded again.", o.path)
	}
	if err != nil {
		return err
	}
	x.remoteMutated = true
	r := remoteFrom(updated, o.remote.Parent)
	x.remote[o.path] = r
	if err = x.put(fileEntry(o.path, o.local, r)); err != nil {
		return err
	}
	x.result.Uploaded++
	x.result.Changed = true
	x.activity("update_up", o.path, "")
	return nil
}

func parseModified(v string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (x *executor) fetch(o op, expect *localItem) (localItem, error) {
	return x.local.writeFile(x.ctx, o.path, expect, o.remote.Size, o.remote.MD5, parseModified(o.remote.Modified), func(w io.Writer) error {
		return x.e.Remote.Download(x.ctx, x.account, o.remote.ID, o.remote.Size, w)
	})
}

func (x *executor) download(o op) error {
	var expect *localItem
	if o.hasLocal {
		expect = &o.local
	}
	written, err := x.fetch(o, expect)
	if err != nil {
		return err
	}
	if err = x.put(fileEntry(o.path, written, o.remote)); err != nil {
		return err
	}
	x.result.Downloaded++
	x.result.Changed = true
	kind := "download"
	if o.hasLocal {
		kind = "update_down"
	}
	x.activity(kind, o.path, "")
	return nil
}

func (x *executor) conflict(o op) error {
	kept := conflictName(o.path, x.e.now())
	if err := x.local.renameConflict(o.path, kept, o.local); err != nil {
		return err
	}
	written, err := x.fetch(o, nil)
	if err != nil {
		return err
	}
	if err = x.put(fileEntry(o.path, written, o.remote)); err != nil {
		return err
	}
	x.result.Conflicts++
	x.result.Downloaded++
	x.result.Changed = true
	x.activity("conflict", o.path, nameOf(kept))
	return nil
}

func (x *executor) trashRemote(o op) error {
	if !o.dir {
		current, err := x.e.Remote.GetObject(x.ctx, x.account, o.base.RemoteID)
		if err == nil && !current.Trashed && current.Version != o.base.RemoteVersion && current.MD5 != o.base.RemoteMD5 {
			return domain.Fail("REMOTE_CHANGED", "%s changed in Drive; it was not deleted and will be compared again.", o.path)
		}
		if err != nil && domain.ErrorCode(err) != "DRIVE_NOT_FOUND" {
			return err
		}
	}
	if err := x.e.Remote.Trash(x.ctx, x.account, o.base.RemoteID); err != nil {
		return err
	}
	x.remoteMutated = true
	if err := x.e.State.DeleteTree(x.pair.ID, o.path); err != nil {
		return err
	}
	for p := range x.remote {
		if p == o.path || under(p, o.path) {
			delete(x.remote, p)
		}
	}
	x.result.DeletedRemote++
	x.result.Changed = true
	x.activity("delete_up", o.path, "Moved to the Google Drive trash")
	return nil
}

func (x *executor) trashLocal(o op) error {
	if err := x.local.toTrash(o.path, o.local); err != nil {
		return err
	}
	if err := x.e.State.DeleteTree(x.pair.ID, o.path); err != nil {
		return err
	}
	x.result.DeletedLocal++
	x.result.Changed = true
	x.activity("delete_down", o.path, "Moved to "+TrashDir)
	return nil
}

var _ Remote = (*drive.Client)(nil)
