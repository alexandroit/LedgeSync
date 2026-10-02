package discovery

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

// UploadFile is a seekable, read-only source handle bound to the complete scan.
// MD5 is used only for Drive's binary-content checksum, never as an identity key.
type UploadFile struct {
	f              *os.File
	tree           *Tree
	ctx            context.Context
	path, expected string
	original       os.FileInfo
	MD5            string
}

func (t *Tree) OpenUpload(ctx context.Context, p, expectedSHA256 string) (*UploadFile, error) {
	if err := t.RevalidateStructure(); err != nil {
		return nil, err
	}
	sha, md := sha256.New(), md5.New()
	if err := t.read(ctx, p, 0, io.MultiWriter(sha, md)); err != nil {
		return nil, err
	}
	if hex.EncodeToString(sha.Sum(nil)) != expectedSHA256 {
		return nil, domain.Fail("SOURCE_CHANGED", "Source content changed; create and approve a new preview.")
	}
	f, err := t.root.OpenFile(p, os.O_RDONLY|safeReadFlags, 0)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Cannot open the approved source file.")
	}
	u := &UploadFile{f: f, tree: t, ctx: ctx, path: p, expected: expectedSHA256, original: t.observed[p], MD5: hex.EncodeToString(md.Sum(nil))}
	if err = u.check(); err != nil {
		f.Close()
		return nil, err
	}
	return u, nil
}
func (u *UploadFile) check() error {
	if err := cancelled(u.ctx); err != nil {
		return err
	}
	st, err := u.f.Stat()
	if err != nil || !same(u.original, st) {
		return domain.Fail("SOURCE_CHANGED", "Source identity or metadata changed during upload.")
	}
	st, err = u.tree.root.Lstat(u.path)
	if err != nil || !same(u.original, st) {
		return domain.Fail("SOURCE_CHANGED", "Source path changed during upload.")
	}
	return nil
}
func (u *UploadFile) Read(p []byte) (int, error) {
	if err := u.check(); err != nil {
		return 0, err
	}
	return u.f.Read(p)
}
func (u *UploadFile) Seek(offset int64, whence int) (int64, error) {
	if err := u.check(); err != nil {
		return 0, err
	}
	return u.f.Seek(offset, whence)
}
func (u *UploadFile) Close() error { return u.f.Close() }
func (u *UploadFile) Verify() error {
	if err := u.check(); err != nil {
		return err
	}
	actual, err := u.tree.HashFile(u.ctx, u.path)
	if err != nil {
		return err
	}
	if actual != u.expected {
		return domain.Fail("SOURCE_CHANGED", "Source content changed during upload; review this transfer.")
	}
	return u.tree.RevalidateStructure()
}

// RevalidateStructure checks directory identities and membership timestamps.
// Individual file contents are checked when streamed and in the final full scan.
// This avoids re-statting every unrelated file before every upload chunk/file.
func (t *Tree) RevalidateStructure() error {
	info, err := os.Lstat(t.rootPath)
	if err != nil || !os.SameFile(t.rootInfo, info) || !info.ModTime().Equal(t.rootInfo.ModTime()) {
		return domain.Fail("SOURCE_CHANGED", "The source root or its entries changed during upload.")
	}
	for _, p := range t.directories {
		after, err := t.root.Lstat(p)
		if err != nil || !same(t.observed[p], after) {
			return domain.Fail("SOURCE_CHANGED", "A source directory changed during upload.")
		}
	}
	return nil
}
