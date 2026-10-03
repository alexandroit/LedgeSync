package driveauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const callbackHost = "127.0.0.1:1234"

func callbackRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = callbackHost
	return r
}

func TestCallbackRejectsNoncanonicalRouteAndMissingStateWithoutConsumingAttempt(t *testing.T) {
	for name, target := range map[string]string{
		"missing-state":   "/?code=synthetic-code",
		"empty-state":     "/?state=&code=synthetic-code",
		"encoded-root":    "/%2e%2e/?state=synthetic-state&code=synthetic-code",
		"encoded-slash":   "%2f?state=synthetic-state&code=synthetic-code",
		"absolute-form":   "http://127.0.0.1:1234/?state=synthetic-state&code=synthetic-code",
		"raw-fragment":    "/?state=synthetic-state&code=synthetic-code#fragment",
		"oversized-query": "/?state=synthetic-state&code=" + strings.Repeat("x", 8192),
	} {
		t.Run(name, func(t *testing.T) {
			result := make(chan callbackResult, 1)
			handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
			w := httptest.NewRecorder()
			// ParseRequestURI requires a leading slash; this preserves an encoded
			// alternate root in URL.RawPath while constructing a valid HTTP target.
			r := callbackRequest("/?state=synthetic-state&code=synthetic-code")
			if name == "encoded-slash" {
				r.URL.RawPath, r.RequestURI = "%2f", target
			} else {
				r = callbackRequest(target)
			}
			handler.ServeHTTP(w, r)
			if w.Code < 400 || len(result) != 0 {
				t.Fatal("untrusted route or state consumed an authorization attempt")
			}
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&code=synthetic-code"))
			if w.Code != 200 || len(result) != 1 {
				t.Fatal("rejected request prevented the valid callback")
			}
		})
	}
}

func TestCallbackRejectsParsedFragments(t *testing.T) {
	// Fragments are not transmitted by conforming browsers. Reject one if a
	// non-browser caller or future routing layer supplies it to the handler.
	result := make(chan callbackResult, 1)
	handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
	r := callbackRequest("/?state=synthetic-state&code=synthetic-code")
	r.URL.Fragment = "synthetic-fragment"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound || len(result) != 0 {
		t.Fatal("parsed URL fragment was accepted")
	}
}

func TestCallbackParameterShapeAndRedactedCompletion(t *testing.T) {
	for name, query := range map[string]string{
		"duplicate-extension":         "code=synthetic-code&authuser=0&authuser=1",
		"duplicate-scope":             "code=synthetic-code&scope=" + url.QueryEscape(Scope) + "&scope=" + url.QueryEscape(Scope),
		"duplicate-error-description": "error=access_denied&error_description=synthetic-description&error_description=other",
		"callback-access-token":       "code=synthetic-code&access_token=synthetic-private-token",
		"callback-refresh-token":      "code=synthetic-code&refresh_token=synthetic-private-token",
		"callback-id-token":           "code=synthetic-code&id_token=synthetic-private-token",
		"callback-token-type":         "code=synthetic-code&token_type=Bearer",
		"callback-token-expiry":       "code=synthetic-code&expires_in=3600",

		"wrong-issuer":                 "code=synthetic-code&iss=https://attacker.invalid",
		"mixed-code-error-description": "code=synthetic-code&error_description=synthetic-description",
		"mixed-code-error-uri":         "code=synthetic-code&error_uri=https://attacker.invalid",
	} {
		t.Run(name, func(t *testing.T) {
			result := make(chan callbackResult, 1)
			handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
			r := callbackRequest("/?state=synthetic-state&" + query)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || len(result) != 1 {
				t.Fatal("trusted-state malformed callback did not terminate safely")
			}
			if got := <-result; !errors.Is(got.err, ErrCallback) || got.code != "" {
				t.Fatal("malformed callback became a code exchange")
			}
			assertRedactedCallback(t, w)
			replay := httptest.NewRecorder()
			handler.ServeHTTP(replay, callbackRequest("/?state=synthetic-state&code=synthetic-code"))
			if replay.Code != http.StatusConflict || len(result) != 0 {
				t.Fatal("failed callback state was reusable")
			}
		})
	}
	for name, query := range map[string]string{
		"minimal":           "code=synthetic-code",
		"google-extensions": "code=synthetic-code&authuser=0&prompt=consent&scope=" + url.QueryEscape(Scope) + "&iss=https://accounts.google.com",
		"future-extension":  "code=synthetic-code&extension=synthetic-description&next=https://attacker.invalid",
		"denied":            "error=access_denied&error_description=synthetic-description&error_uri=https://attacker.invalid",
	} {
		t.Run(name, func(t *testing.T) {
			result := make(chan callbackResult, 1)
			handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&"+query))
			if len(result) != 1 {
				t.Fatal("valid callback was not delivered")
			}
			got := <-result
			if name == "denied" {
				if w.Code != http.StatusBadRequest || !errors.Is(got.err, ErrDenied) || got.code != "" {
					t.Fatal("denied consent was not classified safely")
				}
			} else if w.Code != http.StatusOK || got.err != nil || got.code != "synthetic-code" {
				t.Fatal("valid code callback was rejected")
			}
			assertRedactedCallback(t, w)
		})
	}
}

