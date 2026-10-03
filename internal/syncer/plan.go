package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

type opKind int

const (
	opAdopt        opKind = iota // both sides already match: record the base only
	opForget                     // gone on both sides: drop the base entry
	opMkdirRemote                // create the folder in Drive
	opMkdirLocal                 // create the folder on this computer
	opUpload                     // create the file in Drive
	opUpdateRemote               // store the local content as a new Drive revision
	opDownload                   // create or replace the local file
	opConflict                   // both changed: keep the local version under a new name, then download
	opTrashRemote                // move the Drive item to the Drive trash
	opTrashLocal                 // move the local item to the pair's local trash
)

type op struct {
	kind      opKind
	path      string
	dir       bool
	local     localItem
	hasLocal  bool
	remote    remoteItem
	hasRemote bool
	base      transferstate.SyncEntry
	hasBase   bool
}

type plan struct {
	ops           []op
	issues        []Issue
	localDeletes  int
	remoteDeletes int
	baseFiles     int
	localFiles    int
	remoteFiles   int
}

func localChanged(b transferstate.SyncEntry, l localItem, ok bool) bool {
	if !ok || l.Kind != b.Kind {
		return true
	}
	if l.Kind == "dir" || l.Size == b.LocalSize && l.Mtime == b.LocalMtime {
		return false
	}
	return l.MD5 == "" || l.MD5 != b.LocalMD5
}

func remoteChanged(b transferstate.SyncEntry, r remoteItem, ok bool) bool {
	if !ok || r.Kind != b.Kind || r.ID != b.RemoteID {
		return true
	}
	if r.Kind == "dir" || r.Version == b.RemoteVersion {
		return false
	}
	return r.MD5 == "" || r.MD5 != b.RemoteMD5
}

func sameContent(l localItem, r remoteItem) bool {
	return l.Kind == "file" && r.Kind == "file" && r.MD5 != "" && l.MD5 == r.MD5 && l.Size == r.Size
}

// decide computes the operations that bring both sides to the same state.
func decide(base map[string]transferstate.SyncEntry, local map[string]localItem, remote map[string]remoteItem) *plan {
	pl := &plan{}
	set := map[string]bool{}
	for p, b := range base {
		set[p] = true
		if b.Kind == "file" {
			pl.baseFiles++
		}
	}
	for p, l := range local {
		set[p] = true
		if l.Kind == "file" {
			pl.localFiles++
		}
	}
	for p, r := range remote {
		set[p] = true
		if r.Kind == "file" {
			pl.remoteFiles++
		}
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		if p != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, bOK := base[p]
		l, lOK := local[p]
		r, rOK := remote[p]
		o := op{path: p, local: l, hasLocal: lOK, remote: r, hasRemote: rOK, base: b, hasBase: bOK}
		add := func(kind opKind, dir bool) { o.kind, o.dir = kind, dir; pl.ops = append(pl.ops, o) }
		if !lOK && !rOK {
			if bOK {
				add(opForget, b.Kind == "dir")
			}
			continue
		}
		if lOK && rOK && l.Kind != r.Kind {
			pl.issues = append(pl.issues, Issue{p, "TYPE_CONFLICT", "A file and a folder have the same name on this computer and in Drive; rename one to sync it."})
			continue
		}
		if !bOK {
			switch {
			case lOK && !rOK:
				add(map[bool]opKind{true: opMkdirRemote, false: opUpload}[l.Kind == "dir"], l.Kind == "dir")
			case !lOK && rOK:
				add(map[bool]opKind{true: opMkdirLocal, false: opDownload}[r.Kind == "dir"], r.Kind == "dir")
			case l.Kind == "dir" || sameContent(l, r):
				add(opAdopt, l.Kind == "dir")
			default:
				add(opConflict, false)
			}
			continue
		}
		lc, rc := localChanged(b, l, lOK), remoteChanged(b, r, rOK)
		switch {
		case !lc && !rc:
			if lOK && rOK && (l.Kind == "file" && (l.Size != b.LocalSize || l.Mtime != b.LocalMtime) || r.Version != b.RemoteVersion) {
				add(opAdopt, l.Kind == "dir")
			}
		case lc && !rc:
			switch {
			case !lOK:
				add(opTrashRemote, b.Kind == "dir")
			case l.Kind == "file":
				add(opUpdateRemote, false)
			default:
				add(opAdopt, true)
			}
		case !lc && rc:
			switch {
			case !rOK:
				add(opTrashLocal, b.Kind == "dir")
			case r.Kind == "file":
				add(opDownload, false)
			default:
				add(opAdopt, true)
			}
		default:
			switch {
			case !lOK: // deleted here, changed in Drive: keep the Drive version
				add(map[bool]opKind{true: opMkdirLocal, false: opDownload}[r.Kind == "dir"], r.Kind == "dir")
			case !rOK: // deleted in Drive, changed here: keep the local version
				add(map[bool]opKind{true: opMkdirRemote, false: opUpload}[l.Kind == "dir"], l.Kind == "dir")
			case l.Kind == "dir" || sameContent(l, r):
				add(opAdopt, l.Kind == "dir")
			default:
				add(opConflict, false)
			}
		}
	}
	pl.contain(base)
	return pl
}

