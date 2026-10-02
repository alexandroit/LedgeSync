package driveauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleStore struct {
	Store
	reads, writes, deletes atomic.Int32
	block                  string
	entered                chan struct{}
	resume                 chan struct{}
	once                   sync.Once
}

func (v *lifecycleStore) wait(operation string) {
	if v.block == operation {
		v.once.Do(func() { close(v.entered); <-v.resume })
	}
}
func (v *lifecycleStore) Get(key string) (string, error) {
	v.reads.Add(1)
	v.wait("get")
	return v.Store.Get(key)
}
func (v *lifecycleStore) Set(key, value string) error {
	v.writes.Add(1)
	v.wait("set")
	return v.Store.Set(key, value)
}
func (v *lifecycleStore) Delete(key string) error { v.deletes.Add(1); return v.Store.Delete(key) }

func lifecycleResult(t *testing.T, pending <-chan error) error {
	t.Helper()
	select {
	case err := <-pending:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not finish within its test bound")
		return nil
	}
}
func lifecycleRevokeServer(t *testing.T, f *fixture, handler http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/revoke" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("revocation request exposed unexpected method, URL or headers")
		}
		if err := r.ParseForm(); err != nil || len(r.PostForm) != 1 || r.PostForm.Get("token") != "fake-refresh-token" {
			t.Error("revocation body did not contain exactly the selected refresh token")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	f.service.revokeURL = server.URL + "/revoke"
	return calls
}

func TestRevokeRequiresConfirmationBeforeEverySideEffect(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	counted := &lifecycleStore{Store: f.store}
	f.service.store = counted
	calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) { t.Error("unconfirmed revocation reached HTTP") })
	for _, input := range []struct {
		reference string
		confirmed bool
	}{{account, false}, {"", true}, {"not-an-account", true}} {
		before, _ := f.store.Get(storageKey)
		st, err := f.service.Revoke(context.Background(), input.reference, input.confirmed)
		after, _ := f.store.Get(storageKey)
		if !errors.Is(err, ErrRevokeConfirmation) || st.State != "connected" || before != after {
			t.Fatal("unconfirmed request changed connection state")
		}
	}
	if counted.reads.Load() != 0 || counted.writes.Load() != 0 || counted.deletes.Load() != 0 || calls.Load() != 0 {
		t.Fatal("confirmation was checked after a side effect")
	}
	if New(nil, nil).revokeURL != "https://oauth2.googleapis.com/revoke" {
		t.Fatal("production revocation endpoint changed")
	}
}

func TestUnconfirmedRevokeDoesNotCancelAuthorization(t *testing.T) {
	f := newFixture(t)
	opened := make(chan struct{})
	f.service.openURL = func(string) error { close(opened); return nil }
	finished := make(chan error, 1)
	go func() { _, err := f.service.Connect(context.Background()); finished <- err }()
	<-opened
	if _, err := f.service.Revoke(context.Background(), "drive_"+strings.Repeat("a", 64), false); !errors.Is(err, ErrRevokeConfirmation) {
		t.Fatal(err)
	}
	select {
	case <-finished:
		t.Fatal("unconfirmed request canceled active authorization")
	default:
	}
	if st, err := f.service.Status(context.Background()); err != nil || st.State != "connecting" {
		t.Fatal("active authorization state changed")
	}
	f.service.Cancel()
	if err := lifecycleResult(t, finished); !errors.Is(err, ErrCanceled) {
		t.Fatal(err)
	}
}

func TestConfirmedRevokeClearsOnlyLocalSelectedGrant(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	st, err := f.service.Revoke(context.Background(), account, true)
	if err != nil || st.State != "disconnected" || st.Account != nil || !st.ClientConfigured {
		t.Fatal("confirmed revocation did not disconnect")
	}
	saved, _ := f.store.Get(storageKey)
	if strings.Contains(saved, "fake-refresh-token") || strings.Contains(saved, "fake@example.test") || f.service.runtime.Credential != nil {
		t.Fatal("revocation retained the selected credential")
	}
	if calls.Load() != 1 || f.tokenCalls != 1 || f.aboutCalls != 1 {
		t.Fatal("revocation made an unexpected request or retry")
	}
	// The same stale confirmation cannot accidentally target a later connection.
	if _, err = f.service.Revoke(context.Background(), account, true); !errors.Is(err, ErrIdentity) || calls.Load() != 1 {
		t.Fatal("stale confirmation was accepted after cleanup")
	}
}

