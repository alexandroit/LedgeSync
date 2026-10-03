package syncer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/projects"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// Accounts reports the connected Google account.
type Accounts interface {
	Status(context.Context) (driveauth.Status, error)
}

// Options tune the background loop; zero values select the defaults.
type Options struct {
	PollLocal  time.Duration // local change check, default 10 s
	PollRemote time.Duration // Drive change feed, default 30 s
	FullRemote time.Duration // full Drive listing even without changes, default 10 min
	Retry      time.Duration // wait after a transient failure, default 30 s
	Tick       time.Duration // loop interval, default 2 s
	Now        func() time.Time
	OnChange   func()
	// Validate checks a local folder chosen for sync (for example that it is
	// not a system folder). Protected paths may not overlap a synced folder.
	Validate  func(string) error
	Protected []string
}

type runtimeState struct {
	status      Status
	dirty       bool
	snapshot    *Snapshot
	fingerprint string
	lastLocal   time.Time
	retryAt     time.Time
	cancel      context.CancelFunc
	tracked     map[string]bool
	confirm     string
	pending     []string // paths held by the deletion guard
}

// Manager keeps every pair in sync while it runs.
type Manager struct {
	engine         *Engine
	state          *transferstate.SyncState
	remote         Remote
	accounts       Accounts
	opts           Options
	mu             sync.Mutex
	pairs          map[string]*Pair
	rt             map[string]*runtimeState
	wake           chan struct{}
	ctx            context.Context
	stop           context.CancelFunc
	done           chan struct{}
	cycle          sync.Mutex
	lastRemotePoll time.Time
	accountRef     string
	accountWait    string
	accountAt      time.Time
}

// NewManager loads the saved pairs. Call Start to begin syncing.
func NewManager(state *transferstate.SyncState, remote Remote, accounts Accounts, opts Options) (*Manager, error) {
	if opts.PollLocal <= 0 {
		opts.PollLocal = 10 * time.Second
	}
	if opts.PollRemote <= 0 {
		opts.PollRemote = 30 * time.Second
	}
	if opts.FullRemote <= 0 {
		opts.FullRemote = 10 * time.Minute
	}
	if opts.Retry <= 0 {
		opts.Retry = 30 * time.Second
	}
	if opts.Tick <= 0 {
		opts.Tick = 2 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	m := &Manager{engine: &Engine{State: state, Remote: remote, Now: opts.Now}, state: state, remote: remote, accounts: accounts, opts: opts,
		pairs: map[string]*Pair{}, rt: map[string]*runtimeState{}, wake: make(chan struct{}, 1)}
	m.ctx, m.stop = context.WithCancel(context.Background())
	raw, err := state.Pairs()
	if err != nil {
		return nil, err
	}
	for _, r := range raw {
		var p Pair
		if json.Unmarshal(r, &p) != nil || p.ID == "" {
			return nil, domain.Fail("STATE_INVALID", "The saved sync settings are invalid.")
		}
		pair := p
		m.pairs[p.ID] = &pair
		m.rt[p.ID] = &runtimeState{dirty: true, status: Status{State: "starting", Message: "Starting…", Issues: []Issue{}}}
		if p.Paused {
			m.rt[p.ID].status = Status{State: "paused", Message: pauseMessage(p), Issues: []Issue{}}
		}
	}
	return m, nil
}

func pauseMessage(p Pair) string {
	if p.PauseReason != "" {
		return p.PauseReason
	}
	return "Sync is paused."
}

// Start begins the background loop; Stop cancels it and waits.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done != nil {
		return
	}
	m.done = make(chan struct{})
	go m.loop()
}

func (m *Manager) Stop() {
	m.stop()
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (m *Manager) nudge() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) notify() {
	if m.opts.OnChange != nil {
		m.opts.OnChange()
	}
}

