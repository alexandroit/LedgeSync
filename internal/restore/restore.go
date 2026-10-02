// Package restore downloads a sync pair's managed Drive copy into a new, empty
// local folder. It never writes to the source folder, never overwrites a file,
// and only restores objects recorded and verified in the transfer journal.
package restore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transfer"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

const folderMIME = "application/vnd.google-apps.folder"

// Provider is the read-only Drive surface restore needs.
type Provider interface {
	GetObject(context.Context, string, string) (drive.Object, error)
	Download(context.Context, string, string, int64, io.Writer) error
}

// Request identifies the journal project and the new local target.
type Request struct {
	StateDir   string
	ProjectKey string
	Account    string
	Target     string
	// Protected are folders the target must not be inside, such as the source.
	Protected []string
}

// Progress is a bounded, redacted restore report.
type Progress struct {
	State       string           `json:"state"`
	Target      string           `json:"target"`
	TotalFiles  int              `json:"totalFiles"`
	Files       int              `json:"files"`
	Folders     int              `json:"folders"`
	TotalBytes  int64            `json:"totalBytes"`
	Bytes       int64            `json:"bytes"`
	CurrentPath string           `json:"currentPath,omitempty"`
	ErrorCode   string           `json:"errorCode,omitempty"`
	Message     string           `json:"message"`
	Issues      []transfer.Issue `json:"issues,omitempty"`
}

type item struct {
	node transferstate.Node
	rel  string
}

// PrepareTarget validates or creates an empty directory for a restore. The
// location is checked against protected folders before anything is created.
func PrepareTarget(target string, protected []string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil || abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return "", domain.Fail("RESTORE_TARGET_INVALID", "Choose a named folder for the restored copy.")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", domain.Fail("RESTORE_TARGET_INVALID", "The parent of the restore folder must exist.")
	}
	resolved := filepath.Join(parent, filepath.Base(abs))
	for _, p := range protected {
		if p == "" {
			continue
		}
		base, e := filepath.EvalSymlinks(p)
		if e != nil {
			base = filepath.Clean(p)
		}
		if inside(resolved, base) || inside(base, resolved) {
			return "", domain.Fail("RESTORE_TARGET_INVALID", "Choose a folder outside the source folder and LedgeSync's private data.")
		}
	}
	info, err := os.Lstat(resolved)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if e := os.Mkdir(resolved, 0o755); e != nil {
			return "", domain.Fail("RESTORE_TARGET_INVALID", "The restore folder could not be created.")
		}
	case err != nil:
		return "", domain.Fail("RESTORE_TARGET_INVALID", "The restore folder cannot be inspected.")
	case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
		return "", domain.Fail("RESTORE_TARGET_INVALID", "Choose a folder, not a link or a file.")
	default:
		entries, e := os.ReadDir(resolved)
		if e != nil {
			return "", domain.Fail("RESTORE_TARGET_INVALID", "The restore folder cannot be read.")
		}
		if len(entries) != 0 {
			return "", domain.Fail("RESTORE_TARGET_NOT_EMPTY", "Choose an empty folder. Restores never overwrite or merge into existing files.")
		}
	}
	return resolved, nil
}