func TestFailedRevocationRetainsGrantWithoutRetries(t *testing.T) {
	for _, status := range []int{400, 403, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFixture(t)
			account := f.connect().Account.Reference
			before, _ := f.store.Get(storageKey)
			calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "private-provider-error fake-refresh-token")
			})
			st, err := f.service.Revoke(context.Background(), account, true)
			after, _ := f.store.Get(storageKey)
			if !errors.Is(err, ErrRevokeFailed) || st.State != "connected" || before != after || calls.Load() != 1 {
				t.Fatal("failed revocation changed or retried a grant")
			}
			encoded, _ := json.Marshal(st)
			if strings.Contains(string(encoded), "private-provider-error") || strings.Contains(err.Error(), "fake-refresh-token") {
				t.Fatal("provider data escaped redaction")
			}
			if _, err = f.service.Check(context.Background()); err != nil {
				t.Fatal("failed revocation discarded still-valid access")
			}
		})
	}
	t.Run("network", func(t *testing.T) {
		f := newFixture(t)
		account := f.connect().Account.Reference
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		f.service.revokeURL = server.URL + "/revoke"
		server.Close()
		before, _ := f.store.Get(storageKey)
		st, err := f.service.Revoke(context.Background(), account, true)
		after, _ := f.store.Get(storageKey)
		if !errors.Is(err, ErrRevokeFailed) || st.State != "connected" || before != after || strings.Contains(err.Error(), server.URL) {
			t.Fatal("network failure changed grant or exposed URL")
		}
	})
	t.Run("redirect", func(t *testing.T) {
		f := newFixture(t)
		account := f.connect().Account.Reference
		var followed atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		defer target.Close()
		calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		})
		if _, err := f.service.Revoke(context.Background(), account, true); !errors.Is(err, ErrRevokeFailed) || calls.Load() != 1 || followed.Load() != 0 {
			t.Fatal("credential-bearing redirect followed")
		}
	})
}

func TestConfirmedRevocationCleanupFailureBlocksFurtherUse(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	before, _ := f.store.Get(storageKey)
	f.store.setErr = errors.New("private-vault-error")
	st, err := f.service.Revoke(context.Background(), account, true)
	if !errors.Is(err, ErrRevokedCleanup) || st.State != "revoked_local_cleanup_required" {
		t.Fatal("confirmed revocation misreported cleanup failure")
	}
	if f.service.runtime != nil {
		t.Fatal("revoked in-memory access token was retained")
	}
	after, _ := f.store.Get(storageKey)
	if before != after {
		t.Fatal("failed vault write unexpectedly changed persistent grant")
	}
	for _, operation := range []func() (Status, error){
		func() (Status, error) { return f.service.Connect(context.Background()) },
		func() (Status, error) { return f.service.Check(context.Background()) },
		func() (Status, error) { return f.service.Revoke(context.Background(), account, true) },
	} {
		st, err = operation()
		if !errors.Is(err, ErrRevokedCleanup) || st.State != "revoked_local_cleanup_required" {
			t.Fatal("revoked grant could be reused")
		}
	}
	if st, err = f.service.Status(context.Background()); err != nil || st.State != "revoked_local_cleanup_required" {
		t.Fatal("cleanup state lost in status reconciliation")
	}
	if calls.Load() != 1 || f.tokenCalls != 1 || f.aboutCalls != 1 {
		t.Fatal("cleanup failure caused another remote request")
	}
	f.store.setErr = nil
	st, err = f.service.Disconnect(context.Background())
	if err != nil || st.State != "disconnected" || st.Account != nil {
		t.Fatal("local cleanup retry failed")
	}
	if calls.Load() != 1 {
		t.Fatal("local cleanup repeated remote revocation")
	}
}

