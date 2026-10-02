package driveauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type callbackResult struct {
	code     string
	folderID string
	err      error
}

func randomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", ErrCallback
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Service) authorize(ctx context.Context, client *clientConfig) (*credential, error) {
	token, _, err := s.authorizeSelection(ctx, client, false, "")
	return token, err
}

func (s *Service) authorizeSelection(ctx context.Context, client *clientConfig, folder bool, retainedRefresh string) (*credential, string, error) {
	if ctx.Err() != nil {
		return nil, "", contextError(ctx)
	}
	if s.openURL == nil {
		return nil, "", ErrBrowser
	}
	state, err := randomValue()
	if err != nil {
		return nil, "", err
	}
	verifier, err := randomValue()
	if err != nil {
		return nil, "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, "", ErrCallback
	}
	redirect := "http://" + listener.Addr().String() + "/"
	results := make(chan callbackResult, 1)
	handler := newSelectionCallback(ctx, state, listener.Addr().String(), results, folder)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	server.SetKeepAlivesEnabled(false)
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = server.Serve(listener) }()
	var closeOnce sync.Once
	closeServer := func() {
		closeOnce.Do(func() { _ = server.Close(); <-stopped })
	}
	// Cancellation closes the listener even while a native browser launcher is
	// still returning. The handler also checks this attempt's context directly.
	stopCancellation := context.AfterFunc(ctx, closeServer)
	defer func() { stopCancellation(); closeServer() }()
	u, err := url.Parse(s.authURL)
	if err != nil {
		return nil, "", ErrCallback
	}
	q := u.Query()
	q.Set("client_id", client.ID)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("scope", Scope)
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("access_type", "offline")
	q.Set("prompt", "consent select_account")
	if folder {
		q.Set("prompt", "consent")
		q.Set("trigger_onepick", "true")
		q.Set("allow_folder_selection", "true")
		q.Set("mimetypes", "application/vnd.google-apps.folder")
	}
	u.RawQuery = q.Encode()
	// The launcher must return promptly. No raw URL or launcher error is returned.
	if err = s.openURL(u.String()); err != nil {
		if ctx.Err() != nil {
			return nil, "", contextError(ctx)
		}
		return nil, "", ErrBrowser
	}
	var result callbackResult
	select {
	case <-ctx.Done():
		return nil, "", contextError(ctx)
	case <-stopped:
		if ctx.Err() != nil {
			return nil, "", contextError(ctx)
		}
		return nil, "", ErrCallback
	case result = <-results:
	}
	// The callback has one purpose and must not remain reachable during the
	// credential-bearing exchange, including a slow or failed token response.
	closeServer()
	if ctx.Err() != nil {
		return nil, "", contextError(ctx)
	}
	if result.err != nil {
		return nil, "", result.err
	}
	token, err := s.exchangeWithFallback(ctx, client, result.code, verifier, redirect, "", retainedRefresh)
	return token, result.folderID, err
}

func newCallback(ctx context.Context, state, host string, result chan<- callbackResult) http.Handler {
	return newSelectionCallback(ctx, state, host, result, false)
}

func newSelectionCallback(ctx context.Context, state, host string, result chan<- callbackResult, folder bool) http.Handler {
	var used atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if ctx.Err() != nil {
			http.Error(w, "This authorization attempt is no longer active. Return to LedgeSync to try again.", http.StatusGone)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Use the authorization page in your browser.", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" || r.URL.RawPath != "" || r.URL.Scheme != "" || r.URL.Host != "" || r.URL.User != nil || r.URL.Opaque != "" || r.URL.Fragment != "" || r.URL.RawFragment != "" || strings.Contains(r.RequestURI, "#") || r.Host != host {
			http.Error(w, "Unknown authorization callback.", http.StatusNotFound)
			return
		}
		if len(r.URL.RawQuery) > 8192 {
			http.Error(w, "Invalid authorization callback.", http.StatusBadRequest)
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid authorization state. Return to the Google authorization page.", http.StatusForbidden)
			return
		}
		value := callbackResult{}
		switch {
		case !validCallbackParameters(q):
			value.err = ErrCallback
		case len(q["error"]) == 1 && q.Get("error") != "" && len(q["code"]) == 0:
			value.err = ErrDenied
		case len(q["code"]) == 1 && len(q["error"]) == 0 && safeSecret(q.Get("code")) && len(q.Get("code")) <= 4096:
			value.code = q.Get("code")
		default:
			value.err = ErrCallback
		}
		if folder {
			if value.err == nil && len(q["picked_file_ids"]) == 1 && validFolderID(q.Get("picked_file_ids")) {
				value.folderID = q.Get("picked_file_ids")
			} else if value.err == nil || len(q["picked_file_ids"]) != 0 {
				value.err = ErrCallback
			}
		}
		if ctx.Err() != nil {
			http.Error(w, "This authorization attempt is no longer active. Return to LedgeSync to try again.", http.StatusGone)
			return
		}
		if !used.CompareAndSwap(false, true) {
			http.Error(w, "This authorization callback was already received.", http.StatusConflict)
			return
		}
		body := "LedgeSync received the authorization response. You can close this tab and return to LedgeSync."
		if value.err != nil {
			body = "LedgeSync authorization was not completed. Return to LedgeSync to try again."
		}
		// A fixed length and explicit flush complete the safe page before the
		// receiver closes the server. No code, state or provider text is reflected.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if value.err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = io.WriteString(w, body)
		_ = http.NewResponseController(w).Flush()
		// Buffered, single delivery: a browser retry cannot block the HTTP handler.
		result <- value
	})
}

func validFolderID(id string) bool {
	if len(id) == 0 || len(id) > 256 || id == "root" {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validCallbackParameters(q url.Values) bool {
	for _, values := range q {
		if len(values) != 1 {
			return false
		}
	}
	// This is a code flow: callback tokens must never become credentials. The
	// actual granted scope is independently validated in the token response.
	for _, name := range []string{"access_token", "refresh_token", "id_token", "token_type", "expires_in"} {
		if _, present := q[name]; present {
			return false
		}
	}
	if scope, present := q["scope"]; present && !acceptedScope(scope[0], false) {
		return false
	}
	if issuer, present := q["iss"]; present && issuer[0] != "https://accounts.google.com" {
		return false
	}
	if len(q["error"]) == 0 && (len(q["error_description"]) != 0 || len(q["error_uri"]) != 0) {
		return false
	}
	// RFC 6749 section 4.1.2 requires ignoring unrecognized response parameters.
	// Bounded, single-valued extensions (for example authuser and prompt) are
	// never forwarded, interpreted as credentials, reflected or logged.
	return true
}

func acceptedScope(value string, allowMissing bool) bool {
	if value == "" {
		return allowMissing
	}
	scopes := strings.Fields(value)
	return len(scopes) == 1 && scopes[0] == Scope
}
