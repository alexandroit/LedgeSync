package driveauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

// SelectedFolder contains only the user-selected provider identity. The Drive
// provider must independently check its type and write capability before use.
type SelectedFolder struct {
	ID               string `json:"id"`
	AccountReference string `json:"accountReference"`
}

// ChooseFolder uses Google's native Picker in the system browser. No access
// token enters the desktop webview, callback page or returned DTO.
func (s *Service) ChooseFolder(ctx context.Context, expectedAccountReference string) (SelectedFolder, error) {
	if ctx.Err() != nil {
		return SelectedFolder{}, contextError(ctx)
	}
	if !validAccount(Account{Reference: expectedAccountReference}) {
		return SelectedFolder{}, ErrIdentity
	}
	if acquired, err := s.tryAcquire(); !acquired {
		if err != nil {
			_, _ = s.publish(nil, err)
			return SelectedFolder{}, err
		}
		return SelectedFolder{}, ErrBusy
	}
	defer s.release()
	ctx, done := s.operation(ctx)
	defer done()
	r, err := s.connectedRecord(expectedAccountReference)
	if err != nil {
		_, _ = s.publish(r, err)
		return SelectedFolder{}, err
	}
	// Check the existing grant before opening a selection against its account.
	if !r.Credential.Expiry.After(s.now().Add(time.Minute)) {
		if err = s.refresh(ctx, r); err != nil {
			_, err = s.checkFailure(ctx, r, err)
			return SelectedFolder{}, err
		}
	}
	account, err := s.identity(ctx, r.Credential.AccessToken)
	if err == nil && account.Reference != expectedAccountReference {
		err = ErrIdentity
	}
	if err != nil {
		_, err = s.checkFailure(ctx, r, err)
		return SelectedFolder{}, err
	}
	token, folderID, err := s.authorizeSelection(ctx, r.Client, true, r.Credential.RefreshToken)
	if err != nil {
		_, _ = s.publish(r, err)
		return SelectedFolder{}, err
	}
	account, err = s.identity(ctx, token.AccessToken)
	if err == nil && account.Reference != expectedAccountReference {
		err = ErrIdentity
	}
	if err != nil {
		// A different browser account never replaces or invalidates the prior
		// grant. The newly returned credentials are discarded without saving.
		_, _ = s.publish(r, err)
		return SelectedFolder{}, err
	}
	token.Account = account
	next := &record{Version: 1, Client: r.Client, Credential: token}
	if err = s.save(ctx, next); err != nil {
		_, _ = s.publish(r, err)
		return SelectedFolder{}, err
	}
	_, _ = s.publish(next, nil)
	return SelectedFolder{ID: folderID, AccountReference: account.Reference}, nil
}

func (s *Service) connectedRecord(expected string) (*record, error) {
	r, err := s.load()
	if err != nil {
		return r, err
	}
	if s.clientChanged(r) {
		return r, ErrClientChanged
	}
	if s.grantRevoked(r) {
		return r, ErrRevokedCleanup
	}
	if r.Client == nil || r.Credential == nil || r.Credential.Reconnect {
		return r, ErrReconnect
	}
	if r.Credential.Account.Reference != expected {
		return r, ErrIdentity
	}
	return r, nil
}

// DoAuthorized is the backend-only Drive HTTP port. The caller must close every
// returned body. Credential ownership lasts until Close, EOF or cancellation;
// disconnect can cancel and drain a slow upload/download before vault cleanup.
func (s *Service) DoAuthorized(ctx context.Context, expectedAccountReference string, req *http.Request) (*http.Response, error) {
	if !validAccount(Account{Reference: expectedAccountReference}) {
		return nil, ErrIdentity
	}
	if !allowedDriveRequest(req) {
		return nil, ErrProvider
	}
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	if acquired, err := s.acquireForRequest(ctx); !acquired {
		if err != nil {
			_, _ = s.publish(nil, err)
			return nil, err
		}
		return nil, ErrBusy
	}
	ctx, done := s.operation(ctx)
	limit := requestLimit(req)
	requestCtx, cancel := context.WithTimeout(ctx, limit)
	finish := func() { cancel(); done(); s.release() }
	owned := true
	defer func() {
		if owned {
			finish()
		}
	}()
	r, err := s.connectedRecord(expectedAccountReference)
	if err != nil {
		_, _ = s.publish(r, err)
		return nil, err
	}
	refreshed := false
	refresh := func() error {
		refreshed = true
		if err := s.refresh(requestCtx, r); err != nil {
			return err
		}
		account, err := s.identity(requestCtx, r.Credential.AccessToken)
		if err != nil {
			return err
		}
		if account.Reference != expectedAccountReference {
			return ErrIdentity
		}
		return nil
	}
	// The access token must outlive the whole request, including a slow chunk.
	if !r.Credential.Expiry.After(s.now().Add(limit + time.Minute)) {
		if err = refresh(); err != nil {
			_, err = s.checkFailure(requestCtx, r, err)
			return nil, err
		}
	}
	// Retain the injected transport for local fake tests, but enforce redirect
	// rejection and a per-chunk deadline independent of caller client settings.
	client := *s.http
	client.Timeout = limit
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for {
		request := req.Clone(requestCtx)
		request.Header = req.Header.Clone()
		if request.Header == nil {
			request.Header = make(http.Header)
		}
		for name := range request.Header {
			switch strings.ToLower(name) {
			case "authorization", "proxy-authorization", "cookie", "idempotency-key", "x-idempotency-key":
				delete(request.Header, name)
			}
		}
		request.Header.Set("Authorization", "Bearer "+r.Credential.AccessToken)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			if requestCtx.Err() != nil {
				return nil, contextError(requestCtx)
			}
			return nil, ErrNetwork
		}
		// net/http otherwise returns the credential-bearing request to callers.
		response.Request = nil
		if response.StatusCode == http.StatusUnauthorized {
			_ = response.Body.Close()
			// A mutation may have an uncertain remote outcome. Only read-only
			// GET is replayed, and only with one total refresh budget.
			if req.Method == http.MethodGet && !refreshed {
				if err = refresh(); err == nil {
					continue
				}
			} else {
				err = ErrReconnect
			}
			_, err = s.checkFailure(requestCtx, r, err)
			return nil, err
		}
		if response.StatusCode == http.StatusForbidden {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
			_ = response.Body.Close()
			if readErr != nil || len(data) > 64*1024 {
				return nil, ErrProvider
			}
			if insufficientScope(data) {
				_, err = s.checkFailure(requestCtx, r, ErrScope)
				return nil, err
			}
			response.Body = io.NopCloser(bytes.NewReader(data))
		}
		_, _ = s.publish(r, nil)
		body := &authorizedBody{ReadCloser: response.Body, ctx: requestCtx, finish: finish, closed: make(chan struct{})}
		response.Body = body
		owned = false
		go func() {
			select {
			case <-requestCtx.Done():
				_ = body.Close()
			case <-body.closed:
			}
		}()
		return response, nil
	}
}

