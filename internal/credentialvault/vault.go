// Package credentialvault keeps OAuth credentials in the current user's native
// operating-system vault. It never falls back to files, environment variables,
// subprocesses, or an in-memory store that pretends to provide persistence.
package credentialvault

import "errors"

const serviceName = "com.ledgesync.oauth"

// MaxSecretBytes is the portable Windows Credential Manager blob limit.
const MaxSecretBytes = 2560

var (
	ErrNotFound    = errors.New("credential not found")
	ErrUnavailable = errors.New("operating-system credential vault unavailable or locked")
	ErrInvalidKey  = errors.New("invalid credential reference")
	ErrTooLarge    = errors.New("credential exceeds operating-system vault size limit")
)

// Store is a backend-only secret port. Keys are opaque references, not email
// addresses. Callers must not return values to a webview or include them in logs.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// New constructs a lazy native store without reading or prompting for secrets.
func New() Store { return guardedStore{backend: newNativeStore()} }

type guardedStore struct{ backend Store }

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// normalizeError deliberately does not wrap OS/IPC errors: those may contain
// provider-controlled strings or credential metadata.
func normalizeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrTooLarge):
		return ErrTooLarge
	default:
		return ErrUnavailable
	}
}

func (s guardedStore) Get(key string) (string, error) {
	if !validKey(key) {
		return "", ErrInvalidKey
	}
	value, err := s.backend.Get(key)
	if err != nil {
		return "", normalizeError(err)
	}
	if len(value) > MaxSecretBytes {
		return "", ErrTooLarge
	}
	return value, nil
}

func (s guardedStore) Set(key, value string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	if len(value) > MaxSecretBytes {
		return ErrTooLarge
	}
	return normalizeError(s.backend.Set(key, value))
}

func (s guardedStore) Delete(key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	return normalizeError(s.backend.Delete(key))
}
