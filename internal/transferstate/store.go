// Package transferstate persists create-only copy intentions outside upload roots.
// It contains identifiers and hashes, never OAuth credentials or upload session URLs.
package transferstate

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/domain"
	_ "modernc.org/sqlite"
)

type Node struct {
	Path, Kind, Name, ID, ParentID, OperationID, SHA256, MD5, Status string
	Size                                                             int64
}

type Project struct {
	Key, SourceIdentity, AccountReference, DestinationID string
	Nodes                                                map[string]Node
}

type Store struct {
	db   *sql.DB
	lock *os.File
}

func DefaultDirectory() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", failure()
	}
	return filepath.Join(dir, "LedgeSync", "transfers"), nil
}

func failure() error {
	return domain.Fail("STATE_UNAVAILABLE", "The local transfer journal is unavailable. No further uploads were started.")
}

// Open acquires a kernel-held process lock. Crashes release the lock without
// stale-PID guesses. One writer for this application also blocks overlapping pairs.
func Open(dir, source string) (*Store, error) {
	abs, err := canonicalDirectory(dir)
	if err != nil {
		return nil, failure()
	}
	if source != "" {
		root, e := filepath.EvalSymlinks(source)
		if e != nil {
			return nil, failure()
		}
		// State must never be created inside a source selected for upload.
		differentVolumes := filepath.VolumeName(root) != "" && filepath.VolumeName(abs) != "" && !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(abs))
		rel, e := filepath.Rel(root, abs)
		if !differentVolumes && (e != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return nil, domain.Fail("STATE_INSIDE_SOURCE", "Choose a source outside the LedgeSync settings directory.")
		}
	}
	if prepareStateDirectory(abs) != nil {
		return nil, failure()
	}
	if st, e := os.Lstat(abs); e != nil || !privateNode(abs, st, true) {
		return nil, failure()
	}
	for _, name := range []string{"writer.lock", "transfers.sqlite", "transfers.sqlite-journal", "transfers.sqlite-wal", "transfers.sqlite-shm"} {
		if st, e := os.Lstat(filepath.Join(abs, name)); e == nil && !privateNode(filepath.Join(abs, name), st, false) {
			return nil, failure()
		} else if e != nil && !errors.Is(e, os.ErrNotExist) {
			return nil, failure()
		}
	}
	f, err := os.OpenFile(filepath.Join(abs, "writer.lock"), os.O_CREATE|os.O_RDWR|noFollowFlag, 0600)
	if err != nil {
		return nil, failure()
	}
	if st, e := f.Stat(); e != nil || !privateNode(f.Name(), st, false) {
		f.Close()
		return nil, failure()
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, domain.Fail("TRANSFER_BUSY", "Another LedgeSync process is using the transfer journal.")
	}
	s := &Store{lock: f}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	filename := filepath.Join(abs, "transfers.sqlite")
	private, e := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|noFollowFlag, 0600)
	if e != nil {
		err = failure()
		return nil, err
	}
	if st, e := private.Stat(); e != nil || !privateNode(filename, st, false) {
		private.Close()
		err = failure()
		return nil, err
	}
	private.Close()
	uriPath := filepath.ToSlash(filename)
	if filepath.VolumeName(filename) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	dsn := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{"_defensive": {"1"}, "_dqs": {"0"}, "_pragma": {"trusted_schema(OFF)"}}
	dsn.RawQuery = query.Encode()
	s.db, err = sql.Open("sqlite", dsn.String())
	if err != nil {
		err = failure()
		return nil, err
	}
	s.db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=DELETE", "PRAGMA synchronous=EXTRA", "PRAGMA busy_timeout=1000"} {
		if _, err = s.db.Exec(q); err != nil {
			err = failure()
			return nil, err
		}
	}
	var mode string
	var synchronous, foreignKeys, trusted int
	if s.db.QueryRow("PRAGMA journal_mode").Scan(&mode) != nil || mode != "delete" || s.db.QueryRow("PRAGMA synchronous").Scan(&synchronous) != nil || synchronous != 3 || s.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys) != nil || foreignKeys != 1 || s.db.QueryRow("PRAGMA trusted_schema").Scan(&trusted) != nil || trusted != 0 {
		err = failure()
		return nil, err
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version > 1 {
		err = failure()
		return nil, err
	}
	if version == 0 {
		tx, e := s.db.Begin()
		if e != nil {
			err = failure()
			return nil, err
		}
		_, e = tx.Exec(`CREATE TABLE projects (id TEXT PRIMARY KEY, source_identity TEXT NOT NULL, account_ref TEXT NOT NULL, destination_id TEXT NOT NULL);
CREATE TABLE operations (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), payload BLOB NOT NULL);
CREATE TABLE runs (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), approved_plan BLOB NOT NULL, state TEXT NOT NULL);
CREATE TABLE run_events (run_id TEXT NOT NULL REFERENCES runs(id), sequence INTEGER PRIMARY KEY AUTOINCREMENT, state TEXT NOT NULL);
CREATE TABLE rule_snapshots (source_id TEXT NOT NULL, config_digest TEXT NOT NULL, sources BLOB NOT NULL, PRIMARY KEY(source_id,config_digest));
PRAGMA user_version=1;`)
		if e != nil {
			tx.Rollback()
			err = failure()
			return nil, err
		}
		if e = tx.Commit(); e != nil {
			err = failure()
			return nil, err
		}
	}
	return s, nil
}

