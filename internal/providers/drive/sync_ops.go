package drive

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

const changesURL = "https://www.googleapis.com/drive/v3/changes"

// GoogleNative reports whether a Drive item is a Google-native document,
// shortcut or other non-binary item that has no downloadable content.
func GoogleNative(mime string) bool {
	return mime != folderMIME && strings.HasPrefix(mime, "application/vnd.google-apps.")
}

// IsFolder reports whether o is a Drive folder.
func IsFolder(o Object) bool { return o.MimeType == folderMIME }

// UpdateContent stores new content in an existing binary file as a new
// revision through a resumable session, then verifies size and MD5. Drive keeps
// earlier revisions, so the replacement can be undone from the file's history.
func (c *Client) UpdateContent(ctx context.Context, account, id string, source io.ReadSeeker, size int64, md5 string) (Object, error) {
	if err := validateInput(account, id); err != nil {
		return Object{}, err
	}
	if id == "root" || source == nil || size < 0 || !validMD5(md5) {
		return Object{}, domain.Fail("DRIVE_INVALID_INPUT", "Updating a file requires its ID, size and MD5 checksum.")
	}
	current, err := c.GetObject(ctx, account, id)
	if err != nil {
		return Object{}, err
	}
	if current.Trashed || current.MimeType == folderMIME || GoogleNative(current.MimeType) {
		return Object{}, domain.Fail("DRIVE_IDENTITY_MISMATCH", "The Drive item is no longer a regular file. Nothing was replaced.")
	}
	if current.Size == size && current.MD5 == md5 {
		return current, nil
	}
	length, err := source.Seek(0, io.SeekEnd)
	if err != nil || length != size {
		return Object{}, domain.Fail("SOURCE_CHANGED", "The source no longer matches the expected size.")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return Object{}, domain.Fail("SOURCE_UNREADABLE", "The upload source cannot be read.")
	}
	params := url.Values{"uploadType": {"resumable"}}
	r, err := c.request(ctx, account, http.MethodPatch, uploadURL+"/"+id+"?"+params.Encode(), []byte("{}"), http.Header{
		"Content-Type": {"application/json; charset=UTF-8"}, "X-Upload-Content-Type": {binaryMIME}, "X-Upload-Content-Length": {strconv.FormatInt(size, 10)},
	})
	if err == nil && r.status != http.StatusOK && r.status != http.StatusCreated {
		err = responseError(r)
	}
	if err != nil {
		return Object{}, err
	}
	locations := r.header.Values("Location")
	if len(locations) != 1 || !validSession(locations[0], params, "/upload/drive/v3/files/"+id) {
		return Object{}, unknown()
	}
	if err = c.transfer(ctx, account, locations[0], source, size); err != nil {
		code := domain.ErrorCode(err)
		if code == "UNKNOWN_REMOTE_RESULT" || code == "DRIVE_SESSION_EXPIRED" {
			if o, e := c.GetObject(ctx, account, id); e == nil && o.Size == size && o.MD5 == md5 {
				return o, nil
			}
		}
		return Object{}, err
	}
	o, err := c.GetObject(ctx, account, id)
	if err != nil {
		return Object{}, err
	}
	if o.Trashed || o.Size != size || o.MD5 != md5 {
		return Object{}, domain.Fail("DRIVE_VERIFICATION_FAILED", "Google Drive content size or checksum did not match the local file.")
	}
	return o, nil
}

