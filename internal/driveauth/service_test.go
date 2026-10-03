package driveauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const clientJSON = `{"installed":{"client_id":"123-fake.apps.googleusercontent.com","project_id":"fake-project","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","auth_provider_x509_cert_url":"https://www.googleapis.com/oauth2/v1/certs","client_secret":"fake-client-secret","redirect_uris":["http://localhost"]}}`

type memoryStore struct {
	mu             sync.Mutex
	value          string
	getErr, setErr error
	reads, writes  int
}

func (m *memoryStore) Get(string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads++
	if m.getErr != nil {
		return "", m.getErr
	}
	if m.value == "" {
		return "", ErrNotFound
	}
	return m.value, nil
}
func (m *memoryStore) Set(_ string, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.value = v
	m.writes++
	return nil
}
func (m *memoryStore) Delete(string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = ""
	return nil
}

type fixture struct {
	t                      *testing.T
	service                *Service
	store                  *memoryStore
	server                 *httptest.Server
	mu                     sync.Mutex
	auth                   url.Values
	tokenCalls, aboutCalls int
	refresh                bool
	tokenReply             string
	tokenStatus            int
	identity               string
	tokenHandler           func(http.ResponseWriter, *http.Request)
	aboutHandler           func(http.ResponseWriter, *http.Request)
}

func newFixture(t *testing.T) *fixture {
	return newTestFixture(t, false)
}

func newBundledFixture(t *testing.T) *fixture {
	return newTestFixture(t, true)
}

func newTestFixture(t *testing.T, bundled bool) *fixture {
	t.Helper()
	f := &fixture{t: t, store: &memoryStore{}, tokenStatus: 200, identity: "fake-account-1"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			f.token(w, r)
		case "/about":
			f.about(w, r)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	if bundled {
		var err error
		f.service, err = NewWithClient(f.store, f.open, []byte(clientJSON))
		if err != nil {
			t.Fatal(err)
		}
	} else {
		f.service = New(f.store, f.open)
	}
	f.service.tokenURL = f.server.URL + "/token"
	f.service.aboutURL = f.server.URL + "/about?fields=user(displayName,emailAddress,permissionId)"
	if !bundled {
		if st, err := f.service.ConfigureClient(context.Background(), []byte(clientJSON)); err != nil || st.State != "disconnected" {
			t.Fatalf("configure: %+v %v", st, err)
		}
	}
	return f
}
func (f *fixture) open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	q := u.Query()
	f.mu.Lock()
	f.auth = q
	f.mu.Unlock()
	if u.Scheme != "https" || u.Host != "accounts.google.com" || u.Path != "/o/oauth2/v2/auth" {
		f.t.Errorf("unexpected authorization origin")
	}
	for key, want := range map[string]string{"scope": Scope, "code_challenge_method": "S256", "access_type": "offline", "prompt": "consent select_account", "response_type": "code"} {
		if q.Get(key) != want {
			f.t.Errorf("invalid authorization parameter %s", key)
		}
	}
	if len(q.Get("state")) != 43 || len(q.Get("code_challenge")) != 43 {
		f.t.Error("missing random state/PKCE")
	}
	callback, _ := url.Parse(q.Get("redirect_uri"))
	if callback.Hostname() != "127.0.0.1" || callback.Port() == "" || callback.Path != "/" {
		f.t.Errorf("invalid loopback binding")
	}
	callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"fake-code"}}.Encode()
	resp, err := http.Get(callback.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(data), "fake-code") {
		f.t.Errorf("unexpected callback response: %d", resp.StatusCode)
	}
	return nil
}
func (f *fixture) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokenCalls++
	handler := f.tokenHandler
	reply := f.tokenReply
	status := f.tokenStatus
	auth := f.auth
	f.mu.Unlock()
	if handler != nil {
		handler(w, r)
		return
	}
	if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		f.t.Error("unexpected token request")
	}
	if err := r.ParseForm(); err != nil {
		f.t.Error(err)
	}
	if r.Form.Get("client_id") != "123-fake.apps.googleusercontent.com" || r.Form.Get("client_secret") != "fake-client-secret" {
		f.t.Error("incorrect client exchange")
	}
	grant := r.Form.Get("grant_type")
	if grant == "authorization_code" {
		verifier := r.Form.Get("code_verifier")
		hash := sha256.Sum256([]byte(verifier))
		if len(verifier) != 43 || base64.RawURLEncoding.EncodeToString(hash[:]) != auth.Get("code_challenge") || r.Form.Get("code") != "fake-code" || r.Form.Get("redirect_uri") != auth.Get("redirect_uri") {
			f.t.Error("PKCE exchange mismatch")
		}
	} else if grant != "refresh_token" {
		f.t.Error("invalid grant")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if reply == "" {
		reply = `{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + Scope + `","expires_in":3600}`
	}
	_, _ = io.WriteString(w, reply)
}
func (f *fixture) about(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.aboutCalls++
	handler := f.aboutHandler
	id := f.identity
	f.mu.Unlock()
	if handler != nil {
		handler(w, r)
		return
	}
	if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fake-access-token" || r.URL.Query().Get("fields") != "user(displayName,emailAddress,permissionId)" {
		f.t.Error("unexpected identity request")
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"displayName": "Fake User", "emailAddress": "fake@example.test", "permissionId": id}})
}
func (f *fixture) connect() Status {
	f.t.Helper()
	st, err := f.service.Connect(context.Background())
	if err != nil || st.State != "connected" {
		f.t.Fatalf("connect: %+v %v", st, err)
	}
	return st
}

