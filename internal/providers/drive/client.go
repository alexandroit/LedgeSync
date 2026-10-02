// Package drive implements bounded, create-only Drive operations. Authentication
// and approved-plan execution remain separate ports; this package never owns tokens.
package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

const (
	apiURL            = "https://www.googleapis.com/drive/v3/files"
	uploadURL         = "https://www.googleapis.com/upload/drive/v3/files"
	folderMIME        = "application/vnd.google-apps.folder"
	objectFields      = "id,name,mimeType,parents,size,md5Checksum,version,appProperties,trashed,driveId,capabilities(canAddChildren)"
	maxResponse       = 2 << 20
	maxAttempts       = 4
	operationProperty = "ledgesyncOperation"
)

// Authorizer validates the account binding and adds credentials only inside the
// backend. It must reject redirects and leave a replayable request body intact.
type Authorizer interface {
	DoAuthorized(context.Context, string, *http.Request) (*http.Response, error)
}

type Folder struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Parents        []string `json:"parents"`
	CanAddChildren bool     `json:"canAddChildren"`
}

type FolderPage struct {
	Folders       []Folder `json:"folders"`
	NextPageToken string   `json:"nextPageToken"`
}

type Object struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	MimeType      string            `json:"mimeType"`
	Parents       []string          `json:"parents"`
	Size          int64             `json:"size,string"`
	MD5           string            `json:"md5Checksum"`
	Version       string            `json:"version"`
	AppProperties map[string]string `json:"appProperties"`
	Trashed       bool              `json:"trashed"`
}

type wireObject struct {
	Object
	DriveID      string `json:"driveId"`
	Capabilities struct {
		CanAddChildren bool `json:"canAddChildren"`
	} `json:"capabilities"`
}

type Client struct {
	auth      Authorizer
	chunkSize int64
	attempts  int
	wait      func(context.Context, time.Duration) error
	now       func() time.Time
	mu        sync.Mutex
	parents   map[string]parentCheck
	fresh     map[string]bool
}

type parentCheck struct {
	canonical string
	expires   time.Time
}

// parentCacheTTL bounds how long a verified writable parent is trusted between
// creations in the same folder. The executor separately re-verifies folders.
const parentCacheTTL = 30 * time.Second

// rootAlias is Drive's alias for My Drive. With drive.file the root's metadata
// is not readable, but it is a valid parent and parent-query term.
const rootAlias = "root"

// Options tune bounded transport behavior. Zero values select safe defaults.
type Options struct {
	// ChunkSize is the resumable upload chunk size: a multiple of 256 KiB, at most 8 MiB.
	ChunkSize int64
	// MaxAttempts bounds attempts for one retryable request or upload chunk.
	MaxAttempts int
	// Wait replaces the backoff sleeper; tests use it to avoid real delays.
	Wait func(context.Context, time.Duration) error
}

func New(auth Authorizer) *Client { return NewWithOptions(auth, Options{}) }

func NewWithOptions(auth Authorizer, o Options) *Client {
	c := &Client{auth: auth, chunkSize: 8 << 20, attempts: maxAttempts, wait: waitContext, now: time.Now, parents: map[string]parentCheck{}, fresh: map[string]bool{}}
	if o.ChunkSize != 0 {
		c.chunkSize = o.ChunkSize
	}
	if o.MaxAttempts > 0 {
		c.attempts = min(o.MaxAttempts, 21)
	}
	if o.Wait != nil {
		c.wait = o.Wait
	}
	return c
}

// WithRetries returns a client that makes at most maxRetries+1 attempts for a
// retryable request or upload chunk. It shares no mutable caches with c.
func (c *Client) WithRetries(maxRetries int) *Client {
	attempts := min(max(maxRetries, 0), 20) + 1
	return &Client{auth: c.auth, chunkSize: c.chunkSize, attempts: attempts, wait: c.wait, now: c.now, parents: map[string]parentCheck{}, fresh: map[string]bool{}}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return cancelled()
	case <-t.C:
		return nil
	}
}