// Trash moves a Drive item to the trash, from where the owner can restore it
// for 30 days. An item that no longer exists is treated as already removed.
func (c *Client) Trash(ctx context.Context, account, id string) error {
	if err := validateInput(account, id); err != nil {
		return err
	}
	if id == "root" {
		return domain.Fail("DRIVE_INVALID_INPUT", "My Drive itself cannot be moved to the trash.")
	}
	endpoint := apiURL + "/" + id + "?" + url.Values{"fields": {"id,trashed"}}.Encode()
	for attempt := 0; attempt < c.attempts; attempt++ {
		r, err := c.request(ctx, account, http.MethodPatch, endpoint, []byte(`{"trashed":true}`), http.Header{"Content-Type": {"application/json; charset=UTF-8"}})
		if err == nil && r.status == http.StatusNotFound {
			return nil
		}
		if err == nil && r.status == http.StatusOK {
			var result struct {
				ID      string `json:"id"`
				Trashed bool   `json:"trashed"`
			}
			if json.Unmarshal(r.data, &result) != nil || result.ID != id || !result.Trashed {
				return malformed()
			}
			return nil
		}
		if err == nil {
			err = responseError(r)
		}
		if !retryable(err) || attempt == c.attempts-1 {
			// A lost acknowledgement is reconciled by reading the item.
			if retryable(err) {
				if o, e := c.GetObject(ctx, account, id); e == nil && o.Trashed || domain.ErrorCode(e) == "DRIVE_NOT_FOUND" {
					return nil
				}
			}
			return err
		}
		if err = c.backoff(ctx, attempt, r.header); err != nil {
			return err
		}
	}
	return malformed()
}

// Change is one entry of the Drive change feed. File parents are present for
// items that still exist and are visible.
type Change struct {
	FileID  string
	Removed bool
	Trashed bool
	Parents []string
}

// StartPageToken returns the current position of the account's change feed.
func (c *Client) StartPageToken(ctx context.Context, account string) (string, error) {
	if err := validateInput(account); err != nil {
		return "", err
	}
	var result struct {
		StartPageToken string `json:"startPageToken"`
	}
	if err := c.get(ctx, account, changesURL+"/startPageToken?"+url.Values{"fields": {"startPageToken"}}.Encode(), &result); err != nil {
		return "", err
	}
	if !validToken(result.StartPageToken) {
		return "", malformed()
	}
	return result.StartPageToken, nil
}

func validToken(t string) bool {
	return t != "" && len(t) <= 8192 && !strings.ContainsAny(t, "\x00\r\n ")
}

// Changes returns the changes after token and the token to use next time.
// At most maxPages pages are read per call; the returned token continues.
func (c *Client) Changes(ctx context.Context, account, token string) ([]Change, string, error) {
	if err := validateInput(account); err != nil {
		return nil, "", err
	}
	if !validToken(token) {
		return nil, "", domain.Fail("DRIVE_INVALID_INPUT", "The change position is invalid.")
	}
	const maxPages = 50
	var out []Change
	for page := 0; page < maxPages; page++ {
		params := url.Values{"pageToken": {token}, "pageSize": {"1000"}, "spaces": {"drive"}, "includeRemoved": {"true"},
			"fields": {"nextPageToken,newStartPageToken,changes(fileId,removed,file(parents,trashed))"}}
		var result struct {
			NextPageToken     string `json:"nextPageToken"`
			NewStartPageToken string `json:"newStartPageToken"`
			Changes           []struct {
				FileID  string `json:"fileId"`
				Removed bool   `json:"removed"`
				File    *struct {
					Parents []string `json:"parents"`
					Trashed bool     `json:"trashed"`
				} `json:"file"`
			} `json:"changes"`
		}
		if err := c.get(ctx, account, changesURL+"?"+params.Encode(), &result); err != nil {
			return nil, "", err
		}
		if len(result.Changes) > 1000 {
			return nil, "", malformed()
		}
		for _, ch := range result.Changes {
			if ch.FileID == "" {
				continue // drive-level or non-file changes
			}
			if !validID(ch.FileID) {
				return nil, "", malformed()
			}
			item := Change{FileID: ch.FileID, Removed: ch.Removed}
			if ch.File != nil {
				item.Trashed = ch.File.Trashed
				for _, p := range ch.File.Parents {
					if !validID(p) {
						return nil, "", malformed()
					}
				}
				item.Parents = append([]string{}, ch.File.Parents...)
			}
			out = append(out, item)
		}
		switch {
		case result.NewStartPageToken != "":
			if !validToken(result.NewStartPageToken) {
				return nil, "", malformed()
			}
			return out, result.NewStartPageToken, nil
		case validToken(result.NextPageToken) && result.NextPageToken != token:
			token = result.NextPageToken
		default:
			return nil, "", malformed()
		}
	}
	return out, token, nil
}
