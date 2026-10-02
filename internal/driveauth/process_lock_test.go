package driveauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type syntheticProcessLock struct {
	held               atomic.Bool
	acquired, released atomic.Int32
}

func (l *syntheticProcessLock) acquire() (func(), error) {
	if !l.held.CompareAndSwap(false, true) {
		return nil, ErrBusy
	}
	l.acquired.Add(1)
	var once sync.Once
	return func() { once.Do(func() { l.released.Add(1); l.held.Store(false) }) }, nil
}

type lockCheckedStore struct {
	Store
	t    *testing.T
	lock *syntheticProcessLock
}

func (s lockCheckedStore) Get(k string) (string, error) {
	if !s.lock.held.Load() {
		s.t.Error("vault read without process ownership")
	}
	return s.Store.Get(k)
}
func (s lockCheckedStore) Set(k, v string) error {
	if !s.lock.held.Load() {
		s.t.Error("vault write without process ownership")
	}
	return s.Store.Set(k, v)
}
func (s lockCheckedStore) Delete(k string) error {
	if !s.lock.held.Load() {
		s.t.Error("vault delete without process ownership")
	}
	return s.Store.Delete(k)
}

func TestProcessLockConstructionIsLazy(t *testing.T) {
	store := &memoryStore{}
	calls := 0
	service, err := NewWithClientAndLock(store, nil, []byte(clientJSON), func() (func(), error) {
		calls++
		return nil, ErrStorage
	})
	if err != nil || service == nil || calls != 0 || store.reads != 0 || store.writes != 0 {
		t.Fatal("constructor accessed the lock or vault")
	}
	if _, err := NewWithClientAndLock(store, nil, []byte(clientJSON), nil); !errors.Is(err, ErrStorage) {
		t.Fatal("production constructor accepted a missing protection port")
	}
}

func TestExternalProcessLockFailureStopsEveryCredentialOperation(t *testing.T) {
	for _, failure := range []error{ErrBusy, errors.New("synthetic-private-path failure")} {
		t.Run(map[bool]string{true: "busy", false: "protection"}[failure == ErrBusy], func(t *testing.T) {
			f := newBundledFixture(t)
			account := f.connect().Account.Reference
			reads, writes, tokens, about := f.store.reads, f.store.writes, f.tokenCalls, f.aboutCalls
			f.service.processLock = func() (func(), error) { return nil, failure }
			want, state := ErrBusy, "busy"
			if failure != ErrBusy {
				want, state = ErrStorage, "storage_unavailable"
			}
			for name, call := range map[string]func() (Status, error){
				"status":     func() (Status, error) { return f.service.Status(context.Background()) },
				"configure":  func() (Status, error) { return f.service.ConfigureClient(context.Background(), []byte(clientJSON)) },
				"connect":    func() (Status, error) { return f.service.Connect(context.Background()) },
				"check":      func() (Status, error) { return f.service.Check(context.Background()) },
				"disconnect": func() (Status, error) { return f.service.Disconnect(context.Background()) },
				"revoke":     func() (Status, error) { return f.service.Revoke(context.Background(), account, true) },
			} {
				t.Run(name, func(t *testing.T) {
					f.service.cached = Status{State: "connected", ClientConfigured: true, Account: &Account{Reference: account}}
					st, err := call()
					if !errors.Is(err, want) || st.State != state || st.Account != nil || strings.Contains(st.Message, "synthetic-private-path") {
						t.Fatal("lock failure exposed a cached account or unredacted diagnostic")
					}
				})
			}
			if _, err := f.service.ChooseFolder(context.Background(), account); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if _, err := f.service.DoAuthorized(context.Background(), account, driveRequest(http.MethodGet)); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if f.store.reads != reads || f.store.writes != writes || f.tokenCalls != tokens || f.aboutCalls != about {
				t.Fatal("lock failure reached the vault or provider")
			}
			// Failed attempts must release the local gate so a later safe lock can recover.
			lock := &syntheticProcessLock{}
			f.service.processLock = lock.acquire
			if st, err := f.service.Status(context.Background()); err != nil || st.State != "connected" || lock.held.Load() {
				t.Fatal("lock failure left the service blocked")
			}
		})
	}
}

func TestProcessLockCoversVaultReadsWritesAndLifecycle(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "revoke"}[revoke], func(t *testing.T) {
			f := newBundledFixture(t)
			lock := &syntheticProcessLock{}
			f.service.processLock = lock.acquire
			f.service.store = lockCheckedStore{Store: f.store, t: t, lock: lock}
			account := f.connect().Account.Reference
			if _, err := f.service.Status(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
			pickerBrowser(f, nil)
			if _, err := f.service.ChooseFolder(context.Background(), account); err != nil {
				t.Fatal(err)
			}
			if revoke {
				lifecycleRevokeServer(t, f, func(w http.ResponseWriter, r *http.Request) {
					if !lock.held.Load() {
						t.Error("revocation ran without process ownership")
					}
					w.WriteHeader(http.StatusOK)
				})
				if _, err := f.service.Revoke(context.Background(), account, true); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.service.Disconnect(context.Background()); err != nil {
				t.Fatal(err)
			}
			if lock.held.Load() || lock.acquired.Load() != lock.released.Load() || lock.acquired.Load() != 5 {
				t.Fatal("credential operations did not release exactly one process lock each")
			}
		})
	}
}