func inside(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// plan selects the restorable tree: the newest verified managed root and every
// verified descendant recorded under it, addressed by Drive parent identity.
func plan(nodes map[string]transferstate.Node) (transferstate.Node, []item, error) {
	var root transferstate.Node
	found := false
	children := map[string][]transferstate.Node{}
	for _, n := range nodes {
		if n.Status != "verified" {
			continue
		}
		if n.Path == "" && n.Kind == "directory" {
			if !found || n.Generation > root.Generation {
				root, found = n, true
			}
			continue
		}
		children[n.ParentID] = append(children[n.ParentID], n)
	}
	if !found {
		return root, nil, domain.Fail("RESTORE_UNAVAILABLE", "This sync pair has no verified copy to restore.")
	}
	var out []item
	seen := map[string]bool{}
	var walk func(string, string) error
	walk = func(parentID, prefix string) error {
		list := children[parentID]
		sort.Slice(list, func(i, j int) bool {
			return list[i].Name < list[j].Name || list[i].Name == list[j].Name && list[i].ID < list[j].ID
		})
		for _, n := range list {
			rel := n.Name
			if prefix != "" {
				rel = prefix + "/" + n.Name
			}
			if domain.ValidatePath(rel) != nil || strings.ContainsAny(n.Name, "/\\") || seen[rel] {
				return domain.Fail("RESTORE_UNAVAILABLE", "The recorded copy contains an unsafe or duplicate name.")
			}
			seen[rel] = true
			out = append(out, item{node: n, rel: rel})
			if n.Kind == "directory" {
				if err := walk(n.ID, rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root.ID, ""); err != nil {
		return root, nil, err
	}
	return root, out, nil
}

type hashingWriter struct {
	w        io.Writer
	md5, sha hash.Hash
	n        int64
}

func (h *hashingWriter) Write(p []byte) (int, error) {
	n, err := h.w.Write(p)
	h.md5.Write(p[:n])
	h.sha.Write(p[:n])
	h.n += int64(n)
	return n, err
}

// Runner restores one copy and reports progress.
type Runner struct {
	mu       sync.Mutex
	progress Progress
}

func (r *Runner) Progress() Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.progress
	p.Issues = append([]transfer.Issue(nil), r.progress.Issues...)
	return p
}

func (r *Runner) update(fn func(*Progress)) { r.mu.Lock(); defer r.mu.Unlock(); fn(&r.progress) }

func (r *Runner) issue(path, code, message string) {
	r.update(func(p *Progress) {
		if len(p.Issues) < transfer.MaxIssues {
			p.Issues = append(p.Issues, transfer.Issue{Path: path, Code: code, Message: message})
		}
	})
}

// Run downloads the copy into req.Target, which PrepareTarget has validated.
func (r *Runner) Run(ctx context.Context, provider Provider, req Request) (Progress, error) {
	r.update(func(p *Progress) {
		*p = Progress{State: "restoring", Target: req.Target, Message: "Reading the recorded copy."}
	})
	result, err := r.run(ctx, provider, req)
	r.update(func(p *Progress) {
		switch {
		case err != nil && ctx.Err() != nil:
			p.State, p.ErrorCode, p.Message = "cancelled", "CANCELLED", "Restore stopped. Files already restored remain in the restore folder."
		case err != nil:
			p.State, p.ErrorCode = "failed", domain.ErrorCode(err)
			if e, ok := err.(*domain.Error); ok {
				p.Message = e.Message
			} else {
				p.Message = "The restore could not finish."
			}
		case len(p.Issues) > 0:
			p.State, p.Message = "partial", "Restored the available verified files. Some recorded items are missing or changed in Drive and were not restored."
		default:
			p.State, p.Message = "succeeded", "Restored every recorded file. Each file matched Drive's checksum and the copied content's SHA-256."
		}
		p.CurrentPath = ""
	})
	_ = result
	return r.Progress(), err
}

func (r *Runner) run(ctx context.Context, provider Provider, req Request) (struct{}, error) {
	st, err := transferstate.Open(req.StateDir, "")
	if err != nil {
		return struct{}{}, err
	}
	project, err := st.Load(req.ProjectKey)
	st.Close()
	if err != nil {
		return struct{}{}, err
	}
	if project.AccountReference != "" && project.AccountReference != req.Account {
		return struct{}{}, domain.Fail("ACCOUNT_CHANGED", "This copy belongs to a different Google account. Connect that account to restore it.")
	}
	root, items, err := plan(project.Nodes)
	if err != nil {
		return struct{}{}, err
	}
	var total int64
	files := 0
	for _, it := range items {
		if it.node.Kind == "file" {
			files++
			total += it.node.Size
		}
	}
	r.update(func(p *Progress) { p.TotalFiles, p.TotalBytes = files, total })
	o, err := provider.GetObject(ctx, req.Account, root.ID)
	if err != nil {
		return struct{}{}, err
	}
	if o.Trashed || o.MimeType != folderMIME || o.AppProperties["ledgesyncOperation"] != root.OperationID {
		return struct{}{}, domain.Fail("RESTORE_UNAVAILABLE", "The LedgeSync folder in Drive is trashed or changed. Restore it in Drive first, or run the sync pair again.")
	}
	target, err := os.OpenRoot(req.Target)
	if err != nil {
		return struct{}{}, domain.Fail("RESTORE_TARGET_INVALID", "The restore folder cannot be opened.")
	}
	defer target.Close()
	skipped := map[string]bool{}
	for _, it := range items {
		if ctx.Err() != nil {
			return struct{}{}, domain.Fail("CANCELLED", "Restore cancelled.")
		}
		if skipped[path.Dir(it.rel)] {
			skipped[it.rel] = true
			continue
		}
		r.update(func(p *Progress) { p.CurrentPath, p.Message = it.rel, "Downloading and verifying the recorded copy." })
		o, err := provider.GetObject(ctx, req.Account, it.node.ID)
		if domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
			r.issue(it.rel, "REMOTE_MISSING", "This item is no longer available in Drive.")
			skipped[it.rel] = true
			continue
		}
		if err != nil {
			return struct{}{}, err
		}
		if o.Trashed || len(o.Parents) != 1 || o.Parents[0] != it.node.ParentID || o.Name != it.node.Name || o.AppProperties["ledgesyncOperation"] != it.node.OperationID {
			r.issue(it.rel, "REMOTE_CHANGED", "This item was trashed, moved, renamed or replaced in Drive.")
			skipped[it.rel] = true
			continue
		}
		local := filepath.FromSlash(it.rel)
		if it.node.Kind == "directory" {
			if o.MimeType != folderMIME {
				r.issue(it.rel, "REMOTE_CHANGED", "This folder changed type in Drive.")
				skipped[it.rel] = true
				continue
			}
			if err = target.Mkdir(local, 0o755); err != nil {
				return struct{}{}, domain.Fail("RESTORE_WRITE_FAILED", "A restored folder could not be created: %s", it.rel)
			}
			r.update(func(p *Progress) { p.Folders++ })
			continue
		}
		if o.MimeType == folderMIME || o.Size != it.node.Size || o.MD5 != it.node.MD5 {
			r.issue(it.rel, "REMOTE_CHANGED", "This file's content in Drive no longer matches the verified copy.")
			continue
		}
		f, err := target.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return struct{}{}, domain.Fail("RESTORE_WRITE_FAILED", "A restored file could not be created: %s", it.rel)
		}
		w := &hashingWriter{w: f, md5: md5.New(), sha: sha256.New()}
		err = provider.Download(ctx, req.Account, it.node.ID, it.node.Size, w)
		closeErr := f.Close()
		if err == nil && closeErr != nil {
			err = domain.Fail("RESTORE_WRITE_FAILED", "A restored file could not be saved: %s", it.rel)
		}
		if err == nil && (w.n != it.node.Size || hex.EncodeToString(w.md5.Sum(nil)) != o.MD5 || hex.EncodeToString(w.sha.Sum(nil)) != it.node.SHA256) {
			err = domain.Fail("RESTORE_VERIFICATION_FAILED", "A downloaded file did not match its verified checksum: %s", it.rel)
		}
		if err != nil {
			// Only this run's incomplete file in the new restore folder is removed.
			_ = target.Remove(local)
			if domain.ErrorCode(err) == "RESTORE_VERIFICATION_FAILED" {
				r.issue(it.rel, "RESTORE_VERIFICATION_FAILED", "The downloaded content did not match its verified checksum and was not kept.")
				continue
			}
			return struct{}{}, err
		}
		size := it.node.Size
		r.update(func(p *Progress) { p.Files++; p.Bytes += size })
	}
	return struct{}{}, nil
}
