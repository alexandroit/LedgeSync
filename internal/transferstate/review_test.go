package transferstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A filesystem name is never allowed to become a SQLite URI query or fragment.
// Exercise the actual driver and reopening, not just URL serialization.
func TestStoreSpecialCharacterPathsRemainLiteral(t *testing.T) {
	for _, name := range []string{"with spaces", "question?mark", "hash#mark", "mixed ?#%&_pragma=journal_mode(OFF)"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsRune(name, '?') {
				t.Skip("question marks are not valid Windows directory names")
			}
			base := t.TempDir()
			source := filepath.Join(base, "source")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(base, name)
			store, err := Open(directory, source)
			if err != nil {
				t.Fatal(err)
			}
			p := Project{Key: "project", SourceIdentity: "synthetic-source", AccountReference: "synthetic-account", DestinationID: "synthetic-destination"}
			if err := store.SaveProject(p); err != nil {
				store.Close()
				t.Fatal(err)
			}
			var mode string
			if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
				store.Close()
				t.Fatalf("path altered journal configuration: %s %v", mode, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(filepath.Join(directory, "transfers.sqlite")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("journal was not stored at its literal path: %v", err)
			}
			store, err = Open(directory, source)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			loaded, err := store.Load(p.Key)
			if err != nil || loaded.SourceIdentity != p.SourceIdentity || loaded.AccountReference != p.AccountReference {
				t.Fatalf("state did not persist at literal path: %#v %v", loaded, err)
			}
		})
	}
}
