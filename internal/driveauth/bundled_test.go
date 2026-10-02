package driveauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBundledClientRejectsInvalidBuildConfigurationWithoutVaultAccess(t *testing.T) {
	for name, data := range map[string][]byte{
		"missing":   nil,
		"malformed": []byte(`private-configuration-data`),
		"web":       []byte(strings.Replace(clientJSON, `"installed"`, `"web"`, 1)),
		"endpoint":  []byte(strings.Replace(clientJSON, tokenEndpoint, "https://private.example.test", 1)),
		"duplicate": []byte(strings.Replace(clientJSON, `"project_id":"fake-project"`, `"project_id":"fake-project","project_id":"private-configuration-data"`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			store := &memoryStore{}
			s, err := NewWithClient(store, func(string) error { t.Fatal("browser opened during construction"); return nil }, data)
			if s != nil || !errors.Is(err, ErrBuildConfig) || err.Error() != ErrBuildConfig.Error() {
				t.Fatal("invalid configuration did not fail with a static build error")
			}
			if store.reads != 0 || store.writes != 0 {
				t.Fatal("constructor accessed the credential vault")
			}
		})
	}
}

func TestBundledClientFreshStatusDoesNotPersistConfiguration(t *testing.T) {
	store := &memoryStore{}
	data := []byte(clientJSON)
	s, err := NewWithClient(store, func(string) error { t.Fatal("Status opened browser"); return nil }, data)
	if err != nil {
		t.Fatal(err)
	}
	clear(data) // The caller's buffer is not retained by the service.
	if store.reads != 0 || store.writes != 0 {
		t.Fatal("constructor accessed the credential vault")
	}
	for range 2 {
		st, err := s.Status(context.Background())
		if err != nil || st.State != "disconnected" || !st.ClientConfigured || st.Account != nil || st.Scope != Scope {
			t.Fatal(st, err)
		}
		encoded, _ := json.Marshal(st)
		if strings.Contains(string(encoded), "fake-client-secret") || strings.Contains(string(encoded), "123-fake") {
			t.Fatal("status exposed client configuration")
		}
	}
	if store.writes != 0 || store.value != "" {
		t.Fatal("reading ready state created a vault entry")
	}
	if _, err := s.ConfigureClient(context.Background(), []byte(strings.Replace(clientJSON, "123-fake", "456-other", 1))); !errors.Is(err, ErrManagedClient) {
		t.Fatal("managed client accepted an import", err)
	}
	if store.writes != 0 || s.bundled.ID != "123-fake.apps.googleusercontent.com" {
		t.Fatal("import replaced immutable application configuration")
	}
}

func TestBundledClientVaultErrorsRemainFailClosed(t *testing.T) {
	for name, store := range map[string]Store{
		"absent-store":  nil,
		"unavailable":   &memoryStore{getErr: errors.New("private-vault-failure")},
		"invalid-entry": &memoryStore{value: `{"version":1,"private":"private-vault-failure"}`},
	} {
		t.Run(name, func(t *testing.T) {
			s, err := NewWithClient(store, func(string) error { t.Fatal("browser opened despite unavailable vault"); return nil }, []byte(clientJSON))
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []func(context.Context) (Status, error){s.Status, s.Connect, s.Check, s.Disconnect} {
				st, err := operation(context.Background())
				if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" || !st.ClientConfigured || st.Account != nil || strings.Contains(st.Message, "private-vault-failure") {
					t.Fatal(st, err)
				}
			}
			if memory, ok := store.(*memoryStore); ok && memory.writes != 0 {
				t.Fatal("unreadable saved entry was overwritten")
			}
		})
	}
}

func TestBundledAuthorizationPersistsOnlyAfterConsentAndSurvivesRestart(t *testing.T) {
	f := newBundledFixture(t)
	if f.store.writes != 0 {
		t.Fatal("fresh client was persisted before consent")
	}
	connected := f.connect()
	if f.store.writes != 1 || !strings.Contains(f.store.value, "fake-refresh-token") || strings.Contains(f.store.value, "fake-access-token") {
		t.Fatal("connection did not atomically persist the renewable credential")
	}
	restarted, err := NewWithClient(f.store, nil, []byte(clientJSON))
	if err != nil {
		t.Fatal(err)
	}
	restarted.tokenURL, restarted.aboutURL = f.service.tokenURL, f.service.aboutURL
	st, err := restarted.Status(context.Background())
	if err != nil || st.State != "connected" || st.Account == nil || *st.Account != *connected.Account {
		t.Fatal(st, err)
	}
	if f.store.writes != 1 || f.tokenCalls != 1 || f.aboutCalls != 1 {
		t.Fatal("restart Status refreshed or rewrote the saved account")
	}
	if _, err = restarted.Connect(context.Background()); !errors.Is(err, ErrConnected) {
		t.Fatal("existing account could be silently replaced", err)
	}
	if _, err = restarted.ConfigureClient(context.Background(), []byte(clientJSON)); !errors.Is(err, ErrManagedClient) || f.store.writes != 1 {
		t.Fatal("client import overrode an existing grant")
	}
	st, err = restarted.Check(context.Background())
	if err != nil || st.State != "connected" || f.tokenCalls != 2 || f.aboutCalls != 2 {
		t.Fatal("same client did not retain and refresh saved grant", st, err)
	}
	st, err = restarted.Disconnect(context.Background())
	if err != nil || st.State != "disconnected" || st.Account != nil || !st.ClientConfigured || strings.Contains(f.store.value, "fake-refresh-token") {
		t.Fatal(st, err)
	}
}

