// Package transfer implements explicit, journaled, create-only Drive folder copies.
// Desktop and headless transports share this application boundary.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

type Provider interface {
	GetFolder(context.Context, string, string) (drive.Folder, error)
	GetObject(context.Context, string, string) (drive.Object, error)
	GenerateIDs(context.Context, string, int) ([]string, error)
	CreateFolder(context.Context, string, string, string, string, string) (drive.Object, error)
	Upload(context.Context, string, string, string, string, string, io.ReadSeeker, int64, string) (drive.Object, error)
}

// ChildLister is optional. Providers that list a folder's children let previews
// verify many recorded objects with one request per folder instead of per file.
type ChildLister interface {
	ListChildren(context.Context, string, string) ([]drive.Object, error)
}

type Accounts interface {
	Status(context.Context) (driveauth.Status, error)
}
type Previewer interface {
	PreviewRoot(context.Context, string) (app.Preview, error)
	Preview(context.Context, string) (app.Preview, error)
	Scan(context.Context, config.Config) (app.Preview, error)
}

// Source selects what a preview scans: a folder with the default policy, an
// explicit configuration file, or a saved project's inline configuration.
type Source struct {
	Root       string
	ConfigPath string
	Config     *config.Config
}

// MyDriveID is Drive's alias for the user's My Drive root folder. With the
// drive.file scope the root's own metadata is not readable, but it accepts children.
const MyDriveID = "root"

type Destination struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	AccountReference string   `json:"accountReference"`
	Parents          []string `json:"parents,omitempty"`
	MyDrive          bool     `json:"myDrive,omitempty"`
}

// Plan entry actions.
const (
	ActionCreate      = "create"
	ActionUpload      = "upload"
	ActionKeepBoth    = "keep-both"
	ActionSkip        = "skip"
	ActionResume      = "resume"
	ActionRecreate    = "recreate"
	ActionPaused      = "paused"
	ActionUnsupported = "unsupported"
)

