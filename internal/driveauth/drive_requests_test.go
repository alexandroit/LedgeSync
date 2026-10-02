package driveauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func pickerBrowser(f *fixture, alter func(url.Values)) {
	f.service.openURL = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		q := u.Query()
		f.mu.Lock()
		f.auth = q
		f.mu.Unlock()
		for key, value := range map[string]string{"scope": Scope, "prompt": "consent", "trigger_onepick": "true", "allow_folder_selection": "true", "mimetypes": "application/vnd.google-apps.folder", "code_challenge_method": "S256", "response_type": "code"} {
			if q.Get(key) != value {
				f.t.Errorf("incorrect Picker parameter %s", key)
			}
		}
		if q.Has("allow_multiple") || len(q.Get("state")) != 43 || len(q.Get("code_challenge")) != 43 {
			f.t.Error("unsafe Picker authorization parameters")
		}
		callback, _ := url.Parse(q.Get("redirect_uri"))
		params := url.Values{"state": {q.Get("state")}, "code": {"fake-code"}, "picked_file_ids": {"picked-folder-123"}}
		if alter != nil {
			alter(params)
		}
		callback.RawQuery = params.Encode()
		response, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if strings.Contains(string(body), "picked-folder") || strings.Contains(string(body), "fake-code") {
			f.t.Error("Picker callback reflected sensitive parameters")
		}
		return nil
	}
}

func TestNativePickerKeepsAccountAndRetainsOmittedRefresh(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	pickerBrowser(f, nil)
	f.tokenReply = `{"access_token":"fake-access-token","token_type":"Bearer","scope":"` + Scope + `","expires_in":3600}`
	selected, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference)
	if err != nil || selected.ID != "picked-folder-123" || selected.AccountReference != connected.Account.Reference {
		t.Fatalf("Picker did not return the bound folder: %v", err)
	}
	saved, err := f.service.load()
	if err != nil || saved.Credential.RefreshToken != "fake-refresh-token" || saved.Credential.Reconnect {
		t.Fatal("Picker lost the original valid refresh grant")
	}
	encoded, _ := json.Marshal(selected)
	for _, secret := range []string{"fake-access-token", "fake-refresh-token", "fake-client-secret", "fake-code"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("selection exported a credential")
		}
	}
	if f.tokenCalls != 2 || f.aboutCalls != 3 {
		t.Fatal("Picker did not check both existing and returned account")
	}
}

func TestNativePickerRejectsDifferentAccountWithoutPersisting(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	before, writes := f.store.value, f.store.writes
	pickerBrowser(f, func(url.Values) { f.mu.Lock(); f.identity = "different-browser-account"; f.mu.Unlock() })
	selected, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference)
	if !errors.Is(err, ErrIdentity) || selected.ID != "" || f.store.value != before || f.store.writes != writes {
		t.Fatal("different Picker account affected the saved grant")
	}
	status, _ := f.service.Status(context.Background())
	if status.State != "connected" || status.Account.Reference != connected.Account.Reference {
		t.Fatal("prior account was replaced or invalidated")
	}
}

func TestNativePickerRejectsInvalidSelectionsBeforeExchange(t *testing.T) {
	for name, modify := range map[string]func(url.Values){
		"missing":               func(q url.Values) { q.Del("picked_file_ids") },
		"empty":                 func(q url.Values) { q.Set("picked_file_ids", "") },
		"multiple":              func(q url.Values) { q.Set("picked_file_ids", "one,two") },
		"duplicate":             func(q url.Values) { q.Add("picked_file_ids", "another") },
		"path":                  func(q url.Values) { q.Set("picked_file_ids", "../secret") },
		"oversized":             func(q url.Values) { q.Set("picked_file_ids", strings.Repeat("x", 257)) },
		"alias":                 func(q url.Values) { q.Set("picked_file_ids", "root") },
		"denied-with-selection": func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newBundledFixture(t)
			connected := f.connect()
			before := f.store.value
			pickerBrowser(f, modify)
			if _, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference); !errors.Is(err, ErrCallback) {
				t.Fatalf("invalid selection accepted: %v", err)
			}
			if f.tokenCalls != 1 || f.store.value != before {
				t.Fatal("invalid selection exchanged or changed credentials")
			}
		})
	}
}

