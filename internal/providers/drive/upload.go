package drive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

const binaryMIME = "application/octet-stream"

// validSession is deliberately narrower than a general URL allowlist. The
// session URI carries a secret and remains in this stack frame only.
func validSession(raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.googleapis.com" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Path != "/upload/drive/v3/files" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Get("uploadType") != "resumable" || q.Get("upload_id") == "" {
		return false
	}
	for key, values := range q {
		if len(values) != 1 || (key != "uploadType" && key != "upload_id") || strings.ContainsAny(values[0], "\x00\r\n") {
			return false
		}
	}
	return true
}

func validMD5(s string) bool {
	if len(s) != 32 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func resumeOffset(header http.Header, previous, sent, total int64) (int64, error) {
	values := header.Values("Range")
	if len(values) == 0 {
		if previous == 0 {
			return 0, nil
		}
		return 0, malformed()
	}
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes=0-") {
		return 0, malformed()
	}
	tail := strings.TrimPrefix(values[0], "bytes=0-")
	if tail == "" || strings.IndexFunc(tail, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, malformed()
	}
	last, err := strconv.ParseInt(tail, 10, 64)
	if err != nil || last < 0 || last >= total || last >= sent {
		return 0, malformed()
	}
	offset := last + 1
	if offset < previous {
		return 0, malformed()
	}
	return offset, nil
}

// Upload creates a binary file, never updates an existing object. Source must
// represent the executor's already-approved, read-only snapshot. The executor
// owns source identity checks and a durable journal containing id/operation.
func (c *Client) Upload(ctx context.Context, account, id, parent, name, operation string, source io.ReadSeeker, size int64, md5 string) (Object, error) {
	if err := validateIntent(account, id, parent, name, operation); err != nil {
		return Object{}, err
	}
	if source == nil || size < 0 || !validMD5(md5) {
		return Object{}, domain.Fail("DRIVE_INVALID_INPUT", "Upload requires a source size and approved MD5 checksum.")
	}
	parent, err := c.writableParent(ctx, account, parent)
	if err != nil {
		return Object{}, err
	}
	if !c.takeFresh(account, id) {
		if o, err := c.verified(ctx, account, id, parent, name, operation, binaryMIME, size, md5); domain.ErrorCode(err) != "DRIVE_NOT_FOUND" {
			return o, err
		}
	}
	// Only the source is seeked; no local file writes or metadata changes occur.
	length, err := source.Seek(0, io.SeekEnd)
	if err != nil || length != size {
		return Object{}, domain.Fail("SOURCE_CHANGED", "The source no longer matches the approved upload size.")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return Object{}, domain.Fail("SOURCE_UNREADABLE", "The upload source cannot be read.")
	}

	session, err := c.startUpload(ctx, account, id, parent, name, operation, size)
	if err != nil {
		if domain.ErrorCode(err) == "UNKNOWN_REMOTE_RESULT" || domain.ErrorCode(err) == "DRIVE_CONFLICT" {
			o, reconcileErr := c.verified(ctx, account, id, parent, name, operation, binaryMIME, size, md5)
			if reconcileErr == nil {
				return o, nil
			}
			if domain.ErrorCode(reconcileErr) != "DRIVE_NOT_FOUND" {
				return Object{}, reconcileErr
			}
			return Object{}, unknown()
		}
		return Object{}, err
	}
	if err = c.transfer(ctx, account, session, source, size); err != nil {
		if domain.ErrorCode(err) == "UNKNOWN_REMOTE_RESULT" || domain.ErrorCode(err) == "DRIVE_SESSION_EXPIRED" {
			o, reconcileErr := c.verified(ctx, account, id, parent, name, operation, binaryMIME, size, md5)
			if reconcileErr == nil {
				return o, nil
			}
			if domain.ErrorCode(reconcileErr) != "DRIVE_NOT_FOUND" {
				return Object{}, reconcileErr
			}
		}
		return Object{}, err
	}
	o, err := c.verified(ctx, account, id, parent, name, operation, binaryMIME, size, md5)
	if domain.ErrorCode(err) == "DRIVE_NOT_FOUND" {
		return Object{}, unknown()
	}
	return o, err
}

