package transferstate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Catalog stores saved projects, automation settings and bounded run history.
// It is separate from the transfer journal so the interface can read history
// while a transfer holds the journal's writer lock. It contains no credentials,
// tokens or upload-session URLs; source paths are local application data.
type Catalog struct {
	db *sql.DB
}

const catalogVersion = 1

// MaxCatalogPayload bounds each stored JSON document.
const MaxCatalogPayload = 1 << 20

// HistoryPerProject bounds retained run summaries for each project.
const HistoryPerProject = 200

const catalogSchema = `CREATE TABLE saved_projects (id TEXT PRIMARY KEY, payload BLOB NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE run_history (run_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, finished_at TEXT NOT NULL, payload BLOB NOT NULL);
CREATE INDEX run_history_project ON run_history(project_id, finished_at);
CREATE TABLE settings (key TEXT PRIMARY KEY, payload BLOB NOT NULL);
PRAGMA user_version=1;`

func catalogFailure() error {
	return failure()
}

// OpenCatalog opens the private catalog in dir, creating it when needed.
func OpenCatalog(dir string) (*Catalog, error) {
	abs, err := canonicalDirectory(dir)
	if err != nil || prepareStateDirectory(abs) != nil {
		return nil, catalogFailure()
	}
	if st, e := os.Lstat(abs); e != nil || !privateNode(abs, st, true) {
		return nil, catalogFailure()
	}
	for _, name := range []string{"catalog.sqlite", "catalog.sqlite-journal", "catalog.sqlite-wal", "catalog.sqlite-shm"} {
		if !privateOrAbsent(filepath.Join(abs, name)) {
			return nil, catalogFailure()
		}
	}
	db, err := openPrivateSQLite(filepath.Join(abs, "catalog.sqlite"), 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Catalog{db: db}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version > catalogVersion {
		db.Close()
		return nil, catalogFailure()
	}
	if version == 0 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, catalogFailure()
		}
		if _, e = tx.Exec(catalogSchema); e != nil {
			_ = tx.Rollback()
			db.Close()
			return nil, catalogFailure()
		}
		if tx.Commit() != nil {
			db.Close()
			return nil, catalogFailure()
		}
	}
	return c, nil
}

func (c *Catalog) Close() error { return c.db.Close() }

func encodePayload(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil || len(b) > MaxCatalogPayload {
		return nil, catalogFailure()
	}
	return b, nil
}

// PutProject inserts or replaces one saved project document.
func (c *Catalog) PutProject(id string, project any) error {
	if !validCatalogID(id) {
		return catalogFailure()
	}
	b, err := encodePayload(project)
	if err != nil {
		return err
	}
	_, err = c.db.Exec("INSERT INTO saved_projects(id,payload,updated_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at", id, b, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return catalogFailure()
	}
	return nil
}

// Project decodes one saved project into target. It reports false when absent.
func (c *Catalog) Project(id string, target any) (bool, error) {
	var raw []byte
	err := c.db.QueryRow("SELECT payload FROM saved_projects WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || len(raw) > MaxCatalogPayload || json.Unmarshal(raw, target) != nil {
		return false, catalogFailure()
	}
	return true, nil
}

// Projects returns every saved project document, most recently updated first.
func (c *Catalog) Projects() ([]json.RawMessage, error) {
	rows, err := c.db.Query("SELECT payload FROM saved_projects ORDER BY updated_at DESC, id")
	if err != nil {
		return nil, catalogFailure()
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil || len(raw) > MaxCatalogPayload || !json.Valid(raw) {
			return nil, catalogFailure()
		}
		out = append(out, json.RawMessage(raw))
		if len(out) > 10000 {
			return nil, catalogFailure()
		}
	}
	if rows.Err() != nil {
		return nil, catalogFailure()
	}
	return out, nil
}

// DeleteProject removes the saved project and its local run history. Drive
// files and the transfer journal's identity records are not affected.
func (c *Catalog) DeleteProject(id string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return catalogFailure()
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM saved_projects WHERE id=?", id); err != nil {
		return catalogFailure()
	}
	if _, err = tx.Exec("DELETE FROM run_history WHERE project_id=?", id); err != nil {
		return catalogFailure()
	}
	if tx.Commit() != nil {
		return catalogFailure()
	}
	return nil
}

// RecordRun stores a bounded run summary and trims older history for the project.
func (c *Catalog) RecordRun(runID, projectID string, finishedAt time.Time, summary any) error {
	if !validCatalogID(runID) || projectID == "" || len(projectID) > 256 {
		return catalogFailure()
	}
	b, err := encodePayload(summary)
	if err != nil {
		return err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return catalogFailure()
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO run_history(run_id,project_id,finished_at,payload) VALUES(?,?,?,?) ON CONFLICT(run_id) DO UPDATE SET payload=excluded.payload, finished_at=excluded.finished_at", runID, projectID, finishedAt.UTC().Format(time.RFC3339Nano), b); err != nil {
		return catalogFailure()
	}
	if _, err = tx.Exec("DELETE FROM run_history WHERE project_id=? AND run_id NOT IN (SELECT run_id FROM run_history WHERE project_id=? ORDER BY finished_at DESC LIMIT ?)", projectID, projectID, HistoryPerProject); err != nil {
		return catalogFailure()
	}
	if tx.Commit() != nil {
		return catalogFailure()
	}
	return nil
}

// Runs returns recent run summaries, newest first. An empty project returns all projects.
func (c *Catalog) Runs(projectID string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query, args := "SELECT payload FROM run_history ORDER BY finished_at DESC LIMIT ?", []any{limit}
	if projectID != "" {
		query, args = "SELECT payload FROM run_history WHERE project_id=? ORDER BY finished_at DESC LIMIT ?", []any{projectID, limit}
	}
	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, catalogFailure()
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil || len(raw) > MaxCatalogPayload || !json.Valid(raw) {
			return nil, catalogFailure()
		}
		out = append(out, json.RawMessage(raw))
	}
	if rows.Err() != nil {
		return nil, catalogFailure()
	}
	return out, nil
}

// ClearHistory removes local run summaries only.
func (c *Catalog) ClearHistory() error {
	if _, err := c.db.Exec("DELETE FROM run_history"); err != nil {
		return catalogFailure()
	}
	return nil
}

// PutSetting and Setting store small application preferences.
func (c *Catalog) PutSetting(key string, value any) error {
	if !validCatalogID(key) {
		return catalogFailure()
	}
	b, err := encodePayload(value)
	if err != nil {
		return err
	}
	if _, err = c.db.Exec("INSERT INTO settings(key,payload) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload", key, b); err != nil {
		return catalogFailure()
	}
	return nil
}
func (c *Catalog) Setting(key string, target any) (bool, error) {
	var raw []byte
	err := c.db.QueryRow("SELECT payload FROM settings WHERE key=?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || len(raw) > MaxCatalogPayload || json.Unmarshal(raw, target) != nil {
		return false, catalogFailure()
	}
	return true, nil
}

func validCatalogID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// privateOrAbsent accepts a missing file or a private regular file. Another
// connection's SQLite rollback journal may disappear between the existence check
// and the ACL check; a vanished file is absent, never accepted unchecked.
func privateOrAbsent(filename string) bool {
	for attempt := 0; attempt < 3; attempt++ {
		st, err := os.Lstat(filename)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil {
			return false
		}
		if privateNode(filename, st, false) {
			return true
		}
		if _, err = os.Lstat(filename); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return false
}
