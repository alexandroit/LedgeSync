package driveauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) cachedStatus() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.cached
	if st.Account != nil {
		a := *st.Account
		st.Account = &a
	}
	return st
}
func (s *Service) publish(r *record, err error) (Status, error) {
	st := Status{State: "setup_required", Scope: Scope, Message: ErrSetup.Error()}
	if s.bundled != nil {
		st.ClientConfigured = true
		st.State = "disconnected"
		st.Message = "Ready to request access in your system browser."
	}
	if r != nil && r.Client != nil {
		st.ClientConfigured = true
		st.State = "disconnected"
		st.Message = "Ready to request access in your system browser."
		if r.Credential != nil {
			a := r.Credential.Account
			st.Account = &a
			st.State = "connected"
			st.Message = "Google Drive account connected."
			if r.Credential.Reconnect {
				st.State = "reconnect_required"
				st.Message = ErrReconnect.Error()
			}
		}
	}
	if s.clientChanged(r) {
		st.State = "client_changed"
		st.Message = ErrClientChanged.Error()
	}
	if err != nil {
		st.Message = err.Error()
	}
	if errors.Is(err, ErrStorage) {
		st.State = "storage_unavailable"
	}
	if errors.Is(err, ErrBusy) {
		st.State = "busy"
	}
	if s.grantRevoked(r) {
		st.State = "revoked_local_cleanup_required"
		st.Message = ErrRevokedCleanup.Error()
	}
	s.mu.Lock()
	s.cached = st
	if st.Account != nil {
		a := *st.Account
		s.cached.Account = &a
	}
	s.mu.Unlock()
	return st, err
}

// clientChanged keeps a previously authorized account bound to its original
// client until an explicit Disconnect. A new release must never refresh or
// silently replace an old client's grant, even if the account appears identical.
func (s *Service) clientChanged(r *record) bool {
	return s.bundled != nil && r != nil && r.Client != nil && r.Credential != nil && *s.bundled != *r.Client
}

func (s *Service) bundledClient() *clientConfig {
	if s.bundled == nil {
		return nil
	}
	client := *s.bundled
	return &client
}

