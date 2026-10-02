// Package connections wires native credential storage to the shared OAuth service.
package connections

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/alexandroit/LedgeSync/internal/credentialvault"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

// Official builds set this from a generated, untracked source file. Desktop
// client metadata is extractable from distributed binaries; user tokens are not
// build inputs and always remain in the operating-system credential vault.
var bundledClientJSON string

// NewGoogleDrive never opens a vault or reads credentials until an explicit call.
func NewGoogleDrive(openURL func(string) error) (*driveauth.Service, error) {
	s, err := driveauth.NewWithClientAndLock(vaultAdapter{credentialvault.New()}, openURL, []byte(bundledClientJSON), googleDriveProcessLock)
	if err != nil {
		return nil, err
	}
	// A transfer's Drive requests wait briefly for a concurrent status read or
	// another process's short credential operation instead of failing the run.
	s.SetRequestWait(20 * time.Second)
	return s, nil
}

func googleDriveProcessLock() (func(), error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return nil, driveauth.ErrStorage
	}
	release, err := transferstate.AcquireProcessLock(filepath.Join(directory, "LedgeSync", "auth-lock"))
	if errors.Is(err, transferstate.ErrLockBusy) {
		return nil, driveauth.ErrBusy
	}
	if err != nil {
		return nil, driveauth.ErrStorage
	}
	return release, nil
}

type vaultAdapter struct{ credentialvault.Store }

func (s vaultAdapter) Get(key string) (string, error) {
	value, err := s.Store.Get(key)
	if errors.Is(err, credentialvault.ErrNotFound) {
		return "", driveauth.ErrNotFound
	}
	return value, err
}
func (s vaultAdapter) Delete(key string) error {
	err := s.Store.Delete(key)
	if errors.Is(err, credentialvault.ErrNotFound) {
		return driveauth.ErrNotFound
	}
	return err
}
