package discovery

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sort"
	"strings"

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

// OpenUpload hashes the observed file and opens it only when its SHA-256 equals
// the approved digest. The MD5 of those exact bytes is what Drive must report
// after upload; equality there proves the uploaded content is the approved one.
func (t *Tree) OpenUpload(ctx context.Context, p, expectedSHA256 string) (*UploadFile, error) {
	sha, md := sha256.New(), md5.New()
	if err := t.read(ctx, p, 0, io.MultiWriter(sha, md)); err != nil {
		if domain.ErrorCode(err) == "CANCELLED" {
			return nil, err
		}
		return nil, domain.Fail("SOURCE_CHANGED", "This file changed or became unreadable after the preview: %s", p)
	}
	if hex.EncodeToString(sha.Sum(nil)) != expectedSHA256 {
		return nil, domain.Fail("SOURCE_CHANGED", "This file's content changed after the preview: %s", p)
	}
	f, err := t.root.OpenFile(p, os.O_RDONLY|safeReadFlags, 0)
	if err != nil {
		return nil, domain.Fail("SOURCE_CHANGED", "This file can no longer be opened: %s", p)
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
		return domain.Fail("SOURCE_CHANGED", "This file changed while it was being copied: %s", u.path)
	}
	st, err = u.tree.root.Lstat(u.path)
	if err != nil || !same(u.original, st) {
		return domain.Fail("SOURCE_CHANGED", "This file was moved or replaced while it was being copied: %s", u.path)
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

// Verify confirms the source node was neither modified nor replaced while it
// streamed. Content equality is established by the provider checksum of the
// uploaded bytes against MD5, which OpenUpload bound to the approved SHA-256.
func (u *UploadFile) Verify() error { return u.check() }

func membershipDigest(entries []os.DirEntry) string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	sort.Strings(names)
	return domain.HashBytes([]byte(strings.Join(names, "\x00")))
}
