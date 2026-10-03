package syncer

import (
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

func fileBase(p, md5, id, version string) transferstate.SyncEntry {
	return transferstate.SyncEntry{Path: p, Kind: "file", LocalSize: 3, LocalMtime: 100, LocalMD5: md5, RemoteID: id, RemoteMD5: md5, RemoteVersion: version}
}

func kinds(pl *plan) map[string]opKind {
	out := map[string]opKind{}
	for _, o := range pl.ops {
		out[o.path] = o.kind
	}
	return out
}

func TestDecideCoversEveryThreeWayCase(t *testing.T) {
	base := map[string]transferstate.SyncEntry{
		"same":          fileBase("same", "m1", "id1", "1"),
		"touched":       fileBase("touched", "m2", "id2", "1"),
		"local-edit":    fileBase("local-edit", "m3", "id3", "1"),
		"remote-edit":   fileBase("remote-edit", "m4", "id4", "1"),
		"both-edit":     fileBase("both-edit", "m5", "id5", "1"),
		"both-same":     fileBase("both-same", "m6", "id6", "1"),
		"local-del":     fileBase("local-del", "m7", "id7", "1"),
		"remote-del":    fileBase("remote-del", "m8", "id8", "1"),
		"del-vs-edit":   fileBase("del-vs-edit", "m9", "id9", "1"),
		"edit-vs-del":   fileBase("edit-vs-del", "ma", "ida", "1"),
		"gone-both":     fileBase("gone-both", "mb", "idb", "1"),
		"meta-only":     fileBase("meta-only", "mc", "idc", "1"),
		"replaced-name": fileBase("replaced-name", "md", "idd", "1"),
	}
	unchanged := func(md5 string) localItem { return localItem{Kind: "file", Size: 3, Mtime: 100, MD5: md5} }
	remoteOf := func(id, md5, version string) remoteItem {
		return remoteItem{ID: id, Kind: "file", Size: 3, MD5: md5, Version: version}
	}
	local := map[string]localItem{
		"same":          unchanged("m1"),
		"touched":       {Kind: "file", Size: 3, Mtime: 999, MD5: "m2"},
		"local-edit":    {Kind: "file", Size: 4, Mtime: 200, MD5: "new3"},
		"remote-edit":   unchanged("m4"),
		"both-edit":     {Kind: "file", Size: 4, Mtime: 200, MD5: "L5"},
		"both-same":     {Kind: "file", Size: 3, Mtime: 200, MD5: "S6"},
		"remote-del":    unchanged("m8"),
		"edit-vs-del":   {Kind: "file", Size: 4, Mtime: 200, MD5: "Lx"},
		"meta-only":     unchanged("mc"),
		"replaced-name": unchanged("md"),
		"new-local":     {Kind: "file", Size: 3, MD5: "n1"},
		"new-both-same": {Kind: "file", Size: 3, MD5: "nb"},
		"new-both-diff": {Kind: "file", Size: 3, MD5: "nL"},
		"new-dir":       {Kind: "dir"},
	}
	remote := map[string]remoteItem{
		"same":           remoteOf("id1", "m1", "1"),
		"touched":        remoteOf("id2", "m2", "1"),
		"local-edit":     remoteOf("id3", "m3", "1"),
		"remote-edit":    remoteOf("id4", "R4", "2"),
		"both-edit":      remoteOf("id5", "R5", "2"),
		"both-same":      remoteOf("id6", "S6", "2"),
		"local-del":      remoteOf("id7", "m7", "1"),
		"del-vs-edit":    remoteOf("id9", "R9", "2"),
		"meta-only":      remoteOf("idc", "mc", "5"),
		"replaced-name":  remoteOf("other", "Rd", "1"),
		"new-remote":     remoteOf("r1", "r1", "1"),
		"new-both-same":  remoteOf("r2", "nb", "1"),
		"new-both-diff":  remoteOf("r3", "nR", "1"),
		"new-remote-dir": {ID: "rd", Kind: "dir"},
	}
	got := kinds(decide(base, local, remote))
	want := map[string]opKind{
		"touched":        opAdopt,
		"local-edit":     opUpdateRemote,
		"remote-edit":    opDownload,
		"both-edit":      opConflict,
		"both-same":      opAdopt,
		"local-del":      opTrashRemote,
		"remote-del":     opTrashLocal,
		"del-vs-edit":    opDownload,
		"edit-vs-del":    opUpload,
		"gone-both":      opForget,
		"meta-only":      opAdopt,
		"replaced-name":  opDownload,
		"new-local":      opUpload,
		"new-remote":     opDownload,
		"new-both-same":  opAdopt,
		"new-both-diff":  opConflict,
		"new-dir":        opMkdirRemote,
		"new-remote-dir": opMkdirLocal,
	}
	for p, k := range want {
		if got[p] != k {
			t.Errorf("%s: got %v, want %v", p, got[p], k)
		}
	}
	if _, ok := got["same"]; ok {
		t.Errorf("unchanged file produced an operation: %v", got["same"])
	}
}

func TestDeletedFolderKeepsNewContentFromTheOtherSide(t *testing.T) {
	base := map[string]transferstate.SyncEntry{
		"D":     {Path: "D", Kind: "dir", RemoteID: "d"},
		"D/old": fileBase("D/old", "m1", "o1", "1"),
	}
	// Deleted locally; Drive gained D/new meanwhile.
	remote := map[string]remoteItem{
		"D":     {ID: "d", Kind: "dir"},
		"D/old": {ID: "o1", Kind: "file", Size: 3, MD5: "m1", Version: "1"},
		"D/new": {ID: "n1", Kind: "file", Size: 3, MD5: "nn", Version: "1"},
	}
	got := kinds(decide(base, map[string]localItem{}, remote))
	if got["D"] != opMkdirLocal || got["D/old"] != opTrashRemote || got["D/new"] != opDownload {
		t.Fatalf("local deletion with new Drive content: %v", got)
	}
	// Deleted in Drive; this computer gained D/new meanwhile.
	local := map[string]localItem{
		"D":     {Kind: "dir"},
		"D/old": {Kind: "file", Size: 3, Mtime: 100, MD5: "m1"},
		"D/new": {Kind: "file", Size: 3, MD5: "nn"},
	}
	got = kinds(decide(base, local, map[string]remoteItem{}))
	if got["D"] != opMkdirRemote || got["D/old"] != opTrashLocal || got["D/new"] != opUpload {
		t.Fatalf("Drive deletion with new local content: %v", got)
	}
}

func TestDeletedFolderCoversItsUnchangedContents(t *testing.T) {
	base := map[string]transferstate.SyncEntry{
		"D":     {Path: "D", Kind: "dir", RemoteID: "d"},
		"D/a":   fileBase("D/a", "m1", "a1", "1"),
		"D/E":   {Path: "D/E", Kind: "dir", RemoteID: "e"},
		"D/E/b": fileBase("D/E/b", "m2", "b1", "1"),
	}
	remote := map[string]remoteItem{
		"D":     {ID: "d", Kind: "dir"},
		"D/a":   {ID: "a1", Kind: "file", Size: 3, MD5: "m1", Version: "1"},
		"D/E":   {ID: "e", Kind: "dir"},
		"D/E/b": {ID: "b1", Kind: "file", Size: 3, MD5: "m2", Version: "1"},
	}
	pl := decide(base, map[string]localItem{}, remote)
	if len(pl.ops) != 1 || pl.ops[0].path != "D" || pl.ops[0].kind != opTrashRemote || pl.remoteDeletes != 2 {
		t.Fatalf("folder deletion should be one Drive trash covering 2 files: %+v deletes=%d", pl.ops, pl.remoteDeletes)
	}
}

func TestMassDeletionGuard(t *testing.T) {
	base := map[string]transferstate.SyncEntry{}
	remote := map[string]remoteItem{}
	local := map[string]localItem{}
	for i := 0; i < 40; i++ {
		p := "f" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		base[p] = fileBase(p, "m", "id"+p, "1")
		remote[p] = remoteItem{ID: "id" + p, Kind: "file", Size: 3, MD5: "m", Version: "1"}
		if i >= 25 { // 25 of 40 deleted locally
			continue
		}
		local[p] = localItem{Kind: "file", Size: 3, Mtime: 100, MD5: "m"}
	}
	for p := range base {
		if _, ok := local[p]; !ok {
			delete(local, p)
		}
	}
	// 15 deleted locally: below the minimum.
	pl := decide(base, local, remote)
	if pl.remoteDeletes != 15 || pl.needsConfirmation() {
		t.Fatalf("15 of 40 deletions must not need confirmation: %d %v", pl.remoteDeletes, pl.needsConfirmation())
	}
	// Everything deleted locally (for example an unmounted folder replaced by an empty one).
	pl = decide(base, map[string]localItem{}, remote)
	if !pl.needsConfirmation() || pl.remoteDeletes != 40 {
		t.Fatalf("an emptied side must need confirmation: %d", pl.remoteDeletes)
	}
	if pl.deleteDigest() == decide(base, local, remote).deleteDigest() {
		t.Fatal("different deletion sets must have different digests")
	}
}

func TestConflictNameKeepsExtension(t *testing.T) {
	now := time.Date(2026, 10, 2, 15, 30, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"report.txt":      "report (conflict 2026-10-02 153000).txt",
		"dir/archive.tar": "dir/archive (conflict 2026-10-02 153000).tar",
		".env":            ".env (conflict 2026-10-02 153000)",
		"Makefile":        "Makefile (conflict 2026-10-02 153000)",
	} {
		if got := conflictName(in, now); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestBuiltinExclusionsAndLocalNames(t *testing.T) {
	for _, p := range []string{".DS_Store", "a/._x", "a/" + TrashDir + "/f", tempPrefix + "1", "~$doc.docx", ".~lock.a.odt#", "Thumbs.db"} {
		if !excludedPath(p) {
			t.Errorf("%s should never sync", p)
		}
	}
	for _, p := range []string{"a.txt", ".gitignore", "dir/file", "x~$"} {
		if excludedPath(p) {
			t.Errorf("%s should sync", p)
		}
	}
	for _, n := range []string{"", ".", "..", "a/b", "a\x00b", "a\nb"} {
		if localName(n) {
			t.Errorf("%q must be rejected", n)
		}
	}
}
