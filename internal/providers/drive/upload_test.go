package drive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

const testSession = "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id=PRIVATE_SESSION"

func TestUploadReconcilesPartialChunksLostResponseAndVerifies(t *testing.T) {
	data := strings.Repeat("abcdefgh", 90000)
	var stored []byte
	posts, chunks, queries, reads := 0, 0, 0, 0
	complete := false
	var starts []int64
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			if strings.HasSuffix(r.URL.Path, "/parent-id") {
				parentResponse(w)
				return
			}
			reads++
			if complete {
				encode(w, object("file-id", binaryMIME, int64(len(stored)), digest(string(stored))))
			} else {
				notFound(w)
			}
			return
		}
		if r.Method == "POST" {
			posts++
			if r.Header.Get("X-Upload-Content-Length") != strconv.Itoa(len(data)) {
				t.Error("missing total size")
			}
			w.Header().Set("Location", testSession)
			w.WriteHeader(200)
			return
		}
		if r.Method != "PUT" || r.URL.Query().Get("upload_id") != "PRIVATE_SESSION" {
			t.Error("unexpected session request")
		}
		if strings.HasPrefix(r.Header.Get("Content-Range"), "bytes */") {
			queries++
			if complete {
				w.WriteHeader(200)
				return
			}
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(stored)-1))
			w.WriteHeader(308)
			return
		}
		chunks++
		var start, end, total int64
		if _, err := fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil {
			t.Error(err)
		}
		starts = append(starts, start)
		body, _ := io.ReadAll(r.Body)
		if start != int64(len(stored)) || len(body) != int(end-start+1) || total != int64(len(data)) || int64(len(body)) > cChunkSize {
			t.Errorf("invalid chunk start/length: %d %d", start, len(body))
		}
		if chunks == 1 {
			// The server commits only a prefix. The second request must start at
			// the acknowledged byte, not at the end of the submitted chunk.
			stored = append(stored, body[:123]...)
			w.Header().Set("Range", "bytes=0-122")
			w.WriteHeader(308)
			return
		}
		stored = append(stored, body...)
		complete = len(stored) == len(data)
		if chunks == 2 || complete {
			// Drop both an intermediate response and the final acknowledgement.
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(stored)-1))
		w.WriteHeader(308)
	})
	o, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader(data), int64(len(data)), digest(data))
	if err != nil || o.MD5 != digest(data) || string(stored) != data || posts != 1 || queries != 2 || reads != 2 {
		t.Fatalf("upload = %#v, %v; post/query/read %d/%d/%d", o, err, posts, queries, reads)
	}
	if starts[1] != 123 {
		t.Fatalf("partial acknowledgement not followed: %v", starts)
	}
	// Repeating the same durable operation must not create or upload again.
	_, err = c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader(data), int64(len(data)), digest(data))
	if err != nil || posts != 1 || queries != 2 {
		t.Fatalf("idempotency: %v, %d", err, posts)
	}
}

const cChunkSize = 256 << 10

// Live Google session URIs carry session_crd besides upload_id. Every real
// upload failed with UNKNOWN_REMOTE_RESULT, before sending data, while the
// session check accepted only uploadType and upload_id.
func TestUploadAcceptsGoogleSessionParameters(t *testing.T) {
	complete, puts := false, 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			echo := r.URL.Query()
			if len(echo) != 1 || echo.Get("uploadType") != "resumable" {
				t.Errorf("unexpected initiation query %v", echo)
			}
			echo.Set("upload_id", "PRIVATE_SESSION")
			echo.Set("session_crd", "PRIVATE_CRD")
			w.Header().Set("Location", "https://www.googleapis.com/upload/drive/v3/files?"+echo.Encode())
		case r.Method == "PUT":
			puts++
			if q := r.URL.Query(); q.Get("upload_id") != "PRIVATE_SESSION" || q.Get("session_crd") != "PRIVATE_CRD" {
				t.Error("the session URI was not used as issued")
			}
			io.Copy(io.Discard, r.Body)
			complete = true
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/parent-id"):
			parentResponse(w)
		case complete:
			encode(w, object("file-id", binaryMIME, 4, digest("data")))
		default:
			notFound(w)
		}
	})
	o, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("data"), 4, digest("data"))
	if err != nil || o.ID != "file-id" || puts != 1 {
		t.Fatalf("upload = %#v, %v, puts %d", o, err, puts)
	}
}

