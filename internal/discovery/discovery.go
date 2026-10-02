// Package discovery reads a bounded local tree without source writes or symlink traversal.
package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

type Tree struct {
	root        *os.Root
	rootPath    string
	rootInfo    os.FileInfo
	entries     []domain.Entry
	observed    map[string]os.FileInfo
	directories []string
	identity    string
}

func (t *Tree) Close() error            { return t.root.Close() }
func (t *Tree) RootPath() string        { return t.rootPath }
func (t *Tree) Identity() string        { return t.identity }
func (t *Tree) Entries() []domain.Entry { return append([]domain.Entry{}, t.entries...) }
func (t *Tree) Has(p string) bool       { _, ok := t.observed[p]; return ok }
func cancelled(ctx context.Context) error {
	if ctx.Err() != nil {
		return domain.Fail("CANCELLED", "local scan cancelled")
	}
	return nil
}
func Scan(ctx context.Context, rootPath string) (tree *Tree, err error) {
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
	tree = &Tree{root: r, rootPath: rootPath, rootInfo: info, entries: []domain.Entry{}, observed: map[string]os.FileInfo{}}
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
		for _, child := range children {
			if err := cancelled(ctx); err != nil {
				return err
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
			if e != nil {
				return domain.Fail("SCAN_INCOMPLETE", "cannot inspect %s", p)
			}
			kind := "file"
			size := stat.Size()
			if stat.IsDir() {
				kind = "directory"
				size = 0
			} else if !stat.Mode().IsRegular() {
				return domain.Fail("NODE_UNSUPPORTED", "symlinks and special nodes are unsupported: %s", p)
			}
			tree.observed[p] = stat
			tree.entries = append(tree.entries, domain.Entry{Path: p, Name: path.Base(p), Kind: kind, Size: size, ModifiedAt: stat.ModTime().UTC().Format(time.RFC3339Nano)})
			if kind == "directory" {
				tree.directories = append(tree.directories, p)
				if err := walk(p, depth+1); err != nil {
					return err
				}
			}
		}
		after, e := r.Lstat(dir)
		if e != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			return domain.Fail("SCAN_INCOMPLETE", "directory changed during scan: %s", dir)
		}
		return nil
	}
	if err = walk(".", 0); err != nil {
		return nil, err
	}
	if err = tree.Revalidate(); err != nil {
		return nil, err
	}
	return tree, nil
}
func (t *Tree) Revalidate() error {
	info, err := os.Lstat(t.rootPath)
	if err != nil || !os.SameFile(t.rootInfo, info) || !info.ModTime().Equal(t.rootInfo.ModTime()) {
		return domain.Fail("SOURCE_UNAVAILABLE", "source root changed during scan")
	}
	for p, before := range t.observed {
		after, err := t.root.Lstat(p)
		if err != nil || !same(before, after) {
			return domain.Fail("SCAN_INCOMPLETE", "source entry changed during scan: %s", p)
		}
	}
	return nil
}
func same(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
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