func TestNativePickerDeniedPreservesConnection(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	before := f.store.value
	pickerBrowser(f, func(q url.Values) { q.Del("code"); q.Del("picked_file_ids"); q.Set("error", "access_denied") })
	if _, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference); !errors.Is(err, ErrDenied) || f.store.value != before {
		t.Fatal("denied selection affected saved authorization")
	}
}

func TestNativePickerRequiresExactScopeEvenWithRetainedRefresh(t *testing.T) {
	for _, scope := range []string{"", Scope + " https://www.googleapis.com/auth/drive"} {
		f := newBundledFixture(t)
		connected := f.connect()
		before := f.store.value
		pickerBrowser(f, nil)
		response := map[string]any{"access_token": "fake-access-token", "token_type": "Bearer", "expires_in": 3600, "scope": scope}
		data, _ := json.Marshal(response)
		f.tokenReply = string(data)
		if _, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference); !errors.Is(err, ErrScope) || f.store.value != before {
			t.Fatal("Picker accepted missing or expanded scope")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func driveTransport(f *fixture, handle roundTripFunc) {
	f.service.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.googleapis.com" {
			return handle(r)
		}
		return http.DefaultTransport.RoundTrip(r)
	})
}

func driveResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func driveRequest(method string) *http.Request {
	r, _ := http.NewRequest(method, "https://www.googleapis.com/drive/v3/files?fields=id", nil)
	return r
}

func TestAuthorizedRequestKeepsCredentialsBehindBodyLifetime(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	driveTransport(f, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fake-access-token" || r.Header.Get("Cookie") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("unexpected outbound credentials")
		}
		response := driveResponse(200, `{"id":"created-file"}`)
		response.Request = r
		return response, nil
	})
	req := driveRequest(http.MethodGet)
	req.Header.Set("Authorization", "caller-secret")
	req.Header.Set("Cookie", "caller-cookie")
	req.Header.Set("Proxy-Authorization", "caller-proxy")
	req.Header["authorization"] = []string{"lowercase-caller-secret"}
	req.Header["proxy-authorization"] = []string{"lowercase-caller-proxy"}
	response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, req)
	if err != nil {
		t.Fatal(err)
	}
	if response.Request != nil || req.Header.Get("Authorization") != "caller-secret" {
		t.Fatal("request leaked credentials or mutated caller headers")
	}
	if _, err := f.service.Check(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("credential lock released before response body")
	}
	_, _ = io.ReadAll(response.Body)
	if _, err := f.service.Check(context.Background()); err != nil {
		t.Fatal("EOF did not release credential ownership")
	}
	_ = response.Body.Close()
}

func TestAuthorizedRequestRejectsUntrustedDestinationsBeforeVault(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	reads := f.store.reads
	for _, endpoint := range []string{
		"http://www.googleapis.com/drive/v3/files", "https://www.googleapis.com:443/drive/v3/files", "https://www.googleapis.com.evil.test/drive/v3/files",
		"https://user:secret@www.googleapis.com/drive/v3/files", "https://www.googleapis.com/drive/v2/files", "https://www.googleapis.com/drive/v3/../files",
		"https://www.googleapis.com/drive/v3/files?access_token=secret", "https://www.googleapis.com/drive/v3/files#fragment", "https://www.googleapis.com/drive/v3/%66iles",
		"https://www.googleapis.com/drive/v3/files?fields=id&fields=name", "https://www.googleapis.com/oauth2/v1/tokeninfo",
		"https://www.googleapis.com/drive/v3/files/object/permissions",
	} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		if _, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, req); !errors.Is(err, ErrProvider) {
			t.Error("unsafe destination was accepted")
		}
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete, http.MethodHead} {
		if _, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(method)); !errors.Is(err, ErrProvider) {
			t.Error("unsupported method accepted")
		}
	}
	if f.store.reads != reads {
		t.Fatal("invalid request touched vault")
	}
}

