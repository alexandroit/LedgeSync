// Package syncer keeps a local folder and a Google Drive folder in two-way
// sync, like Google Drive for desktop: additions, changes and deletions on
// either side reach the other. Every decision is three-way against the last
// state both sides agreed on, so a change is never mistaken for a deletion.
// Replaced Drive content stays in the file's revision history; deletions go to
// the Drive trash or to the pair's local trash folder; when both sides changed
// the same file, both versions are kept. A cycle that would delete many files
// at once pauses for confirmation.
package syncer

import (
	"path"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/projects"
)

// Destination is the Drive folder that holds the synced folder.
type Destination struct {
	FolderID string `json:"folderId"` // "root" for My Drive
	Name     string `json:"name"`
}

// Pair is one local folder kept in sync with one Drive folder.
type Pair struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	LocalRoot        string          `json:"localRoot"`
	AccountReference string          `json:"accountReference"`
	Parent           Destination     `json:"parent"`
	RemoteRootID     string          `json:"remoteRootId"`
	Policy           projects.Policy `json:"policy"`
	Paused           bool            `json:"paused"`
	PauseCode        string          `json:"pauseCode,omitempty"`
	PauseReason      string          `json:"pauseReason,omitempty"`
	ConfirmedDeletes string          `json:"confirmedDeletes,omitempty"`
	CreatedAt        string          `json:"createdAt"`
	LastSyncAt       string          `json:"lastSyncAt,omitempty"`
}

// Issue is an item that was not synced and why.
type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Status is the live state of a pair for the interface.
type Status struct {
	Pair          Pair    `json:"pair"`
	State         string  `json:"state"` // starting, syncing, synced, paused, waiting, confirm_deletes, error
	Message       string  `json:"message"`
	ErrorCode     string  `json:"errorCode,omitempty"`
	Uploads       int     `json:"uploads"`
	Downloads     int     `json:"downloads"`
	Done          int     `json:"done"`
	Total         int     `json:"total"`
	CurrentPath   string  `json:"currentPath,omitempty"`
	LocalDeletes  int     `json:"localDeletes,omitempty"`
	RemoteDeletes int     `json:"remoteDeletes,omitempty"`
	Issues        []Issue `json:"issues"`
	LastSyncAt    string  `json:"lastSyncAt,omitempty"`
	DriveURL      string  `json:"driveUrl,omitempty"`
}

// Result summarizes one sync cycle.
type Result struct {
	Uploaded      int     `json:"uploaded"`
	Downloaded    int     `json:"downloaded"`
	DeletedLocal  int     `json:"deletedLocal"`
	DeletedRemote int     `json:"deletedRemote"`
	Conflicts     int     `json:"conflicts"`
	FoldersMade   int     `json:"foldersMade"`
	Issues        []Issue `json:"issues"`
	Changed       bool    `json:"changed"`
}

const (
	// TrashDir is the pair's local trash, inside the synced folder so a move is
	// a same-volume rename. It is never synced.
	TrashDir = ".ledgesync-trash"
	// TrashRetention bounds how long files deleted by sync stay recoverable locally.
	TrashRetention = 30 * 24 * time.Hour
	tempPrefix     = ".ledgesync-tmp-"
	// Mass-deletion guard: a cycle that deletes at least deleteGuardMinimum
	// files and more than deleteGuardShare of the synced files on either side,
	// or empties one side completely, waits for explicit confirmation.
	deleteGuardMinimum = 20
	deleteGuardShare   = 0.30
	maxRemoteItems     = 500000
)

// builtinExcluded reports names that are never synced in either direction:
// the pair's own trash and temporary files, and operating-system metadata.
func builtinExcluded(name string) bool {
	switch {
	case name == TrashDir, strings.HasPrefix(name, tempPrefix):
		return true
	case name == ".DS_Store", name == "Thumbs.db", name == "desktop.ini", name == "Icon\r", name == ".localized":
		return true
	case strings.HasPrefix(name, "._"), strings.HasPrefix(name, "~$"):
		return true
	case strings.HasPrefix(name, ".~lock.") && strings.HasSuffix(name, "#"):
		return true
	}
	return false
}

// excludedPath applies builtinExcluded to every component of p.
func excludedPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if builtinExcluded(part) {
			return true
		}
	}
	return false
}

var windowsReserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

// localName reports whether a Drive name can be stored as a local file name on
// this operating system. Names that cannot are reported, never altered.
func localName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\x00") {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(name, `<>:"\|?*`) || strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
			return false
		}
		stem := strings.ToUpper(name)
		if i := strings.IndexByte(stem, '.'); i >= 0 {
			stem = stem[:i]
		}
		if windowsReserved[stem] {
			return false
		}
	}
	return true
}

// caseInsensitive reports whether this platform's default file systems treat
// names that differ only in letter case as the same file.
func caseInsensitive() bool { return runtime.GOOS == "darwin" || runtime.GOOS == "windows" }

// foldKey is the comparison key for collision checks on the local file system.
func foldKey(p string) string {
	if caseInsensitive() {
		return strings.ToLower(p)
	}
	return p
}

// conflictName returns the name for the local version kept when both sides
// changed: "report (conflict 2026-10-02 153000).txt".
func conflictName(p string, now time.Time) string {
	dir, file := path.Split(p)
	ext := path.Ext(file)
	stem := strings.TrimSuffix(file, ext)
	if stem == "" {
		stem, ext = file, ""
	}
	return dir + stem + " (conflict " + now.Format("2006-01-02 150405") + ")" + ext
}

func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func depth(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

// under reports whether p is strictly inside dir.
func under(p, dir string) bool {
	if dir == "" {
		return p != ""
	}
	return strings.HasPrefix(p, dir+"/")
}
