package connections

import (
	"errors"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/credentialvault"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

func TestBundledClientConstructionDoesNotOpenVault(t *testing.T) {
	previous := bundledClientJSON
	defer func() { bundledClientJSON = previous }()
	bundledClientJSON = `{"installed":{"client_id":"123-fake.apps.googleusercontent.com","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","client_secret":"synthetic-client-secret","redirect_uris":["http://localhost"]}}`
	calls := 0
	service, err := NewGoogleDrive(func(string) error { calls++; return nil })
	if service == nil || err != nil || calls != 0 {
		t.Fatal("construction did not accept a valid bundled client without opening a browser")
	}
	for _, malformed := range []string{"", `{"web":{"client_secret":"synthetic-private-value"}}`} {
		bundledClientJSON = malformed
		service, err := NewGoogleDrive(nil)
		if service != nil || !errors.Is(err, driveauth.ErrBuildConfig) {
			t.Fatal("unconfigured build did not fail closed")
		}
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
