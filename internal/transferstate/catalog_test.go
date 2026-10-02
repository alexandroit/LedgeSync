package transferstate

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCatalogStoresBoundedPrivateDocuments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	c, err := OpenCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.PutProject("p1", map[string]string{"name": "one"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if ok, err := c.Project("p1", &got); !ok || err != nil || got["name"] != "one" {
		t.Fatalf("project round trip: %v %v %v", ok, err, got)
	}
	if err = c.PutProject("../escape", map[string]string{}); err == nil {
		t.Fatal("unsafe identifier accepted")
	}
	if err = c.PutProject("big", strings.Repeat("x", MaxCatalogPayload)); err == nil {
		t.Fatal("oversized payload accepted")
	}
	start := time.Now()
	for i := 0; i < HistoryPerProject+5; i++ {
		if err = c.RecordRun("run"+strings.Repeat("0", 3)+string(rune('a'+i%26))+time.Duration(i).String(), "p1", start.Add(time.Duration(i)*time.Second), map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := c.Runs("p1", 1000)
	if err != nil || len(runs) != HistoryPerProject {
		t.Fatalf("history retention = %d, %v", len(runs), err)
	}
	var newest map[string]int
	if json.Unmarshal(runs[0], &newest) != nil || newest["i"] != HistoryPerProject+4 {
		t.Fatalf("history is not newest first: %s", runs[0])
	}
	if err = c.DeleteProject("p1"); err != nil {
		t.Fatal(err)
	}
	if runs, _ = c.Runs("p1", 10); len(runs) != 0 {
		t.Fatal("deleting a project kept its history")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dir, "catalog.sqlite"))
		if err != nil || st.Mode().Perm()&0o077 != 0 {
			t.Fatalf("catalog permissions = %v %v", st.Mode(), err)
		}
	}
}

func TestJournalMigrationKeepsPrivateBackupAndData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	// Create the directory exactly as the application does: Windows rejects an
	// existing directory without the protected owner-only DACL.
	if err := prepareStateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "transfers.sqlite")
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	uriPath := filepath.ToSlash(filename)
	if filepath.VolumeName(filename) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: uriPath}).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO projects(id,source_identity,account_ref,destination_id) VALUES('k','s','a','d')"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO runs(id,project_id,approved_plan,state) VALUES('r1','k','{}','succeeded')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version = %d %v", version, err)
	}
	var state string
	if err = store.db.QueryRow("SELECT state FROM runs WHERE id='r1'").Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("migrated run lost: %q %v", state, err)
	}
	if err = store.BeginRun("r2", "k", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishRun("r2", "partial", map[string]int{"skippedFiles": 1}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	backup := filepath.Join(dir, "transfers.v1-backup.sqlite")
	st, err := os.Lstat(backup)
	if err != nil {
		t.Fatal("no backup before migration")
	}
	if !privateNode(backup, st, false) {
		t.Fatal("migration backup is not private")
	}
	backupURI := filepath.ToSlash(backup)
	if filepath.VolumeName(backup) != "" && !strings.HasPrefix(backupURI, "/") {
		backupURI = "/" + backupURI
	}
	old, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: backupURI}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err = old.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("backup is not the v1 journal: %d %v", version, err)
	}
}
