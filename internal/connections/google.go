// Package connections wires native credential storage to the shared OAuth service.
package connections

import (
	"errors"
	"io"
	"os"

	"github.com/alexandroit/LedgeSync/internal/credentialvault"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

const MaxClientFileBytes = 32 * 1024

// NewGoogleDrive never opens a vault or reads credentials until an explicit call.
func NewGoogleDrive(openURL func(string) error) *driveauth.Service {
	return driveauth.New(vaultAdapter{credentialvault.New()}, openURL)
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

// ReadClientFile reads only the file deliberately selected by the native picker.
// Error messages never contain its path or contents. Tokens cannot be imported.
func ReadClientFile(path string) ([]byte, error) {
	failure := errors.New("OAUTH_CLIENT_FILE: choose a readable Google Desktop app JSON file no larger than 32 KiB")
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > MaxClientFileBytes {
		return nil, failure
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, failure
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, failure
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxClientFileBytes+1))
	if err != nil || len(data) > MaxClientFileBytes {
		return nil, failure
	}
	return data, nil
}