func TestUploadEmptyFileAndVerificationFailures(t *testing.T) {
	for _, tc := range []struct{ name, change, code string }{
		{"empty", "", ""}, {"checksum", "checksum", "DRIVE_VERIFICATION_FAILED"}, {"size", "size", "DRIVE_VERIFICATION_FAILED"}, {"parent", "parent", "DRIVE_IDENTITY_MISMATCH"}, {"operation", "operation", "DRIVE_IDENTITY_MISMATCH"}, {"trashed", "trashed", "DRIVE_IDENTITY_MISMATCH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			complete, puts := false, 0
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					w.Header().Set("Location", testSession)
					return
				}
				if r.Method == "PUT" {
					puts++
					if r.ContentLength != 0 || r.Header.Get("Content-Range") != "bytes */0" {
						t.Error("invalid empty upload")
					}
					complete = true
					w.WriteHeader(201)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/parent-id") {
					parentResponse(w)
					return
				}
				if !complete {
					notFound(w)
					return
				}
				o := object("file-id", binaryMIME, 0, digest(""))
				switch tc.change {
				case "checksum":
					o.MD5 = digest("different")
				case "size":
					o.Size = 2
				case "parent":
					o.Parents = []string{"wrong"}
				case "operation":
					o.AppProperties[operationProperty] = "different"
				case "trashed":
					o.Trashed = true
				}
				encode(w, o)
			})
			_, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader(""), 0, digest(""))
			if tc.code != "" {
				assertCode(t, err, tc.code)
			} else if err != nil {
				t.Fatal(err)
			}
			if puts != 1 {
				t.Fatal(puts)
			}
		})
	}
}

func TestUploadRejectsSessionURLWithoutSendingCredentials(t *testing.T) {
	for _, location := range []string{
		"http://www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id=PRIVATE",
		"https://evil.invalid/upload/drive/v3/files?uploadType=resumable&upload_id=PRIVATE",
		"https://www.googleapis.com:443/upload/drive/v3/files?uploadType=resumable&upload_id=PRIVATE",
		"https://user@www.googleapis.com/upload/drive/v3/files?uploadType=resumable&upload_id=PRIVATE",
		"https://www.googleapis.com/drive/v3/files?uploadType=resumable&upload_id=PRIVATE",
		testSession + "&upload_id=SECOND", testSession + "#fragment", testSession + "&access_token=SECRET", "",
		testSession + "&uploadType=resumable", testSession + "&key=API", testSession + "&session_crd=", testSession + "&bad-key=x",
		"https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart&upload_id=PRIVATE",
	} {
		t.Run(location, func(t *testing.T) {
			puts, posts := 0, 0
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					w.Header().Set("Location", location)
					return
				}
				if r.Method == "PUT" {
					puts++
					return
				}
				if strings.HasSuffix(r.URL.Path, "/parent-id") {
					parentResponse(w)
					return
				}
				notFound(w)
			})
			_, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
			assertCode(t, err, "UNKNOWN_REMOTE_RESULT")
			if puts != 0 || posts != 1 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("session escaped: %d %d %v", puts, posts, err)
			}
		})
	}
}

func TestUploadInterruptionHasBoundedRetriesAndExpiredSessionReconciles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{{"unavailable", 503, "UNKNOWN_REMOTE_RESULT"}, {"expired", 404, "DRIVE_SESSION_EXPIRED"}, {"scope", 403, "DRIVE_PERMISSION_DENIED"}, {"regressing", 308, "DRIVE_INVALID_RESPONSE"}} {
		t.Run(tc.name, func(t *testing.T) {
			puts, posts := 0, 0
			c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					w.Header().Set("Location", testSession)
					return
				}
				if r.Method == "PUT" {
					puts++
					if tc.status == 308 {
						w.Header().Set("Range", "bytes=0-999")
					}
					w.WriteHeader(tc.status)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/parent-id") {
					parentResponse(w)
					return
				}
				notFound(w)
			})
			_, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
			assertCode(t, err, tc.code)
			if posts != 1 || puts > maxAttempts {
				t.Fatalf("unbounded retry: %d %d", posts, puts)
			}
		})
	}
}