func TestAuthorizedGETRecoversExpiredAccessWithoutExportingTokens(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	calls := 0
	driveTransport(f, func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return driveResponse(401, "private-provider-error"), nil
		}
		return driveResponse(200, `{"id":"recovered"}`), nil
	})
	response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	if string(data) != `{"id":"recovered"}` || calls != 2 || f.tokenCalls != 2 {
		t.Fatal("read-only request did not recover within one refresh budget")
	}
	status, _ := f.service.Status(context.Background())
	if status.State != "connected" {
		t.Fatal("successful refresh invalidated the grant")
	}
}

func TestNativePickerClientChangeAndStaleAccountStopBeforeBrowser(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	opens := 0
	f.service.openURL = func(string) error { opens++; return nil }
	if _, err := f.service.ChooseFolder(context.Background(), "drive_"+strings.Repeat("a", 64)); !errors.Is(err, ErrIdentity) {
		t.Fatal("stale account allowed selection")
	}
	f.service.bundled = &clientConfig{ID: "different-client.apps.googleusercontent.com", Secret: "different-secret"}
	if _, err := f.service.ChooseFolder(context.Background(), connected.Account.Reference); !errors.Is(err, ErrClientChanged) {
		t.Fatal("changed client allowed selection")
	}
	if opens != 0 || f.tokenCalls != 1 {
		t.Fatal("rejected selection opened browser or exchanged credentials")
	}
}

func TestAuthorizedGETRefreshesOnceAndMutationsAreNeverReplayed(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			f := newBundledFixture(t)
			connected := f.connect()
			calls := 0
			driveTransport(f, func(*http.Request) (*http.Response, error) {
				calls++
				return driveResponse(401, "private-provider-error"), nil
			})
			_, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(method))
			if !errors.Is(err, ErrReconnect) {
				t.Fatal("401 did not require reconnection")
			}
			want := 1
			if method == http.MethodGet {
				want = 2
			}
			if calls != want || f.tokenCalls != want {
				t.Fatal("refresh/replay budget violated")
			}
			if _, err = f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(method)); !errors.Is(err, ErrReconnect) || calls != want {
				t.Fatal("revoked grant caused another request")
			}
		})
	}
}

func TestAuthorizedExpiredTokenConsumesOnlyOneRefreshBudget(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	f.service.runtime.Credential.Expiry = time.Time{}
	calls := 0
	driveTransport(f, func(*http.Request) (*http.Response, error) { calls++; return driveResponse(401, ""), nil })
	if _, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet)); !errors.Is(err, ErrReconnect) || calls != 1 || f.tokenCalls != 2 {
		t.Fatal("expiry plus 401 exceeded one refresh")
	}
}

func TestAuthorizedScopeLossVersusFileACL(t *testing.T) {
	for _, scopeFailure := range []bool{true, false} {
		f := newBundledFixture(t)
		connected := f.connect()
		reason := "insufficientFilePermissions"
		if scopeFailure {
			reason = "insufficientPermissions"
		}
		driveTransport(f, func(*http.Request) (*http.Response, error) {
			return driveResponse(403, `{"error":{"errors":[{"domain":"global","reason":"`+reason+`"}]}}`), nil
		})
		response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet))
		if scopeFailure {
			if !errors.Is(err, ErrScope) || response != nil {
				t.Fatal("scope loss not blocked")
			}
		} else {
			if err != nil || response.StatusCode != 403 {
				t.Fatal("file ACL error invalidated grant")
			}
			_ = response.Body.Close()
		}
		status, _ := f.service.Status(context.Background())
		expected := "connected"
		if scopeFailure {
			expected = "reconnect_required"
		}
		if status.State != expected || f.tokenCalls != 1 {
			t.Fatal("incorrect scope-loss state")
		}
	}
}