func (m *Manager) loop() {
	defer func() {
		m.mu.Lock()
		close(m.done)
		m.mu.Unlock()
	}()
	ticker := time.NewTicker(m.opts.Tick)
	defer ticker.Stop()
	for {
		m.tick()
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}

// account returns the connected account, re-reading the credential vault at
// most once a minute (or after an authorization failure).
func (m *Manager) account(ctx context.Context) (string, string) {
	m.mu.Lock()
	if !m.accountAt.IsZero() && m.opts.Now().Sub(m.accountAt) < time.Minute {
		ref, wait := m.accountRef, m.accountWait
		m.mu.Unlock()
		return ref, wait
	}
	m.mu.Unlock()
	ref, wait := m.fetchAccount(ctx)
	m.mu.Lock()
	m.accountRef, m.accountWait, m.accountAt = ref, wait, m.opts.Now()
	m.mu.Unlock()
	return ref, wait
}

func (m *Manager) forgetAccount() {
	m.mu.Lock()
	m.accountAt = time.Time{}
	m.mu.Unlock()
}

// AccountChanged makes the next check re-read the connection, for example
// right after the user connects or disconnects Google Drive.
func (m *Manager) AccountChanged() {
	m.forgetAccount()
	m.nudge()
}

func (m *Manager) fetchAccount(ctx context.Context) (string, string) {
	status, err := m.accounts.Status(ctx)
	if err != nil || status.State != "connected" || status.Account == nil {
		message := "Connect Google Drive to sync."
		if status.State == "reconnect_required" || status.State == "client_changed" || errors.Is(err, driveauth.ErrScope) {
			message = "Reconnect Google Drive to grant the access sync needs."
		}
		return "", message
	}
	return status.Account.Reference, ""
}

func (m *Manager) setStatus(id string, fn func(*Status)) {
	m.mu.Lock()
	rt, ok := m.rt[id]
	if ok {
		fn(&rt.status)
	}
	m.mu.Unlock()
	if ok {
		m.notify()
	}
}

func (m *Manager) tick() {
	if m.ctx.Err() != nil {
		return
	}
	account, waiting := m.account(m.ctx)
	m.mu.Lock()
	ids := make([]string, 0, len(m.pairs))
	for id, p := range m.pairs {
		if !p.Paused {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	sort.Strings(ids)
	if account == "" {
		for _, id := range ids {
			m.setStatus(id, func(s *Status) {
				if s.State != "waiting" || s.Message != waiting {
					s.State, s.Message, s.ErrorCode = "waiting", waiting, "AUTH_REQUIRED"
				}
			})
		}
		return
	}
	now := m.opts.Now()
	if now.Sub(m.lastRemotePoll) >= m.opts.PollRemote {
		m.lastRemotePoll = now
		m.pollRemote(account)
	}
	for _, id := range ids {
		m.mu.Lock()
		pair, rt := m.pairs[id], m.rt[id]
		if pair == nil || rt == nil {
			m.mu.Unlock()
			continue
		}
		p := *pair
		checkLocal := now.Sub(rt.lastLocal) >= m.opts.PollLocal
		m.mu.Unlock()
		if checkLocal {
			fp, err := fingerprint(p.LocalRoot)
			m.mu.Lock()
			rt.lastLocal = now
			if err != nil {
				m.mu.Unlock()
				m.setStatus(id, func(s *Status) {
					s.State, s.ErrorCode, s.Message = "waiting", "SYNC_FOLDER_UNAVAILABLE", "The local folder is not available. Connect the disk or check the folder."
				})
				continue
			}
			if fp != rt.fingerprint {
				rt.dirty = true
			}
			m.mu.Unlock()
		}
		m.mu.Lock()
		if rt.snapshot != nil && now.Sub(rt.snapshot.taken) > m.opts.FullRemote {
			rt.snapshot, rt.dirty = nil, true
		}
		run := rt.dirty && !now.Before(rt.retryAt)
		m.mu.Unlock()
		if run {
			m.runCycle(id, account, nil)
		}
		if m.ctx.Err() != nil {
			return
		}
	}
}

// pollRemote reads the Drive change feed and marks pairs whose tracked items
// or folders changed. Unrelated changes elsewhere in Drive are ignored.
func (m *Manager) pollRemote(account string) {
	token, err := m.state.Cursor(account)
	if err != nil {
		return
	}
	if token == "" {
		if token, err = m.remote.StartPageToken(m.ctx, account); err == nil {
			_ = m.state.SetCursor(account, token)
		}
		return
	}
	changes, next, err := m.remote.Changes(m.ctx, account, token)
	if err != nil {
		if code := domain.ErrorCode(err); code == "DRIVE_NOT_FOUND" || code == "DRIVE_REQUEST_FAILED" {
			// An expired position: restart from now and relist every pair.
			if fresh, e := m.remote.StartPageToken(m.ctx, account); e == nil {
				_ = m.state.SetCursor(account, fresh)
			}
			m.mu.Lock()
			for _, rt := range m.rt {
				rt.snapshot, rt.dirty = nil, true
			}
			m.mu.Unlock()
		}
		return
	}
	_ = m.state.SetCursor(account, next)
	if len(changes) == 0 {
		return
	}
	m.mu.Lock()
	for id, rt := range m.rt {
		pair := m.pairs[id]
		if pair == nil {
			continue
		}
		for _, ch := range changes {
			relevant := ch.FileID == pair.RemoteRootID || rt.tracked[ch.FileID]
			for _, parent := range ch.Parents {
				relevant = relevant || parent == pair.RemoteRootID || rt.tracked[parent]
			}
			if relevant || rt.tracked == nil {
				rt.snapshot, rt.dirty = nil, true
				break
			}
		}
	}
	m.mu.Unlock()
}

func classify(err error) (state string, pause bool) {
	code := domain.ErrorCode(err)
	switch {
	case errors.Is(err, context.Canceled), code == "CANCELLED":
		return "paused", false
	case code == "ACCOUNT_CHANGED", code == "SYNC_REMOTE_MISSING", code == "SYNC_OVERLAP", code == "STATE_INVALID":
		return "error", true
	case strings.HasPrefix(code, "AUTH_"), code == "SYNC_FOLDER_UNAVAILABLE", code == "DRIVE_NETWORK", code == "DRIVE_RATE_LIMIT", code == "DRIVE_UNAVAILABLE":
		return "waiting", false
	}
	return "error", false
}

func (m *Manager) runCycle(id, account string, progress Progress) (Result, error) {
	m.cycle.Lock()
	defer m.cycle.Unlock()
	m.mu.Lock()
	pair, rt := m.pairs[id], m.rt[id]
	if pair == nil || rt == nil || pair.Paused {
		m.mu.Unlock()
		return Result{}, domain.Fail("SYNC_NOT_FOUND", "This sync is not active.")
	}
	ctx, cancel := context.WithCancel(m.ctx)
	rt.cancel = cancel
	p := *pair
	snapshot := rt.snapshot
	rt.dirty = false
	m.mu.Unlock()
	defer cancel()
	m.setStatus(id, func(s *Status) {
		s.State, s.ErrorCode, s.Done, s.Total, s.CurrentPath = "syncing", "", 0, 0, ""
		s.Message = "Checking for changes…"
	})
	result, snap, err := m.engine.Cycle(ctx, p, account, snapshot, func(done, total int, current string, uploads, downloads int) {
		m.setStatus(id, func(s *Status) {
			s.Done, s.Total, s.CurrentPath, s.Uploads, s.Downloads = done, total, current, uploads, downloads
			if total > 0 {
				s.Message = "Syncing…"
			}
		})
		if progress != nil {
			progress(done, total, current, uploads, downloads)
		}
	})
	now := m.opts.Now()
	fp, _ := fingerprint(p.LocalRoot)
	entries, _ := m.state.Entries(id)
	m.mu.Lock()
	defer func() { m.mu.Unlock(); m.notify() }()
	pair, rt = m.pairs[id], m.rt[id]
	if pair == nil || rt == nil {
		return result, err
	}
	rt.cancel = nil
	rt.fingerprint = fp
	rt.tracked = map[string]bool{pair.RemoteRootID: true}
	for _, e := range entries {
		if e.RemoteID != "" {
			rt.tracked[e.RemoteID] = true
		}
	}
	issues := result.Issues
	if issues == nil {
		issues = []Issue{}
	}
	s := &rt.status
	s.Issues, s.Done, s.Total, s.CurrentPath, s.Uploads, s.Downloads, s.LocalDeletes, s.RemoteDeletes = issues, 0, 0, "", 0, 0, 0, 0
	if localDeletes, remoteDeletes, digest, ok := ConfirmationNeeded(err); ok {
		var held *errConfirm
		errors.As(err, &held)
		rt.snapshot, rt.confirm, rt.pending = snap, digest, held.paths
		s.State, s.ErrorCode, s.LocalDeletes, s.RemoteDeletes = "confirm_deletes", "SYNC_DELETE_CONFIRMATION", localDeletes, remoteDeletes
		s.Message = "Many files were deleted on one side. Confirm to delete them on the other side too, or restore them."
		return result, err
	}
	rt.confirm, rt.pending = "", nil
	if err != nil {
		state, pause := classify(err)
		rt.snapshot = nil
		if strings.HasPrefix(domain.ErrorCode(err), "AUTH_") {
			m.accountAt = time.Time{}
		}
		s.State, s.ErrorCode = state, domain.ErrorCode(err)
		var safe *domain.Error
		if errors.As(err, &safe) {
			s.Message = safe.Message
		} else if state == "paused" {
			s.Message = "Sync is paused."
		} else {
			s.Message = "Sync could not finish. It will be retried."
		}
		if state != "paused" {
			rt.dirty, rt.retryAt = true, now.Add(m.opts.Retry)
		}
		if pause {
			pair.Paused, pair.PauseCode, pair.PauseReason = true, s.ErrorCode, s.Message
			_ = m.state.PutPair(pair.ID, pair)
		}
		return result, err
	}
	rt.snapshot = snap
	rt.dirty = result.Changed
	pair.LastSyncAt = now.UTC().Format(time.RFC3339)
	pair.ConfirmedDeletes = ""
	_ = m.state.PutPair(pair.ID, pair)
	s.State, s.ErrorCode, s.LastSyncAt = "synced", "", pair.LastSyncAt
	s.Message = "Up to date"
	if len(issues) > 0 {
		s.Message = "Up to date, with items that were not synced"
	}
	return result, nil
}

func newPairID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func overlaps(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if caseInsensitive() {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	sep := string(filepath.Separator)
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, sep)+sep) || strings.HasPrefix(b, strings.TrimSuffix(a, sep)+sep)
}

// Add starts syncing localRoot with a folder of the same name inside parent
// ("root" for My Drive). An existing folder with that name is reused, so
// adding the same folder on another computer joins the same Drive folder.
func (m *Manager) Add(ctx context.Context, localRoot string, parent Destination) (Status, error) {
	abs, err := filepath.Abs(localRoot)
	if err != nil {
		return Status{}, domain.Fail("CONFIG_INVALID", "Choose a local folder.")
	}
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		abs = resolved
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 {
		return Status{}, domain.Fail("CONFIG_INVALID", "Choose an existing local folder.")
	}
	if m.opts.Validate != nil {
		if err = m.opts.Validate(abs); err != nil {
			return Status{}, err
		}
	}
	for _, protected := range m.opts.Protected {
		if protected != "" && overlaps(abs, protected) {
			return Status{}, domain.Fail("SYNC_OVERLAP", "This folder contains LedgeSync's own data or is inside it; choose another folder.")
		}
	}
	m.mu.Lock()
	for _, p := range m.pairs {
		if overlaps(abs, p.LocalRoot) {
			m.mu.Unlock()
			return Status{}, domain.Fail("SYNC_OVERLAP", "This folder is already synced, or is inside or contains a synced folder.")
		}
	}
	m.mu.Unlock()
	m.forgetAccount()
	account, waiting := m.account(ctx)
	if account == "" {
		return Status{}, domain.Fail("AUTH_REQUIRED", "%s", waiting)
	}
	folderID := parent.FolderID
	if folderID == "" {
		folderID = "root"
	}
	if folderID != "root" {
		folder, err := m.remote.GetFolder(ctx, account, folderID)
		if err != nil {
			return Status{}, err
		}
		if !folder.CanAddChildren {
			return Status{}, domain.Fail("DRIVE_PERMISSION_DENIED", "The selected Drive folder does not allow adding files.")
		}
		if parent.Name == "" {
			parent.Name = folder.Name
		}
	} else if parent.Name == "" {
		parent.Name = "My Drive"
	}
	parent.FolderID = folderID
	name := filepath.Base(abs)
	if name == "" || name == string(filepath.Separator) || name == "." || !localName(name) {
		return Status{}, domain.Fail("CONFIG_INVALID", "Choose a named folder, not the root of a disk.")
	}
	children, err := m.remote.ListChildren(ctx, account, folderID)
	if err != nil {
		return Status{}, err
	}
	var matches []string
	for _, c := range children {
		if c.Name == name && c.MimeType == "application/vnd.google-apps.folder" && !c.Trashed {
			matches = append(matches, c.ID)
		}
	}
	remoteID := ""
	switch len(matches) {
	case 0:
		ids, err := m.remote.GenerateIDs(ctx, account, 1)
		if err != nil {
			return Status{}, err
		}
		created, err := m.remote.CreateFolder(ctx, account, ids[0], folderID, name, newOperation())
		if err != nil {
			return Status{}, err
		}
		remoteID = created.ID
	case 1:
		remoteID = matches[0]
	default:
		return Status{}, domain.Fail("SYNC_AMBIGUOUS_FOLDER", "Google Drive has more than one folder named %s here; rename one or choose another location.", name)
	}
	pair := &Pair{ID: newPairID(), Name: name, LocalRoot: abs, AccountReference: account, Parent: parent, RemoteRootID: remoteID,
		Policy: projects.DefaultPolicy(), CreatedAt: m.opts.Now().UTC().Format(time.RFC3339)}
	if err = m.state.PutPair(pair.ID, pair); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	m.pairs[pair.ID] = pair
	m.rt[pair.ID] = &runtimeState{dirty: true, status: Status{State: "starting", Message: "Starting…", Issues: []Issue{}}}
	m.mu.Unlock()
	_ = m.state.AddActivity(pair.ID, transferstate.SyncActivity{Kind: "sync_added", Path: "", Detail: parent.Name + "/" + name})
	m.notify()
	m.nudge()
	return m.statusOf(pair.ID)
}

func (m *Manager) statusOf(id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pair, rt := m.pairs[id], m.rt[id]
	if pair == nil || rt == nil {
		return Status{}, domain.Fail("SYNC_NOT_FOUND", "This sync no longer exists.")
	}
	s := rt.status
	s.Pair = *pair
	s.LastSyncAt = pair.LastSyncAt
	s.DriveURL = "https://drive.google.com/drive/folders/" + pair.RemoteRootID
	if s.Issues == nil {
		s.Issues = []Issue{}
	}
	return s, nil
}

// List returns every pair's status, by name.
func (m *Manager) List() []Status {
	m.mu.Lock()
	ids := make([]string, 0, len(m.pairs))
	for id := range m.pairs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	out := make([]Status, 0, len(ids))
	for _, id := range ids {
		if s, err := m.statusOf(id); err == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Pair.Name) < strings.ToLower(out[j].Pair.Name) || out[i].Pair.Name == out[j].Pair.Name && out[i].Pair.ID < out[j].Pair.ID
	})
	return out
}

