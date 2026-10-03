package transferstate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncState stores two-way sync pairs, the last state both sides agreed on
// (the base used for three-way decisions), pending creations, change cursors
// and bounded activity. It contains no credentials or upload-session URLs.
type SyncState struct {
	db *sql.DB
}

// SyncEntry is the last synchronized state of one path. Local fields describe
// the local file when both sides matched; remote fields describe the Drive item.
type SyncEntry struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"` // "file" or "dir"
	LocalSize      int64  `json:"localSize"`
	LocalMtime     int64  `json:"localMtime"`
	LocalMD5       string `json:"localMd5"`
	RemoteID       string `json:"remoteId"`
	RemoteMD5      string `json:"remoteMd5"`
	RemoteVersion  string `json:"remoteVersion"`
	RemoteModified string `json:"remoteModified"`
}

// SyncIntent records a reserved Drive ID before a creation so a lost response
// or process exit is reconciled by identity instead of creating a duplicate.
type SyncIntent struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	RemoteID  string `json:"remoteId"`
	Operation string `json:"operation"`
}

// SyncActivity is one bounded, redacted event shown in the interface.
type SyncActivity struct {
	At     string `json:"at"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Detail string `json:"detail,omitempty"`
}

const syncStateVersion = 1

// ActivityPerPair bounds retained activity events for each pair.
const ActivityPerPair = 500

const syncSchema = `CREATE TABLE sync_pairs (id TEXT PRIMARY KEY, payload BLOB NOT NULL);
CREATE TABLE sync_entries (pair_id TEXT NOT NULL, path TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY(pair_id, path));
CREATE TABLE sync_intents (pair_id TEXT NOT NULL, path TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY(pair_id, path));
CREATE TABLE sync_activity (seq INTEGER PRIMARY KEY AUTOINCREMENT, pair_id TEXT NOT NULL, payload BLOB NOT NULL);
CREATE INDEX sync_activity_pair ON sync_activity(pair_id, seq);
CREATE TABLE sync_cursors (account TEXT PRIMARY KEY, token TEXT NOT NULL);
PRAGMA user_version=1;`

// OpenSyncState opens the private sync database in dir, creating it when needed.
func OpenSyncState(dir string) (*SyncState, error) {
	abs, err := canonicalDirectory(dir)
	if err != nil || prepareStateDirectory(abs) != nil {
		return nil, failure()
	}
	if st, e := os.Lstat(abs); e != nil || !privateNode(abs, st, true) {
		return nil, failure()
	}
	for _, name := range []string{"sync.sqlite", "sync.sqlite-journal", "sync.sqlite-wal", "sync.sqlite-shm"} {
		if !privateOrAbsent(filepath.Join(abs, name)) {
			return nil, failure()
		}
	}
	db, err := openPrivateSQLite(filepath.Join(abs, "sync.sqlite"), 5*time.Second)
	if err != nil {
		return nil, err
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version > syncStateVersion {
		db.Close()
		return nil, failure()
	}
	if version == 0 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, failure()
		}
		if _, e = tx.Exec(syncSchema); e != nil {
			_ = tx.Rollback()
			db.Close()
			return nil, failure()
		}
		if tx.Commit() != nil {
			db.Close()
			return nil, failure()
		}
	}
	return &SyncState{db: db}, nil
}

func (s *SyncState) Close() error { return s.db.Close() }

func validSyncPath(p string) bool {
	if len(p) > 4096 || strings.ContainsAny(p, "\x00\\") || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	if p == "" {
		return true
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// PutPair inserts or replaces one pair document.
func (s *SyncState) PutPair(id string, pair any) error {
	if !validCatalogID(id) {
		return failure()
	}
	b, err := encodePayload(pair)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec("INSERT INTO sync_pairs(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", id, b); err != nil {
		return failure()
	}
	return nil
}

// Pairs returns every pair document.
func (s *SyncState) Pairs() ([]json.RawMessage, error) {
	rows, err := s.db.Query("SELECT payload FROM sync_pairs ORDER BY id")
	if err != nil {
		return nil, failure()
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil || len(raw) > MaxCatalogPayload || !json.Valid(raw) {
			return nil, failure()
		}
		out = append(out, json.RawMessage(raw))
	}
	if rows.Err() != nil {
		return nil, failure()
	}
	return out, nil
}

// DeletePair removes the pair and all of its local sync state. Files on either
// side are not affected.
func (s *SyncState) DeletePair(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return failure()
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM sync_pairs WHERE id=?", "DELETE FROM sync_entries WHERE pair_id=?", "DELETE FROM sync_intents WHERE pair_id=?", "DELETE FROM sync_activity WHERE pair_id=?"} {
		if _, err = tx.Exec(q, id); err != nil {
			return failure()
		}
	}
	if tx.Commit() != nil {
		return failure()
	}
	return nil
}

// Entries returns the base state of a pair keyed by path.
func (s *SyncState) Entries(pairID string) (map[string]SyncEntry, error) {
	rows, err := s.db.Query("SELECT payload FROM sync_entries WHERE pair_id=?", pairID)
	if err != nil {
		return nil, failure()
	}
	defer rows.Close()
	out := map[string]SyncEntry{}
	for rows.Next() {
		var raw []byte
		var e SyncEntry
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &e) != nil || !validSyncPath(e.Path) {
			return nil, failure()
		}
		out[e.Path] = e
	}
	if rows.Err() != nil {
		return nil, failure()
	}
	return out, nil
}

// PutEntry records that path is synchronized on both sides.
func (s *SyncState) PutEntry(pairID string, e SyncEntry) error {
	if !validCatalogID(pairID) || !validSyncPath(e.Path) || (e.Kind != "file" && e.Kind != "dir") {
		return failure()
	}
	b, err := encodePayload(e)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec("INSERT INTO sync_entries(pair_id,path,payload) VALUES(?,?,?) ON CONFLICT(pair_id,path) DO UPDATE SET payload=excluded.payload", pairID, e.Path, b); err != nil {
		return failure()
	}
	return nil
}

// DeleteEntry forgets one path; DeleteTree forgets a directory and everything below it.
func (s *SyncState) DeleteEntry(pairID, path string) error {
	if _, err := s.db.Exec("DELETE FROM sync_entries WHERE pair_id=? AND path=?", pairID, path); err != nil {
		return failure()
	}
	return nil
}

func (s *SyncState) DeleteTree(pairID, dir string) error {
	if dir == "" {
		_, err := s.db.Exec("DELETE FROM sync_entries WHERE pair_id=?", pairID)
		if err != nil {
			return failure()
		}
		return nil
	}
	// Binary text comparison: every path below dir sorts between "dir/" and
	// "dir0" ('0' follows '/'). LIKE would match other letter cases.
	if _, err := s.db.Exec("DELETE FROM sync_entries WHERE pair_id=? AND (path=? OR (path>? AND path<?))", pairID, dir, dir+"/", dir+"0"); err != nil {
		return failure()
	}
	return nil
}

// PutIntent, Intents and DeleteIntent manage pending creations.
func (s *SyncState) PutIntent(pairID string, i SyncIntent) error {
	if !validCatalogID(pairID) || !validSyncPath(i.Path) || i.Path == "" {
		return failure()
	}
	b, err := encodePayload(i)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec("INSERT INTO sync_intents(pair_id,path,payload) VALUES(?,?,?) ON CONFLICT(pair_id,path) DO UPDATE SET payload=excluded.payload", pairID, i.Path, b); err != nil {
		return failure()
	}
	return nil
}

func (s *SyncState) Intents(pairID string) (map[string]SyncIntent, error) {
	rows, err := s.db.Query("SELECT payload FROM sync_intents WHERE pair_id=?", pairID)
	if err != nil {
		return nil, failure()
	}
	defer rows.Close()
	out := map[string]SyncIntent{}
	for rows.Next() {
		var raw []byte
		var i SyncIntent
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &i) != nil || !validSyncPath(i.Path) {
			return nil, failure()
		}
		out[i.Path] = i
	}
	if rows.Err() != nil {
		return nil, failure()
	}
	return out, nil
}

func (s *SyncState) DeleteIntent(pairID, path string) error {
	if _, err := s.db.Exec("DELETE FROM sync_intents WHERE pair_id=? AND path=?", pairID, path); err != nil {
		return failure()
	}
	return nil
}

// AddActivity appends one event and trims the pair's history.
func (s *SyncState) AddActivity(pairID string, a SyncActivity) error {
	if !validCatalogID(pairID) {
		return failure()
	}
	if a.At == "" {
		a.At = time.Now().UTC().Format(time.RFC3339)
	}
	b, err := encodePayload(a)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return failure()
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO sync_activity(pair_id,payload) VALUES(?,?)", pairID, b); err != nil {
		return failure()
	}
	if _, err = tx.Exec("DELETE FROM sync_activity WHERE pair_id=? AND seq NOT IN (SELECT seq FROM sync_activity WHERE pair_id=? ORDER BY seq DESC LIMIT ?)", pairID, pairID, ActivityPerPair); err != nil {
		return failure()
	}
	if tx.Commit() != nil {
		return failure()
	}
	return nil
}

// Activity returns recent events, newest first. An empty pair returns all pairs.
func (s *SyncState) Activity(pairID string, limit int) ([]SyncActivity, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query, args := "SELECT payload FROM sync_activity ORDER BY seq DESC LIMIT ?", []any{limit}
	if pairID != "" {
		query, args = "SELECT payload FROM sync_activity WHERE pair_id=? ORDER BY seq DESC LIMIT ?", []any{pairID, limit}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, failure()
	}
	defer rows.Close()
	out := []SyncActivity{}
	for rows.Next() {
		var raw []byte
		var a SyncActivity
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &a) != nil {
			return nil, failure()
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		return nil, failure()
	}
	return out, nil
}

// Cursor and SetCursor keep the Drive change token of each account.
func (s *SyncState) Cursor(account string) (string, error) {
	var token string
	err := s.db.QueryRow("SELECT token FROM sync_cursors WHERE account=?", account).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", failure()
	}
	return token, nil
}

func (s *SyncState) SetCursor(account, token string) error {
	if account == "" || len(account) > 256 || len(token) > 8192 {
		return failure()
	}
	if _, err := s.db.Exec("INSERT INTO sync_cursors(account,token) VALUES(?,?) ON CONFLICT(account) DO UPDATE SET token=excluded.token", account, token); err != nil {
		return failure()
	}
	return nil
}
