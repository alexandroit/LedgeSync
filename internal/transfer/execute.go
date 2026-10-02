package transfer

import (
	"context"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

const folderMIME = "application/vnd.google-apps.folder"

// destinationCheckInterval bounds how long a run trusts the destination check.
const destinationCheckInterval = time.Minute

// newSourceCheckInterval bounds directory probes for newly created rule files.
// Known rule files and the configuration are checked before every operation.
const newSourceCheckInterval = time.Second

func sameParents(a, b []string) bool {
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}
func (s *Service) checkDestination(ctx context.Context, d Destination) error {
	if d.ID == MyDriveID {
		// drive.file cannot read My Drive's metadata. Placement of the managed
		// folder is confirmed by the provider when it is created and verified.
		return nil
	}
	f, err := s.provider.GetFolder(ctx, d.AccountReference, d.ID)
	if domain.ErrorCode(err) == "DRIVE_NOT_FOUND" || domain.ErrorCode(err) == "DRIVE_NOT_FOLDER" {
		return domain.Fail("DESTINATION_UNAVAILABLE", "The selected Drive folder is missing, trashed or no longer available to LedgeSync. Choose the destination again.")
	}
	if err != nil {
		return err
	}
	if f.ID != d.ID || (!d.MyDrive && f.Name != d.Name) || !f.CanAddChildren || !sameParents(f.Parents, d.Parents) {
		return domain.Fail("DESTINATION_CHANGED", "The Drive destination changed or is no longer writable. Select it again and create a new preview.")
	}
	return nil
}

// verify checks a recorded node against Drive. A node whose parent is the My
// Drive alias accepts any single parent here; such nodes are only reconciled
// through the provider, which confirms membership in My Drive.
func verify(n transferstate.Node, o drive.Object) error {
	if o.ID != n.ID || o.Name != n.Name || o.Trashed || len(o.Parents) != 1 || (n.ParentID != MyDriveID && o.Parents[0] != n.ParentID) || o.AppProperties["ledgesyncOperation"] != n.OperationID {
		return domain.Fail("REMOTE_CHANGED", "A recorded Drive object changed. Review the destination before continuing.")
	}
	if n.Kind == "directory" {
		if o.MimeType != folderMIME {
			return domain.Fail("REMOTE_CHANGED", "A recorded Drive folder changed type.")
		}
	} else if o.MimeType == folderMIME || o.Size != n.Size || n.MD5 == "" || o.MD5 != n.MD5 {
		return domain.Fail("REMOTE_CHANGED", "A Drive file does not match the approved content checksum and size.")
	}
	return nil
}

// sourceGuard rechecks the approved policy before every externally visible
// mutation without rereading every rule file or rehashing unrelated content.
type sourceGuard struct {
	tree                    *discovery.Tree
	known, basenames, roots []string
	configPath              string
	configInfo              os.FileInfo
	lastFullCheck           time.Time
	now                     func() time.Time
}

func (g *sourceGuard) check(ctx context.Context, full bool) error {
	if ctx.Err() != nil {
		return domain.Fail("CANCELLED", "Upload cancelled.")
	}
	if err := g.tree.RootUnchanged(); err != nil {
		return err
	}
	if g.configPath != "" {
		info, err := os.Lstat(g.configPath)
		if err != nil || !discovery.Same(g.configInfo, info) {
			return domain.Fail("CONFIG_CHANGED", "The project configuration changed; approve a new preview.")
		}
	}
	basenames, roots := []string(nil), []string(nil)
	if full || g.now().Sub(g.lastFullCheck) >= newSourceCheckInterval {
		basenames, roots = g.basenames, g.roots
		g.lastFullCheck = g.now()
	}
	if err := g.tree.CheckPolicySources(ctx, g.known, basenames, roots); err != nil {
		if domain.ErrorCode(err) == "CANCELLED" {
			return err
		}
		return domain.Fail("RULES_CHANGED", "Ignore rules changed during the upload; approve a new preview.")
	}
	return nil
}

// idPool reserves Drive identifiers in bounded batches. Unused reservations are harmless.
type idPool struct {
	provider  Provider
	account   string
	remaining int
	ids       []string
}

func (p *idPool) next(ctx context.Context) (string, error) {
	if len(p.ids) == 0 {
		count := min(max(p.remaining, 1), 1000)
		ids, err := p.provider.GenerateIDs(ctx, p.account, count)
		if err != nil {
			return "", err
		}
		if len(ids) != count {
			return "", domain.Fail("DRIVE_INVALID_RESPONSE", "Drive could not reserve upload identities.")
		}
		p.ids = ids
	}
	id := p.ids[0]
	p.ids = p.ids[1:]
	p.remaining--
	if id == "" {
		return "", domain.Fail("DRIVE_INVALID_RESPONSE", "Drive could not reserve an upload identity.")
	}
	return id, nil
}

// progressReader reports how much of the current file has been read for upload.
type progressReader struct {
	io.ReadSeeker
	mu       sync.Mutex
	position int64
	report   func(int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	r.mu.Lock()
	r.position += int64(n)
	position := r.position
	r.mu.Unlock()
	r.report(position)
	return n, err
}
func (r *progressReader) Seek(offset int64, whence int) (int64, error) {
	position, err := r.ReadSeeker.Seek(offset, whence)
	if err == nil {
		r.mu.Lock()
		r.position = position
		r.mu.Unlock()
		r.report(position)
	}
	return position, err
}

func (s *Service) issue(path, code, message string) {
	s.change(func(v *Status) {
		v.SkippedFiles++
		if len(v.Issues) < MaxIssues {
			v.Issues = append(v.Issues, Issue{Path: path, Code: code, Message: message})
		}
	})
}

// localIssue reports whether err only concerns the local copy of one approved item.
func localIssue(err error) bool {
	switch domain.ErrorCode(err) {
	case "SOURCE_CHANGED", "LOCAL_MISSING":
		return true
	}
	return false
}

func (s *Service) execute(ctx context.Context, a *approved, run string) (result error) {
	st, err := transferstate.Open(s.stateDir, a.Preview.SourceRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	state, err := st.Load(a.Key)
	if err != nil {
		return err
	}
	digest, err := domain.Digest(state)
	if err != nil || digest != a.StateDigest {
		return domain.Fail("PLAN_STALE", "Transfer history changed after preview. Create a new preview.")
	}
	account := a.Destination.AccountReference
	if _, err = s.account(ctx, account); err != nil {
		return err
	}
	if err = s.checkDestination(ctx, a.Destination); err != nil {
		return err
	}
	cfg := config.Default(a.Preview.SourceRoot)
	configPath := ""
	var configInfo os.FileInfo
	switch {
	case a.Inline:
		cfg = a.Config
	case a.IsConfig:
		configPath = a.Source
		if configInfo, err = os.Lstat(configPath); err != nil {
			return domain.Fail("CONFIG_CHANGED", "The project configuration is no longer available; approve a new preview.")
		}
		if cfg, err = config.Load(configPath); err != nil {
			return err
		}
	}
	cfg.Source.Root = a.Preview.SourceRoot
	if d, e := domain.Digest(cfg); e != nil || d != a.Preview.Plan.ConfigDigest {
		return domain.Fail("CONFIG_CHANGED", "The project configuration changed; approve a new preview.")
	}
	tree, err := app.ScanTree(ctx, cfg)
	if err != nil {
		return err
	}
	defer tree.Close()
	if tree.Identity() != a.Preview.Plan.SourceIdentity {
		return domain.Fail("SOURCE_UNAVAILABLE", "The source folder was replaced or its volume changed. Choose it again and create a new preview.")
	}
	snapshot, err := policy.Resolve(ctx, cfg, tree)
	if err != nil {
		return err
	}
	if snapshot.Digest != a.Preview.Plan.RulesDigest {
		return domain.Fail("RULES_CHANGED", "Ignore rules changed after preview; approve a new preview.")
	}
	known := []string{}
	for _, m := range snapshot.Materials {
		known = append(known, m.Source)
	}
	if err = st.ObserveRules(a.Preview.Plan.SourceIdentity, a.Preview.Plan.ConfigDigest+"|"+app.TraversalProfile, known); err != nil {
		return err
	}
	basenames, roots := policy.Selectors(cfg)
	guard := &sourceGuard{tree: tree, known: known, basenames: basenames, roots: roots, configPath: configPath, configInfo: configInfo, now: s.now}
	if err = guard.check(ctx, true); err != nil {
		return err
	}
	if err = s.reverify(ctx, a); err != nil {
		return err
	}
	state.SourceIdentity, state.AccountReference, state.DestinationID = a.Preview.Plan.SourceIdentity, account, a.Destination.ID
	if err = st.SaveProject(state); err != nil {
		return err
	}
	if err = st.BeginRun(run, a.Key, a); err != nil {
		return err
	}
	defer func() {
		outcome := "succeeded"
		if result != nil {
			outcome = "needs_review"
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
		} else if snapshot := s.Status(); snapshot.SkippedFiles > 0 {
			outcome = "partial"
		}
		summary := s.Status()
		summary.State = outcome
		if result != nil {
			summary.ErrorCode = domain.ErrorCode(result)
		}
		if e := st.FinishRun(run, outcome, summary); e != nil {
			result = e
		}
	}()
	pool := &idPool{provider: s.provider, account: account}
	for _, n := range a.Nodes {
		if n.ID == "" {
			pool.remaining++
		}
	}
	completed := map[string]transferstate.Node{}
	lastDestinationCheck := s.now()
	var sentBefore int64
	for _, planned := range a.Nodes {
		n := planned
		action := a.Actions[n.OperationID]
		if err = guard.check(ctx, false); err != nil {
			return err
		}
		if s.now().Sub(lastDestinationCheck) >= destinationCheckInterval {
			if err = s.checkDestination(ctx, a.Destination); err != nil {
				return err
			}
			lastDestinationCheck = s.now()
		}
		parent := a.Destination.ID
		if n.Path != "" {
			parentNode, ok := completed[parentOf(n.Path)]
			if !ok {
				s.issue(n.Path, "PARENT_NOT_COPIED", "Not copied because its folder was not copied in this run.")
				continue
			}
			parent = parentNode.ID
		}
		if n.ParentID != "" && n.ParentID != parent && !(n.Path == "" && a.Destination.ID == MyDriveID) {
			return domain.Fail("REMOTE_CHANGED", "The managed destination hierarchy no longer matches the approved plan.")
		}
		s.change(func(v *Status) {
			v.CurrentPath = n.Path
			v.State = "uploading"
			v.Message = "Uploading and verifying the approved folder contents."
		})
		if action == ActionSkip {
			if n.Kind == "directory" {
				// A folder trashed or moved after the preview must not receive new files.
				o, e := s.provider.GetObject(ctx, account, n.ID)
				if domain.ErrorCode(e) == "DRIVE_NOT_FOUND" {
					return domain.Fail("REMOTE_CHANGED", "A copied Drive folder was removed after the preview. Create a new preview.")
				}
				if e != nil {
					return e
				}
				if e = verify(n, o); e != nil {
					return e
				}
			}
		} else {
			before := sentBefore
			n, err = s.apply(ctx, st, a, tree, pool, n, parent, action, func(position int64) {
				s.change(func(v *Status) { v.SentBytes = before + position })
			})
			if localIssue(err) {
				s.issue(n.Path, domain.ErrorCode(err), safeMessage(err))
				continue
			}
			if err != nil {
				return err
			}
			if n.Kind == "file" {
				sentBefore += n.Size
			}
		}
		completed[n.Path] = n
		s.change(func(v *Status) {
			if n.Path == "" {
				v.RemoteFolderID = n.ID
			}
			if n.Kind == "file" {
				v.CompletedFiles++
				v.UploadedBytes += n.Size
				v.SentBytes = sentBefore
			}
		})
	}
	if err = guard.check(ctx, true); err != nil {
		return err
	}
	if root, ok := completed[""]; ok {
		s.change(func(v *Status) {
			v.State = "verifying"
			v.CurrentPath = ""
			v.Message = "Confirming the location of the LedgeSync folder in Google Drive."
		})
		o, e := s.provider.GetObject(ctx, account, root.ID)
		if domain.ErrorCode(e) == "DRIVE_NOT_FOUND" {
			return domain.Fail("REMOTE_CHANGED", "The LedgeSync folder was removed from Drive during the copy.")
		}
		if e != nil {
			return e
		}
		if e = verify(root, o); e != nil {
			return domain.Fail("REMOTE_CHANGED", "The LedgeSync folder was moved, renamed or trashed in Drive during the copy.")
		}
	}
	return nil
}

// apply creates or reconciles one approved node. The journal records the
// reserved identity before the provider request and verification afterwards.
func (s *Service) apply(ctx context.Context, st *transferstate.Store, a *approved, tree *discovery.Tree, pool *idPool, n transferstate.Node, parent, action string, report func(int64)) (transferstate.Node, error) {
	account := a.Destination.AccountReference
	if n.Path != "" && !tree.Has(n.Path) {
		return n, domain.Fail("LOCAL_MISSING", "Removed from the source folder after the preview.")
	}
	var source *discovery.UploadFile
	if n.Kind == "file" {
		if action == ActionResume && n.ID != "" && n.MD5 != "" {
			// A reserved file may already exist from an interrupted run.
			o, e := s.provider.GetObject(ctx, account, n.ID)
			if e == nil {
				if e = verify(n, o); e != nil {
					return n, e
				}
				n.Status = "verified"
				return n, st.SaveNode(a.Key, n)
			}
			if domain.ErrorCode(e) != "DRIVE_NOT_FOUND" {
				return n, e
			}
		}
		var err error
		source, err = tree.OpenUpload(ctx, n.Path, n.SHA256)
		if err != nil {
			return n, err
		}
		defer source.Close()
		n.MD5 = source.MD5
	}
	if n.ID == "" {
		id, err := pool.next(ctx)
		if err != nil {
			return n, err
		}
		n.ID = id
	}
	n.ParentID = parent
	n.Status = "intent"
	if err := st.SaveNode(a.Key, n); err != nil {
		return n, err
	}
	var o drive.Object
	var err error
	if source != nil {
		o, err = s.provider.Upload(ctx, account, n.ID, parent, n.Name, n.OperationID, &progressReader{ReadSeeker: source, report: report}, n.Size, n.MD5)
	} else {
		o, err = s.provider.CreateFolder(ctx, account, n.ID, parent, n.Name, n.OperationID)
	}
	if err != nil {
		return n, err
	}
	if n.Path == "" && parent == MyDriveID && len(o.Parents) == 1 {
		// The provider confirmed that this folder is a child of My Drive; record
		// the canonical root identifier for later verification.
		n.ParentID = o.Parents[0]
	}
	if err = verify(n, o); err != nil {
		return n, err
	}
	n.Status = "verified"
	return n, st.SaveNode(a.Key, n)
}

// reverify confirms that the recorded objects the plan relies on are unchanged
// since the preview, using one listing per recorded folder where supported.
func (s *Service) reverify(ctx context.Context, a *approved) error {
	index := &remoteIndex{s: s, account: a.Destination.AccountReference, listed: map[string]drive.Object{}, done: map[string]bool{}}
	byPath := map[string]transferstate.Node{}
	for _, n := range a.Nodes {
		byPath[n.Path] = n
	}
	remote := map[string]drive.Object{}
	for _, n := range a.Nodes {
		action := a.Actions[n.OperationID]
		if (action != ActionSkip && action != ActionResume) || n.ID == "" {
			continue
		}
		if parent, ok := byPath[parentOf(n.Path)]; ok && n.Path != "" && parent.ID != "" && a.Actions[parent.OperationID] == ActionSkip {
			if err := index.list(ctx, parent.ID); err != nil {
				return err
			}
		}
		o, err := index.get(ctx, n.ID)
		if domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
			if action == ActionResume {
				continue
			}
			return domain.Fail("REMOTE_CHANGED", "A copied Drive item was removed after the preview. Create a new preview.")
		}
		if err != nil {
			return err
		}
		if err = verify(n, o); err != nil {
			return err
		}
		remote[n.OperationID] = o
	}
	digest, err := domain.Digest(remote)
	if err != nil || digest != a.RemoteDigest {
		return domain.Fail("PLAN_STALE", "Drive changed after preview. Create a new preview to reconcile the transfer.")
	}
	return nil
}