func (m *Manager) Get(id string) (Status, error) { return m.statusOf(id) }

func (m *Manager) update(id string, fn func(*Pair, *runtimeState) error) error {
	m.mu.Lock()
	pair, rt := m.pairs[id], m.rt[id]
	if pair == nil || rt == nil {
		m.mu.Unlock()
		return domain.Fail("SYNC_NOT_FOUND", "This sync no longer exists.")
	}
	if err := fn(pair, rt); err != nil {
		m.mu.Unlock()
		return err
	}
	err := m.state.PutPair(pair.ID, pair)
	m.mu.Unlock()
	m.notify()
	m.nudge()
	return err
}

// Pause stops syncing a pair; a running pass is cancelled safely.
func (m *Manager) Pause(id string) error {
	return m.update(id, func(p *Pair, rt *runtimeState) error {
		p.Paused, p.PauseCode, p.PauseReason = true, "USER_PAUSED", "Sync is paused."
		rt.status.State, rt.status.Message, rt.status.ErrorCode = "paused", p.PauseReason, ""
		if rt.cancel != nil {
			rt.cancel()
		}
		return nil
	})
}

// Resume restarts a paused pair with a full comparison.
func (m *Manager) Resume(id string) error {
	return m.update(id, func(p *Pair, rt *runtimeState) error {
		p.Paused, p.PauseCode, p.PauseReason = false, "", ""
		rt.dirty, rt.snapshot, rt.retryAt = true, nil, time.Time{}
		rt.status.State, rt.status.Message, rt.status.ErrorCode = "starting", "Starting…", ""
		return nil
	})
}