func (s *Service) load() (*record, error) {
	if s.store == nil {
		return nil, ErrStorage
	}
	data, err := s.store.Get(storageKey)
	if errors.Is(err, ErrNotFound) {
		return &record{Version: 1, Client: s.bundledClient()}, nil
	}
	if err != nil || len(data) > 64*1024 {
		return nil, ErrStorage
	}
	var r record
	if strictJSON([]byte(data), &r) != nil || r.Version != 1 || r.Client == nil || !clientIDPattern.MatchString(r.Client.ID) || !safeSecret(r.Client.Secret) {
		return nil, ErrStorage
	}
	if c := r.Credential; c != nil {
		if !safeSecret(c.RefreshToken) || !validAccount(c.Account) {
			return nil, ErrStorage
		}
	}
	// A client-only legacy record contains no grant to migrate or discard. Use
	// the immutable application configuration without changing the saved entry.
	if s.bundled != nil && r.Credential == nil {
		r.Client = s.bundledClient()
	}
	if s.runtime != nil && s.runtime.Credential != nil && r.Credential != nil && s.runtime.Client.ID == r.Client.ID && s.runtime.Client.Secret == r.Client.Secret && s.runtime.Credential.RefreshToken == r.Credential.RefreshToken && s.runtime.Credential.Account.Reference == r.Credential.Account.Reference {
		r.Credential.AccessToken = s.runtime.Credential.AccessToken
		r.Credential.Expiry = s.runtime.Credential.Expiry
	}
	return &r, nil
}
func (s *Service) save(ctx context.Context, r *record) error {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return ErrStorage
	}
	// Windows Credential Manager permits 2560 UTF-8 bytes. Cosmetic labels may
	// be fetched again, but the immutable account binding must always remain.
	if len(data) > 2560 && r.Credential != nil {
		compact := *r
		c := *r.Credential
		c.Account = Account{Reference: c.Account.Reference}
		compact.Credential = &c
		data, err = json.Marshal(&compact)
	}
	if err != nil || len(data) > 2560 || s.store == nil || s.store.Set(storageKey, string(data)) != nil {
		return ErrStorage
	}
	runtime := *r
	if r.Credential != nil {
		c := *r.Credential
		runtime.Credential = &c
	}
	s.runtime = &runtime
	if r.Credential == nil {
		s.revokedBinding = ""
	}
	return nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	if ctx.Err() != nil {
		return s.cachedStatus(), contextError(ctx)
	}
	if acquired, err := s.tryAcquire(); !acquired {
		if err != nil {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), nil
	}
	defer s.release()
	r, err := s.load()
	return s.publish(r, err)
}
func (s *Service) ConfigureClient(ctx context.Context, data []byte) (Status, error) {
	if acquired, err := s.tryAcquire(); !acquired {
		if err != nil {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), ErrBusy
	}
	defer s.release()
	ctx, done := s.operation(ctx)
	defer done()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if s.bundled != nil {
		return s.publish(r, ErrManagedClient)
	}
	if r.Credential != nil {
		return s.publish(r, ErrConnected)
	}
	client, err := parseClient(data)
	if err != nil {
		return s.publish(r, err)
	}
	next := &record{Version: 1, Client: client}
	if err = s.save(ctx, next); err != nil {
		return s.publish(r, err)
	}
	return s.publish(next, nil)
}
func (s *Service) operation(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	s.mu.Lock()
	s.cancel = cancel
	if s.stopping {
		cancel()
	}
	s.mu.Unlock()
	return ctx, func() { cancel(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }
}

// Cancel stops the current authorization/check; it does not remove credentials.
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Connect(ctx context.Context) (Status, error) {
	if acquired, err := s.tryAcquire(); !acquired {
		if err != nil {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), ErrBusy
	}
	defer s.release()
	ctx, done := s.operation(ctx)
	defer done()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, ErrSetup)
	}
	if s.grantRevoked(r) {
		return s.publish(r, ErrRevokedCleanup)
	}
	if s.clientChanged(r) {
		return s.publish(r, ErrClientChanged)
	}
	if r.Credential != nil && !r.Credential.Reconnect {
		return s.publish(r, ErrConnected)
	}
	st, _ := s.publish(r, nil)
	st.State = "connecting"
	st.Message = "Complete the authorization in your system browser."
	s.mu.Lock()
	s.cached = st
	s.mu.Unlock()
	token, err := s.authorize(ctx, r.Client)
	if err != nil {
		return s.publish(r, err)
	}
	account, err := s.identity(ctx, token.AccessToken)
	if err != nil {
		return s.publish(r, err)
	}
	if r.Credential != nil && r.Credential.Account.Reference != account.Reference {
		return s.publish(r, ErrIdentity)
	}
	token.Account = account
	next := &record{Version: 1, Client: r.Client, Credential: token}
	if err = s.save(ctx, next); err != nil {
		return s.publish(r, err)
	}
	return s.publish(next, nil)
}

