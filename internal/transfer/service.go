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
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
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
type Accounts interface {
	Status(context.Context) (driveauth.Status, error)
}
type Previewer interface {
	PreviewRoot(context.Context, string) (app.Preview, error)
	Preview(context.Context, string) (app.Preview, error)
}
type Destination struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	AccountReference string   `json:"accountReference"`
	Parents          []string `json:"parents,omitempty"`
}
type Entry struct {
	RelativePath string `json:"relativePath"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	Action       string `json:"action"`
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
	ExcludedCount    int      `json:"excludedCount"`
	Entries          []Entry  `json:"entries"`
	ExpiresAt        string   `json:"expiresAt"`
	Warnings         []string `json:"warnings"`
}
type Status struct {
	State          string `json:"state"`
	PlanDigest     string `json:"planDigest,omitempty"`
	TotalFiles     int    `json:"totalFiles"`
	CompletedFiles int    `json:"completedFiles"`
	TotalBytes     int64  `json:"totalBytes"`
	UploadedBytes  int64  `json:"uploadedBytes"`
	CurrentPath    string `json:"currentPath,omitempty"`
	Message        string `json:"message"`
	RemoteFolderID string `json:"remoteFolderId,omitempty"`
}
type approved struct {
	Plan                                        Plan
	Source                                      string
	IsConfig                                    bool
	Preview                                     app.Preview
	Fingerprint, StateDigest, RemoteDigest, Key string
	Destination                                 Destination
	Nodes                                       []transferstate.Node
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
}

func New(local Previewer, provider Provider, accounts Accounts, stateDir string) *Service {
	return &Service{local: local, provider: provider, accounts: accounts, stateDir: stateDir, now: time.Now, status: Status{State: "idle", Message: "Choose a Drive destination and preview the folder upload."}}
}
func busyError() error {
	return domain.Fail("TRANSFER_BUSY", "Wait for the current Drive operation or cancel it.")
}
func (s *Service) Busy() bool     { s.mu.Lock(); defer s.mu.Unlock(); return s.busy }
func (s *Service) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *Service) Invalidate()    { s.mu.Lock(); defer s.mu.Unlock(); s.pending = nil }
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
	folder, err := s.provider.GetFolder(ctx, account, id)
	if err != nil {
		return nil, err
	}
	if !folder.CanAddChildren {
		return nil, domain.Fail("DESTINATION_READ_ONLY", "The selected Drive folder cannot accept files.")
	}
	d := Destination{folder.ID, folder.Name, account, folder.Parents}
	s.mu.Lock()
	s.destination = &d
	s.pending = nil
	s.mu.Unlock()
	return &d, nil
}
func (s *Service) scan(ctx context.Context, source string, isConfig bool) (app.Preview, error) {
	if isConfig {
		return s.local.Preview(ctx, source)
	}
	return s.local.PreviewRoot(ctx, source)
}
func fingerprint(p app.Preview) (string, error) {
	return domain.Digest(struct {
		Source, Config, Rules string
		Entries               []domain.Entry
	}{p.Plan.SourceIdentity, p.Plan.ConfigDigest, p.Plan.RulesDigest, p.Entries})
}
func projectKey(p app.Preview, d Destination) string {
	return domain.HashBytes([]byte(p.Plan.SourceIdentity + "\x00" + d.AccountReference + "\x00" + d.ID))
}
func operationID(key, p, kind, hash string) string {
	return domain.HashBytes([]byte(key + "\x00" + p + "\x00" + kind + "\x00" + hash))
}
func (s *Service) Preview(ctx context.Context, source string, isConfig bool) (Plan, error) {
	ctx, finish, err := s.reserve(ctx)
	if err != nil {
		return Plan{}, err
	}
	defer finish()
	s.mu.Lock()
	s.pending = nil
	s.status = Status{State: "planning", Message: "Reading the local folder and checking the Drive destination."}
	s.mu.Unlock()
	var result Plan
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == nil {
			s.status = Status{State: "idle", Message: "No upload has been approved. Create a new preview."}
		}
	}()
	d := s.CurrentDestination()
	if d == nil {
		return result, domain.Fail("DESTINATION_REQUIRED", "Choose a Drive destination first.")
	}
	if _, err = s.account(ctx, d.AccountReference); err != nil {
		return result, err
	}
	if err = s.checkDestination(ctx, *d); err != nil {
		return result, err
	}
	p, err := s.scan(ctx, source, isConfig)
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
	if err = st.ObserveRules(p.Plan.SourceIdentity, p.Plan.ConfigDigest, p.RuleSources); err != nil {
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
	stateDigest, _ := domain.Digest(state)
	result = Plan{SourceName: filepath.Base(p.SourceRoot), DestinationName: d.Name, DestinationID: d.ID, AccountReference: d.AccountReference, Entries: []Entry{}, Warnings: []string{"Creates an app-managed folder inside the selected destination. Existing files are never overwritten or deleted.", "Active ignore rules apply. Review excluded files before approving.", "Uploads run only after approval. Adding files later requires a new preview and upload."}, ExpiresAt: s.now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)}
	if result.SourceName == "." || result.SourceName == string(filepath.Separator) || result.SourceName == "" {
		return Plan{}, domain.Fail("SOURCE_UNAVAILABLE", "Select a named folder rather than a filesystem root.")
	}
	nodes := []transferstate.Node{{Path: "", Kind: "directory", Name: result.SourceName, OperationID: operationID(key, "", "directory", "")}}
	for _, e := range p.Entries {
		if e.Decision == "exclude" {
			result.ExcludedCount++
			continue
		}
		nodes = append(nodes, transferstate.Node{Path: e.Path, Kind: e.Kind, Name: path.Base(e.Path), OperationID: operationID(key, e.Path, e.Kind, e.SHA256), SHA256: e.SHA256, Size: e.Size})
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
	remote := map[string]drive.Object{}
	parents := map[string]bool{"": true}
	for i, n := range nodes {
		if n.Path != "" {
			parent := path.Dir(n.Path)
			if parent == "." {
				parent = ""
			}
			if !parents[parent] {
				return Plan{}, domain.Fail("PLAN_INVALID", "An included entry has an excluded parent folder.")
			}
		}
		if n.Kind == "directory" {
			parents[n.Path] = true
		}
		action := "upload"
		if n.Kind == "directory" {
			action = "create"
		}
		if old, ok := state.Nodes[n.OperationID]; ok {
			if old.Path != n.Path || old.Kind != n.Kind || old.SHA256 != n.SHA256 || old.Size != n.Size {
				return Plan{}, domain.Fail("STATE_INVALID", "The saved transfer identity does not match the source.")
			}
			n = old
			o, e := s.provider.GetObject(ctx, d.AccountReference, n.ID)
			if e != nil {
				if domain.ErrorCode(e) != "DRIVE_NOT_FOUND" || n.Status == "verified" {
					return Plan{}, e
				}
				action = "resume"
			} else {
				if e = verify(n, o); e != nil {
					return Plan{}, e
				}
				remote[n.OperationID] = o
				action = "skip"
			}
		} else if n.Kind == "file" {
			for _, old := range state.Nodes {
				if old.Path == n.Path {
					n.Name = path.Base(n.Path) + ".ledgesync-" + n.OperationID[:12]
					action = "keep-both"
					break
				}
			}
		}
		nodes[i] = n
		result.Entries = append(result.Entries, Entry{n.Path, n.Kind, n.Size, action})
		if n.Kind == "file" {
			result.FileCount++
			result.TotalBytes += n.Size
		} else {
			result.FolderCount++
		}
	}
	remoteDigest, _ := domain.Digest(remote)
	result.ExpiresAt = s.now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	a := &approved{Plan: result, Source: source, IsConfig: isConfig, Preview: p, Fingerprint: fp, StateDigest: stateDigest, RemoteDigest: remoteDigest, Key: key, Destination: *d, Nodes: nodes}
	result.PlanDigest, err = domain.Digest(a)
	if err != nil {
		return Plan{}, err
	}
	a.Plan = result
	s.mu.Lock()
	s.pending = a
	s.status = Status{State: "awaiting_approval", PlanDigest: result.PlanDigest, TotalFiles: result.FileCount, TotalBytes: result.TotalBytes, Message: "Review the complete folder upload, then approve this exact preview."}
	s.mu.Unlock()
	return result, nil
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
	s.status = Status{State: "uploading", PlanDigest: digest, TotalFiles: a.Plan.FileCount, TotalBytes: a.Plan.TotalBytes, Message: "Revalidating the approved upload before creating remote files."}
	initial := s.status
	s.mu.Unlock()
	go func() {
		err := s.execute(ctx, a)
		wasCancelled := ctx.Err() != nil
		s.mu.Lock()
		defer s.mu.Unlock()
		cancel()
		s.busy = false
		s.cancel = nil
		if err != nil {
			s.status.State = "failed"
			s.status.Message = safeMessage(err)
			if wasCancelled {
				s.status.State = "cancelled"
				s.status.Message = "Upload stopped. Completed files remain in Drive; create a new preview to reconcile and continue."
			} else if code := domain.ErrorCode(err); code == "UNKNOWN_REMOTE_RESULT" || code == "REMOTE_CHANGED" || code == "SOURCE_CHANGED" || code == "STATE_UNAVAILABLE" {
				s.status.State = "needs_review"
			}
		} else {
			s.status.State = "succeeded"
			s.status.Message = "Folder upload completed. Every included file was verified in Google Drive."
		}
		s.status.CurrentPath = ""
		close(done)
	}()
	return initial, nil
}
func safeMessage(err error) string {
	if _, ok := err.(*domain.Error); ok {
		return err.Error()
	}
	return "Upload stopped. Check the connection and create a new preview before retrying."
}
func (s *Service) change(fn func(*Status)) { s.mu.Lock(); defer s.mu.Unlock(); fn(&s.status) }
func runID() string                        { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