// SyncNow requests an immediate full comparison.
func (m *Manager) SyncNow(id string) error {
	return m.update(id, func(p *Pair, rt *runtimeState) error {
		rt.dirty, rt.snapshot, rt.retryAt = true, nil, time.Time{}
		return nil
	})
}

// ConfirmDeletes approves exactly the deletions the guard reported.
func (m *Manager) ConfirmDeletes(id string) error {
	return m.update(id, func(p *Pair, rt *runtimeState) error {
		if rt.confirm == "" {
			return domain.Fail("SYNC_NOTHING_TO_CONFIRM", "There are no deletions waiting for confirmation.")
		}
		p.ConfirmedDeletes = rt.confirm
		rt.dirty, rt.retryAt = true, time.Time{}
		return nil
	})
}

// RestoreDeletes declines the deletions the guard reported: the deleted items
// are forgotten as synced, so the next pass copies them back from the side
// that still has them instead of deleting them there.
func (m *Manager) RestoreDeletes(id string) error {
	m.mu.Lock()
	rt, pair := m.rt[id], m.pairs[id]
	if rt == nil || pair == nil {
		m.mu.Unlock()
		return domain.Fail("SYNC_NOT_FOUND", "This sync no longer exists.")
	}
	if rt.confirm == "" {
		m.mu.Unlock()
		return domain.Fail("SYNC_NOTHING_TO_CONFIRM", "There are no deletions waiting for confirmation.")
	}
	paths := append([]string{}, rt.pending...)
	m.mu.Unlock()
	m.cycle.Lock()
	defer m.cycle.Unlock()
	for _, p := range paths {
		if err := m.state.DeleteTree(id, p); err != nil {
			return err
		}
	}
	_ = m.state.AddActivity(id, transferstate.SyncActivity{Kind: "restore_deletes", Detail: "Deleted items will be copied back"})
	return m.update(id, func(p *Pair, rt *runtimeState) error {
		rt.confirm, rt.pending, rt.dirty, rt.retryAt = "", nil, true, time.Time{}
		p.ConfirmedDeletes = ""
		return nil
	})
}

