// Package discovery reads a bounded local tree without source writes or symlink traversal.
package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

const MaxDepth = 512
const MaxEntries = 250000

// Entry kinds. Links and special nodes are recorded but never followed, opened or copied.
const (
	KindFile      = "file"
	KindDirectory = "directory"
	KindSymlink   = "symlink"
	KindSpecial   = "special"
)

// Options customize traversal. Callbacks run on the scanning goroutine, top-down.
type Options struct {
	// Directory runs after a directory's complete listing has been recorded and
	// before any of its subdirectories is considered for traversal.
	Directory func(ctx context.Context, t *Tree, dir string) error
	// Prune runs before entering a subdirectory. A pruned directory remains an
	// entry; its descendants are neither listed nor read.
	Prune func(ctx context.Context, t *Tree, dir string) (bool, error)
}

type Tree struct {
	root        *os.Root
	rootPath    string
	rootInfo    os.FileInfo
	entries     []domain.Entry
	observed    map[string]os.FileInfo
	directories []string
	pruned      map[string]bool
	membership  map[string]string
	identity    string
}

func (t *Tree) Close() error            { return t.root.Close() }
func (t *Tree) RootPath() string        { return t.rootPath }
func (t *Tree) Identity() string        { return t.identity }
func (t *Tree) Entries() []domain.Entry { return append([]domain.Entry{}, t.entries...) }
func (t *Tree) Has(p string) bool       { _, ok := t.observed[p]; return ok }

// Pruned reports whether a directory was recorded without reading its descendants.
func (t *Tree) Pruned(p string) bool { return t.pruned[p] }

// Info returns the metadata observed for p during the scan.
func (t *Tree) Info(p string) (os.FileInfo, bool) { i, ok := t.observed[p]; return i, ok }

// Directories returns the listed directories in traversal order, including ".".
func (t *Tree) Directories() []string {
	return append([]string{"."}, t.directories...)
}

func cancelled(ctx context.Context) error {
	if ctx.Err() != nil {
		return domain.Fail("CANCELLED", "local scan cancelled")
	}
	return nil
}

func Scan(ctx context.Context, rootPath string) (*Tree, error) {
	return ScanWith(ctx, rootPath, Options{})
}