func TestRevocationRejectsStaleAccountAndChangedClient(t *testing.T) {
	t.Run("stale-account", func(t *testing.T) {
		f := newFixture(t)
		account := f.connect().Account.Reference
		calls := lifecycleRevokeServer(t, f, func(http.ResponseWriter, *http.Request) { t.Error("stale account reached revocation") })
		r, err := f.service.load()
		if err != nil {
			t.Fatal(err)
		}
		r.Credential.Account.Reference = "drive_" + strings.Repeat("b", 64)
		r.Credential.Account.DisplayName = "Other synthetic account"
		if err = f.service.save(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		before, _ := f.store.Get(storageKey)
		if _, err = f.service.Revoke(context.Background(), account, false); !errors.Is(err, ErrRevokeConfirmation) {
			t.Fatal(err)
		}
		st, err := f.service.Revoke(context.Background(), account, true)
		after, _ := f.store.Get(storageKey)
		if !errors.Is(err, ErrIdentity) || st.Account.Reference == account || before != after || calls.Load() != 0 {
			t.Fatal("stale confirmation retargeted another account")
		}
	})
	t.Run("changed-client", func(t *testing.T) {
		f := newFixture(t)
		account := f.connect().Account.Reference
		calls := lifecycleRevokeServer(t, f, func(http.ResponseWriter, *http.Request) { t.Error("changed client reached revocation") })
		original, _ := parseClient([]byte(clientJSON))
		f.service.bundled = original
		r, err := f.service.load()
		if err != nil {
			t.Fatal(err)
		}
		r.Client.ID = "other-client.apps.googleusercontent.com"
		data, _ := json.Marshal(r)
		if err = f.store.Set(storageKey, string(data)); err != nil {
			t.Fatal(err)
		}
		loaded, err := f.service.load()
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Credential.AccessToken != "" {
			t.Fatal("access token crossed OAuth client boundary")
		}
		before, _ := f.store.Get(storageKey)
		for _, operation := range []func() (Status, error){
			func() (Status, error) { return f.service.Check(context.Background()) },
			func() (Status, error) { return f.service.Connect(context.Background()) },
			func() (Status, error) { return f.service.Revoke(context.Background(), account, true) },
		} {
			st, err := operation()
			if !errors.Is(err, ErrClientChanged) || st.State != "client_changed" {
				t.Fatal("client isolation guard failed")
			}
		}
		after, _ := f.store.Get(storageKey)
		if before != after || calls.Load() != 0 || f.tokenCalls != 1 || f.aboutCalls != 1 {
			t.Fatal("changed client grant was used or replaced")
		}
		if _, err = f.service.Disconnect(context.Background()); err != nil {
			t.Fatal("explicit local disconnection was blocked")
		}
	})
}

func TestDisconnectAndRevokeCancelInFlightChecks(t *testing.T) {
	for _, action := range []string{"disconnect", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := newFixture(t)
			account := f.connect().Account.Reference
			entered := make(chan struct{})
			f.aboutHandler = func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }
			calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
			checked := make(chan error, 1)
			go func() { _, err := f.service.Check(context.Background()); checked <- err }()
			<-entered
			var st Status
			var err error
			if action == "disconnect" {
				st, err = f.service.Disconnect(context.Background())
			} else {
				st, err = f.service.Revoke(context.Background(), account, true)
			}
			if err != nil || st.State != "disconnected" || st.Account != nil {
				t.Fatal("lifecycle action did not drain and clear")
			}
			if err = lifecycleResult(t, checked); !errors.Is(err, ErrCanceled) {
				t.Fatal("in-flight check was not canceled")
			}
			want := int32(0)
			if action == "revoke" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatal("unexpected revocation count")
			}
			saved, _ := f.store.Get(storageKey)
			if strings.Contains(saved, "fake-refresh-token") {
				t.Fatal("check restored removed credential")
			}
		})
	}
}