func (s *Service) Check(ctx context.Context) (Status, error) {
	if acquired, err := s.tryAcquire(); !acquired {
		if err != nil {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), ErrBusy
	}
	defer s.release()
	ctx, done := s.operation(ctx)
	defer done()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, ErrSetup)
	}
	if s.grantRevoked(r) {
		return s.publish(r, ErrRevokedCleanup)
	}
	if s.clientChanged(r) {
		return s.publish(r, ErrClientChanged)
	}
	if r.Credential == nil {
		return s.publish(r, ErrReconnect)
	}
	if r.Credential.Reconnect {
		return s.publish(r, ErrReconnect)
	}
	refreshed := false
	if !r.Credential.Expiry.After(s.now().Add(time.Minute)) {
		if err = s.refresh(ctx, r); err != nil {
			return s.checkFailure(ctx, r, err)
		}
		refreshed = true
	}
	account, err := s.identity(ctx, r.Credential.AccessToken)
	// One refresh for an unexpectedly expired access token, never an interactive loop.
	if errors.Is(err, ErrReconnect) && !refreshed {
		if err = s.refresh(ctx, r); err == nil {
			account, err = s.identity(ctx, r.Credential.AccessToken)
		}
	}
	if err != nil {
		return s.checkFailure(ctx, r, err)
	}
	if r.Credential.Account.Reference != account.Reference {
		return s.checkFailure(ctx, r, ErrIdentity)
	}
	r.Credential.Account = account
	if err = s.save(ctx, r); err != nil {
		return s.publish(r, err)
	}
	return s.publish(r, nil)
}
func (s *Service) refresh(ctx context.Context, r *record) error {
	token, err := s.exchange(ctx, r.Client, "", "", "", r.Credential.RefreshToken)
	if err != nil {
		return err
	}
	token.Account = r.Credential.Account
	r.Credential = token
	// Persist rotation before the account read so a transient about.get failure
	// does not lose the latest refresh token. The account binding remains fixed.
	return s.save(ctx, r)
}
func (s *Service) checkFailure(ctx context.Context, r *record, err error) (Status, error) {
	if errors.Is(err, ErrReconnect) || errors.Is(err, ErrIdentity) || errors.Is(err, ErrScope) || errors.Is(err, ErrScopeNotGranted) || errors.Is(err, ErrScopeUnexpected) {
		r.Credential.Reconnect = true
		if saveErr := s.save(ctx, r); saveErr != nil {
			return s.publish(r, saveErr)
		}
	}
	return s.publish(r, err)
}

// tryAcquire/release serialize reads and mutations locally and across processes.
// A false/nil result means a local operation owns the current cached status;
// an external lock failure must never report that cache as a current vault read.
func (s *Service) tryAcquire() (bool, error) {
	s.mu.Lock()
	if s.stopping || !s.op.TryLock() {
		s.mu.Unlock()
		return false, nil
	}
	s.operationDone = make(chan struct{})
	s.mu.Unlock()
	if err := s.acquireProcess(); err != nil {
		s.release()
		return false, err
	}
	return true, nil
}

func (s *Service) acquireProcess() error {
	if s.processLock == nil {
		return nil
	}
	release, err := s.processLock()
	if err != nil || release == nil {
		if release != nil {
			release()
		}
		if errors.Is(err, ErrBusy) {
			return ErrBusy
		}
		return ErrStorage
	}
	s.mu.Lock()
	s.processRelease = release
	s.mu.Unlock()
	return nil
}

func (s *Service) release() {
	s.mu.Lock()
	release := s.processRelease
	s.processRelease = nil
	s.mu.Unlock()
	// Keep the local gate closed until kernel ownership has been released. A
	// draining lifecycle operation must not begin cleanup before this completes.
	if release != nil {
		release()
	}
	s.mu.Lock()
	done := s.operationDone
	s.operationDone = nil
	s.op.Unlock()
	if done != nil {
		close(done)
	}
	s.mu.Unlock()
}

// drain excludes new starts, cancels the current operation and waits for all its
// vault writes to return before allowing credential removal. A native vault call
// cannot be interrupted through Store; timeout reports failure without claiming
// cleanup and never starts a goroutine that might remove credentials later.
func (s *Service) drain(ctx context.Context) (func(), error) {
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	s.stopping = true
	if s.cancel != nil {
		s.cancel()
	}
	pending := s.operationDone
	s.mu.Unlock()
	reset := func() { s.mu.Lock(); s.stopping = false; s.mu.Unlock() }
	if pending != nil {
		select {
		case <-ctx.Done():
			reset()
			return nil, contextError(ctx)
		case <-pending:
		}
	}
	s.mu.Lock()
	if ctx.Err() != nil {
		s.stopping = false
		s.mu.Unlock()
		return nil, contextError(ctx)
	}
	if !s.op.TryLock() {
		s.stopping = false
		s.mu.Unlock()
		return nil, ErrBusy
	}
	s.operationDone = make(chan struct{})
	s.mu.Unlock()
	if err := s.acquireProcess(); err != nil {
		s.release()
		reset()
		return nil, err
	}
	return func() { s.release(); reset() }, nil
}

