package drive

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

type testAuth struct {
	client  *http.Client
	server  *url.URL
	account string
	mu      sync.Mutex
	calls   int
}

func (a *testAuth) DoAuthorized(ctx context.Context, account string, req *http.Request) (*http.Response, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if account != a.account {
		return nil, domain.Fail("AUTH_ACCOUNT_CHANGED", "The account changed.")
	}
	if req.URL.Host != "www.googleapis.com" || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected request destination")
	}
	copy := req.Clone(ctx)
	u := *req.URL
	u.Host, u.Scheme = a.server.Host, a.server.Scheme
	copy.URL = &u
	return a.client.Do(copy)
}

func fixture(t *testing.T, handler http.HandlerFunc) (*Client, *testAuth) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	endpoint, _ := url.Parse(server.URL)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	auth := &testAuth{client: client, server: endpoint, account: "account-one"}
	c := New(auth)
	c.chunkSize = 256 << 10
	c.wait = func(ctx context.Context, _ time.Duration) error {
		if ctx.Err() != nil {
			return cancelled()
		}
		return nil
	}
	return c, auth
}

func encode(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func parentResponse(w http.ResponseWriter) {
	encode(w, map[string]any{"id": "parent-id", "name": "Destination", "mimeType": folderMIME, "capabilities": map[string]bool{"canAddChildren": true}})
}
func notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"error":{"message":"redacted personal provider message"}}`)
}
func object(id, mime string, size int64, digest string) Object {
	return Object{ID: id, Name: "source.txt", MimeType: mime, Parents: []string{"parent-id"}, Size: size, MD5: digest, Version: "1", AppProperties: map[string]string{operationProperty: "operation-one"}}
}
func digest(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	if domain.ErrorCode(err) != code {
		t.Fatalf("code = %s, want %s; error %v", domain.ErrorCode(err), code, err)
	}
}

func TestFolderPagesPreserveDuplicatesAndFollowExplicitCursor(t *testing.T) {
	pages := 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.Method != "GET" || r.URL.Path != "/drive/v3/files" || r.URL.Query().Get("spaces") != "drive" || !strings.Contains(r.URL.Query().Get("q"), "'parent-id' in parents") {
			t.Errorf("unexpected listing request")
		}
		f := func(id string) map[string]any {
			return map[string]any{"id": id, "name": "Duplicate", "mimeType": folderMIME, "parents": []string{"parent-id"}, "capabilities": map[string]bool{"canAddChildren": true}}
		}
		if r.URL.Query().Get("pageToken") == "" {
			encode(w, map[string]any{"files": []any{f("first"), f("second")}, "nextPageToken": "page-two"})
		} else {
			encode(w, map[string]any{"files": []any{f("third")}})
		}
	})
	first, err := c.ListFolders(context.Background(), "account-one", "parent-id", "")
	if err != nil || len(first.Folders) != 2 || first.Folders[0].Name != first.Folders[1].Name || first.NextPageToken != "page-two" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	second, err := c.ListFolders(context.Background(), "account-one", "parent-id", first.NextPageToken)
	if err != nil || len(second.Folders) != 1 || second.NextPageToken != "" || pages != 2 {
		t.Fatalf("second page = %#v, %v", second, err)
	}
}

func TestListingErrorsNeverReturnPartialSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"incomplete", `{"files":[],"incompleteSearch":true}`, "DRIVE_INCOMPLETE_LISTING", 200},
		{"repeated_cursor", `{"files":[],"nextPageToken":"same"}`, "DRIVE_INVALID_RESPONSE", 200},
		{"duplicate_identity", `{"files":[{"id":"id","name":"A","mimeType":"application/vnd.google-apps.folder"},{"id":"id","name":"A","mimeType":"application/vnd.google-apps.folder"}]}`, "DRIVE_INVALID_RESPONSE", 200},
		{"bad_json", `{`, "DRIVE_INVALID_RESPONSE", 200},
		{"scope", `{"error":{"errors":[{"reason":"insufficientPermissions","message":"PRIVATE"}]}}`, "DRIVE_PERMISSION_DENIED", 403},
		{"quota", `{"error":{"errors":[{"reason":"storageQuotaExceeded"}]}}`, "DRIVE_STORAGE_FULL", 403},
		{"missing", `{}`, "DRIVE_NOT_FOUND", 404},
		{"redirect", `{}`, "DRIVE_REQUEST_FAILED", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Location", "https://untrusted.invalid/token")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			page, err := c.ListFolders(context.Background(), "account-one", "", "same")
			assertCode(t, err, tc.code)
			if len(page.Folders) != 0 || page.NextPageToken != "" || requests != 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("unsafe partial result: %#v, %v", page, err)
			}
		})
	}
}

func TestReadRetriesAreBoundedAndCancellable(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			calls := 0
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if success && calls == 3 {
					parentResponse(w)
				} else {
					w.WriteHeader(503)
				}
			})
			_, err := c.GetFolder(context.Background(), "account-one", "parent-id")
			if success {
				if err != nil || calls != 3 {
					t.Fatalf("retry success: %d %v", calls, err)
				}
			} else {
				assertCode(t, err, "DRIVE_UNAVAILABLE")
				if calls != maxAttempts {
					t.Fatal(calls)
				}
			}
		})
	}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) })
	c.wait = func(context.Context, time.Duration) error { cancel(); return cancelled() }
	_, err := c.GetFolder(ctx, "account-one", "parent-id")
	assertCode(t, err, "CANCELLED")
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestGenerateIDsRejectsDuplicatesAndBadInput(t *testing.T) {
	for _, raw := range []string{`{"ids":["one","one"]}`, `{"ids":["one"]}`, `{"ids":["one","bad/id"]}`} {
		c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, raw) })
		_, err := c.GenerateIDs(context.Background(), "account-one", 2)
		assertCode(t, err, "DRIVE_INVALID_RESPONSE")
	}
	c, auth := fixture(t, func(w http.ResponseWriter, r *http.Request) { encode(w, map[string]any{"ids": []string{"one", "two"}}) })
	ids, err := c.GenerateIDs(context.Background(), "account-one", 2)
	if err != nil || !reflect.DeepEqual(ids, []string{"one", "two"}) {
		t.Fatalf("%v %v", ids, err)
	}
	_, err = c.GenerateIDs(context.Background(), "account-one", 0)
	assertCode(t, err, "DRIVE_INVALID_INPUT")
	_, err = c.GetObject(context.Background(), "account-one", "../../secret")
	assertCode(t, err, "DRIVE_INVALID_INPUT")
	if auth.calls != 1 {
		t.Fatal("invalid inputs reached network")
	}
}

func TestCreateFolderReconcilesLostAcknowledgementAndNeverOverwrites(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			created, posts := false, 0
			expected := object("new-id", folderMIME, 0, "")
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/parent-id") {
					parentResponse(w)
					return
				}
				if r.Method == "GET" {
					if created {
						encode(w, expected)
					} else {
						notFound(w)
					}
					return
				}
				if r.Method != "POST" {
					t.Error("unexpected mutation method")
				}
				posts++
				var metadata Object
				if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil || metadata.ID != expected.ID || !hasParent(metadata, "parent-id") || metadata.AppProperties[operationProperty] != "operation-one" {
					t.Error("creation identity not bound")
				}
				created = true
				if lost {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				encode(w, expected)
			})
			for i := 0; i < 2; i++ {
				o, err := c.CreateFolder(context.Background(), "account-one", "new-id", "parent-id", "source.txt", "operation-one")
				if err != nil || o.ID != "new-id" {
					t.Fatalf("%#v %v", o, err)
				}
			}
			if posts != 1 {
				t.Fatalf("POST count = %d", posts)
			}
		})
	}
}

func TestCreateFolderForeignIDAndDeniedParentBlockMutation(t *testing.T) {
	for _, denied := range []bool{true, false} {
		posts := 0
		c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				posts++
				t.Error("unexpected mutation")
			}
			if strings.HasSuffix(r.URL.Path, "/parent-id") {
				if denied {
					encode(w, map[string]any{"id": "parent-id", "name": "No write", "mimeType": folderMIME})
				} else {
					parentResponse(w)
				}
				return
			}
			o := object("new-id", folderMIME, 0, "")
			o.AppProperties = nil
			encode(w, o)
		})
		_, err := c.CreateFolder(context.Background(), "account-one", "new-id", "parent-id", "source.txt", "operation-one")
		if denied {
			assertCode(t, err, "DRIVE_PERMISSION_DENIED")
		} else {
			assertCode(t, err, "DRIVE_IDENTITY_MISMATCH")
		}
		if posts != 0 {
			t.Fatal(posts)
		}
	}
}

func TestCreateFolderAmbiguousFailureDoesNotRetryPOST(t *testing.T) {
	posts := 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			w.WriteHeader(503)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/parent-id") {
			parentResponse(w)
			return
		}
		notFound(w)
	})
	_, err := c.CreateFolder(context.Background(), "account-one", "new-id", "parent-id", "source.txt", "operation-one")
	assertCode(t, err, "UNKNOWN_REMOTE_RESULT")
	if posts != 1 {
		t.Fatal(posts)
	}
}

func TestGetFolderRejectsSharedDriveAndShortcuts(t *testing.T) {
	for _, tc := range []struct{ extra, code string }{{`"mimeType":"application/vnd.google-apps.shortcut"`, "DRIVE_NOT_FOLDER"}, {`"mimeType":"application/vnd.google-apps.folder","driveId":"shared"`, "DRIVE_UNSUPPORTED"}, {`"mimeType":"application/vnd.google-apps.folder","trashed":true`, "DRIVE_NOT_FOLDER"}} {
		c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, `{"id":"folder","name":"Name",%s}`, tc.extra)
		})
		_, err := c.GetFolder(context.Background(), "account-one", "folder")
		assertCode(t, err, tc.code)
	}
}

type authFunc func(context.Context, string, *http.Request) (*http.Response, error)

func (f authFunc) DoAuthorized(ctx context.Context, account string, req *http.Request) (*http.Response, error) {
	return f(ctx, account, req)
}

type errorBody struct{ err error }

func (b errorBody) Read([]byte) (int, error) { return 0, b.err }
func (b errorBody) Close() error             { return nil }

func TestRealAuthorizationErrorsNeverBecomeNetworkRetries(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"scope", driveauth.ErrScope, "AUTH_REQUIRED"},
		{"revoked", driveauth.ErrReconnect, "AUTH_REQUIRED"},
		{"identity", driveauth.ErrIdentity, "ACCOUNT_CHANGED"},
		{"client", driveauth.ErrClientChanged, "AUTH_CLIENT_CHANGED"},
		{"cancelled", driveauth.ErrCanceled, "CANCELLED"},
		{"busy", driveauth.ErrBusy, "AUTH_BUSY"},
		{"vault", driveauth.ErrStorage, "AUTH_STORAGE_UNAVAILABLE"},
		{"cleanup", driveauth.ErrRevokedCleanup, "AUTH_STORAGE_UNAVAILABLE"},
		{"configuration", driveauth.ErrBuildConfig, "AUTH_CONFIGURATION_REQUIRED"},
		{"provider", driveauth.ErrProvider, "DRIVE_REQUEST_FAILED"},
	} {
		for _, bodyFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/body=%v", tc.name, bodyFailure), func(t *testing.T) {
				calls := 0
				c := New(authFunc(func(_ context.Context, _ string, req *http.Request) (*http.Response, error) {
					calls++
					err := fmt.Errorf("PRIVATE_DIAGNOSTIC: %w", tc.err)
					if bodyFailure {
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: errorBody{err}}, nil
					}
					return nil, err
				}))
				c.wait = func(context.Context, time.Duration) error {
					t.Error("authentication intervention was retried")
					return nil
				}
				_, err := c.GetObject(context.Background(), "account-one", "file-id")
				assertCode(t, err, tc.code)
				if calls != 1 || strings.Contains(err.Error(), "PRIVATE_DIAGNOSTIC") {
					t.Fatalf("unsafe retry or diagnostic: %d %v", calls, err)
				}
				_, err = c.request(context.Background(), "account-one", http.MethodPost, apiURL, []byte(`{}`), nil)
				assertCode(t, err, tc.code)
				if calls != 2 {
					t.Fatal("mutation authorization failure was replayed")
				}
			})
		}
	}
}