func cancelled() error { return domain.Fail("CANCELLED", "The Drive operation was cancelled.") }
func malformed() error {
	return domain.Fail("DRIVE_INVALID_RESPONSE", "Google Drive returned an invalid response.")
}
func unknown() error {
	return domain.Fail("UNKNOWN_REMOTE_RESULT", "The remote result is uncertain. Reconcile the recorded object ID before retrying.")
}

// IDs are opaque but are never allowed to alter a query or URL path.
func validID(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validName(s string) bool {
	return s != "" && len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00/\\") && s != "." && s != ".."
}

func validateInput(account string, ids ...string) error {
	if account == "" || len(account) > 256 {
		return domain.Fail("AUTH_REQUIRED", "Connect the selected Google Drive account first.")
	}
	for _, id := range ids {
		if !validID(id) {
			return domain.Fail("DRIVE_INVALID_INPUT", "The Drive object reference is invalid.")
		}
	}
	return nil
}

type response struct {
	status int
	header http.Header
	data   []byte
}

func (c *Client) request(ctx context.Context, account, method, endpoint string, body []byte, headers http.Header) (response, error) {
	if ctx.Err() != nil {
		return response{}, cancelled()
	}
	if c.auth == nil {
		return response{}, domain.Fail("AUTH_REQUIRED", "Connect Google Drive first.")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return response{}, domain.Fail("DRIVE_INVALID_INPUT", "The Drive request is invalid.")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.auth.DoAuthorized(reqCtx, account, req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if ctx.Err() != nil {
			return response{}, cancelled()
		}
		return response{}, authorizerError(err)
	}
	if resp == nil || resp.Body == nil {
		return response{}, malformed()
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return response{}, cancelled()
		}
		return response{}, authorizerError(err)
	}
	if len(raw) > maxResponse {
		return response{}, malformed()
	}
	return response{resp.StatusCode, resp.Header.Clone(), raw}, nil
}

// Authorizer failures can also occur while consuming its owned response body.
// Authentication intervention must not be retried as a transient network error.
// Match identities, never messages; wrapped diagnostic text is not propagated.
func authorizerError(err error) error {
	var safe *domain.Error
	if errors.As(err, &safe) {
		return safe
	}
	switch {
	case errors.Is(err, driveauth.ErrCanceled), errors.Is(err, context.Canceled):
		return cancelled()
	case errors.Is(err, driveauth.ErrScope):
		return domain.Fail("AUTH_REQUIRED", "The Drive permission grant changed. Reconnect Google Drive before continuing.")
	case errors.Is(err, driveauth.ErrReconnect), errors.Is(err, driveauth.ErrNotFound), errors.Is(err, driveauth.ErrDenied):
		return domain.Fail("AUTH_REQUIRED", "Reconnect Google Drive before continuing.")
	case errors.Is(err, driveauth.ErrIdentity):
		return domain.Fail("ACCOUNT_CHANGED", "The Google Drive account changed. Select the destination again before continuing.")
	case errors.Is(err, driveauth.ErrClientChanged):
		return domain.Fail("AUTH_CLIENT_CHANGED", "The application authorization client changed. Disconnect the saved account before reconnecting.")
	case errors.Is(err, driveauth.ErrBusy):
		return domain.Fail("AUTH_BUSY", "Another Google Drive authorization operation is running. Wait before retrying.")
	case errors.Is(err, driveauth.ErrStorage), errors.Is(err, driveauth.ErrRevokedCleanup):
		return domain.Fail("AUTH_STORAGE_UNAVAILABLE", "The saved Google Drive authorization is unavailable. Check the system credential vault and connection status.")
	case errors.Is(err, driveauth.ErrSetup), errors.Is(err, driveauth.ErrBuildConfig), errors.Is(err, driveauth.ErrClient), errors.Is(err, driveauth.ErrManagedClient):
		return domain.Fail("AUTH_CONFIGURATION_REQUIRED", "This application build needs a valid Google Drive authorization configuration.")
	case errors.Is(err, driveauth.ErrProvider), errors.Is(err, driveauth.ErrToken):
		return domain.Fail("DRIVE_REQUEST_FAILED", "Google Drive authorization could not validate this request. Check the connection status.")
	case errors.Is(err, driveauth.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return domain.Fail("DRIVE_NETWORK", "The Google Drive request timed out. Check the connection before retrying.")
	default:
		return domain.Fail("DRIVE_NETWORK", "Google Drive could not be reached. Check the connection and retry.")
	}
}

func responseError(r response) error {
	var payload struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.data, &payload)
	for _, reason := range payload.Error.Errors {
		switch reason.Reason {
		case "storageQuotaExceeded":
			return domain.Fail("DRIVE_STORAGE_FULL", "Google Drive storage is full.")
		case "rateLimitExceeded", "userRateLimitExceeded", "sharingRateLimitExceeded":
			return domain.Fail("DRIVE_RATE_LIMIT", "Google Drive temporarily limited requests. Try again later.")
		case "dailyLimitExceeded":
			return domain.Fail("DRIVE_QUOTA", "The Google Drive application quota has been reached.")
		case "insufficientPermissions", "appNotAuthorizedToFile", "insufficientFilePermissions":
			return domain.Fail("DRIVE_PERMISSION_DENIED", "This item is not available with the current Drive authorization or permissions.")
		}
	}
	switch r.status {
	case http.StatusUnauthorized:
		return domain.Fail("AUTH_REQUIRED", "Reconnect the Google Drive account.")
	case http.StatusForbidden:
		return domain.Fail("DRIVE_PERMISSION_DENIED", "Google Drive denied this operation. Check the selected folder permissions.")
	case http.StatusNotFound:
		return domain.Fail("DRIVE_NOT_FOUND", "The Drive item is missing or is not available to LedgeSync.")
	case http.StatusConflict:
		return domain.Fail("DRIVE_CONFLICT", "The Drive object ID already exists and must be reconciled.")
	case http.StatusTooManyRequests:
		return domain.Fail("DRIVE_RATE_LIMIT", "Google Drive temporarily limited requests. Try again later.")
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return domain.Fail("DRIVE_UNAVAILABLE", "Google Drive is temporarily unavailable.")
	default:
		return domain.Fail("DRIVE_REQUEST_FAILED", "Google Drive did not accept this request.")
	}
}

func retryable(err error) bool {
	switch domain.ErrorCode(err) {
	case "DRIVE_NETWORK", "DRIVE_RATE_LIMIT", "DRIVE_UNAVAILABLE":
		return true
	}
	return false
}

func (c *Client) backoff(ctx context.Context, attempt int, header http.Header) error {
	delay := (250 * time.Millisecond << attempt) + time.Duration(rand.IntN(200))*time.Millisecond
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		delay = max(delay, min(time.Duration(seconds)*time.Second, 30*time.Second))
	}
	return c.wait(ctx, delay)
}

