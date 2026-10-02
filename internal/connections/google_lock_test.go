package connections

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

func TestGoogleDriveProcessLockWiringIsLazyAndSeparateFromTransferJournal(t *testing.T) {
	base := t.TempDir()
	// Each supported OS chooses its own native configuration root. All roots
	// point to disposable test data; this test never invokes a credential store.
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AppData", base)
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	previous := bundledClientJSON
	t.Cleanup(func() { bundledClientJSON = previous })
	bundledClientJSON = `{"installed":{"client_id":"123-fake.apps.googleusercontent.com","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","client_secret":"synthetic-client-secret","redirect_uris":["http://localhost"]}}`
	if service, err := NewGoogleDrive(nil); err != nil || service == nil {
		t.Fatal("synthetic configuration was not accepted")
	}
	application := filepath.Join(config, "LedgeSync")
	if _, err := os.Stat(application); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("constructor touched the configuration directory")
	}
	release, err := googleDriveProcessLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if next, err := googleDriveProcessLock(); next != nil || !errors.Is(err, driveauth.ErrBusy) {
		t.Fatal("native lock contention did not become a safe authorization error")
	}
	entries, err := os.ReadDir(application)
	if err != nil || len(entries) != 1 || entries[0].Name() != "auth-lock" {
		t.Fatal("authorization lock opened a transfer journal")
	}
	release()
	if err := os.Remove(filepath.Join(application, "auth-lock", "operation.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(application, "auth-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(application, "auth-lock"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if next, err := googleDriveProcessLock(); next != nil || !errors.Is(err, driveauth.ErrStorage) {
		t.Fatal("unsafe lock path did not become a safe storage error")
	}
}
