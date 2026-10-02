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
	"strings"
	"sync/atomic"
	"time"
)

type callbackResult struct {
	code string
	err  error
}

func randomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", ErrCallback
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Service) authorize(ctx context.Context, client *clientConfig) (*credential, error) {
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	if s.openURL == nil {
		return nil, ErrBrowser
	}
	state, err := randomValue()
	if err != nil {
		return nil, err
	}
	verifier, err := randomValue()
	if err != nil {
		return nil, err
	}
	challenge := sha256.Sum256([]byte(verifier))
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, ErrCallback
	}
	redirect := "http://" + listener.Addr().String() + "/"
	results := make(chan callbackResult, 1)
	handler := newCallback(state, listener.Addr().String(), results)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	server.SetKeepAlivesEnabled(false)
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-stopped }()
	u, err := url.Parse(s.authURL)
	if err != nil {
		return nil, ErrCallback
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
	u.RawQuery = q.Encode()
	// The launcher must return promptly. No raw URL or launcher error is returned.
	if err = s.openURL(u.String()); err != nil {
		return nil, ErrBrowser
	}
	var result callbackResult
	select {
	case <-ctx.Done():
		return nil, contextError(ctx)
	case <-stopped:
		return nil, ErrCallback
	case result = <-results:
	}
	if result.err != nil {
		return nil, result.err
	}
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	return s.exchange(ctx, client, result.code, verifier, redirect, "")
}

func newCallback(state, host string, result chan<- callbackResult) http.Handler {
	var used atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Use the authorization page in your browser.", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" || r.Host != host {
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
		case len(q["error"]) == 1 && q.Get("error") != "" && len(q["code"]) == 0:
			value.err = ErrDenied
		case len(q["code"]) == 1 && len(q["error"]) == 0 && safeSecret(q.Get("code")) && len(q.Get("code")) <= 4096:
			value.code = q.Get("code")
		default:
			value.err = ErrCallback
		}
		if !used.CompareAndSwap(false, true) {
			http.Error(w, "This authorization callback was already received.", http.StatusConflict)
			return
		}
		if value.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "LedgeSync authorization was not completed. Return to LedgeSync to try again.")
		} else {
			_, _ = io.WriteString(w, "LedgeSync received the authorization response. You can close this tab and return to LedgeSync.")
		}
		// Buffered, single delivery: a browser retry cannot block the HTTP handler.
		result <- value
	})
}

func acceptedScope(value string, allowMissing bool) bool {
	if value == "" {
		return allowMissing
	}
	scopes := strings.Fields(value)
	return len(scopes) == 1 && scopes[0] == Scope
}