type Entry struct {
	RelativePath string `json:"relativePath"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	Action       string `json:"action"`
	Note         string `json:"note,omitempty"`
}
type Plan struct {
	PlanDigest       string   `json:"planDigest"`
	SourceName       string   `json:"sourceName"`
	DestinationName  string   `json:"destinationName"`
	DestinationID    string   `json:"destinationId"`
	AccountReference string   `json:"accountReference"`
	FileCount        int      `json:"fileCount"`
	FolderCount      int      `json:"folderCount"`
	TotalBytes       int64    `json:"totalBytes"`
	TransferBytes    int64    `json:"transferBytes"`
	NewFiles         int      `json:"newFiles"`
	ChangedFiles     int      `json:"changedFiles"`
	UnchangedFiles   int      `json:"unchangedFiles"`
	RecreatedItems   int      `json:"recreatedItems"`
	PausedFiles      int      `json:"pausedFiles"`
	ExcludedCount    int      `json:"excludedCount"`
	UnsupportedCount int      `json:"unsupportedCount"`
	ConflictPolicy   string   `json:"conflictPolicy"`
	SourceIdentity   string   `json:"sourceIdentity"`
	ConfigDigest     string   `json:"configDigest"`
	RulesDigest      string   `json:"rulesDigest"`
	SourceDigest     string   `json:"sourceDigest"`
	Entries          []Entry  `json:"entries"`
	ExpiresAt        string   `json:"expiresAt"`
	Warnings         []string `json:"warnings"`
}

// Issue is a bounded, redacted per-item outcome. Paths are source-relative.
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Run states: idle, planning, awaiting_approval, uploading, verifying, succeeded,
// partial (approved items skipped because the source changed), failed,
// cancelled and needs_review (remote result or state requires reconciliation).
type Status struct {
	State          string  `json:"state"`
	PlanDigest     string  `json:"planDigest,omitempty"`
	RunID          string  `json:"runId,omitempty"`
	TotalFiles     int     `json:"totalFiles"`
	CompletedFiles int     `json:"completedFiles"`
	SkippedFiles   int     `json:"skippedFiles"`
	PausedFiles    int     `json:"pausedFiles"`
	TotalBytes     int64   `json:"totalBytes"`
	UploadedBytes  int64   `json:"uploadedBytes"`
	TransferBytes  int64   `json:"transferBytes"`
	SentBytes      int64   `json:"sentBytes"`
	CurrentPath    string  `json:"currentPath,omitempty"`
	Message        string  `json:"message"`
	ErrorCode      string  `json:"errorCode,omitempty"`
	RemoteFolderID string  `json:"remoteFolderId,omitempty"`
	Issues         []Issue `json:"issues,omitempty"`
	StartedAt      string  `json:"startedAt,omitempty"`
	FinishedAt     string  `json:"finishedAt,omitempty"`
}

// MaxIssues bounds per-run diagnostics kept in memory and in the journal.
const MaxIssues = 200

type approved struct {
	Plan                     Plan
	Source                   string
	IsConfig                 bool
	Inline                   bool
	Config                   config.Config
	Preview                  app.Preview
	Fingerprint, StateDigest string
	RemoteDigest, Key        string
	Destination              Destination
	Nodes                    []transferstate.Node
	Actions                  map[string]string
}
type Service struct {
	mu          sync.Mutex
	provider    Provider
	accounts    Accounts
	local       Previewer
	stateDir    string
	destination *Destination
	pending     *approved
	status      Status
	busy        bool
	cancel      context.CancelFunc
	done        chan struct{}
	now         func() time.Time
	observers   []func(Status)
}

func New(local Previewer, provider Provider, accounts Accounts, stateDir string) *Service {
	return &Service{local: local, provider: provider, accounts: accounts, stateDir: stateDir, now: time.Now, status: Status{State: "idle", Message: "Choose a Drive destination and preview the folder upload."}}
}
func busyError() error {
	return domain.Fail("TRANSFER_BUSY", "Wait for the current Drive operation or cancel it.")
}
func (s *Service) Busy() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.busy }
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Issues = append([]Issue(nil), s.status.Issues...)
	return st
}
func (s *Service) Invalidate() { s.mu.Lock(); defer s.mu.Unlock(); s.pending = nil }

// StateDirectory returns the private journal directory used by this service.
func (s *Service) StateDirectory() string { return s.stateDir }

// OnFinish registers a callback invoked with each terminal run status.
func (s *Service) OnFinish(fn func(Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observers = append(s.observers, fn)
}

func (s *Service) CurrentDestination() *Destination {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.destination == nil {
		return nil
	}
	d := *s.destination
	d.Parents = append([]string{}, d.Parents...)
	return &d
}
func (s *Service) reserve(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return nil, nil, busyError()
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.busy, s.cancel, s.done = true, cancel, done
	return ctx, func() {
		cancel()
		s.mu.Lock()
		s.busy = false
		s.cancel = nil
		close(done)
		s.mu.Unlock()
	}, nil
}
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) CancelAndWait() {
	s.mu.Lock()
	done := s.done
	busy := s.busy
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if busy && done != nil {
		<-done
	}
}
func (s *Service) account(ctx context.Context, expected string) (string, error) {
	a, err := s.accounts.Status(ctx)
	if err != nil {
		return "", err
	}
	if a.State != "connected" || a.Account == nil || a.Account.Reference == "" {
		return "", domain.Fail("AUTH_REQUIRED", "Connect Google Drive before uploading.")
	}
	if expected != "" && a.Account.Reference != expected {
		return "", domain.Fail("ACCOUNT_CHANGED", "The connected Google account changed; select the destination again.")
	}
	return a.Account.Reference, nil
}

// SetDestination validates a destination folder. "root" selects My Drive even
// when the restricted drive.file scope cannot read the root folder's metadata.
func (s *Service) SetDestination(ctx context.Context, id, expectedAccount string) (*Destination, error) {
	ctx, finish, err := s.reserve(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	account, err := s.account(ctx, expectedAccount)
	if err != nil {
		return nil, err
	}
	d, err := s.resolveDestination(ctx, account, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.destination = &d
	s.pending = nil
	s.mu.Unlock()
	return &d, nil
}

func (s *Service) resolveDestination(ctx context.Context, account, id string) (Destination, error) {
	folder, err := s.provider.GetFolder(ctx, account, id)
	if id == MyDriveID && domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
		return Destination{ID: MyDriveID, Name: "My Drive", AccountReference: account, MyDrive: true}, nil
	}
	if err != nil {
		return Destination{}, err
	}
	if !folder.CanAddChildren {
		return Destination{}, domain.Fail("DESTINATION_READ_ONLY", "The selected Drive folder cannot accept files.")
	}
	name := folder.Name
	if id == MyDriveID {
		name = "My Drive"
	}
	return Destination{ID: folder.ID, Name: name, AccountReference: account, Parents: folder.Parents, MyDrive: id == MyDriveID}, nil
}

// RestoreDestination re-selects a saved destination after validating it again.
func (s *Service) RestoreDestination(ctx context.Context, d Destination) (*Destination, error) {
	id := d.ID
	if d.MyDrive {
		id = MyDriveID
	}
	return s.SetDestination(ctx, id, d.AccountReference)
}

func (s *Service) scan(ctx context.Context, src Source) (app.Preview, error) {
	switch {
	case src.Config != nil:
		return s.local.Scan(ctx, *src.Config)
	case src.ConfigPath != "":
		return s.local.Preview(ctx, src.ConfigPath)
	default:
		return s.local.PreviewRoot(ctx, src.Root)
	}
}

// Fingerprint returns the approval binding of a local preview. Automatic jobs
// compare it to detect local changes without contacting Drive.
func Fingerprint(p app.Preview) (string, error) { return fingerprint(p) }

// fingerprint binds approval to the selected content, rules and configuration.
// Excluded entries and directory timestamps are deliberately not part of it.
func fingerprint(p app.Preview) (string, error) {
	type item struct {
		Path, Kind, Decision, SHA256 string
		Size                         int64
	}
	items := []item{}
	for _, e := range p.Entries {
		if e.Decision == app.DecisionExclude {
			continue
		}
		items = append(items, item{e.Path, e.Kind, e.Decision, e.SHA256, e.Size})
	}
	return domain.Digest(struct {
		Source, Config, Rules, Profile string
		Entries                        []item
	}{p.Plan.SourceIdentity, p.Plan.ConfigDigest, p.Plan.RulesDigest, app.TraversalProfile, items})
}
func projectKey(p app.Preview, d Destination) string {
	return ProjectKey(p.Plan.SourceIdentity, d.AccountReference, d.ID)
}

// ProjectKey identifies a source/account/destination pair in the journal. It is
// derived from identities only, so a copy can be restored after the source is lost.
func ProjectKey(sourceIdentity, account, destinationID string) string {
	return domain.HashBytes([]byte(sourceIdentity + "\x00" + account + "\x00" + destinationID))
}

// operationID is stable for a project, path, kind and content. Generation 0 keeps
// the original alpha identity; later generations replace a recorded copy that is
// missing or was changed in Drive without ever reusing or modifying that object.
func operationID(key, p, kind, hash string, generation int) string {
	if generation == 0 {
		return domain.HashBytes([]byte(key + "\x00" + p + "\x00" + kind + "\x00" + hash))
	}
	return domain.HashBytes([]byte(key + "\x00" + p + "\x00" + kind + "\x00" + hash + "\x00generation:" + strconv.Itoa(generation)))
}

// MaxGenerations bounds replacement copies for one path and content.
const MaxGenerations = 1000

func parentOf(p string) string {
	if p == "" {
		return ""
	}
	parent := path.Dir(p)
	if parent == "." {
		return ""
	}
	return parent
}

// configFor returns the exact configuration a preview was computed with.
func configFor(p app.Preview, src Source) (config.Config, error) {
	var c config.Config
	switch {
	case src.Config != nil:
		c = *src.Config
	case src.ConfigPath != "":
		loaded, err := config.Load(src.ConfigPath)
		if err != nil {
			return config.Config{}, err
		}
		c = loaded
	default:
		c = config.Default(p.SourceRoot)
	}
	c.Source.Root = p.SourceRoot
	if d, err := domain.Digest(c); err != nil || d != p.Plan.ConfigDigest {
		return config.Config{}, domain.Fail("CONFIG_CHANGED", "The project configuration changed while it was being read. Preview again.")
	}
	return c, nil
}

func (s *Service) Preview(ctx context.Context, source string, isConfig bool) (Plan, error) {
	if isConfig {
		return s.PreviewSource(ctx, Source{ConfigPath: source})
	}
	return s.PreviewSource(ctx, Source{Root: source})
}

func (s *Service) PreviewSource(ctx context.Context, src Source) (Plan, error) {
	ctx, finish, err := s.reserve(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer finish()
	s.mu.Lock()
	s.pending = nil
	s.status = Status{State: "planning", Message: "Reading the local folder and checking the Drive destination."}
	s.mu.Unlock()
	var planErr error
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == nil {
			s.status = Status{State: "idle", Message: "No upload has been approved. Create a new preview."}
			if planErr != nil {
				s.status.ErrorCode = domain.ErrorCode(planErr)
			}
		}
	}()
	result, err := s.preview(ctx, src)
	planErr = err
	return result, err
}

func (s *Service) preview(ctx context.Context, src Source) (Plan, error) {
	var result Plan
	d := s.CurrentDestination()
	if d == nil {
		return result, domain.Fail("DESTINATION_REQUIRED", "Choose a Drive destination first.")
	}
	if _, err := s.account(ctx, d.AccountReference); err != nil {
		return result, err
	}
	if err := s.checkDestination(ctx, *d); err != nil {
		return result, err
	}
	p, err := s.scan(ctx, src)
	if err != nil {
		return result, err
	}
	cfg, err := configFor(p, src)
	if err != nil {
		return result, err
	}
	fp, err := fingerprint(p)
	if err != nil {
		return result, err
	}
	st, err := transferstate.Open(s.stateDir, p.SourceRoot)
	if err != nil {
		return result, err
	}
	defer st.Close()
	if err = st.ObserveRules(p.Plan.SourceIdentity, p.Plan.ConfigDigest+"|"+app.TraversalProfile, p.RuleSources); err != nil {
		return result, err
	}
	key := projectKey(p, *d)
	state, err := st.Load(key)
	if err != nil {
		return result, err
	}
	if state.SourceIdentity != "" && (state.SourceIdentity != p.Plan.SourceIdentity || state.AccountReference != d.AccountReference || state.DestinationID != d.ID) {
		return result, domain.Fail("STATE_INVALID", "The saved project identity does not match this source and destination.")
	}
	stateDigest, err := domain.Digest(state)
	if err != nil {
		return result, err
	}
	result = Plan{SourceName: filepath.Base(p.SourceRoot), DestinationName: d.Name, DestinationID: d.ID, AccountReference: d.AccountReference, ConflictPolicy: cfg.Sync.ConflictPolicy, SourceIdentity: p.Plan.SourceIdentity, ConfigDigest: p.Plan.ConfigDigest, RulesDigest: p.Plan.RulesDigest, SourceDigest: fp, Entries: []Entry{}}
	if result.SourceName == "." || result.SourceName == string(filepath.Separator) || result.SourceName == "" || filepath.VolumeName(p.SourceRoot)+string(filepath.Separator) == p.SourceRoot {
		return Plan{}, domain.Fail("SOURCE_UNAVAILABLE", "Select a named folder rather than a filesystem root.")
	}
	nodes, actions, remote, err := s.plan(ctx, key, *d, p, cfg, state, &result)
	if err != nil {
		return Plan{}, err
	}
	result.Warnings = planWarnings(result, *d)
	remoteDigest, err := domain.Digest(remote)
	if err != nil {
		return Plan{}, err
	}
	result.ExpiresAt = s.now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	a := &approved{Plan: result, Source: src.Root, IsConfig: src.ConfigPath != "", Inline: src.Config != nil, Config: cfg, Preview: p, Fingerprint: fp, StateDigest: stateDigest, RemoteDigest: remoteDigest, Key: key, Destination: *d, Nodes: nodes, Actions: actions}
	if a.IsConfig {
		a.Source = src.ConfigPath
	}
	result.PlanDigest, err = domain.Digest(a)
	if err != nil {
		return Plan{}, err
	}
	a.Plan = result
	s.mu.Lock()
	s.pending = a
	s.status = Status{State: "awaiting_approval", PlanDigest: result.PlanDigest, TotalFiles: result.FileCount, TotalBytes: result.TotalBytes, TransferBytes: result.TransferBytes, Message: "Review the complete folder upload, then approve this exact preview."}
	s.mu.Unlock()
	return result, nil
}

func planWarnings(p Plan, d Destination) []string {
	w := []string{"Copies into a LedgeSync-managed folder inside " + d.Name + ". Existing Drive files are never overwritten or deleted.", "Active ignore rules apply. Review excluded files before approving.", "Only this preview is approved. Files added or changed later are copied by a new preview, or by an automatic job you enable."}
	if p.UnsupportedCount > 0 {
		w = append(w, strconv.Itoa(p.UnsupportedCount)+" symbolic link or special item(s) are listed but never followed or copied.")
	}
	if p.ChangedFiles > 0 {
		w = append(w, strconv.Itoa(p.ChangedFiles)+" changed file(s) are copied as separate .ledgesync- versions; earlier copies remain.")
	}
	if p.PausedFiles > 0 {
		w = append(w, strconv.Itoa(p.PausedFiles)+" changed file(s) are paused by the project's conflict policy and will not be copied.")
	}
	if p.RecreatedItems > 0 {
		w = append(w, strconv.Itoa(p.RecreatedItems)+" previously copied item(s) are missing, trashed, moved or renamed in Drive. New copies are made; the existing Drive items are left unchanged.")
	}
	return w
}

// remoteIndex verifies recorded objects using one listing per known folder.
type remoteIndex struct {
	s       *Service
	account string
	listed  map[string]drive.Object
	done    map[string]bool
}

func (r *remoteIndex) list(ctx context.Context, folder string) error {
	lister, ok := r.s.provider.(ChildLister)
	if !ok || folder == "" || r.done[folder] {
		return nil
	}
	r.done[folder] = true
	children, err := lister.ListChildren(ctx, r.account, folder)
	if domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
		return nil
	}
	if err != nil {
		return err
	}
	for _, o := range children {
		r.listed[o.ID] = o
	}
	return nil
}

func (r *remoteIndex) get(ctx context.Context, id string) (drive.Object, error) {
	if o, ok := r.listed[id]; ok {
		return o, nil
	}
	return r.s.provider.GetObject(ctx, r.account, id)
}

// plan reconciles the local selection with recorded identities. Recorded objects
// that are missing, trashed, renamed or moved are never modified or adopted: the
// affected item, and for folders its whole subtree, gets a new copy generation.
func (s *Service) plan(ctx context.Context, key string, d Destination, p app.Preview, cfg config.Config, state transferstate.Project, result *Plan) ([]transferstate.Node, map[string]string, map[string]drive.Object, error) {
	nodes := []transferstate.Node{{Path: "", Kind: "directory", Name: result.SourceName}}
	for _, e := range p.Entries {
		switch e.Decision {
		case app.DecisionExclude:
			result.ExcludedCount++
		case app.DecisionUnsupported:
			result.UnsupportedCount++
			result.Entries = append(result.Entries, Entry{e.Path, e.Kind, 0, ActionUnsupported, "Links and special files are never followed or copied."})
		default:
			if e.Kind != "file" && e.Kind != "directory" {
				return nil, nil, nil, domain.Fail("PLAN_INVALID", "An unsupported item was selected for upload.")
			}
			nodes = append(nodes, transferstate.Node{Path: e.Path, Kind: e.Kind, Name: path.Base(e.Path), SHA256: e.SHA256, Size: e.Size})
		}
	}
	// Parents are always created before children; empty included directories remain.
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Path == "" {
			return nodes[j].Path != ""
		}
		if nodes[j].Path == "" {
			return false
		}
		return nodes[i].Path < nodes[j].Path
	})
	byPath := map[string][]transferstate.Node{}
	for _, n := range state.Nodes {
		byPath[n.Path] = append(byPath[n.Path], n)
	}
	latest := func(n transferstate.Node) (transferstate.Node, bool, int, error) {
		var found transferstate.Node
		known := false
		for generation := 0; generation < MaxGenerations; generation++ {
			old, ok := state.Nodes[operationID(key, n.Path, n.Kind, n.SHA256, generation)]
			if !ok {
				return found, known, generation, nil
			}
			if old.Path != n.Path || old.Kind != n.Kind || old.SHA256 != n.SHA256 || old.Size != n.Size || old.Generation != generation {
				return found, false, 0, domain.Fail("STATE_INVALID", "The saved transfer identity does not match the source.")
			}
			found, known = old, true
		}
		return found, false, 0, domain.Fail("STATE_INVALID", "Too many replacement copies are recorded for one item.")
	}
	index := &remoteIndex{s: s, account: d.AccountReference, listed: map[string]drive.Object{}, done: map[string]bool{}}
	remote := map[string]drive.Object{}
	actions := map[string]string{}
	parents := map[string]bool{"": true}
	fresh := map[string]bool{}
	recordedFolder := map[string]string{}
	for i, n := range nodes {
		if n.Path != "" && !parents[parentOf(n.Path)] {
			return nil, nil, nil, domain.Fail("PLAN_INVALID", "An included entry has an excluded parent folder.")
		}
		if n.Kind == "directory" {
			parents[n.Path] = true
		}
		old, known, next, err := latest(n)
		if err != nil {
			return nil, nil, nil, err
		}
		action := ActionUpload
		if n.Kind == "directory" {
			action = ActionCreate
		}
		note := ""
		replace := n.Path != "" && fresh[parentOf(n.Path)]
		if known && !replace {
			if parentID := recordedFolder[parentOf(n.Path)]; n.Path != "" && parentID != "" {
				if err = index.list(ctx, parentID); err != nil {
					return nil, nil, nil, err
				}
			}
			o, e := index.get(ctx, old.ID)
			switch {
			case e == nil && verify(old, o) == nil:
				n, action = old, ActionSkip
				if old.Status != "verified" {
					// An interrupted run's object exists; finish its verification.
					action = ActionResume
				}
				remote[old.OperationID] = o
			case e == nil:
				replace, note = true, "The earlier copy was trashed, renamed, moved or changed in Drive and is left unchanged."
			case domain.ErrorCode(e) == "DRIVE_NOT_FOUND" && old.Status == "intent":
				n, action = old, ActionResume
			case domain.ErrorCode(e) == "DRIVE_NOT_FOUND":
				replace, note = true, "The earlier copy is no longer available in Drive."
			default:
				return nil, nil, nil, e
			}
		}
		if replace {
			n.Generation, n.ID, n.ParentID, n.Status, n.MD5 = next, "", "", "", ""
			if known {
				action = ActionRecreate
				result.RecreatedItems++
			}
			if note == "" && known {
				note = "Its parent folder is copied again."
			}
		}
		if n.OperationID == "" {
			n.OperationID = operationID(key, n.Path, n.Kind, n.SHA256, n.Generation)
		}
		if n.Path == "" && n.Name == "" {
			n.Name = result.SourceName
		}
		if n.Kind == "directory" {
			if replace {
				fresh[n.Path] = true
			} else if n.ID != "" && action != ActionResume {
				recordedFolder[n.Path] = n.ID
			}
		}
		if n.Kind == "file" && !known && !replace {
			for _, other := range byPath[n.Path] {
				if other.SHA256 != n.SHA256 {
					action = ActionKeepBoth
					break
				}
			}
			if action == ActionKeepBoth {
				if cfg.Sync.ConflictPolicy == "pause" {
					action, note = ActionPaused, "Changed since the last copy. The conflict policy pauses changed files; nothing is copied."
				} else {
					n.Name = path.Base(n.Path) + ".ledgesync-" + n.OperationID[:12]
					note = "Changed since the last copy; the earlier copy remains."
				}
			}
		}
		nodes[i] = n
		actions[n.OperationID] = action
		result.Entries = append(result.Entries, Entry{n.Path, n.Kind, n.Size, action, note})
		if n.Kind == "file" {
			switch action {
			case ActionSkip:
				result.UnchangedFiles++
			case ActionKeepBoth:
				result.ChangedFiles++
			case ActionPaused:
				result.PausedFiles++
			case ActionUpload:
				result.NewFiles++
			}
			if action == ActionPaused {
				continue
			}
			result.FileCount++
			result.TotalBytes += n.Size
			if action != ActionSkip {
				result.TransferBytes += n.Size
			}
		} else {
			result.FolderCount++
		}
	}
	executable := nodes[:0]
	for _, n := range nodes {
		if actions[n.OperationID] != ActionPaused {
			executable = append(executable, n)
		}
	}
	sort.SliceStable(result.Entries, func(i, j int) bool { return result.Entries[i].RelativePath < result.Entries[j].RelativePath })
	return executable, actions, remote, nil
}

func (s *Service) Start(ctx context.Context, digest string) (Status, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return Status{}, busyError()
	}
	a := s.pending
	if a == nil || digest == "" || a.Plan.PlanDigest != digest {
		s.mu.Unlock()
		return Status{}, domain.Fail("PLAN_REQUIRED", "Create and approve a fresh upload preview.")
	}
	expires, err := time.Parse(time.RFC3339Nano, a.Plan.ExpiresAt)
	if err != nil || !s.now().Before(expires) {
		s.pending = nil
		s.mu.Unlock()
		return Status{}, domain.Fail("PLAN_EXPIRED", "This upload preview expired. Create a new preview.")
	}
	s.pending = nil
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.busy, s.cancel, s.done = true, cancel, done
	run := runID()
	s.status = Status{State: "uploading", PlanDigest: digest, RunID: run, TotalFiles: a.Plan.FileCount, TotalBytes: a.Plan.TotalBytes, TransferBytes: a.Plan.TransferBytes, PausedFiles: a.Plan.PausedFiles, Message: "Revalidating the approved upload before creating remote files.", StartedAt: s.now().UTC().Format(time.RFC3339)}
	initial := s.status
	s.mu.Unlock()
	go func() {
		err := s.execute(ctx, a, run)
		wasCancelled := ctx.Err() != nil
		s.mu.Lock()
		cancel()
		s.busy = false
		s.cancel = nil
		if err != nil {
			s.status.State = "failed"
			s.status.ErrorCode = domain.ErrorCode(err)
			s.status.Message = safeMessage(err)
			if wasCancelled {
				s.status.State = "cancelled"
				s.status.ErrorCode = "CANCELLED"
				s.status.Message = "Upload stopped. Completed files remain in Drive; create a new preview to reconcile and continue."
			} else if code := domain.ErrorCode(err); code == "UNKNOWN_REMOTE_RESULT" || code == "REMOTE_CHANGED" || code == "STATE_UNAVAILABLE" || code == "DRIVE_VERIFICATION_FAILED" || code == "DRIVE_IDENTITY_MISMATCH" {
				s.status.State = "needs_review"
			}
		} else if len(s.status.Issues) > 0 || s.status.SkippedFiles > 0 {
			s.status.State = "partial"
			s.status.Message = "Copied and verified the approved items that were unchanged. Some files changed after the preview and were not copied; preview again to copy their current versions."
		} else {
			s.status.State = "succeeded"
			s.status.Message = "Folder upload completed. Every approved item was verified in Google Drive."
			if s.status.PausedFiles > 0 {
				s.status.Message = "Folder upload completed. Changed files paused by the conflict policy were not copied."
			}
		}
		s.status.CurrentPath = ""
		s.status.SentBytes = 0
		s.status.FinishedAt = s.now().UTC().Format(time.RFC3339)
		final := s.status
		final.Issues = append([]Issue(nil), s.status.Issues...)
		observers := append([]func(Status){}, s.observers...)
		close(done)
		s.mu.Unlock()
		for _, fn := range observers {
			fn(final)
		}
	}()
	return initial, nil
}
func safeMessage(err error) string {
	if e, ok := err.(*domain.Error); ok {
		return e.Message
	}
	return "Upload stopped. Check the connection and create a new preview before retrying."
}
func (s *Service) change(fn func(*Status)) { s.mu.Lock(); defer s.mu.Unlock(); fn(&s.status) }
func runID() string                        { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