func assertRedactedCallback(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, value := range []string{"synthetic-state", "synthetic-code", "synthetic-private-token", "synthetic-description", "attacker.invalid"} {
		if strings.Contains(w.Body.String(), value) || strings.Contains(w.Header().Get("Location"), value) {
			t.Fatal("callback page reflected authorization material")
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'; base-uri 'none'" || !w.Flushed {
		t.Fatal("callback completion was not flushed with protective headers")
	}
}

func TestExpiredOrCanceledCallbackNeverDeliversCode(t *testing.T) {
	expired, stopExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopExpired()
	canceled, stopCanceled := context.WithCancel(context.Background())
	stopCanceled()
	for name, ctx := range map[string]context.Context{"expired": expired, "canceled": canceled} {
		t.Run(name, func(t *testing.T) {
			result := make(chan callbackResult, 1)
			handler := newCallback(ctx, "synthetic-state", callbackHost, result)
			for range 2 {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&code=synthetic-code"))
				if w.Code != http.StatusGone || len(result) != 0 || strings.Contains(w.Body.String(), "synthetic-") {
					t.Fatal("expired or canceled attempt accepted a callback")
				}
			}
		})
	}
}

func TestConcurrentCallbackReplayDeliversExactlyOnce(t *testing.T) {
	result := make(chan callbackResult, 1)
	handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
	var accepted, rejected atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		workers.Go(func() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&code=synthetic-code"))
			switch w.Code {
			case http.StatusOK:
				accepted.Add(1)
			case http.StatusConflict:
				rejected.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 1 || rejected.Load() != 23 || len(result) != 1 {
		t.Fatal("concurrent callbacks reused the authorization state")
	}
}

func TestEachAuthorizationUsesIndependentFreshStateAndPKCEVerifier(t *testing.T) {
	f := newFixture(t)
	states, verifiers := map[string]bool{}, map[string]bool{}
	f.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error("invalid token request")
		}
		verifier := r.Form.Get("code_verifier")
		f.mu.Lock()
		state, challenge := f.auth.Get("state"), f.auth.Get("code_challenge")
		f.mu.Unlock()
		hash := sha256.Sum256([]byte(verifier))
		decodedState, errState := base64.RawURLEncoding.DecodeString(state)
		decodedVerifier, errVerifier := base64.RawURLEncoding.DecodeString(verifier)
		if errState != nil || errVerifier != nil || len(decodedState) != 32 || len(decodedVerifier) != 32 || state == verifier || states[state] || verifiers[verifier] || base64.RawURLEncoding.EncodeToString(hash[:]) != challenge {
			t.Error("state/verifier independence, freshness, entropy length or S256 binding failed")
		}
		states[state], verifiers[verifier] = true, true
		_, _ = io.WriteString(w, `{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"`+Scope+`","expires_in":3600}`)
	}
	for range 3 {
		f.connect()
		if _, err := f.service.Disconnect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.tokenCalls != 3 {
		t.Fatal("expected three independent authorization attempts")
	}
}

func TestLoopbackListenerIsClosedBeforeTokenExchange(t *testing.T) {
	f := newFixture(t)
	f.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error("invalid token request")
		}
		redirect, err := url.Parse(r.Form.Get("redirect_uri"))
		if err != nil || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" {
			t.Error("token exchange did not retain the exact loopback binding")
		} else if conn, err := net.DialTimeout("tcp4", redirect.Host, 100*time.Millisecond); err == nil {
			conn.Close()
			t.Error("callback listener remained reachable during token exchange")
		}
		_, _ = io.WriteString(w, `{"access_token":"fake-access-token","refresh_token":"fake-refresh-token","token_type":"Bearer","scope":"`+Scope+`","expires_in":3600}`)
	}
	f.connect()
}