func (c *Client) get(ctx context.Context, account, endpoint string, target any) error {
	for attempt := 0; attempt < c.attempts; attempt++ {
		r, err := c.request(ctx, account, http.MethodGet, endpoint, nil, nil)
		if err == nil && r.status == http.StatusOK {
			if err = json.Unmarshal(r.data, target); err != nil {
				return malformed()
			}
			return nil
		}
		if err == nil {
			err = responseError(r)
		}
		if !retryable(err) || attempt == c.attempts-1 {
			return err
		}
		if err = c.backoff(ctx, attempt, r.header); err != nil {
			return err
		}
	}
	return malformed()
}

func (c *Client) object(ctx context.Context, account, id string) (wireObject, error) {
	var o wireObject
	if err := validateInput(account, id); err != nil {
		return o, err
	}
	err := c.get(ctx, account, apiURL+"/"+id+"?"+url.Values{"fields": {objectFields}}.Encode(), &o)
	if err != nil {
		return o, err
	}
	if !validID(o.ID) || (id != "root" && o.ID != id) || o.Name == "" || o.MimeType == "" || o.Size < 0 {
		return wireObject{}, malformed()
	}
	if o.DriveID != "" {
		return wireObject{}, domain.Fail("DRIVE_UNSUPPORTED", "Shared drive destinations are not supported by this upload workflow.")
	}
	for _, parent := range o.Parents {
		if !validID(parent) {
			return wireObject{}, malformed()
		}
	}
	return o, nil
}

func (c *Client) GetObject(ctx context.Context, account, id string) (Object, error) {
	o, err := c.object(ctx, account, id)
	return o.Object, err
}

