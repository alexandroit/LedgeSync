package discovery

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadReaderRejectsMutationAndPreservesSource(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	filename := filepath.Join(root, "content.bin")
	if err := os.WriteFile(filename, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	digest, err := tree.HashFile(ctx, "content.bin")
	if err != nil {
		t.Fatal(err)
	}
	u, err := tree.OpenUpload(ctx, "content.bin", digest)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	data, err := io.ReadAll(u)
	if err != nil || string(data) != "original" {
		t.Fatalf("read %q: %v", data, err)
	}
	if u.MD5 == "" || u.Verify() != nil {
		t.Fatal("source did not verify")
	}
	if err = os.WriteFile(filename, []byte("changed content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Seek(0, io.SeekStart); err == nil {
		t.Fatal("seek accepted changed source")
	}
	if u.Verify() == nil {
		t.Fatal("post-verification accepted changed source")
	}
}
func TestUploadReaderRejectsReplacedParentAndNewRules(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	digest, _ := tree.HashFile(ctx, "sub/file")
	if err = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = tree.OpenUpload(ctx, "sub/file", digest); err == nil {
		t.Fatal("new directory policy escaped structure validation")
	}
}
func TestUploadReaderCancellationAndDigestMismatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	if _, err = tree.OpenUpload(ctx, "file", "different"); err == nil {
		t.Fatal("accepted a different approved digest")
	}
	digest, _ := tree.HashFile(ctx, "file")
	u, err := tree.OpenUpload(ctx, "file", digest)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	cancel()
	if _, err = u.Read(make([]byte, 1)); err == nil {
		t.Fatal("cancelled upload read succeeded")
	}
}
