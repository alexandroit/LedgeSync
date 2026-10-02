package connections

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/credentialvault"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

func TestReadClientFileBoundsAndRedaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private-client.json")
	want := []byte(`{"installed":{"fixture":"synthetic-only"}}`)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadClientFile(path)
	if err != nil || string(got) != string(want) {
		t.Fatal("selected regular file was not read")
	}
	if err := os.WriteFile(path, make([]byte, MaxClientFileBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{path, dir, filepath.Join(dir, "missing-private-client.json")} {
		if data, err := ReadClientFile(bad); data != nil || err == nil || strings.Contains(err.Error(), dir) {
			t.Fatal("invalid file was accepted or its path leaked")
		}
	}
}

func TestReadClientFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("symlinks unavailable on this runner")
	}
	if data, err := ReadClientFile(link); data != nil || err == nil {
		t.Fatal("symlink was accepted")
	}
}

type errorVault struct{ err error }

func (s errorVault) Get(string) (string, error) { return "", s.err }
func (s errorVault) Set(string, string) error   { return s.err }
func (s errorVault) Delete(string) error        { return s.err }

func TestVaultBoundaryMapsMissingOnly(t *testing.T) {
	for _, input := range []error{credentialvault.ErrNotFound, credentialvault.ErrUnavailable} {
		adapter := vaultAdapter{errorVault{input}}
		_, got := adapter.Get("google-drive-oauth-v1")
		deleted := adapter.Delete("google-drive-oauth-v1")
		if errors.Is(input, credentialvault.ErrNotFound) {
			if !errors.Is(got, driveauth.ErrNotFound) || !errors.Is(deleted, driveauth.ErrNotFound) {
				t.Fatal("missing credential not normalized")
			}
		} else if !errors.Is(got, input) || !errors.Is(deleted, input) {
			t.Fatal("vault failure lost")
		}
	}
}
