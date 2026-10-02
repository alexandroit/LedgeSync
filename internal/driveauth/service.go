package driveauth

import (
	"context"
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
	if r != nil && r.Client != nil {
		st.ClientConfigured = true
		st.State = "disconnected"
		st.Message = "Ready to request access in your system browser."
		if r.Credential != nil {
			a := r.Credential.Account
			st.Account = &a
			st.State = "connected"
			st.Message = "Google Drive account connected. File synchronization is not enabled."
			if r.Credential.Reconnect {
				st.State = "reconnect_required"
				st.Message = ErrReconnect.Error()
			}
		}
	}
	if err != nil {
		st.Message = err.Error()
	}
	if errors.Is(err, ErrStorage) {
		st.State = "storage_unavailable"
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
func (s *Service) load() (*record, error) {
	if s.store == nil {
		return nil, ErrStorage
	}
	data, err := s.store.Get(storageKey)
	if errors.Is(err, ErrNotFound) {
		return &record{Version: 1}, nil
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
	return nil
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	if ctx.Err() != nil {
		return s.cachedStatus(), contextError(ctx)
	}
	if !s.op.TryLock() {
		return s.cachedStatus(), nil
	}
	defer s.op.Unlock()
	r, err := s.load()
	return s.publish(r, err)
}
func (s *Service) ConfigureClient(ctx context.Context, data []byte) (Status, error) {
	if !s.op.TryLock() {
		return s.cachedStatus(), ErrBusy
	}
	defer s.op.Unlock()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
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
	if !s.op.TryLock() {
		return s.cachedStatus(), ErrBusy
	}
	defer s.op.Unlock()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, ErrSetup)
	}
	if r.Credential != nil && !r.Credential.Reconnect {
		return s.publish(r, ErrConnected)
	}
	ctx, done := s.operation(ctx)
	defer done()
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
	if !s.op.TryLock() {
		return s.cachedStatus(), ErrBusy
	}
	defer s.op.Unlock()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, ErrSetup)
	}
	if r.Credential == nil {
		return s.publish(r, ErrReconnect)
	}
	if r.Credential.Reconnect {
		return s.publish(r, ErrReconnect)
	}
	ctx, done := s.operation(ctx)
	defer done()
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
	if errors.Is(err, ErrReconnect) || errors.Is(err, ErrIdentity) || errors.Is(err, ErrScope) {
		r.Credential.Reconnect = true
		if saveErr := s.save(ctx, r); saveErr != nil {
			return s.publish(r, saveErr)
		}
	}
	return s.publish(r, err)
}

// Disconnect removes local account tokens only. The client configuration remains.
// Revocation at Google and cloud-file deletion are separate actions.
func (s *Service) Disconnect(ctx context.Context) (Status, error) {
	s.Cancel()
	if !s.op.TryLock() {
		return s.cachedStatus(), ErrBusy
	}
	defer s.op.Unlock()
	r, err := s.load()
	if err != nil {
		return s.publish(nil, err)
	}
	if r.Client == nil {
		return s.publish(r, nil)
	}
	next := &record{Version: 1, Client: r.Client}
	if err = s.save(ctx, next); err != nil {
		return s.publish(r, err)
	}
	return s.publish(next, nil)
}
func contextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	return ErrCanceled
}