func TestAuthorizedRedirectIsNeverFollowed(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	calls := 0
	driveTransport(f, func(*http.Request) (*http.Response, error) {
		calls++
		r := driveResponse(307, "")
		r.Header.Set("Location", "https://www.googleapis.com/drive/v3/files/redirected")
		return r, nil
	})
	response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet))
	if err != nil || response.StatusCode != 307 || calls != 1 {
		t.Fatal("credential-bearing redirect was followed")
	}
	_ = response.Body.Close()
}

func TestAuthorizedBodyDisconnectCancelsAndDrains(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	reader, writer := io.Pipe()
	defer writer.Close()
	var requests atomic.Int32
	driveTransport(f, func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		response := driveResponse(200, "")
		response.Body = reader
		return response, nil
	})
	response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet))
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() { _, err := io.ReadAll(response.Body); readDone <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := f.service.Disconnect(ctx)
	if err != nil || status.State != "disconnected" {
		t.Fatal("disconnect did not drain active response")
	}
	select {
	case err := <-readDone:
		if !errors.Is(err, ErrCanceled) {
			t.Fatal("read cancellation was not redacted")
		}
	case <-ctx.Done():
		t.Fatal("response read was left active")
	}
	if _, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet)); !errors.Is(err, ErrReconnect) || requests.Load() != 1 {
		t.Fatal("request continued after disconnect")
	}
}

func TestAuthorizedWrongAccountNeverContactsProvider(t *testing.T) {
	f := newBundledFixture(t)
	f.connect()
	wrong := "drive_" + strings.Repeat("a", 64)
	driveTransport(f, func(*http.Request) (*http.Response, error) {
		t.Error("provider contacted with wrong account")
		return driveResponse(200, ""), nil
	})
	if _, err := f.service.DoAuthorized(context.Background(), wrong, driveRequest(http.MethodGet)); !errors.Is(err, ErrIdentity) {
		t.Fatal("account binding ignored")
	}
}

func TestAuthorizedRequestWaitsBrieflyForShortCredentialOperations(t *testing.T) {
	f := newBundledFixture(t)
	connected := f.connect()
	driveTransport(f, func(r *http.Request) (*http.Response, error) { return driveResponse(200, `{"id":"waited"}`), nil })
	f.service.SetRequestWait(2 * time.Second)
	// Hold the credential gate as a status read would, then release it shortly.
	if acquired, err := f.service.tryAcquire(); !acquired || err != nil {
		t.Fatal("could not hold the gate")
	}
	go func() { time.Sleep(100 * time.Millisecond); f.service.release() }()
	response, err := f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet))
	if err != nil {
		t.Fatalf("request did not wait for a short operation: %v", err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	// A long operation still makes the request fail busy after the bound.
	f.service.SetRequestWait(150 * time.Millisecond)
	if acquired, err := f.service.tryAcquire(); !acquired || err != nil {
		t.Fatal("could not hold the gate")
	}
	defer f.service.release()
	started := time.Now()
	if _, err = f.service.DoAuthorized(context.Background(), connected.Account.Reference, driveRequest(http.MethodGet)); !errors.Is(err, ErrBusy) {
		t.Fatalf("held gate did not report busy: %v", err)
	}
	if time.Since(started) < 100*time.Millisecond {
		t.Fatal("request did not wait before reporting busy")
	}
}

func TestDataRequestsGetLongerCredentialBoundThanMetadata(t *testing.T) {
	put, _ := http.NewRequest(http.MethodPut, "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id=x", nil)
	media, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files/abc?alt=media", nil)
	meta, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/drive/v3/files/abc?fields=id", nil)
	if requestLimit(put) != 5*time.Minute || requestLimit(media) != 5*time.Minute || requestLimit(meta) != time.Minute {
		t.Fatal("unexpected request bounds")
	}
}