func grantBinding(r *record) string {
	if r == nil || r.Client == nil || r.Credential == nil {
		return ""
	}
	value, _ := json.Marshal(struct {
		Client           clientConfig
		Account, Refresh string
	}{*r.Client, r.Credential.Account.Reference, r.Credential.RefreshToken})
	hash := sha256.Sum256(value)
	clear(value)
	return hex.EncodeToString(hash[:])
}
func (s *Service) grantRevoked(r *record) bool {
	// A lock failure publishes a nil record after releasing the operation gate.
	// Do not read the gate-protected tombstone without a credential to compare.
	if r == nil || r.Client == nil || r.Credential == nil {
		return false
	}
	return s.revokedBinding != "" && s.revokedBinding == grantBinding(r)
}
func (s *Service) disconnectedRecord(r *record) *record {
	next := &record{Version: 1, Client: r.Client}
	if s.bundled != nil {
		next.Client = s.bundledClient()
	}
	return next
}

// Disconnect removes local account tokens only. It first cancels and drains any
// in-flight operation so a delayed refresh/authorization cannot restore them.
// Bundled services select their application client; legacy services retain the
// imported client configuration. Google grants and cloud files are unchanged.
func (s *Service) Disconnect(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	release, err := s.drain(ctx)
	if err != nil {
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrStorage) {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), err
	}
	defer release()
	s.runtime = nil
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, nil)
	}
	next := s.disconnectedRecord(r)
	if err = s.save(ctx, next); err != nil {
		return s.publish(r, err)
	}
	return s.publish(next, nil)
}

// Revoke removes the Google grant only after explicit confirmation of its shared
// Google Cloud project impact. A rejected confirmation has no side effects,
// including cancellation or vault reads. Network failures are never retried.
func (s *Service) Revoke(ctx context.Context, expectedAccountReference string, confirmed bool) (Status, error) {
	if !confirmed || !validAccount(Account{Reference: expectedAccountReference}) {
		return s.cachedStatus(), ErrRevokeConfirmation
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	release, err := s.drain(ctx)
	if err != nil {
		if errors.Is(err, ErrBusy) || errors.Is(err, ErrStorage) {
			return s.publish(nil, err)
		}
		return s.cachedStatus(), err
	}
	defer release()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if s.clientChanged(r) {
		return s.publish(r, ErrClientChanged)
	}
	if r.Client == nil || r.Credential == nil || r.Credential.Account.Reference != expectedAccountReference {
		return s.publish(r, ErrIdentity)
	}
	if s.grantRevoked(r) {
		return s.publish(r, ErrRevokedCleanup)
	}
	// Register a cancellation function only after the prior operation drained.
	// operation() intentionally cancels while stopping; lifecycle owns this gate.
	revokeCtx, stop := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = stop
	s.mu.Unlock()
	defer func() { stop(); s.mu.Lock(); s.cancel = nil; s.mu.Unlock() }()
	if err = s.revokeToken(revokeCtx, r.Credential.RefreshToken); err != nil {
		return s.publish(r, err)
	}
	// Google has confirmed revocation. Drop the process access token immediately,
	// then complete local cleanup even if the caller cancels after that response.
	s.revokedBinding = grantBinding(r)
	s.runtime = nil
	cleanup, finish := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finish()
	next := s.disconnectedRecord(r)
	if err = s.save(cleanup, next); err != nil {
		return s.publish(r, ErrRevokedCleanup)
	}
	return s.publish(next, nil)
}

func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ErrCanceled
}
