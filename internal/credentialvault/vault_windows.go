//go:build windows

package credentialvault

import (
	"errors"
	"github.com/danieljoos/wincred"
)

type windowsStore struct{}

func newNativeStore() Store { return windowsStore{} }

func windowsError(err error) error {
	if errors.Is(err, wincred.ErrElementNotFound) {
		return ErrNotFound
	}
	return normalizeError(err)
}

func (windowsStore) Get(key string) (string, error) {
	credential, err := wincred.GetGenericCredential(serviceName + "/" + key)
	if err != nil {
		return "", windowsError(err)
	}
	if credential == nil {
		return "", ErrUnavailable
	}
	defer clear(credential.CredentialBlob)
	return string(credential.CredentialBlob), nil
}

func (windowsStore) Set(key, value string) error {
	credential := wincred.NewGenericCredential(serviceName + "/" + key)
	// LocalMachine means this user's subsequent logins on this machine; it does
	// not grant other users access and does not enable roaming credentials.
	credential.Persist = wincred.PersistLocalMachine
	credential.Comment = "LedgeSync Google Drive authorization"
	credential.CredentialBlob = []byte(value)
	defer clear(credential.CredentialBlob)
	return windowsError(credential.Write())
}

func (windowsStore) Delete(key string) error {
	credential := wincred.NewGenericCredential(serviceName + "/" + key)
	return windowsError(credential.Delete())
}