func TestUploadLostInitiationNeverCreatesTwice(t *testing.T) {
	posts, puts := 0, 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		if r.Method == "PUT" {
			puts++
			return
		}
		if strings.HasSuffix(r.URL.Path, "/parent-id") {
			parentResponse(w)
			return
		}
		notFound(w)
	})
	_, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
	assertCode(t, err, "UNKNOWN_REMOTE_RESULT")
	if posts != 1 || puts != 0 {
		t.Fatalf("post/put = %d/%d", posts, puts)
	}
}

func TestUploadRejectsChangedSourceBeforeMutationAndCancelledAccount(t *testing.T) {
	posts := 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			posts++
		}
		if strings.HasSuffix(r.URL.Path, "/parent-id") {
			parentResponse(w)
			return
		}
		notFound(w)
	})
	_, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("changed"), 1, digest("x"))
	assertCode(t, err, "SOURCE_CHANGED")
	if posts != 0 {
		t.Fatal(posts)
	}
	_, err = c.Upload(context.Background(), "different-account", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
	assertCode(t, err, "AUTH_ACCOUNT_CHANGED")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Upload(ctx, "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
	assertCode(t, err, "CANCELLED")
}

func TestResumeOffsetValidatesAcknowledgedBytes(t *testing.T) {
	for _, tc := range []struct {
		header                   string
		before, sent, size, want int64
		invalid                  bool
	}{
		{"", 0, 10, 10, 0, false}, {"bytes=0-3", 2, 10, 20, 4, false}, {"bytes=0-9", 0, 10, 10, 10, false},
		{"", 2, 10, 10, 0, true}, {"bytes=0-1", 3, 10, 10, 0, true}, {"bytes=0-10", 0, 10, 20, 0, true},
		{"bytes=2-3", 0, 10, 10, 0, true}, {"bytes=0-+1", 0, 10, 10, 0, true}, {"bytes=0-9223372036854775807", 0, 10, 10, 0, true},
	} {
		h := make(http.Header)
		if tc.header != "" {
			h.Set("Range", tc.header)
		}
		n, err := resumeOffset(h, tc.before, tc.sent, tc.size)
		if tc.invalid {
			assertCode(t, err, "DRIVE_INVALID_RESPONSE")
		} else if err != nil || n != tc.want {
			t.Fatalf("offset %s = %d %v", tc.header, n, err)
		}
	}
}

func TestUploadConflictReconcilesMatchingExistingID(t *testing.T) {
	posts := 0
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			w.WriteHeader(409)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/parent-id") {
			parentResponse(w)
			return
		}
		if posts == 0 {
			notFound(w)
			return
		}
		encode(w, object("file-id", binaryMIME, 1, digest("x")))
	})
	o, err := c.Upload(context.Background(), "account-one", "file-id", "parent-id", "source.txt", "operation-one", strings.NewReader("x"), 1, digest("x"))
	if err != nil || o.ID != "file-id" || posts != 1 {
		t.Fatalf("%v %v %d", o, err, posts)
	}
}

func TestRequestErrorsDoNotExposeSessionOrProviderBody(t *testing.T) {
	c, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"error":{"message":"PRIVATE_SESSION REAL_CONTENT SECRET_TOKEN"}}`)
	})
	_, err := c.GetObject(context.Background(), "account-one", "file-id")
	assertCode(t, err, "DRIVE_REQUEST_FAILED")
	for _, text := range []string{"PRIVATE_SESSION", "REAL_CONTENT", "SECRET_TOKEN"} {
		if strings.Contains(err.Error(), text) {
			t.Fatal("provider body leaked")
		}
	}
	if domain.ErrorCode(err) == "INTERNAL_ERROR" {
		t.Fatal("untyped error")
	}
}