func TestConnectionPKCEPersistenceRestartAndDisconnect(t *testing.T) {
	f := newFixture(t)
	st := f.connect()
	if st.Account == nil || !validAccount(*st.Account) || st.Account.Email != "fake@example.test" {
		t.Fatalf("missing safe identity: %+v", st)
	}
	data, _ := json.Marshal(st)
	for _, secret := range []string{"fake-client-secret", "fake-access-token", "fake-refresh-token", "fake-code", "fake-account-1"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("DTO exposed %s", secret)
		}
	}
	saved, _ := f.store.Get(storageKey)
	if strings.Contains(saved, "fake-access-token") || strings.Contains(saved, "expiry") || !strings.Contains(saved, "fake-refresh-token") {
		t.Fatal("unexpected persisted credential fields")
	}
	if len(saved) > 2560 {
		t.Fatal("record exceeds Windows vault capacity")
	}
	st, err := f.service.Check(context.Background())
	if err != nil || st.State != "connected" {
		t.Fatalf("check: %+v %v", st, err)
	}
	if f.tokenCalls != 1 || f.aboutCalls != 2 {
		t.Fatalf("unexpected network counts %d %d", f.tokenCalls, f.aboutCalls)
	}
	restarted := New(f.store, nil)
	restarted.tokenURL = f.service.tokenURL
	restarted.aboutURL = f.service.aboutURL
	if _, err = restarted.Check(context.Background()); err != nil {
		t.Fatalf("restart refresh: %v", err)
	}
	if f.tokenCalls != 2 {
		t.Fatal("restart must refresh without persisted access token")
	}
	before := f.aboutCalls + f.tokenCalls
	st, err = restarted.Disconnect(context.Background())
	if err != nil || st.State != "disconnected" || st.Account != nil {
		t.Fatalf("disconnect: %+v %v", st, err)
	}
	saved, _ = f.store.Get(storageKey)
	if strings.Contains(saved, "fake-refresh-token") || strings.Contains(saved, "fake@example.test") {
		t.Fatal("disconnect retained account credential")
	}
	if before != f.aboutCalls+f.tokenCalls {
		t.Fatal("disconnect must not revoke or perform cloud operations")
	}
}
func TestClientImportRejectsNonDesktopAndCustomEndpoints(t *testing.T) {
	for name, data := range map[string]string{
		"web":             strings.Replace(clientJSON, "installed", "web", 1),
		"service-account": `{"type":"service_account"}`,
		"custom-token":    strings.Replace(clientJSON, tokenEndpoint, "https://attacker.invalid/token", 1),
		"custom-auth":     strings.Replace(clientJSON, "https://accounts.google.com/o/oauth2/auth", "https://attacker.invalid/auth", 1),
		"custom-certs":    strings.Replace(clientJSON, "https://www.googleapis.com/oauth2/v1/certs", "https://attacker.invalid/certs", 1),
		"custom-redirect": strings.Replace(clientJSON, "http://localhost", "http://attacker.invalid", 1),
		"client-id":       strings.Replace(clientJSON, "123-fake.apps.googleusercontent.com", "attacker.invalid", 1),
		"unknown":         strings.Replace(clientJSON, `"installed":`, `"extra":true,"installed":`, 1),
		"duplicate":       strings.Replace(clientJSON, `"project_id":"fake-project"`, `"project_id":"fake-project","project_id":"other"`, 1),
		"trailing":        clientJSON + `{}`,
		"oversize":        strings.Repeat("x", 32769),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseClient([]byte(data)); !errors.Is(err, ErrClient) {
				t.Fatalf("expected safe client error: %v", err)
			}
		})
	}
}
func TestSetupVaultFailureAndRedaction(t *testing.T) {
	secretErr := errors.New("secret-refresh-token-here")
	for name, store := range map[string]*memoryStore{"missing": {}, "locked": {getErr: secretErr}, "corrupt": {value: `{"secret":"secret-refresh-token-here"}`}} {
		t.Run(name, func(t *testing.T) {
			s := New(store, nil)
			st, err := s.Status(context.Background())
			if name == "missing" {
				if err != nil || st.State != "setup_required" {
					t.Fatal(st, err)
				}
			} else if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" {
				t.Fatal(st, err)
			}
			data, _ := json.Marshal(st)
			if strings.Contains(string(data), "secret-refresh-token-here") {
				t.Fatal("storage error leaked")
			}
		})
	}
	s := New(&memoryStore{}, nil)
	if _, err := s.Connect(context.Background()); !errors.Is(err, ErrSetup) {
		t.Fatal(err)
	}
	f := newFixture(t)
	f.store.setErr = secretErr
	st, err := f.service.Connect(context.Background())
	if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" || st.Account != nil {
		t.Fatal(st, err)
	}
}
func TestInvalidTokenResponsesAreRedacted(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
		want   error
	}{
		"malformed":       {`secret-provider-body`, 200, ErrToken},
		"missing-access":  {`{"refresh_token":"secret-provider-body","token_type":"Bearer","expires_in":3600}`, 200, ErrToken},
		"bad-type":        {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"MAC","scope":"` + Scope + `","expires_in":3600}`, 200, ErrToken},
		"bad-expiry":      {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + Scope + `","expires_in":-1}`, 200, ErrToken},
		"missing-refresh": {`{"access_token":"fake-access-token","token_type":"Bearer","scope":"` + Scope + `","expires_in":3600}`, 200, ErrToken},
		"broad-scope":     {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/gmail.readonly","expires_in":3600}`, 200, ErrScopeUnexpected},
		"narrow-scope":    {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"https://www.googleapis.com/auth/drive.file","expires_in":3600}`, 200, ErrScopeNotGranted},
		"missing-scope":   {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","expires_in":3600}`, 200, ErrScope},
		"extra-scope":     {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + Scope + ` https://www.googleapis.com/auth/drive.appdata","expires_in":3600}`, 200, ErrScopeUnexpected},
		"repeated-scope":  {`{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + Scope + ` ` + Scope + `","expires_in":3600}`, 200, ErrScopeUnexpected},
		"invalid-grant":   {`{"error":"invalid_grant","error_description":"secret-provider-body"}`, 400, ErrReconnect},
		"provider-error":  {`{"error":"secret-provider-body"}`, 500, ErrNetwork},
		"oversize":        {strings.Repeat("secret-provider-body", 4096), 200, ErrToken},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.tokenReply = tc.body
			f.tokenStatus = tc.status
			st, err := f.service.Connect(context.Background())
			if !errors.Is(err, tc.want) || st.Account != nil {
				t.Fatal(st, err)
			}
			encoded, _ := json.Marshal(st)
			if strings.Contains(string(encoded), "secret-provider-body") {
				t.Fatal("provider error leaked")
			}
			if f.aboutCalls != 0 {
				t.Fatal("invalid token reached Drive API")
			}
		})
	}
}
func TestCallbackValidationAndSingleUse(t *testing.T) {
	results := make(chan callbackResult, 1)
	handler := newCallback(context.Background(), "fake-state", "127.0.0.1:1234", results)
	for _, tc := range []struct {
		method, path, host string
		status             int
	}{
		{"POST", "/?state=fake-state&code=fake-code", "127.0.0.1:1234", 405},
		{"GET", "/other?state=fake-state&code=fake-code", "127.0.0.1:1234", 404},
		{"GET", "/?state=fake-state&code=fake-code", "attacker.invalid", 404},
		{"GET", "/?state=wrong&code=fake-code", "127.0.0.1:1234", 403},
		{"GET", "/?state=fake-state&state=wrong&code=fake-code", "127.0.0.1:1234", 403},
		{"GET", "/?state=%zz&code=fake-code", "127.0.0.1:1234", 403},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s => %d", tc.path, w.Code)
		}
		if len(results) != 0 {
			t.Fatal("untrusted callback consumed authorization")
		}
	}
	r := httptest.NewRequest("GET", "/?state=fake-state&code=fake-code", nil)
	r.Host = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || len(results) != 1 || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("valid callback not delivered")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 409 || len(results) != 1 {
		t.Fatal("duplicate callback accepted")
	}
	if result := <-results; result.code != "fake-code" || result.err != nil {
		t.Fatal("incorrect callback")
	}
}
func TestDeniedAndMalformedTrustedCallback(t *testing.T) {
	for query, want := range map[string]error{"error=access_denied&error_description=secret": ErrDenied, "code=one&code=two": ErrCallback, "code=one&error=denied": ErrCallback, "": ErrCallback} {
		results := make(chan callbackResult, 1)
		handler := newCallback(context.Background(), "state", "127.0.0.1:1234", results)
		r := httptest.NewRequest("GET", "/?state=state&"+query, nil)
		r.Host = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := <-results; !errors.Is(got.err, want) {
			t.Fatal(got.err)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("callback error leaked")
		}
	}
}
func TestCancellationTimeoutBrowserFailureAndListenerClosure(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "browser"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			opened := make(chan string, 1)
			f.service.timeout = 60 * time.Millisecond
			f.service.openURL = func(raw string) error {
				u, _ := url.Parse(raw)
				opened <- u.Query().Get("redirect_uri")
				if mode == "browser" {
					return errors.New("secret-browser-command")
				}
				return nil
			}
			finished := make(chan error, 1)
			go func() { _, err := f.service.Connect(context.Background()); finished <- err }()
			address := <-opened
			if mode == "cancel" {
				f.service.Cancel()
			}
			var want error
			switch mode {
			case "cancel":
				want = ErrCanceled
			case "timeout":
				want = ErrTimeout
			case "browser":
				want = ErrBrowser
			}
			select {
			case err := <-finished:
				if !errors.Is(err, want) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("authorization did not finish")
			}
			client := &http.Client{Timeout: time.Second}
			if resp, err := client.Get(address); err == nil {
				resp.Body.Close()
				t.Fatal("loopback listener retained after completion")
			}
			if f.tokenCalls != 0 || f.aboutCalls != 0 {
				t.Fatal("canceled authorization reached Google")
			}
		})
	}
}
func TestOperationsCannotReplaceAccountAndConcurrentConnect(t *testing.T) {
	f := newFixture(t)
	opened := make(chan struct{}, 1)
	f.service.openURL = func(string) error { opened <- struct{}{}; return nil }
	finished := make(chan error, 1)
	go func() { _, err := f.service.Connect(context.Background()); finished <- err }()
	<-opened
	if st, err := f.service.Status(context.Background()); err != nil || st.State != "connecting" {
		t.Fatal(st, err)
	}
	for _, call := range []func() (Status, error){func() (Status, error) { return f.service.Connect(context.Background()) }, func() (Status, error) { return f.service.ConfigureClient(context.Background(), []byte(clientJSON)) }, func() (Status, error) { return f.service.Check(context.Background()) }} {
		if _, err := call(); !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
	}
	f.service.Cancel()
	<-finished
	f.service.openURL = f.open
	f.connect()
	if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrConnected) {
		t.Fatal(err)
	}
	if _, err := f.service.ConfigureClient(context.Background(), []byte(clientJSON)); !errors.Is(err, ErrConnected) {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.identity = "other-account"
	f.mu.Unlock()
	st, err := f.service.Check(context.Background())
	if !errors.Is(err, ErrIdentity) || st.State != "reconnect_required" {
		t.Fatal(st, err)
	}
	if _, err = f.service.Connect(context.Background()); !errors.Is(err, ErrIdentity) {
		t.Fatal("account replacement during reconnect was accepted", err)
	}
	if _, err = f.service.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.connect()
}
func TestRefreshRotationPersistsAndOmittedRefreshIsRetained(t *testing.T) {
	f := newFixture(t)
	f.connect()
	f.service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	f.tokenReply = `{"access_token":"fake-access-token","refresh_token":"rotated-refresh","token_type":"Bearer","expires_in":3600}`
	if _, err := f.service.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, _ := f.store.Get(storageKey)
	if !strings.Contains(saved, "rotated-refresh") {
		t.Fatal("refresh rotation lost")
	}
	f.service.now = func() time.Time { return time.Now().Add(4 * time.Hour) }
	f.tokenReply = `{"access_token":"fake-access-token","token_type":"Bearer","expires_in":3600}`
	if _, err := f.service.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, _ = f.store.Get(storageKey)
	if !strings.Contains(saved, "rotated-refresh") {
		t.Fatal("omitted refresh erased existing token")
	}
}
func TestRevokedGrantStopsRefreshAndPromptsReconnect(t *testing.T) {
	f := newFixture(t)
	f.connect()
	f.service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	f.tokenReply = `{"error":"invalid_grant","error_description":"secret-provider-body"}`
	f.tokenStatus = 400
	st, err := f.service.Check(context.Background())
	if !errors.Is(err, ErrReconnect) || st.State != "reconnect_required" {
		t.Fatal(st, err)
	}
	before := f.tokenCalls
	if _, err = f.service.Check(context.Background()); !errors.Is(err, ErrReconnect) {
		t.Fatal(err)
	}
	if f.tokenCalls != before {
		t.Fatal("revoked grant repeatedly refreshed")
	}
	f.tokenStatus = 200
	f.tokenReply = ""
	f.connect()
}
func TestUnexpectedAccessTokenExpiryRefreshesOnlyOnce(t *testing.T) {
	f := newFixture(t)
	f.connect()
	f.aboutHandler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":"fake-expired"}`)
	}
	st, err := f.service.Check(context.Background())
	if !errors.Is(err, ErrReconnect) || st.State != "reconnect_required" {
		t.Fatal(st, err)
	}
	if f.tokenCalls != 2 || f.aboutCalls != 3 {
		t.Fatalf("refresh budget not enforced: %d %d", f.tokenCalls, f.aboutCalls)
	}
}
func TestRedirectsNeverReceiveCredentials(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1) }))
	defer target.Close()
	f := newFixture(t)
	f.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}
	if _, err := f.service.Connect(context.Background()); !errors.Is(err, ErrToken) {
		t.Fatal(err)
	}
	if followed.Load() != 0 {
		t.Fatal("credential-bearing redirect followed")
	}
}
func TestNetworkCancellationAndRotationStorageFailure(t *testing.T) {
	t.Run("cancel-network", func(t *testing.T) {
		f := newFixture(t)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		f.tokenHandler = func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release }
		done := make(chan error, 1)
		go func() { _, err := f.service.Connect(context.Background()); done <- err }()
		<-entered
		f.service.Cancel()
		err := <-done
		close(release)
		if !errors.Is(err, ErrCanceled) {
			t.Fatal(err)
		}
	})
	t.Run("save-rotation", func(t *testing.T) {
		f := newFixture(t)
		f.connect()
		f.service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		f.store.setErr = errors.New("private-vault-error")
		st, err := f.service.Check(context.Background())
		if !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" {
			t.Fatal(st, err)
		}
		if f.aboutCalls != 1 {
			t.Fatal("continued after failed refresh persistence")
		}
	})
}
func TestVaultSizeAndMetadataCompaction(t *testing.T) {
	f := newFixture(t)
	f.aboutHandler = func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"displayName": strings.Repeat("N", 1000), "emailAddress": strings.Repeat("E", 1000), "permissionId": "fake-account-1"}})
	}
	f.tokenReply = fmt.Sprintf(`{"access_token":"fake-access-token","refresh_token":"%s","token_type":"Bearer","scope":"%s","expires_in":3600}`, strings.Repeat("R", 512), Scope)
	st := f.connect()
	if len(st.Account.DisplayName) != 1000 {
		t.Fatal("current account metadata lost")
	}
	saved, _ := f.store.Get(storageKey)
	if len(saved) > 2560 || !strings.Contains(saved, st.Account.Reference) {
		t.Fatal("bounded account binding lost")
	}
	var stored record
	if err := json.Unmarshal([]byte(saved), &stored); err != nil || stored.Credential.Account.DisplayName != "" {
		t.Fatal("metadata compaction not applied")
	}
	f2 := newFixture(t)
	f2.tokenReply = fmt.Sprintf(`{"access_token":"fake-access-token","refresh_token":"%s","token_type":"Bearer","scope":"%s","expires_in":3600}`, strings.Repeat("R", 3000), Scope)
	if st, err := f2.service.Connect(context.Background()); !errors.Is(err, ErrStorage) || st.Account != nil {
		t.Fatal(st, err)
	}
}

func TestConnectAcceptsFullDriveReportedWithEarlierOrSignInScopes(t *testing.T) {
	for _, scope := range []string{
		legacyScope + " " + Scope,
		"openid email profile " + Scope,
		"https://www.googleapis.com/auth/userinfo.email " + Scope + " https://www.googleapis.com/auth/userinfo.profile",
	} {
		f := newFixture(t)
		f.tokenReply = `{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"` + scope + `","expires_in":3600}`
		st, err := f.service.Connect(context.Background())
		if err != nil || st.State != "connected" || st.Account == nil {
			t.Fatal(scope, st, err)
		}
	}
}