func (c *Client) startUpload(ctx context.Context, account, id, parent, name, operation string, size int64) (string, error) {
	endpoint := uploadURL + "?" + url.Values{"uploadType": {"resumable"}, "fields": {objectFields}}.Encode()
	r, err := c.request(ctx, account, http.MethodPost, endpoint, metadata(id, parent, name, operation, binaryMIME), http.Header{
		"Content-Type": {"application/json; charset=UTF-8"}, "X-Upload-Content-Type": {binaryMIME}, "X-Upload-Content-Length": {strconv.FormatInt(size, 10)},
	})
	if err != nil {
		if retryable(err) || domain.ErrorCode(err) == "DRIVE_INVALID_RESPONSE" {
			return "", unknown()
		}
		return "", err
	}
	if r.status != http.StatusOK && r.status != http.StatusCreated {
		err = responseError(r)
		if retryable(err) {
			return "", unknown()
		}
		return "", err
	}
	locations := r.header.Values("Location")
	if len(locations) != 1 || !validSession(locations[0]) {
		return "", unknown()
	}
	return locations[0], nil
}

func (c *Client) transfer(ctx context.Context, account, session string, source io.ReadSeeker, size int64) error {
	// A chunk, including its replayable HTTP body, is bounded to 8 MiB. Partial
	// acknowledgements cause a seek to the actual committed offset, not a blind
	// increment by bytes sent. Session state is not persisted or returned.
	if c.chunkSize < 256<<10 || c.chunkSize > 8<<20 || c.chunkSize%(256<<10) != 0 {
		return domain.Fail("DRIVE_INVALID_INPUT", "The upload chunk configuration is invalid.")
	}
	buffer := make([]byte, c.chunkSize)
	var offset, sent int64
	failures := 0
	query := false
	for {
		if ctx.Err() != nil {
			return cancelled()
		}
		var body []byte
		contentRange := fmt.Sprintf("bytes */%d", size)
		if !query && size > 0 && offset < size {
			n := min(c.chunkSize, size-offset)
			if _, err := source.Seek(offset, io.SeekStart); err != nil {
				return sourceError(ctx, err)
			}
			if _, err := io.ReadFull(source, buffer[:n]); err != nil {
				return sourceError(ctx, err)
			}
			body = buffer[:n]
			contentRange = fmt.Sprintf("bytes %d-%d/%d", offset, offset+n-1, size)
			sent = max(sent, offset+n)
		}
		r, err := c.request(ctx, account, http.MethodPut, session, body, http.Header{"Content-Type": {binaryMIME}, "Content-Range": {contentRange}})
		if err == nil {
			switch r.status {
			case http.StatusOK, http.StatusCreated:
				// Ignore the acknowledgement body; a separate metadata read verifies
				// identity and server-computed checksum before claiming success.
				return nil
			case 308:
				next, parseErr := resumeOffset(r.header, offset, sent, size)
				if parseErr != nil {
					return parseErr
				}
				// A status query that reports where to resume is progress. A data
				// chunk without progress, or committed bytes that are never
				// finalized, is a stall and consumes the bounded retry budget.
				if next > offset || (len(body) == 0 && next < size) {
					failures = 0
				} else {
					failures++
				}
				offset = next
				query = offset == size
				if failures >= c.attempts {
					return unknown()
				}
				if failures > 0 {
					if err := c.backoff(ctx, failures-1, r.header); err != nil {
						return err
					}
				}
				continue
			case http.StatusNotFound, http.StatusGone:
				return domain.Fail("DRIVE_SESSION_EXPIRED", "The upload session expired. Resume the recorded operation to reconcile its object ID before starting another session.")
			default:
				err = responseError(r)
			}
		}
		if !retryable(err) {
			return err
		}
		failures++
		if failures >= c.attempts {
			return unknown()
		}
		if err := c.backoff(ctx, failures-1, r.header); err != nil {
			return err
		}
		query = true // Reconcile committed bytes before sending any further data.
	}
}

// sourceError keeps the local reader's safe classification. A source that
// changed while streaming is a per-file local condition, not a remote failure.
func sourceError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return cancelled()
	}
	var safe *domain.Error
	if errors.As(err, &safe) && (safe.Code == "SOURCE_CHANGED" || safe.Code == "CANCELLED") {
		return safe
	}
	return domain.Fail("SOURCE_CHANGED", "The upload source could not be read completely.")
}
