package credentialvault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type fakeStore struct {
	value string
	err   error
	calls int
}

func (s *fakeStore) Get(string) (string, error)       { s.calls++; return s.value, s.err }
func (s *fakeStore) Set(_ string, value string) error { s.calls++; s.value = value; return s.err }
func (s *fakeStore) Delete(string) error              { s.calls++; return s.err }

func TestRejectInvalidReferencesBeforeVaultAccess(t *testing.T) {
	for _, key := range []string{"", "../account", "UPPERCASE", "account@example.test", "a\x00b", strings.Repeat("a", 129), "a b"} {
		backend := &fakeStore{}
		store := guardedStore{backend}
		if _, err := store.Get(key); err != ErrInvalidKey {
			t.Fatal("invalid key accepted by Get")
		}
		if err := store.Set(key, "synthetic"); err != ErrInvalidKey {
			t.Fatal("invalid key accepted by Set")
		}
		if err := store.Delete(key); err != ErrInvalidKey {
			t.Fatal("invalid key accepted by Delete")
		}
		if backend.calls != 0 {
			t.Fatal("invalid key reached native vault")
		}
	}
}

func TestBoundedValuesAndSafeErrors(t *testing.T) {
	backend := &fakeStore{}
	store := guardedStore{backend}
	if err := store.Set("google-drive", strings.Repeat("x", MaxSecretBytes)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("google-drive", strings.Repeat("x", MaxSecretBytes+1)); err != ErrTooLarge {
		t.Fatal("oversized write accepted")
	}
	if backend.calls != 1 {
		t.Fatal("oversized value reached native vault")
	}
	backend.value = "synthetic-sensitive-data"
	backend.err = fmt.Errorf("provider leaked synthetic-sensitive-data")
	if value, err := store.Get("google-drive"); value != "" || err != ErrUnavailable {
		t.Fatal("provider error or value was exposed")
	}
	if err := store.Set("google-drive", "synthetic"); err != ErrUnavailable {
		t.Fatal("Set exposed a provider error")
	}
	if err := store.Delete("google-drive"); err != ErrUnavailable {
		t.Fatal("Delete exposed a provider error")
	}
	backend.err = fmt.Errorf("wrapped: %w", ErrNotFound)
	if _, err := store.Get("google-drive"); err != ErrNotFound {
		t.Fatal("not-found identity lost")
	}
	backend.err = nil
	backend.value = strings.Repeat("x", MaxSecretBytes+1)
	if value, err := store.Get("google-drive"); value != "" || err != ErrTooLarge {
		t.Fatal("oversized read accepted")
	}
}

// This test runs only on an explicitly opted-in disposable/native test user.
// It never enumerates credentials; every operation addresses a random key that
// this test owns, and no secret values are included in assertion diagnostics.
func TestNativeVaultLifecycle(t *testing.T) {
	if os.Getenv("LEDGESYNC_TEST_NATIVE_VAULT") != "1" {
		t.Skip("native vault test requires explicit isolated-runner opt-in")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("random fixture failed")
	}
	key := "integration-test-" + hex.EncodeToString(random[:])
	store := New()
	if _, err := store.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new fixture lookup: %v", err)
	}
	if err := store.Set(key, "synthetic-first-value"); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	t.Cleanup(func() {
		err := store.Delete(key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Errorf("fixture cleanup: %v", err)
		}
	})
	if value, err := store.Get(key); err != nil || value != "synthetic-first-value" {
		t.Fatal("fixture read failed")
	}
	if err := store.Set(key, "synthetic-updated-value"); err != nil {
		t.Fatalf("update fixture: %v", err)
	}
	if value, err := store.Get(key); err != nil || value != "synthetic-updated-value" {
		t.Fatal("updated fixture read failed")
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	if _, err := store.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatal("fixture retained after removal")
	}
}
