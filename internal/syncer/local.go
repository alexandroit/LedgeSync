package syncer

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/filters"
	"github.com/alexandroit/LedgeSync/internal/policy"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

type localItem struct {
	Kind  string // "file" or "dir"
	Size  int64
	Mtime int64
	MD5   string
}

type localScan struct {
	items    map[string]localItem
	excluded func(p, kind string) (bool, error)
	issues   []Issue
	rules    string // digest of the rule material that decided exclusions
}

// scanLocal lists the included files and folders of the pair with the same
// ignore-rule engine as previews. MD5 is computed only for files whose size or
// modification time differ from the last synchronized state.
func scanLocal(ctx context.Context, pair Pair, base map[string]transferstate.SyncEntry) (*localScan, error) {
	cfg, err := pair.Policy.Config(pair.LocalRoot)
	if err != nil {
		return nil, err
	}
	tree, err := app.ScanTree(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	cfg.Source.Root = tree.RootPath()
	snapshot, err := policy.Resolve(ctx, cfg, tree)
	if err != nil {
		return nil, err
	}
	engine, err := filters.New(cfg, snapshot)
	if err != nil {
		return nil, err
	}
	scan := &localScan{items: map[string]localItem{}, rules: snapshot.Digest}
	scan.excluded = func(p, kind string) (bool, error) {
		if excludedPath(p) {
			return true, nil
		}
		k := discovery.KindFile
		if kind == "dir" {
			k = discovery.KindDirectory
		}
		explanation, err := engine.Explain(ctx, p, k)
		if err != nil {
			return false, err
		}
		return explanation.Decision == app.DecisionExclude, nil
	}
	root, err := os.OpenRoot(tree.RootPath())
	if err != nil {
		return nil, domain.Fail("SOURCE_UNREADABLE", "The local folder cannot be opened.")
	}
	defer root.Close()
	for _, e := range tree.Entries() {
		if excludedPath(e.Path) {
			continue
		}
		kind := "file"
		if e.Kind == discovery.KindDirectory {
			kind = "dir"
		}
		excluded, err := scan.excluded(e.Path, kind)
		if err != nil {
			return nil, err
		}
		if excluded {
			continue
		}
		switch {
		case tree.Pruned(e.Path):
			return nil, domain.Fail("SCAN_INCOMPLETE", "traversal pruning disagreed with the complete policy for %s", e.Path)
		case e.Kind == discovery.KindSymlink:
			scan.issues = append(scan.issues, Issue{e.Path, "LINK_SKIPPED", "Symbolic links are not synced."})
			continue
		case e.Kind == discovery.KindSpecial:
			scan.issues = append(scan.issues, Issue{e.Path, "SPECIAL_SKIPPED", "Special files are not synced."})
			continue
		}
		item := localItem{Kind: kind}
		if kind == "file" {
			info, ok := tree.Info(e.Path)
			if !ok {
				continue
			}
			item.Size, item.Mtime = info.Size(), info.ModTime().UnixNano()
			if b, known := base[e.Path]; known && b.Kind == "file" && b.LocalSize == item.Size && b.LocalMtime == item.Mtime && b.LocalMD5 != "" {
				item.MD5 = b.LocalMD5
			} else {
				item.MD5, err = hashFile(ctx, root, e.Path)
				if errors.Is(err, fs.ErrNotExist) {
					continue // removed while scanning; the next cycle sees the deletion
				}
				if err != nil {
					return nil, err
				}
			}
		}
		scan.items[e.Path] = item
	}
	return scan, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func hashFile(ctx context.Context, root *os.Root, p string) (string, error) {
	f, err := root.Open(filepath.FromSlash(p))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", domain.Fail("SOURCE_UNREADABLE", "%s is not a regular file.", p)
	}
	h := md5.New()
	if _, err = io.Copy(h, ctxReader{ctx, f}); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", domain.Fail("SOURCE_UNREADABLE", "%s could not be read.", p)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fingerprint is a cheap digest of names, sizes and times used to notice local
// changes between full cycles. It skips the pair's own trash and temporaries.
func fingerprint(rootPath string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == rootPath {
				return err
			}
			return nil
		}
		if p != rootPath && builtinExcluded(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(rootPath, p)
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%t\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano(), d.IsDir())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// localFS performs every write inside the synced folder through an os.Root,
// so no operation can leave the folder, and checks the target first.
type localFS struct {
	root  *os.Root
	now   func() time.Time
	stamp string
}

func openLocal(rootPath string, now func() time.Time) (*localFS, error) {
	st, err := os.Lstat(rootPath)
	if err != nil || !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 {
		return nil, domain.Fail("SYNC_FOLDER_UNAVAILABLE", "The local folder is not available. Connect the disk or choose the folder again.")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, domain.Fail("SYNC_FOLDER_UNAVAILABLE", "The local folder cannot be opened.")
	}
	return &localFS{root: root, now: now, stamp: now().UTC().Format("20060102-150405")}, nil
}

func (l *localFS) Close() error { return l.root.Close() }

func native(p string) string { return filepath.FromSlash(p) }

// lstat returns the current item at p; ok is false when nothing exists there.
func (l *localFS) lstat(p string) (localItem, bool, error) {
	st, err := l.root.Lstat(native(p))
	if errors.Is(err, fs.ErrNotExist) {
		return localItem{}, false, nil
	}
	if err != nil {
		return localItem{}, false, err
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		return localItem{Kind: "link"}, true, nil
	case st.IsDir():
		return localItem{Kind: "dir"}, true, nil
	case st.Mode().IsRegular():
		return localItem{Kind: "file", Size: st.Size(), Mtime: st.ModTime().UnixNano()}, true, nil
	}
	return localItem{Kind: "special"}, true, nil
}

// unchanged reports whether p still matches what the scan observed.
func (l *localFS) unchanged(p string, expect localItem, existed bool) bool {
	current, ok, err := l.lstat(p)
	if err != nil {
		return false
	}
	if !existed {
		return !ok
	}
	if !ok || current.Kind != expect.Kind {
		return false
	}
	return expect.Kind != "file" || current.Size == expect.Size && current.Mtime == expect.Mtime
}

// realDir checks that every component of dir is a real directory, never a link.
func (l *localFS) realDir(dir string) bool {
	if dir == "" {
		return true
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		item, ok, err := l.lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || !ok || item.Kind != "dir" {
			return false
		}
	}
	return true
}

func (l *localFS) mkdir(p string) error {
	if err := l.root.MkdirAll(native(p), 0o755); err != nil {
		return domain.Fail("LOCAL_WRITE_FAILED", "The folder %s could not be created.", p)
	}
	if !l.realDir(p) {
		return domain.Fail("LOCAL_WRITE_FAILED", "%s is not a folder.", p)
	}
	return nil
}

func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// writeFile stores downloaded content at p. The content goes to a temporary
// file in the same folder and replaces p only after its size and MD5 match and
// p still matches the scan (absent, or the expected existing file).
func (l *localFS) writeFile(ctx context.Context, p string, expect *localItem, size int64, md5sum string, modified time.Time, fill func(io.Writer) error) (localItem, error) {
	dir := parentOf(p)
	if !l.realDir(dir) {
		return localItem{}, domain.Fail("LOCAL_WRITE_FAILED", "The folder for %s is not available.", p)
	}
	tmp := tempPrefix + randomSuffix()
	if dir != "" {
		tmp = dir + "/" + tmp
	}
	f, err := l.root.OpenFile(native(tmp), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return localItem{}, domain.Fail("LOCAL_WRITE_FAILED", "%s could not be written.", p)
	}
	keep := false
	defer func() {
		if !keep {
			_ = l.root.Remove(native(tmp))
		}
	}()
	h := md5.New()
	counter := &countingWriter{}
	err = fill(io.MultiWriter(f, h, counter))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		if ctx.Err() != nil {
			return localItem{}, ctx.Err()
		}
		var safe *domain.Error
		if errors.As(err, &safe) {
			return localItem{}, err
		}
		return localItem{}, domain.Fail("LOCAL_WRITE_FAILED", "%s could not be written.", p)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if counter.n != size || md5sum != "" && got != md5sum {
		return localItem{}, domain.Fail("DOWNLOAD_VERIFICATION_FAILED", "The downloaded content of %s did not match Google Drive. It will be retried.", p)
	}
	if expect == nil && !l.unchanged(p, localItem{}, false) || expect != nil && !l.unchanged(p, *expect, true) {
		return localItem{}, domain.Fail("LOCAL_CHANGED", "%s changed on this computer during the download. It will be compared again.", p)
	}
	if err = l.root.Rename(native(tmp), native(p)); err != nil {
		return localItem{}, domain.Fail("LOCAL_WRITE_FAILED", "%s could not be replaced.", p)
	}
	keep = true
	if !modified.IsZero() {
		_ = l.root.Chtimes(native(p), l.now(), modified)
	}
	current, ok, err := l.lstat(p)
	if err != nil || !ok || current.Kind != "file" {
		return localItem{}, domain.Fail("LOCAL_WRITE_FAILED", "%s could not be verified after writing.", p)
	}
	current.MD5 = got
	return current, nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// toTrash moves p into the pair's local trash, keeping its relative path, if
// it still matches the scan. Folders move with everything inside them.
func (l *localFS) toTrash(p string, expect localItem) error {
	if !l.unchanged(p, expect, true) {
		return domain.Fail("LOCAL_CHANGED", "%s changed on this computer and was not deleted. It will be compared again.", p)
	}
	dest := TrashDir + "/" + l.stamp + "/" + p
	if err := l.root.MkdirAll(native(parentOf(dest)), 0o755); err != nil {
		return domain.Fail("LOCAL_WRITE_FAILED", "The local trash could not be prepared.")
	}
	if _, ok, _ := l.lstat(dest); ok {
		dest = dest + " " + randomSuffix()
	}
	if err := l.root.Rename(native(p), native(dest)); err != nil {
		return domain.Fail("LOCAL_WRITE_FAILED", "%s could not be moved to the local trash.", p)
	}
	return nil
}

// renameConflict keeps the local version of a conflicting file under a new name.
func (l *localFS) renameConflict(p, to string, expect localItem) error {
	if !l.unchanged(p, expect, true) {
		return domain.Fail("LOCAL_CHANGED", "%s changed on this computer during the conflict check. It will be compared again.", p)
	}
	if _, ok, _ := l.lstat(to); ok {
		return domain.Fail("LOCAL_WRITE_FAILED", "A file named %s already exists.", to)
	}
	if err := l.root.Rename(native(p), native(to)); err != nil {
		return domain.Fail("LOCAL_WRITE_FAILED", "%s could not be renamed to keep both versions.", p)
	}
	return nil
}

// open opens a local file for upload after checking it still matches the scan.
func (l *localFS) open(p string, expect localItem) (*os.File, error) {
	if !l.unchanged(p, expect, true) {
		return nil, domain.Fail("LOCAL_CHANGED", "%s changed on this computer. It will be uploaded on the next pass.", p)
	}
	f, err := l.root.Open(native(p))
	if err != nil {
		return nil, domain.Fail("SOURCE_UNREADABLE", "%s could not be opened.", p)
	}
	return f, nil
}

// maintain removes expired trash folders and stale temporary files.
func (l *localFS) maintain() {
	if entries, err := fs.ReadDir(l.root.FS(), TrashDir); err == nil {
		for _, e := range entries {
			stamp, err := time.Parse("20060102-150405", strings.SplitN(e.Name(), " ", 2)[0])
			if err == nil && e.IsDir() && l.now().Sub(stamp) > TrashRetention {
				_ = l.root.RemoveAll(native(TrashDir + "/" + e.Name()))
			}
		}
	}
	cutoff := l.now().Add(-time.Hour)
	_ = fs.WalkDir(l.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == TrashDir {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), tempPrefix) {
			if info, e := d.Info(); e == nil && info.ModTime().Before(cutoff) {
				_ = l.root.Remove(native(p))
			}
		}
		return nil
	})
}