func TestProcessLockLastsThroughAuthorizedBody(t *testing.T) {
	for _, end := range []string{"close", "eof", "cancel"} {
		t.Run(end, func(t *testing.T) {
			f := newBundledFixture(t)
			account := f.connect().Account.Reference
			lock := &syntheticProcessLock{}
			f.service.processLock = lock.acquire
			other, err := NewWithClientAndLock(f.store, nil, []byte(clientJSON), lock.acquire)
			if err != nil {
				t.Fatal(err)
			}
			driveTransport(f, func(*http.Request) (*http.Response, error) { return driveResponse(200, "synthetic-file"), nil })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response, err := f.service.DoAuthorized(ctx, account, driveRequest(http.MethodGet))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if st, err := f.service.Status(context.Background()); err != nil || st.State != "connected" || st.Account == nil {
				t.Fatal("local status lost its existing in-process busy behavior")
			}
			if st, err := other.Status(context.Background()); !errors.Is(err, ErrBusy) || st.Account != nil || st.State != "busy" {
				t.Fatal("another service read credentials while a response owned them")
			}
			switch end {
			case "close":
				_ = response.Body.Close()
			case "eof":
				if _, err := io.ReadAll(response.Body); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
				select {
				case <-response.Body.(*authorizedBody).closed:
				case <-time.After(2 * time.Second):
					t.Fatal("cancellation did not release process lock")
				}
			}
			if lock.held.Load() || lock.acquired.Load() != 1 || lock.released.Load() != 1 {
				t.Fatal("body did not release exactly once")
			}
			if st, err := other.Status(context.Background()); err != nil || st.State != "connected" {
				t.Fatal("another service could not acquire after body completion")
			}
		})
	}
}

func TestProcessLockRemainsHeldUntilVaultWriteDrains(t *testing.T) {
	f := newBundledFixture(t)
	f.connect()
	lock := &syntheticProcessLock{}
	f.service.processLock = lock.acquire
	blocked := &lifecycleStore{Store: lockCheckedStore{Store: f.store, t: t, lock: lock}, block: "set", entered: make(chan struct{}), resume: make(chan struct{})}
	f.service.store = blocked
	other, err := NewWithClientAndLock(f.store, nil, []byte(clientJSON), lock.acquire)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := f.service.Check(context.Background()); finished <- err }()
	select {
	case <-blocked.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("write was not reached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.service.Disconnect(ctx); !errors.Is(err, ErrTimeout) {
		t.Fatal("drain claimed completion before a vault write returned")
	}
	if !lock.held.Load() || lock.released.Load() != 0 {
		t.Fatal("drain timeout released in-flight credential ownership")
	}
	if _, err := other.Disconnect(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("another process cleaned credentials during a pending vault write")
	}
	close(blocked.resume)
	_ = lifecycleResult(t, finished)
	if lock.held.Load() {
		t.Fatal("finished vault call retained process lock")
	}
	if st, err := other.Disconnect(context.Background()); err != nil || st.State != "disconnected" {
		t.Fatal("cleanup could not acquire after write drained")
	}
}

func TestProcessLockWithoutReleaseFailsClosed(t *testing.T) {
	store := &memoryStore{}
	service, err := NewWithClientAndLock(store, nil, []byte(clientJSON), func() (func(), error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if st, err := service.Status(context.Background()); !errors.Is(err, ErrStorage) || st.State != "storage_unavailable" || store.reads != 0 {
		t.Fatal("invalid lock provider reached the vault")
	}
}

// Failed cross-process acquisition releases the operation gate before its
// caller publishes a redacted nil-record status. A new credential transaction
// may update the process-local revocation tombstone in that interval.
func TestProcessLockFailureStatusDoesNotReadConcurrentCredentialState(t *testing.T) {
	s := New(nil, nil)
	started, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		close(started)
		for i := 0; i < 10000; i++ {
			acquired, err := s.tryAcquire()
			if !acquired || err != nil {
				t.Error("synthetic credential transaction did not acquire its gate")
				return
			}
			s.revokedBinding = "synthetic-revoked-binding"
			s.revokedBinding = ""
			s.release()
		}
	}()
	<-started
	for i := 0; i < 10000; i++ {
		st, err := s.publish(nil, ErrBusy)
		if !errors.Is(err, ErrBusy) || st.State != "busy" || st.Account != nil {
			t.Error("lock failure borrowed a concurrent credential status")
			break
		}
	}
	<-finished
}