func (c *Client) GetFolder(ctx context.Context, account, id string) (Folder, error) {
	o, err := c.object(ctx, account, id)
	if err != nil {
		return Folder{}, err
	}
	if o.Trashed || o.MimeType != folderMIME {
		return Folder{}, domain.Fail("DRIVE_NOT_FOLDER", "Select an available Google Drive folder.")
	}
	return Folder{o.ID, o.Name, o.Parents, o.Capabilities.CanAddChildren}, nil
}

// ListFolders returns one explicit page. Callers must consume every page before
// declaring a listing complete, including empty pages that have a next token.
// An empty parent lists all folders visible under the granted drive.file scope.
func (c *Client) ListFolders(ctx context.Context, account, parent, pageToken string) (FolderPage, error) {
	if err := validateInput(account); err != nil {
		return FolderPage{}, err
	}
	if parent != "" && !validID(parent) {
		return FolderPage{}, domain.Fail("DRIVE_INVALID_INPUT", "The parent folder reference is invalid.")
	}
	if len(pageToken) > 8192 || strings.ContainsAny(pageToken, "\x00\r\n") {
		return FolderPage{}, domain.Fail("DRIVE_INVALID_INPUT", "The folder page reference is invalid.")
	}
	query := "trashed = false and mimeType = '" + folderMIME + "'"
	if parent != "" {
		query += " and '" + parent + "' in parents"
	}
	params := url.Values{"q": {query}, "pageSize": {"1000"}, "spaces": {"drive"}, "corpora": {"user"}, "fields": {"nextPageToken,incompleteSearch,files(" + objectFields + ")"}}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	var page struct {
		Files            []wireObject `json:"files"`
		NextPageToken    string       `json:"nextPageToken"`
		IncompleteSearch bool         `json:"incompleteSearch"`
	}
	if err := c.get(ctx, account, apiURL+"?"+params.Encode(), &page); err != nil {
		return FolderPage{}, err
	}
	if page.IncompleteSearch {
		return FolderPage{}, domain.Fail("DRIVE_INCOMPLETE_LISTING", "Google Drive could not complete the folder listing. Retry before selecting a destination.")
	}
	if len(page.Files) > 1000 || len(page.NextPageToken) > 8192 || (page.NextPageToken != "" && page.NextPageToken == pageToken) || strings.ContainsAny(page.NextPageToken, "\x00\r\n") {
		return FolderPage{}, malformed()
	}
	result := FolderPage{Folders: make([]Folder, 0, len(page.Files)), NextPageToken: page.NextPageToken}
	seen := make(map[string]bool)
	for _, f := range page.Files {
		if !validID(f.ID) || seen[f.ID] || f.Name == "" || f.MimeType != folderMIME || f.Trashed {
			return FolderPage{}, malformed()
		}
		seen[f.ID] = true
		if f.DriveID != "" {
			continue
		} // Unsupported shared-drive folders are not offered.
		for _, id := range f.Parents {
			if !validID(id) {
				return FolderPage{}, malformed()
			}
		}
		if parent != "" && parent != "root" && !hasParent(f.Object, parent) {
			return FolderPage{}, malformed()
		}
		result.Folders = append(result.Folders, Folder{f.ID, f.Name, f.Parents, f.Capabilities.CanAddChildren})
	}
	return result, nil
}

func (c *Client) GenerateIDs(ctx context.Context, account string, count int) ([]string, error) {
	if err := validateInput(account); err != nil {
		return nil, err
	}
	if count < 1 || count > 1000 {
		return nil, domain.Fail("DRIVE_INVALID_INPUT", "Request between 1 and 1000 Drive object IDs.")
	}
	var result struct {
		IDs []string `json:"ids"`
	}
	if err := c.get(ctx, account, apiURL+"/generateIds?"+url.Values{"count": {strconv.Itoa(count)}, "space": {"drive"}, "type": {"files"}}.Encode(), &result); err != nil {
		return nil, err
	}
	if len(result.IDs) != count {
		return nil, malformed()
	}
	seen := make(map[string]bool)
	for _, id := range result.IDs {
		if !validID(id) || id == "root" || seen[id] {
			return nil, malformed()
		}
		seen[id] = true
	}
	c.mu.Lock()
	if len(c.fresh)+len(result.IDs) <= 100000 {
		for _, id := range result.IDs {
			c.fresh[account+"\x00"+id] = true
		}
	}
	c.mu.Unlock()
	return result.IDs, nil
}