func TestBundledClientRetainsMatchingImportedAccount(t *testing.T) {
	f := newFixture(t)
	connected := f.connect()
	saved, writes := f.store.value, f.store.writes
	s, err := NewWithClient(f.store, nil, []byte(clientJSON))
	if err != nil {
		t.Fatal(err)
	}
	s.tokenURL, s.aboutURL = f.service.tokenURL, f.service.aboutURL
	st, err := s.Status(context.Background())
	if err != nil || st.State != "connected" || st.Account == nil || *st.Account != *connected.Account || f.store.value != saved || f.store.writes != writes {
		t.Fatal("matching imported grant was replaced", st, err)
	}
	st, err = s.Check(context.Background())
	if err != nil || st.State != "connected" || st.Account == nil || *st.Account != *connected.Account || f.tokenCalls != 2 || f.aboutCalls != 2 {
		t.Fatal("matching imported grant did not refresh normally", st, err)
	}
}

func TestBundledClientFailedInitialConsentDoesNotPersist(t *testing.T) {
	f := newBundledFixture(t)
	f.tokenReply = `{"error":"invalid_grant","error_description":"private-provider-data"}`
	f.tokenStatus = 400
	st, err := f.service.Connect(context.Background())
	if !errors.Is(err, ErrReconnect) || st.State != "disconnected" || !st.ClientConfigured || st.Account != nil || f.store.writes != 0 || f.store.value != "" || f.aboutCalls != 0 {
		t.Fatal("failed initial consent persisted configuration or credentials", st, err)
	}
	st, err = f.service.Status(context.Background())
	if err != nil || st.State != "disconnected" || !st.ClientConfigured || f.tokenCalls != 1 || f.store.writes != 0 {
		t.Fatal("status retried a failed consent", st, err)
	}
}

func TestBundledClientChangeRequiresExplicitDisconnect(t *testing.T) {
	for name, config := range map[string]string{
		"id":     strings.Replace(clientJSON, "123-fake", "456-other", 1),
		"secret": strings.Replace(clientJSON, "fake-client-secret", "different-client-secret", 1),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t) // An alpha.2 imported client and saved account.
			connected := f.connect()
			saved, writes := f.store.value, f.store.writes
			s, err := NewWithClient(f.store, func(string) error { t.Fatal("changed-client grant opened browser"); return nil }, []byte(config))
			if err != nil {
				t.Fatal(err)
			}
			s.tokenURL, s.aboutURL = f.service.tokenURL, f.service.aboutURL
			st, err := s.Status(context.Background())
			if err != nil || st.State != "client_changed" || !st.ClientConfigured || st.Account == nil || *st.Account != *connected.Account || st.Message != ErrClientChanged.Error() {
				t.Fatal(st, err)
			}
			for _, operation := range []func(context.Context) (Status, error){s.Connect, s.Check} {
				st, err = operation(context.Background())
				if !errors.Is(err, ErrClientChanged) || st.State != "client_changed" || st.Account == nil || *st.Account != *connected.Account {
					t.Fatal(st, err)
				}
			}
			if f.store.value != saved || f.store.writes != writes || f.tokenCalls != 1 || f.aboutCalls != 1 {
				t.Fatal("new client touched an old client's token or network")
			}
			f.store.setErr = errors.New("private-vault-failure")
			st, err = s.Disconnect(context.Background())
			if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" || f.store.value != saved {
				t.Fatal("failed disconnect discarded the old grant", st, err)
			}
			f.store.setErr = nil
			st, err = s.Status(context.Background())
			if err != nil || st.State != "client_changed" {
				t.Fatal("failed disconnect lost the migration guard", st, err)
			}
			st, err = s.Disconnect(context.Background())
			if err != nil || st.State != "disconnected" || !st.ClientConfigured || st.Account != nil {
				t.Fatal(st, err)
			}
			var persisted record
			if err := json.Unmarshal([]byte(f.store.value), &persisted); err != nil || persisted.Credential != nil || persisted.Client == nil || *persisted.Client != *s.bundled {
				t.Fatal("disconnect did not select the bundled client and clear credentials")
			}
			restarted, err := NewWithClient(f.store, nil, []byte(config))
			if err != nil {
				t.Fatal(err)
			}
			st, err = restarted.Status(context.Background())
			if err != nil || st.State != "disconnected" || !st.ClientConfigured || st.Account != nil || f.tokenCalls != 1 || f.aboutCalls != 1 {
				t.Fatal("disconnected state was not retained without network", st, err)
			}
		})
	}
}

func TestBundledClientReplacesLegacyClientOnlyRecordWithoutVaultWrite(t *testing.T) {
	f := newBundledFixture(t)
	legacy := New(f.store, nil)
	oldJSON := strings.Replace(clientJSON, "123-fake", "456-legacy", 1)
	if _, err := legacy.ConfigureClient(context.Background(), []byte(oldJSON)); err != nil {
		t.Fatal(err)
	}
	saved, writes := f.store.value, f.store.writes
	st, err := f.service.Status(context.Background())
	if err != nil || st.State != "disconnected" || !st.ClientConfigured || st.Account != nil || f.store.value != saved || f.store.writes != writes {
		t.Fatal("status mutated a legacy client-only record", st, err)
	}
	f.connect() // The fake server checks that the bundled client, not the legacy one, is used.
	if strings.Contains(f.store.value, "456-legacy") || f.store.writes != writes+1 {
		t.Fatal("successful consent did not atomically replace the legacy client-only record")
	}
}