func TestCancellationClosesListenerWhileBrowserLauncherReturns(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			opened := make(chan string, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			f.service.openURL = func(raw string) error {
				u, _ := url.Parse(raw)
				redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
				opened <- redirect.Host
				<-release
				return nil
			}
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			finished := make(chan error, 1)
			go func() { _, err := f.service.Connect(ctx); finished <- err }()
			address := <-opened
			if mode == "cancel" {
				f.service.Cancel()
			} else {
				<-ctx.Done()
			}
			closed := false
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				conn, err := net.DialTimeout("tcp4", address, 20*time.Millisecond)
				if err != nil {
					closed = true
					break
				}
				conn.Close()
				time.Sleep(time.Millisecond)
			}
			if !closed {
				t.Error("cancellation left callback listener open while launcher returned")
			}
			releaseOnce.Do(func() { close(release) })
			want := ErrCanceled
			if mode == "timeout" {
				want = ErrTimeout
			}
			select {
			case err := <-finished:
				if !errors.Is(err, want) {
					t.Fatal("canceled attempt returned the wrong safe error")
				}
			case <-time.After(time.Second):
				t.Fatal("authorization did not finish after launcher returned")
			}
			if f.tokenCalls != 0 || f.aboutCalls != 0 {
				t.Fatal("canceled attempt reached the provider")
			}
		})
	}
}

func TestCallbackFailureNamesTheCheckWithoutValues(t *testing.T) {
	for query, reason := range map[string]string{
		"code=synthetic-code&authuser=0&authuser=synthetic-value":  "(repeated parameter)",
		"code=synthetic-code&iss=https://synthetic-issuer.invalid": "(unexpected issuer)",
		"code=synthetic-code&access_token=synthetic-private-token": "(token in callback)",
		"code=synthetic-code&error_description=synthetic-text":     "(error details without an error)",
		"code=synthetic-code&error=access_denied":                  "(no usable code or error)",
	} {
		result := make(chan callbackResult, 1)
		handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
		handler.ServeHTTP(httptest.NewRecorder(), callbackRequest("/?state=synthetic-state&"+query))
		got := <-result
		if !errors.Is(got.err, ErrCallback) || !strings.Contains(got.err.Error(), reason) || strings.Contains(got.err.Error(), "synthetic") {
			t.Fatalf("%s: %v", query, got.err)
		}
	}
}

func TestCallbackScopeProblemsAreExplained(t *testing.T) {
	for name, tc := range map[string]struct {
		scope string
		want  error
	}{
		"only-file-scope":   {legacyScope, ErrScopeNotGranted},
		"only-sign-in":      {"openid email", ErrScopeNotGranted},
		"empty-scope":       {"", ErrScopeNotGranted},
		"foreign-scope":     {Scope + " https://www.googleapis.com/auth/gmail.readonly", ErrScopeUnexpected},
		"repeated-in-value": {Scope + " " + Scope, ErrScopeUnexpected},
	} {
		t.Run(name, func(t *testing.T) {
			result := make(chan callbackResult, 1)
			handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&code=synthetic-code&scope="+url.QueryEscape(tc.scope)))
			if w.Code != http.StatusBadRequest || len(result) != 1 {
				t.Fatal("an unusable grant must terminate safely")
			}
			if got := <-result; !errors.Is(got.err, tc.want) || got.code != "" {
				t.Fatalf("got %v, want %v", got.err, tc.want)
			}
			assertRedactedCallback(t, w)
		})
	}
}

func TestCallbackAcceptsFullDriveWithEarlierOrSignInScopes(t *testing.T) {
	for _, scope := range []string{Scope, legacyScope + " " + Scope, "openid email profile " + Scope, "https://www.googleapis.com/auth/userinfo.email " + Scope} {
		result := make(chan callbackResult, 1)
		handler := newCallback(context.Background(), "synthetic-state", callbackHost, result)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, callbackRequest("/?state=synthetic-state&code=synthetic-code&scope="+url.QueryEscape(scope)))
		if w.Code != http.StatusOK || len(result) != 1 {
			t.Fatalf("scope %q: full Drive access must be accepted: %d", scope, w.Code)
		}
		if got := <-result; got.err != nil || got.code != "synthetic-code" {
			t.Fatalf("scope %q: %+v", scope, got)
		}
	}
}