// takeFresh reports whether id was reserved by this client and never used.
// Such an ID cannot exist yet, so the pre-creation existence read is skipped;
// any conflict on creation is still reconciled by identity.
func (c *Client) takeFresh(account, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := account + "\x00" + id
	if c.fresh[key] {
		delete(c.fresh, key)
		return true
	}
	return false
}

func hasParent(o Object, parent string) bool { return len(o.Parents) == 1 && o.Parents[0] == parent }

func validateIntent(account, id, parent, name, operation string) error {
	if err := validateInput(account, id, parent, operation); err != nil {
		return err
	}
	if id == "root" || id == parent || !validName(name) || len(operation) > 100 {
		return domain.Fail("DRIVE_INVALID_INPUT", "The Drive creation intent is invalid.")
	}
	return nil
}

func verify(o Object, id, parent, name, operation, mime string, size int64, md5 string) error {
	if o.ID != id || !hasParent(o, parent) || o.Name != name || o.Trashed || o.MimeType != mime || o.AppProperties[operationProperty] != operation {
		return domain.Fail("DRIVE_IDENTITY_MISMATCH", "The Drive object does not match the approved upload identity. No existing object was overwritten.")
	}
	if mime != folderMIME && (o.Size != size || o.MD5 != md5) {
		return domain.Fail("DRIVE_VERIFICATION_FAILED", "Google Drive content size or checksum did not match the approved source.")
	}
	return nil
}

func (c *Client) verified(ctx context.Context, account, id, parent, name, operation, mime string, size int64, md5 string) (Object, error) {
	o, err := c.GetObject(ctx, account, id)
	if err != nil {
		return Object{}, err
	}
	expected := parent
	if parent == rootAlias && len(o.Parents) == 1 {
		// The object reports My Drive's canonical ID. Accept it only after a
		// parent query confirms that this exact object is a child of My Drive.
		if err = c.confirmRootChild(ctx, account, o, operation); err != nil {
			return Object{}, err
		}
		expected = o.Parents[0]
	}
	if err = verify(o, id, expected, name, operation, mime, size, md5); err != nil {
		return Object{}, err
	}
	return o, nil
}

func (c *Client) confirmRootChild(ctx context.Context, account string, o Object, operation string) error {
	if !validID(operation) {
		return domain.Fail("DRIVE_INVALID_INPUT", "The Drive creation intent is invalid.")
	}
	query := "'root' in parents and trashed = false and appProperties has { key='" + operationProperty + "' and value='" + operation + "' }"
	params := url.Values{"q": {query}, "pageSize": {"100"}, "spaces": {"drive"}, "corpora": {"user"}, "fields": {"nextPageToken,incompleteSearch,files(id,parents)"}}
	var page struct {
		Files []struct {
			ID      string   `json:"id"`
			Parents []string `json:"parents"`
		} `json:"files"`
		IncompleteSearch bool `json:"incompleteSearch"`
	}
	if err := c.get(ctx, account, apiURL+"?"+params.Encode(), &page); err != nil {
		return err
	}
	if page.IncompleteSearch {
		return domain.Fail("DRIVE_INCOMPLETE_LISTING", "Google Drive could not confirm the copy's location. Retry before continuing.")
	}
	for _, f := range page.Files {
		if f.ID == o.ID && len(f.Parents) == 1 && f.Parents[0] == o.Parents[0] {
			return nil
		}
	}
	return domain.Fail("DRIVE_IDENTITY_MISMATCH", "The Drive object is not in My Drive as approved. No existing object was overwritten.")
}