func TestDisconnectWaitsForStartedVaultWriteBeforeClearing(t *testing.T) {
	f := newFixture(t)
	f.connect()
	blocked := &lifecycleStore{Store: f.store, block: "set", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	var resume sync.Once
	defer resume.Do(func() { close(blocked.resume) })
	checked := make(chan error, 1)
	go func() { _, err := f.service.Check(context.Background()); checked <- err }()
	<-blocked.entered
	disconnected := make(chan error, 1)
	go func() { _, err := f.service.Disconnect(context.Background()); disconnected <- err }()
	// Wait for the lifecycle gate without timing assumptions about scheduling.
	deadline := time.Now().Add(time.Second)
	for {
		f.service.mu.Lock()
		stopping := f.service.stopping
		f.service.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drain gate not acquired")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-disconnected:
		t.Fatal("cleanup returned before prior vault write completed")
	default:
	}
	if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("new authorization started while draining")
	}
	resume.Do(func() { close(blocked.resume) })
	_ = lifecycleResult(t, checked)
	if err := lifecycleResult(t, disconnected); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.store.Get(storageKey)
	if strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("started write resurrected cleared token")
	}
	if st, err := f.service.Status(context.Background()); err != nil || st.State != "disconnected" {
		t.Fatal("stale operation state survived cleanup")
	}
}

func TestDrainTimeoutDoesNotScheduleDelayedCleanup(t *testing.T) {
	f := newFixture(t)
	f.connect()
	blocked := &lifecycleStore{Store: f.store, block: "set", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	var resume sync.Once
	defer resume.Do(func() { close(blocked.resume) })
	checked := make(chan error, 1)
	go func() { _, err := f.service.Check(context.Background()); checked <- err }()
	<-blocked.entered
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := f.service.Disconnect(ctx); !errors.Is(err, ErrTimeout) {
		t.Fatal("blocked cleanup was incorrectly reported as successful")
	}
	resume.Do(func() { close(blocked.resume) })
	_ = lifecycleResult(t, checked)
	if blocked.writes.Load() != 1 {
		t.Fatal("timed-out lifecycle scheduled a late vault mutation")
	}
	saved, _ := f.store.Get(storageKey)
	if !strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("timed-out action performed delayed cleanup")
	}
	if _, err := f.service.Disconnect(context.Background()); err != nil {
		t.Fatal("retry after drained failure was blocked")
	}
}

func TestDisconnectCancelsBeforeOperationRegistration(t *testing.T) {
	f := newFixture(t)
	blocked := &lifecycleStore{Store: f.store, block: "get", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	var resume sync.Once
	defer resume.Do(func() { close(blocked.resume) })
	opened := atomic.Int32{}
	f.service.openURL = func(string) error { opened.Add(1); return nil }
	connected := make(chan error, 1)
	go func() { _, err := f.service.Connect(context.Background()); connected <- err }()
	<-blocked.entered
	disconnected := make(chan error, 1)
	go func() { _, err := f.service.Disconnect(context.Background()); disconnected <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		f.service.mu.Lock()
		stopping := f.service.stopping
		f.service.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drain gate not acquired")
		}
		time.Sleep(time.Millisecond)
	}
	resume.Do(func() { close(blocked.resume) })
	if err := lifecycleResult(t, connected); !errors.Is(err, ErrCanceled) {
		t.Fatal("starting operation escaped cancellation")
	}
	if err := lifecycleResult(t, disconnected); err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 0 || f.tokenCalls != 0 || f.aboutCalls != 0 {
		t.Fatal("canceled startup launched authorization")
	}
}

