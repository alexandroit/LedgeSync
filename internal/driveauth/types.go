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
const aboutEndpoint = "https://www.googleapis.com/drive/v3/about?fields=user(displayName,emailAddress,permissionId)"

var (
	ErrNotFound  = errors.New("credential not found")
	ErrStorage   = errors.New("The operating system credential vault is unavailable or its saved entry is invalid. Unlock the vault and try again.")
	ErrBusy      = errors.New("A Google Drive authorization operation is already running.")
	ErrSetup     = errors.New("Import a Google OAuth Desktop app client JSON file before connecting.")
	ErrClient    = errors.New("Choose a valid Google OAuth Desktop app client JSON file downloaded from Google Cloud.")
	ErrConnected = errors.New("Disconnect the current account before replacing its OAuth client or connecting a different account.")
	ErrDenied    = errors.New("Google Drive access was not granted. You can connect again when ready.")
	ErrCanceled  = errors.New("Google Drive authorization was canceled.")
	ErrTimeout   = errors.New("Google Drive authorization timed out. Connect again to restart.")
	ErrBrowser   = errors.New("The system browser could not be opened. Check your default browser and try again.")
	ErrCallback  = errors.New("The authorization callback was invalid. Connect again to restart.")
	ErrNetwork   = errors.New("Google could not be reached. Check your connection and try again.")
	ErrToken     = errors.New("Google returned an invalid authorization response. Connect again to restart.")
	ErrScope     = errors.New("The authorization did not grant exactly the requested Google Drive file access. Connect again.")
	ErrReconnect = errors.New("Google authorization has expired or was revoked. Connect again to authorize access.")
	ErrIdentity  = errors.New("The authorized Google account changed. Disconnect before selecting another account.")
	ErrProvider  = errors.New("Google Drive account access could not be checked. Confirm that the Drive API is enabled for this OAuth client and try again.")
)

// Store must use an OS credential vault, never a plaintext fallback. Missing
// entries must return ErrNotFound (or an error wrapping it). Errors are redacted.
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

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
	store                       Store
	openURL                     func(string) error
	op                          sync.Mutex
	mu                          sync.Mutex
	cancel                      context.CancelFunc
	cached                      Status
	http                        *http.Client
	authURL, tokenURL, aboutURL string // private seams for local fake HTTP tests only
	timeout                     time.Duration
	runtime                     *record // access tokens are process-local, never serialized
	now                         func() time.Time
}

func New(store Store, openURL func(string) error) *Service {
	return &Service{store: store, openURL: openURL,
		http:    &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		authURL: authEndpoint, tokenURL: tokenEndpoint, aboutURL: aboutEndpoint,
		timeout: 3 * time.Minute, now: time.Now,
		cached: Status{State: "setup_required", Scope: Scope, Message: ErrSetup.Error()},
	}
}