// AllowedDriveRequest reports whether req is inside the Drive request boundary
// enforced by DoAuthorized. Test transports use it to exercise the same contract.
func AllowedDriveRequest(req *http.Request) bool { return allowedDriveRequest(req) }

func allowedDriveRequest(req *http.Request) bool {
	if req == nil || req.URL == nil || req.RequestURI != "" || (req.Host != "" && req.Host != "www.googleapis.com") || len(req.Trailer) != 0 {
		return false
	}
	u := req.URL
	if u.Scheme != "https" || u.Host != "www.googleapis.com" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.RawFragment != "" || u.OmitHost || len(u.RawQuery) > 8192 || strings.Contains(u.Path, "\\") || path.Clean(u.Path) != u.Path {
		return false
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPost && req.Method != http.MethodPut && req.Method != http.MethodPatch {
		return false
	}
	if req.Method == http.MethodGet && req.Body != nil && req.Body != http.NoBody {
		return false
	}
	upload := strings.HasPrefix(u.Path, "/upload/")
	resource := strings.TrimPrefix(u.Path, "/upload")
	switch {
	case resource == "/drive/v3/files":
	case strings.HasPrefix(resource, "/drive/v3/files/") && validDriveResourceID(strings.TrimPrefix(resource, "/drive/v3/files/")):
	case !upload && req.Method == http.MethodGet && (u.Path == "/drive/v3/changes" || u.Path == "/drive/v3/changes/startPageToken"):
	default:
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if len(values) != 1 || strings.EqualFold(key, "access_token") || strings.EqualFold(key, "oauth_token") || strings.EqualFold(key, "refresh_token") {
			return false
		}
	}
	return true
}

func validDriveResourceID(value string) bool {
	return value == "root" || validFolderID(value)
}

type authorizedBody struct {
	io.ReadCloser
	ctx    context.Context
	finish func()
	closed chan struct{}
	once   sync.Once
}

func (b *authorizedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		canceled := b.ctx.Err() != nil
		_ = b.Close()
		if err != io.EOF {
			if canceled {
				return n, contextError(b.ctx)
			}
			return n, ErrNetwork
		}
	}
	return n, err
}

func (b *authorizedBody) Close() error {
	b.once.Do(func() {
		_ = b.ReadCloser.Close()
		b.finish()
		close(b.closed)
	})
	return nil
}

// SetRequestWait lets authorized Drive requests wait up to d for a short
// credential operation, such as a status read, instead of failing as busy.
// A browser authorization or lifecycle change still makes requests fail busy.
func (s *Service) SetRequestWait(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestWait = d
}

func (s *Service) acquireForRequest(ctx context.Context) (bool, error) {
	s.mu.Lock()
	wait := s.requestWait
	s.mu.Unlock()
	deadline := time.Now().Add(wait)
	for {
		acquired, err := s.tryAcquire()
		if acquired {
			return true, nil
		}
		if err != nil && !errors.Is(err, ErrBusy) {
			return false, err
		}
		s.mu.Lock()
		stopping := s.stopping
		s.mu.Unlock()
		if wait <= 0 || stopping || !time.Now().Before(deadline) {
			return false, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, contextError(ctx)
		case <-timer.C:
		}
	}
}

// requestLimit bounds how long one request may own the credential. Metadata
// requests are short; an upload chunk or ranged download of at most 8 MiB gets
// enough time for slow connections (about 220 kbit/s) without becoming unbounded.
func requestLimit(req *http.Request) time.Duration {
	if req.Method == http.MethodPut || req.Method == http.MethodGet && req.URL != nil && req.URL.Query().Get("alt") == "media" {
		return 5 * time.Minute
	}
	return time.Minute
}
