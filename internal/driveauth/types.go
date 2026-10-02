// Package driveauth implements the bounded native Google Drive authorization flow.
// Credentials remain behind Store; none of its public DTOs contain OAuth secrets.
package driveauth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

const Scope = "https://www.googleapis.com/auth/drive.file"
const storageKey = "google-drive-oauth-v1"
const authEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
const tokenEndpoint = "https://oauth2.googleapis.com/token"
const revokeEndpoint = "https://oauth2.googleapis.com/revoke"
const aboutEndpoint = "https://www.googleapis.com/drive/v3/about?fields=user(displayName,emailAddress,permissionId)"

var (
	ErrNotFound           = errors.New("credential not found")
	ErrStorage            = errors.New("The operating system credential vault is unavailable or its saved entry is invalid. Unlock the vault and try again.")
	ErrBusy               = errors.New("A Google Drive authorization operation is already running.")
	ErrSetup              = errors.New("This LedgeSync build does not contain a valid Google Drive authorization configuration. Install a correctly configured build.")
	ErrClient             = errors.New("Choose a valid Google OAuth Desktop app client JSON file downloaded from Google Cloud.")
	ErrBuildConfig        = errors.New("This LedgeSync build does not contain a valid Google Drive authorization configuration. Install a correctly configured build.")
	ErrManagedClient      = errors.New("The Google Drive authorization configuration is provided by this LedgeSync build and cannot be replaced in the application.")
	ErrClientChanged      = errors.New("This LedgeSync build uses a different Google authorization client. Disconnect the saved account before connecting again.")
	ErrConnected          = errors.New("Disconnect the current account before replacing its OAuth client or connecting a different account.")
	ErrDenied             = errors.New("Google Drive access was not granted. You can connect again when ready.")
	ErrCanceled           = errors.New("Google Drive authorization was canceled.")
	ErrTimeout            = errors.New("Google Drive authorization timed out. Connect again to restart.")
	ErrBrowser            = errors.New("The system browser could not be opened. Check your default browser and try again.")
	ErrCallback           = errors.New("The authorization callback was invalid. Connect again to restart.")
	ErrNetwork            = errors.New("Google could not be reached. Check your connection and try again.")
	ErrToken              = errors.New("Google returned an invalid authorization response. Connect again to restart.")
	ErrScope              = errors.New("The authorization did not grant exactly the requested Google Drive file access. Connect again.")
	ErrReconnect          = errors.New("Google authorization has expired or was revoked. Connect again to authorize access.")
	ErrIdentity           = errors.New("The authorized Google account changed. Disconnect before selecting another account.")
	ErrRevokeConfirmation = errors.New("Revoking access at Google may remove this account’s authorizations for other applications whose OAuth clients share this Google Cloud project. Confirm this impact before revoking remotely.")
	ErrRevokeFailed       = errors.New("Google revocation could not be confirmed. Local credentials were retained. No automatic retry was made; check the account authorization before trying again.")
	ErrRevokedCleanup     = errors.New("Google confirmed access revocation, but the local credentials could not be removed. Unlock the operating system credential vault and disconnect from this device to finish cleanup.")
	ErrProvider           = errors.New("Google Drive account access could not be checked. Confirm that the Drive API is enabled for this OAuth client and try again.")
)

// Store must use an OS credential vault, never a plaintext fallback. Missing
// entries must return ErrNotFound (or an error wrapping it). Errors are redacted.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// ProcessLock tries to acquire exclusive ownership of the account's local
// credential transaction without waiting. Return ErrBusy for contention and
// ErrStorage when native protection is unavailable. The returned release must
// remain valid through all vault calls and any authorized response body.
type ProcessLock func() (release func(), err error)

type Account struct {
	Reference   string `json:"reference"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type Status struct {
	State            string   `json:"state"`
	ClientConfigured bool     `json:"clientConfigured"`
	Account          *Account `json:"account,omitempty"`
	Message          string   `json:"message"`
	Scope            string   `json:"scope"`
}

type clientConfig struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}
type credential struct {
	AccessToken  string    `json:"-"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"-"`
	Account      Account   `json:"account"`
	Reconnect    bool      `json:"reconnect"`
}
type record struct {
	Version    int           `json:"version"`
	Client     *clientConfig `json:"client,omitempty"`
	Credential *credential   `json:"credential,omitempty"`
}

// Service serializes credential mutations. Cancellation remains available while
// Connect/Check wait on a browser or network. It never logs callback URLs or bodies.
type Service struct {
	store                                  Store
	processLock                            ProcessLock
	processRelease                         func()
	openURL                                func(string) error
	op                                     sync.Mutex
	mu                                     sync.Mutex
	cancel                                 context.CancelFunc
	operationDone                          chan struct{}
	stopping                               bool   // lifecycle drain prevents new operations while cancellation completes
	revokedBinding                         string // process-local tombstone if confirmed remote revocation cannot be removed from the vault
	cached                                 Status
	http                                   *http.Client
	authURL, tokenURL, aboutURL, revokeURL string // private seams for local fake HTTP tests only
	timeout                                time.Duration
	runtime                                *record // access tokens are process-local, never serialized
	now                                    func() time.Time
	bundled                                *clientConfig // immutable application client; never supplied by the frontend
	requestWait                            time.Duration // bounded wait of Drive requests for short credential operations
}

func New(store Store, openURL func(string) error) *Service {
	return &Service{store: store, openURL: openURL,
		http:    &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		authURL: authEndpoint, tokenURL: tokenEndpoint, aboutURL: aboutEndpoint, revokeURL: revokeEndpoint,
		timeout: 3 * time.Minute, now: time.Now,
		cached: Status{State: "setup_required", Scope: Scope, Message: ErrSetup.Error()},
	}
}

// NewWithClient creates a service with the application's installed-client
// configuration. Parsing is strict and errors never contain configuration data.
// Construction does not read or write the credential vault. The parsed client is
// independent of clientJSON, which callers may clear after this function returns.
func NewWithClient(store Store, openURL func(string) error, clientJSON []byte) (*Service, error) {
	client, err := parseClient(clientJSON)
	if err != nil {
		return nil, ErrBuildConfig
	}
	s := New(store, openURL)
	s.bundled = client
	s.cached = Status{State: "disconnected", ClientConfigured: true, Scope: Scope, Message: "Ready to request access in your system browser."}
	return s, nil
}

// NewWithClientAndLock adds cross-process credential serialization. Construction
// is lazy: it neither acquires the lock nor reads the native vault. Production
// wiring must provide this port; New/NewWithClient remain usable by synthetic
// tests without a native filesystem or credential store.
func NewWithClientAndLock(store Store, openURL func(string) error, clientJSON []byte, lock ProcessLock) (*Service, error) {
	if lock == nil {
		return nil, ErrStorage
	}
	s, err := NewWithClient(store, openURL, clientJSON)
	if err != nil {
		return nil, err
	}
	s.processLock = lock
	return s, nil
}