// ScanWith lists the tree top-down. A directory's complete listing is recorded
// before any child directory is entered, so rule files that can affect a child
// are always known to Options.Directory before Options.Prune decides about it.
func ScanWith(ctx context.Context, rootPath string, opts Options) (tree *Tree, err error) {
	if err = cancelled(ctx); err != nil {
		return nil, err
	}
	rootPath, err = filepath.Abs(rootPath)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "invalid source root")
	}
	info, err := os.Lstat(rootPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "source root is missing, unreadable, not a directory, or a symlink")
	}
	r, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "cannot open source root")
	}
	tree = &Tree{root: r, rootPath: rootPath, rootInfo: info, entries: []domain.Entry{}, observed: map[string]os.FileInfo{}, pruned: map[string]bool{}, membership: map[string]string{}}
	defer func() {
		if err != nil {
			r.Close()
		}
	}()
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "source root identity changed")
	}
	real, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "cannot resolve source root")
	}
	identity, err := rootIdentity(real, info)
	if err != nil {
		return nil, domain.Fail("CAPABILITY_UNSUPPORTED", "source filesystem identity is unavailable")
	}
	tree.identity = domain.HashBytes([]byte(real + "\x00" + identity))
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := cancelled(ctx); err != nil {
			return err
		}
		if depth > MaxDepth {
			return domain.Fail("SCAN_INCOMPLETE", "directory depth exceeds %d", MaxDepth)
		}
		before, e := r.Lstat(dir)
		if e != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return domain.Fail("SCAN_INCOMPLETE", "directory changed or is unreadable: %s", dir)
		}
		f, e := r.OpenFile(dir, os.O_RDONLY|safeReadFlags, 0)
		if e != nil {
			return domain.Fail("SCAN_INCOMPLETE", "cannot read directory: %s", dir)
		}
		current, e := f.Stat()
		if e != nil || !os.SameFile(before, current) {
			f.Close()
			return domain.Fail("SCAN_INCOMPLETE", "directory identity changed: %s", dir)
		}
		children, e := f.ReadDir(-1)
		f.Close()
		if e != nil {
			return domain.Fail("SCAN_INCOMPLETE", "cannot finish directory listing: %s", dir)
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		tree.membership[dir] = membershipDigest(children)
		var subdirectories []string
		for i, child := range children {
			if err := cancelled(ctx); err != nil {
				return err
			}
			if i > 0 && children[i-1].Name() == child.Name() {
				return domain.Fail("SCAN_INCOMPLETE", "directory listing repeated an entry: %s", dir)
			}
			p := child.Name()
			if dir != "." {
				p = dir + "/" + p
			}
			if err := domain.ValidatePath(p); err != nil {
				return err
			}
			if len(tree.entries) >= MaxEntries {
				return domain.Fail("SCAN_INCOMPLETE", "entry limit %d exceeded", MaxEntries)
			}
			stat, e := r.Lstat(p)
			if errors.Is(e, fs.ErrNotExist) {
				// Removed after the listing: equivalent to removal before the scan.
				continue
			}
			if e != nil {
				return domain.Fail("SCAN_INCOMPLETE", "cannot inspect %s", p)
			}
			kind, size := KindFile, stat.Size()
			switch {
			case stat.Mode()&os.ModeSymlink != 0:
				kind, size = KindSymlink, 0
			case stat.IsDir():
				kind, size = KindDirectory, 0
			case !stat.Mode().IsRegular():
				kind, size = KindSpecial, 0
			}
			tree.observed[p] = stat
			tree.entries = append(tree.entries, domain.Entry{Path: p, Name: path.Base(p), Kind: kind, Size: size, ModifiedAt: stat.ModTime().UTC().Format(time.RFC3339Nano)})
			if kind == KindDirectory {
				subdirectories = append(subdirectories, p)
			}
		}
		if opts.Directory != nil {
			if err := opts.Directory(ctx, tree, dir); err != nil {
				return err
			}
		}
		for _, p := range subdirectories {
			if opts.Prune != nil {
				prune, err := opts.Prune(ctx, tree, p)
				if err != nil {
					return err
				}
				if prune {
					tree.pruned[p] = true
					continue
				}
			}
			tree.directories = append(tree.directories, p)
			if err := walk(p, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err = walk(".", 0); err != nil {
		return nil, err
	}
	if err = tree.RootUnchanged(); err != nil {
		return nil, err
	}
	return tree, nil
}

// RootUnchanged checks that the selected root is still the same directory.
// Membership changes inside the root are evaluated per entry by callers.
func (t *Tree) RootUnchanged() error {
	info, err := os.Lstat(t.rootPath)
	if err != nil || !info.IsDir() || !os.SameFile(t.rootInfo, info) {
		return domain.Fail("SOURCE_UNAVAILABLE", "the source folder is missing, was replaced, or its volume is unavailable")
	}
	return nil
}

// Revalidate strictly checks every observed entry, including the root's metadata.
func (t *Tree) Revalidate() error {
	info, err := os.Lstat(t.rootPath)
	if err != nil || !os.SameFile(t.rootInfo, info) || !info.ModTime().Equal(t.rootInfo.ModTime()) {
		return domain.Fail("SOURCE_UNAVAILABLE", "source root changed during scan")
	}
	paths := make([]string, 0, len(t.observed))
	for p := range t.observed {
		paths = append(paths, p)
	}
	return t.RevalidatePaths(paths)
}

// RevalidatePaths checks that the given observed entries are unchanged.
func (t *Tree) RevalidatePaths(paths []string) error {
	for _, p := range paths {
		before, ok := t.observed[p]
		if !ok {
			return domain.Fail("SCAN_INCOMPLETE", "source entry was not observed: %s", p)
		}
		after, err := t.root.Lstat(p)
		if err != nil || !same(before, after) {
			return domain.Fail("SCAN_INCOMPLETE", "source entry changed during scan: %s", p)
		}
	}
	return nil
}

// Same reports whether two observations describe the same unchanged node.
func Same(a, b os.FileInfo) bool { return same(a, b) }

func same(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
}

// CheckPolicySources detects policy changes without rereading every rule file.
// Known sources must be unchanged; no selector may have gained a new file in any
// traversed directory, and absent root-relative sources must still be absent.
func (t *Tree) CheckPolicySources(ctx context.Context, known, basenames, rootFiles []string) error {
	present := map[string]bool{}
	for _, p := range known {
		present[p] = true
		before, ok := t.observed[p]
		if !ok {
			return domain.Fail("RULES_CHANGED", "an ignore-policy source was not part of the approved scan: %s", p)
		}
		after, err := t.root.Lstat(p)
		if err != nil || !same(before, after) {
			return domain.Fail("RULES_CHANGED", "ignore-policy source changed: %s", p)
		}
	}
	absent := func(p string) error {
		if present[p] {
			return nil
		}
		if _, err := t.root.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			if err == nil {
				return domain.Fail("RULES_CHANGED", "a new ignore-policy source appeared: %s", p)
			}
			return domain.Fail("RULES_CHANGED", "cannot check ignore-policy source: %s", p)
		}
		return nil
	}
	for _, dir := range t.Directories() {
		if err := cancelled(ctx); err != nil {
			return err
		}
		for _, name := range basenames {
			p := name
			if dir != "." {
				p = dir + "/" + name
			}
			if err := absent(p); err != nil {
				return err
			}
		}
	}
	for _, p := range rootFiles {
		if err := absent(p); err != nil {
			return err
		}
	}
	return nil
}

// ReadFile reads only a previously observed regular file with pre/post identity checks.
func (t *Tree) ReadFile(ctx context.Context, p string, limit int64) ([]byte, error) {
	var b strings.Builder
	err := t.read(ctx, p, limit, &b)
	if err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
func (t *Tree) HashFile(ctx context.Context, p string) (string, error) {
	h := sha256.New()
	if err := t.read(ctx, p, 0, h); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (t *Tree) read(ctx context.Context, p string, limit int64, w io.Writer) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if err := domain.ValidatePath(p); err != nil {
		return err
	}
	original, ok := t.observed[p]
	if !ok || !original.Mode().IsRegular() {
		return domain.Fail("RULE_SOURCE_UNAVAILABLE", "not an observed regular file: %s", p)
	}
	before, err := t.root.Lstat(p)
	if err != nil || !same(original, before) || before.Mode().Perm()&0444 == 0 {
		return domain.Fail("SCAN_INCOMPLETE", "file changed or is unreadable: %s", p)
	}
	if limit > 0 && before.Size() > limit {
		return domain.Fail("RULE_PARSE_ERROR", "rule source exceeds %d bytes: %s", limit, p)
	}
	f, err := t.root.OpenFile(p, os.O_RDONLY|safeReadFlags, 0)
	if err != nil {
		return domain.Fail("SCAN_INCOMPLETE", "cannot open %s", p)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !same(before, opened) {
		return domain.Fail("SCAN_INCOMPLETE", "file identity changed: %s", p)
	}
	buf := make([]byte, 64<<10)
	var n int64
	for {
		if err := cancelled(ctx); err != nil {
			return err
		}
		count, e := f.Read(buf)
		if count > 0 {
			n += int64(count)
			if limit > 0 && n > limit {
				return domain.Fail("RULE_PARSE_ERROR", "rule source exceeds byte limit: %s", p)
			}
			if _, err = w.Write(buf[:count]); err != nil {
				return err
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return domain.Fail("SCAN_INCOMPLETE", "cannot finish reading %s", p)
		}
	}
	after, err := f.Stat()
	if err != nil || !same(before, after) || n != before.Size() {
		return domain.Fail("SCAN_INCOMPLETE", "file changed while reading: %s", p)
	}
	linked, err := t.root.Lstat(p)
	if err != nil || !same(before, linked) {
		return domain.Fail("SCAN_INCOMPLETE", "file path changed while reading: %s", p)
	}
	return nil
}