// writableParent returns the canonical parent ID. My Drive may be unreadable
// under drive.file; it then remains the "root" alias and creation proves access.
func (c *Client) writableParent(ctx context.Context, account, parent string) (string, error) {
	key := account + "\x00" + parent
	c.mu.Lock()
	cached, ok := c.parents[key]
	c.mu.Unlock()
	if ok && c.now().Before(cached.expires) {
		return cached.canonical, nil
	}
	folder, err := c.GetFolder(ctx, account, parent)
	if parent == rootAlias && domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
		return rootAlias, nil
	}
	if err != nil {
		return "", err
	}
	if !folder.CanAddChildren {
		return "", domain.Fail("DRIVE_PERMISSION_DENIED", "The selected Drive folder does not allow adding files.")
	}
	c.mu.Lock()
	c.parents[key] = parentCheck{canonical: folder.ID, expires: c.now().Add(parentCacheTTL)}
	c.mu.Unlock()
	return folder.ID, nil
}

// ListChildren returns every non-trashed child of parent visible to the app.
// The listing is consumed to exhaustion; an incomplete listing is an error.
func (c *Client) ListChildren(ctx context.Context, account, parent string) ([]Object, error) {
	if err := validateInput(account, parent); err != nil {
		return nil, err
	}
	query := "'" + parent + "' in parents and trashed = false"
	var out []Object
	token := ""
	for pages := 0; ; pages++ {
		if pages >= 1000 {
			return nil, malformed()
		}
		params := url.Values{"q": {query}, "pageSize": {"1000"}, "spaces": {"drive"}, "corpora": {"user"}, "fields": {"nextPageToken,incompleteSearch,files(" + objectFields + ")"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Files            []wireObject `json:"files"`
			NextPageToken    string       `json:"nextPageToken"`
			IncompleteSearch bool         `json:"incompleteSearch"`
		}
		if err := c.get(ctx, account, apiURL+"?"+params.Encode(), &page); err != nil {
			return nil, err
		}
		if page.IncompleteSearch {
			return nil, domain.Fail("DRIVE_INCOMPLETE_LISTING", "Google Drive could not complete the folder listing. Retry before continuing.")
		}
		if len(page.Files) > 1000 || len(page.NextPageToken) > 8192 || (page.NextPageToken != "" && page.NextPageToken == token) || strings.ContainsAny(page.NextPageToken, "\x00\r\n") {
			return nil, malformed()
		}
		for _, f := range page.Files {
			if !validID(f.ID) || f.Name == "" || f.MimeType == "" || f.Size < 0 || f.DriveID != "" {
				return nil, malformed()
			}
			for _, id := range f.Parents {
				if !validID(id) {
					return nil, malformed()
				}
			}
			out = append(out, f.Object)
		}
		if page.NextPageToken == "" {
			return out, nil
		}
		token = page.NextPageToken
	}
}

func metadata(id, parent, name, operation, mime string) []byte {
	raw, _ := json.Marshal(map[string]any{"id": id, "parents": []string{parent}, "name": name, "mimeType": mime, "appProperties": map[string]string{operationProperty: operation}})
	return raw
}

// CreateFolder reconciles the exact preallocated ID before and after a single
// create attempt. A lost response is never retried using a fresh identity.
func (c *Client) CreateFolder(ctx context.Context, account, id, parent, name, operation string) (Object, error) {
	if err := validateIntent(account, id, parent, name, operation); err != nil {
		return Object{}, err
	}
	canonicalParent, err := c.writableParent(ctx, account, parent)
	if err != nil {
		return Object{}, err
	}
	parent = canonicalParent
	if !c.takeFresh(account, id) {
		if o, err := c.verified(ctx, account, id, parent, name, operation, folderMIME, 0, ""); domain.ErrorCode(err) != "DRIVE_NOT_FOUND" {
			return o, err
		}
	}
	r, sendErr := c.request(ctx, account, http.MethodPost, apiURL+"?"+url.Values{"fields": {objectFields}}.Encode(), metadata(id, parent, name, operation, folderMIME), http.Header{"Content-Type": {"application/json; charset=UTF-8"}})
	if sendErr == nil && r.status != http.StatusOK && r.status != http.StatusCreated && r.status != http.StatusConflict {
		sendErr = responseError(r)
		if !retryable(sendErr) {
			return Object{}, sendErr
		}
	}
	if sendErr != nil && !retryable(sendErr) {
		return Object{}, sendErr
	}
	o, err := c.verified(ctx, account, id, parent, name, operation, folderMIME, 0, "")
	if err != nil && domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
		return Object{}, unknown()
	}
	return o, err
}
