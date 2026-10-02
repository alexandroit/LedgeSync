package connections

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
)

func TestSourceSelectionCannotContainOrEnterSettingsBeforeCreation(t *testing.T) {
	base := t.TempDir()
	settings := filepath.Join(base, "config", "LedgeSync")
	if err := os.Mkdir(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceSelection(base, false, settings); domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
		t.Fatalf("source ancestor accepted: %v", err)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatal("preflight created settings")
	}
	inside := filepath.Join(settings, "auth-lock")
	if err := os.MkdirAll(inside, 0700); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{settings, inside, filepath.Dir(settings), base} {
		if err := validateSourceSelection(source, false, settings); domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
			t.Fatalf("settings source accepted: %v", err)
		}
	}
	safe := filepath.Join(base, "source")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceSelection(safe, false, settings); err != nil {
		t.Fatalf("independent source rejected: %v", err)
	}
}

func TestSourceSelectionChecksConfigRootAndResolvedIdentities(t *testing.T) {
	base := t.TempDir()
	settings := filepath.Join(base, "config", "LedgeSync")
	if err := os.MkdirAll(settings, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(base)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(base, "project.json")
	if err = os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = validateSourceSelection(filename, true, settings); domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
		t.Fatalf("config source ancestor accepted: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(base, alias); err != nil {
		t.Skip("host does not allow directory symlinks")
	}
	if err = validateSourceSelection(alias, false, settings); domain.ErrorCode(err) != "STATE_INSIDE_SOURCE" {
		t.Fatalf("aliased source accepted: %v", err)
	}
}