// contain keeps a deleted folder when the other side has new or changed
// content inside it, and lets a folder deletion cover its unchanged contents.
func (pl *plan) contain(base map[string]transferstate.SyncEntry) {
	order := make([]int, 0, len(pl.ops))
	for i, o := range pl.ops {
		if o.dir && (o.kind == opTrashRemote || o.kind == opTrashLocal) {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(a, b int) bool { return depth(pl.ops[order[a]].path) > depth(pl.ops[order[b]].path) })
	removed := map[int]bool{}
	for _, i := range order {
		o := &pl.ops[i]
		keep := map[opKind]bool{opDownload: true, opMkdirLocal: true, opConflict: true}
		convert := opMkdirLocal
		if o.kind == opTrashLocal {
			keep = map[opKind]bool{opUpload: true, opMkdirRemote: true, opUpdateRemote: true, opConflict: true}
			convert = opMkdirRemote
		}
		survivor := false
		for j, q := range pl.ops {
			if !removed[j] && under(q.path, o.path) && keep[q.kind] {
				survivor = true
				break
			}
		}
		if survivor {
			o.kind = convert
			continue
		}
		for j, q := range pl.ops {
			if j != i && under(q.path, o.path) && q.kind == o.kind {
				removed[j] = true
			}
		}
	}
	kept := pl.ops[:0]
	for i, o := range pl.ops {
		if removed[i] {
			continue
		}
		kept = append(kept, o)
		if o.kind == opTrashLocal || o.kind == opTrashRemote {
			count := 0
			if !o.dir {
				count = 1
			} else {
				for p, b := range base {
					if b.Kind == "file" && under(p, o.path) {
						count++
					}
				}
			}
			if o.kind == opTrashLocal {
				pl.localDeletes += count
			} else {
				pl.remoteDeletes += count
			}
		}
	}
	pl.ops = kept
}

// needsConfirmation applies the mass-deletion guard.
func (pl *plan) needsConfirmation() bool {
	if pl.localDeletes == 0 && pl.remoteDeletes == 0 {
		return false
	}
	share := func(n int) bool {
		return n >= deleteGuardMinimum && float64(n) > deleteGuardShare*float64(max(pl.baseFiles, 1))
	}
	emptied := pl.baseFiles > 0 && (pl.localFiles == 0 && pl.remoteDeletes > 0 || pl.remoteFiles == 0 && pl.localDeletes > 0)
	return emptied || share(pl.localDeletes) || share(pl.remoteDeletes)
}

// deleteDigest identifies the exact set of deletions a confirmation covers.
func (pl *plan) deleteDigest() string {
	var lines []string
	for _, o := range pl.ops {
		switch o.kind {
		case opTrashLocal:
			lines = append(lines, "L:"+o.path)
		case opTrashRemote:
			lines = append(lines, "R:"+o.path)
		}
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
