// Package connections wires native credential storage to the shared OAuth service.
package connections

import (
	"errors"

	"github.com/alexandroit/LedgeSync/internal/credentialvault"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

// Official builds set this from a generated, untracked source file. Desktop
// client metadata is extractable from distributed binaries; user tokens are not
// build inputs and always remain in the operating-system credential vault.
var bundledClientJSON string

// NewGoogleDrive never opens a vault or reads credentials until an explicit call.
func NewGoogleDrive(openURL func(string) error) (*driveauth.Service, error) {
	return driveauth.NewWithClient(vaultAdapter{credentialvault.New()}, openURL, []byte(bundledClientJSON))
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