// Resolve existing ancestry before comparing paths. This expands Windows 8.3
// names consistently with the source, and macOS's root-owned /var and /tmp
// aliases. User-controlled state symlinks remain forbidden, including the leaf.
func canonicalDirectory(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", failure()
	}
	nearest := ""
	var missing []string
	for p := abs; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if p == abs || !trustedSystemAlias(p, info) {
					return "", failure()
				}
			} else if !info.IsDir() {
				return "", failure()
			}
			if nearest == "" {
				nearest = p
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return "", failure()
		} else if nearest == "" {
			missing = append(missing, filepath.Base(p))
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if nearest == "" {
		return "", failure()
	}
	resolved, err := filepath.EvalSymlinks(nearest)
	if err != nil {
		return "", failure()
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	return resolved, nil
}

// ObserveRules preserves the fail-closed rule-source baseline across process
// restarts and destination changes. Changing the configuration creates a new
// explicit baseline; silently removing a previously observed source does not.
func (s *Store) ObserveRules(sourceID, configDigest string, sources []string) error {
	var old []byte
	err := s.db.QueryRow("SELECT sources FROM rule_snapshots WHERE source_id=? AND config_digest=?", sourceID, configDigest).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return failure()
	}
	if err == nil {
		var previous []string
		if len(old) > 16<<20 || json.Unmarshal(old, &previous) != nil {
			return failure()
		}
		current := map[string]bool{}
		for _, p := range sources {
			current[p] = true
		}
		for _, p := range previous {
			if !current[p] {
				return domain.Fail("RULE_SOURCE_UNAVAILABLE", "A previously observed ignore-policy source disappeared. Restore it or explicitly change the project configuration before uploading.")
			}
		}
	}
	if sources == nil {
		sources = []string{}
	}
	b, err := json.Marshal(sources)
	if err != nil {
		return failure()
	}
	_, err = s.db.Exec("INSERT INTO rule_snapshots(source_id,config_digest,sources) VALUES(?,?,?) ON CONFLICT(source_id,config_digest) DO UPDATE SET sources=excluded.sources", sourceID, configDigest, b)
	if err != nil {
		return failure()
	}
	return nil
}

func (s *Store) Close() error {
	if s.db != nil {
		s.db.Close()
	}
	if s.lock != nil {
		unlockFile(s.lock)
		return s.lock.Close()
	}
	return nil
}

func (s *Store) Load(key string) (Project, error) {
	p := Project{Key: key, Nodes: map[string]Node{}}
	err := s.db.QueryRow("SELECT source_identity,account_ref,destination_id FROM projects WHERE id=?", key).Scan(&p.SourceIdentity, &p.AccountReference, &p.DestinationID)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, failure()
	}
	rows, err := s.db.Query("SELECT id,payload FROM operations WHERE project_id=?", key)
	if err != nil {
		return p, failure()
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		var n Node
		if rows.Scan(&id, &raw) != nil || len(raw) > 32768 || json.Unmarshal(raw, &n) != nil || n.OperationID != id || !validNode(n) {
			return p, failure()
		}
		p.Nodes[id] = n
		if len(p.Nodes) > 500000 {
			return p, failure()
		}
	}
	if rows.Err() != nil {
		return p, failure()
	}
	return p, nil
}

func (s *Store) SaveProject(p Project) error {
	_, err := s.db.Exec("INSERT INTO projects(id,source_identity,account_ref,destination_id) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", p.Key, p.SourceIdentity, p.AccountReference, p.DestinationID)
	if err != nil {
		return failure()
	}
	return nil
}
func (s *Store) SaveNode(key string, n Node) error {
	if !validNode(n) {
		return failure()
	}
	b, err := json.Marshal(n)
	if err != nil {
		return failure()
	}
	result, err := s.db.Exec("INSERT INTO operations(id,project_id,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload WHERE project_id=excluded.project_id", n.OperationID, key, b)
	if err != nil {
		return failure()
	}
	if rows, e := result.RowsAffected(); e != nil || rows != 1 {
		return failure()
	}
	return nil
}

func validNode(n Node) bool {
	if n.Status != "intent" && n.Status != "acknowledged" && n.Status != "verified" {
		return false
	}
	for _, id := range []string{n.ID, n.ParentID, n.OperationID} {
		if len(id) == 0 || len(id) > 256 {
			return false
		}
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				return false
			}
		}
	}
	if len(n.Name) > 1024 || !utf8.ValidString(n.Name) || domain.ValidatePath(n.Name) != nil || strings.Contains(n.Name, "/") {
		return false
	}
	if n.Path != "" && domain.ValidatePath(n.Path) != nil {
		return false
	}
	if n.Kind == "directory" {
		return n.Size == 0 && n.MD5 == "" && n.SHA256 == ""
	}
	if n.Kind != "file" || n.Path == "" || n.Size < 0 {
		return false
	}
	for _, h := range []struct {
		value string
		size  int
	}{{n.SHA256, 64}, {n.MD5, 32}} {
		if len(h.value) != h.size || strings.ToLower(h.value) != h.value {
			return false
		}
		if _, err := hex.DecodeString(h.value); err != nil {
			return false
		}
	}
	return true
}
func (s *Store) BeginRun(id, key string, plan any) error {
	b, err := json.Marshal(plan)
	if err != nil {
		return failure()
	}
	_, err = s.db.Exec("INSERT INTO runs(id,project_id,approved_plan,state) VALUES(?,?,?,'uploading')", id, key, b)
	if err != nil {
		return failure()
	}
	return nil
}
func (s *Store) FinishRun(id, state string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return failure()
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE runs SET state=? WHERE id=?", state, id); err != nil {
		return failure()
	}
	if _, err = tx.Exec("INSERT INTO run_events(run_id,state) VALUES(?,?)", id, state); err != nil {
		return failure()
	}
	if tx.Commit() != nil {
		return failure()
	}
	return nil
}