func TestProviderJSONRejectsDuplicateAndCaseAliasedFields(t *testing.T) {
	valid := `{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + Scope + `","expires_in":3600}`
	for _, field := range []string{
		`"scope":"https://www.googleapis.com/auth/drive",`,
		`"Scope":"https://www.googleapis.com/auth/drive",`,
		`"\u017Fcope":"https://www.googleapis.com/auth/drive",`,
		`"access_to\u212Aen":"other-token",`,
		`"access_token":"other-token",`,
		`"Access_Token":"other-token",`,
		`"TOKEN_TYPE":"MAC",`,
	} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			f.tokenReply = "{" + field + valid[1:]
			if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrToken) {
				t.Fatal("ambiguous token response accepted")
			}
			if f.aboutCalls != 0 {
				t.Fatal("ambiguous token reached Drive")
			}
		})
	}
	t.Run("unknown-extension", func(t *testing.T) {
		f := newFixture(t)
		f.tokenReply = `{"provider_extension":{"supported":[true]},` + valid[1:]
		f.connect()
	})
	t.Run("error-alias", func(t *testing.T) {
		f := newFixture(t)
		f.tokenStatus = 400
		f.tokenReply = `{"error":"temporary_failure","Error":"invalid_grant"}`
		if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrToken) {
			t.Fatal("ambiguous OAuth error accepted")
		}
	})
	t.Run("identity-alias", func(t *testing.T) {
		f := newFixture(t)
		f.aboutHandler = func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"user":{"permissionId":"one","PermissionId":"two"}}`)
		}
		if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrProvider) {
			t.Fatal("ambiguous identity accepted")
		}
	})
}

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (transport lifecycleTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestConfirmedRevokeFinishesCleanupAfterCallerCancellation(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	f.service.http = &http.Client{Transport: lifecycleTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		// The transport has received a successful response. Cancellation now must
		// not preserve a locally usable token for a grant Google already revoked.
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	st, err := f.service.Revoke(ctx, account, true)
	if err != nil || st.State != "disconnected" || calls.Load() != 1 {
		t.Fatal("confirmed revocation lost cleanup after caller cancellation")
	}
	saved, _ := f.store.Get(storageKey)
	if strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("canceled caller retained remotely revoked credential")
	}
}

func TestRevokeDrainsStartedVaultWriteBeforeRemoteRequest(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	blocked := &lifecycleStore{Store: f.store, block: "set", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	var resume sync.Once
	defer resume.Do(func() { close(blocked.resume) })
	checked := make(chan error, 1)
	go func() { _, err := f.service.Check(context.Background()); checked <- err }()
	<-blocked.entered
	calls := lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	revoked := make(chan error, 1)
	go func() { _, err := f.service.Revoke(context.Background(), account, true); revoked <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		f.service.mu.Lock()
		stopping := f.service.stopping
		f.service.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drain gate not acquired")
		}
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 0 {
		t.Fatal("remote revoke started before prior local write drained")
	}
	resume.Do(func() { close(blocked.resume) })
	_ = lifecycleResult(t, checked)
	if err := lifecycleResult(t, revoked); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.store.Get(storageKey)
	if calls.Load() != 1 || strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("revocation did not leave the drained account disconnected")
	}
}

func TestDisconnectCannotBeUndoneByPendingAuthorizationSave(t *testing.T) {
	f := newFixture(t)
	blocked := &lifecycleStore{Store: f.store, block: "set", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	var resume sync.Once
	defer resume.Do(func() { close(blocked.resume) })
	connected := make(chan error, 1)
	go func() { _, err := f.service.Connect(context.Background()); connected <- err }()
	<-blocked.entered
	disconnected := make(chan error, 1)
	go func() { _, err := f.service.Disconnect(context.Background()); disconnected <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		f.service.mu.Lock()
		stopping := f.service.stopping
		f.service.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drain gate not acquired")
		}
		time.Sleep(time.Millisecond)
	}
	resume.Do(func() { close(blocked.resume) })
	_ = lifecycleResult(t, connected)
	if err := lifecycleResult(t, disconnected); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.store.Get(storageKey)
	if strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("authorization restored credentials after disconnect completed")
	}
	if st, err := f.service.Status(context.Background()); err != nil || st.State != "disconnected" {
		t.Fatal("authorization restored connected status after disconnect")
	}
}

func TestRevokeVaultFailureStopsBeforeNetwork(t *testing.T) {
	f := newFixture(t)
	account := f.connect().Account.Reference
	calls := lifecycleRevokeServer(t, f, func(http.ResponseWriter, *http.Request) { t.Error("unavailable vault reached remote revocation") })
	f.store.getErr = errors.New("private-vault-diagnostic")
	st, err := f.service.Revoke(context.Background(), account, true)
	if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" || calls.Load() != 0 || strings.Contains(st.Message, "private-vault-diagnostic") {
		t.Fatal("unavailable vault failed to stop revocation safely")
	}
}