// Remove stops syncing and forgets the pair. No file is deleted on either side.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	rt := m.rt[id]
	if rt == nil {
		m.mu.Unlock()
		return domain.Fail("SYNC_NOT_FOUND", "This sync no longer exists.")
	}
	if rt.cancel != nil {
		rt.cancel()
	}
	if p := m.pairs[id]; p != nil {
		p.Paused = true
	}
	m.mu.Unlock()
	m.cycle.Lock()
	defer m.cycle.Unlock()
	if err := m.state.DeletePair(id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.pairs, id)
	delete(m.rt, id)
	m.mu.Unlock()
	m.notify()
	return nil
}

// Activity returns recent events of one pair, or of all pairs.
func (m *Manager) Activity(id string, limit int) ([]transferstate.SyncActivity, error) {
	return m.state.Activity(id, limit)
}

// RunOnce runs one pass now and waits for it (used by the CLI).
func (m *Manager) RunOnce(ctx context.Context, id string, progress Progress) (Result, error) {
	account, waiting := m.account(ctx)
	if account == "" {
		return Result{}, domain.Fail("AUTH_REQUIRED", "%s", waiting)
	}
	stop := context.AfterFunc(ctx, func() {
		m.mu.Lock()
		if rt := m.rt[id]; rt != nil && rt.cancel != nil {
			rt.cancel()
		}
		m.mu.Unlock()
	})
	defer stop()
	return m.runCycle(id, account, progress)
}
