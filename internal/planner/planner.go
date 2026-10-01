// Package planner constructs sealed offline plan envelopes. It has no executor.
package planner

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
)

func Build(c config.Config, sourceIdentity, rulesDigest string, entries []domain.Entry, remote domain.Inventory, now time.Time) (domain.Plan, error) {
	return BuildContext(context.Background(), c, sourceIdentity, rulesDigest, entries, remote, now)
}

func BuildContext(ctx context.Context, c config.Config, sourceIdentity, rulesDigest string, entries []domain.Entry, remote domain.Inventory, now time.Time) (domain.Plan, error) {
	p := domain.Plan{SchemaVersion: "1.0", ProjectID: c.Project.ID, CreatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.Add(15 * time.Minute).UTC().Format(time.RFC3339Nano), Mode: "copy", SourceIdentity: sourceIdentity, DestinationIdentity: remote.Identity, RulesDigest: rulesDigest, Operations: []domain.Operation{}, Risks: []string{"Offline simulation using a fake destination. This plan cannot be applied.", "Google Drive authentication, transfer, durable state, recovery, and scheduling are not implemented."}, ScanComplete: domain.ScanComplete{Source: true, Destination: remote.Complete}}
	if err := c.Validate(); err != nil {
		return p, err
	}
	if c.Sync.Mode != "copy" || c.Sync.OverwritePolicy != "deny" || c.Automation.Enabled {
		return p, domain.Fail("CAPABILITY_UNSUPPORTED", "offline preview supports manual copy with overwritePolicy deny only")
	}
	if !remote.Complete || remote.Identity == "" {
		return p, domain.Fail("SCAN_INCOMPLETE", "destination inventory is incomplete")
	}
	if !strings.HasPrefix(remote.Identity, "fake:") {
		return p, domain.Fail("CAPABILITY_UNSUPPORTED", "only fake destinations are supported")
	}
	var err error
	p.ConfigDigest, err = domain.Digest(c)
	if err != nil {
		return p, err
	}
	local := append([]domain.Entry{}, entries...)
	sort.Slice(local, func(i, j int) bool { return local[i].Path < local[j].Path })
	objects := append([]domain.RemoteEntry{}, remote.Entries...)
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Path == objects[j].Path {
			return objects[i].ObjectID < objects[j].ObjectID
		}
		return objects[i].Path < objects[j].Path
	})
	p.InventoryDigest, err = domain.Digest(struct {
		Source      []domain.Entry       `json:"source"`
		Destination []domain.RemoteEntry `json:"destination"`
	}{local, objects})
	if err != nil {
		return p, err
	}
	p.PlanID = "offline-" + domain.HashBytes([]byte(p.ConfigDigest + rulesDigest + p.InventoryDigest + p.CreatedAt))[:24]
	byPath := map[string][]domain.RemoteEntry{}
	ids := map[string]bool{}
	for _, r := range objects {
		if ctx.Err() != nil {
			return p, domain.Fail("CANCELLED", "planning cancelled")
		}
		if err := domain.ValidatePath(r.Path); err != nil {
			return p, err
		}
		if r.ObjectID == "" || ids[r.ObjectID] {
			return p, domain.Fail("AMBIGUOUS_DESTINATION", "missing or repeated fake object identity")
		}
		ids[r.ObjectID] = true
		if len(byPath[r.Path]) > 0 {
			return p, domain.Fail("AMBIGUOUS_DESTINATION", "duplicate destination display path: %s", r.Path)
		}
		byPath[r.Path] = append(byPath[r.Path], r)
	}
	opID := func(kind, p string) string { return "op-" + domain.HashBytes([]byte(kind + "\x00" + p))[:24] }
	created := map[string]string{}
	blocked := map[string]bool{}
	localPaths := map[string]bool{}
	// Append explicitly below so the returned envelope owns all operation slices.
	appendOp := func(kind, relative, reason string, r *domain.RemoteEntry, deps []string, size int64, digest string) {
		op := domain.Operation{OperationID: opID(kind, relative), Type: kind, RelativePath: relative, DependsOn: append([]string{}, deps...), ExpectedSize: size, Explanation: reason}
		if r != nil {
			id, version := r.ObjectID, r.Version
			op.ObjectID = &id
			op.ObservedRemoteVersion = &version
		}
		if digest != "" {
			d := digest
			op.SourceDigest = &d
		}
		p.Operations = append(p.Operations, op)
		if kind == "upload-new" {
			p.Summary.UploadBytes += size
		}
	}
	var ensureDir func(string) bool
	ensureDir = func(dir string) bool {
		if dir == "." || dir == "" {
			return true
		}
		if blocked[dir] {
			return false
		}
		if _, ok := created[dir]; ok {
			return true
		}
		if !ensureDir(path.Dir(dir)) {
			blocked[dir] = true
			return false
		}
		if r := byPath[dir]; len(r) > 0 {
			if len(r) == 1 && r[0].Kind == "directory" && r[0].Managed {
				created[dir] = ""
				return true
			}
			appendOp("conflict", dir, "Destination directory identity or ownership is ambiguous; preserve it.", nil, nil, 0, "")
			blocked[dir] = true
			return false
		}
		deps := []string{}
		if parent := created[path.Dir(dir)]; parent != "" {
			deps = append(deps, parent)
		}
		appendOp("create-directory", dir, "Container required by selected content; fake preview only.", nil, deps, 0, "")
		created[dir] = opID("create-directory", dir)
		return true
	}
	for _, e := range local {
		if ctx.Err() != nil {
			return p, domain.Fail("CANCELLED", "planning cancelled")
		}
		if err := domain.ValidatePath(e.Path); err != nil {
			return p, err
		}
		if localPaths[e.Path] {
			return p, domain.Fail("SCAN_INCOMPLETE", "duplicate source path")
		}
		localPaths[e.Path] = true
		if e.Decision == "exclude" {
			appendOp("skip", e.Path, "Excluded by policy; any destination object is preserved.", nil, nil, 0, "")
			continue
		}
		if e.Decision != "include" {
			return p, domain.Fail("SCAN_INCOMPLETE", "source entry has no valid selection decision")
		}
		if e.Kind == "directory" {
			continue
		}
		if e.Kind != "file" || len(e.SHA256) != 64 {
			return p, domain.Fail("SCAN_INCOMPLETE", "selected source file has no complete content fingerprint")
		}
		if !ensureDir(path.Dir(e.Path)) {
			appendOp("conflict", e.Path, "Parent destination conflict blocks this file.", nil, nil, e.Size, e.SHA256)
			continue
		}
		r := byPath[e.Path]
		if len(r) > 0 {
			if len(r) == 1 && r[0].Managed && r[0].Kind == "file" && r[0].VerifiedSHA256 == e.SHA256 {
				appendOp("skip", e.Path, "Known managed fake object has the same verified SHA-256.", &r[0], nil, e.Size, e.SHA256)
			} else {
				appendOp("conflict", e.Path, "Name collision is not identity; preserve existing content and review.", nil, nil, e.Size, e.SHA256)
			}
			continue
		}
		deps := []string{}
		if parent := created[path.Dir(e.Path)]; parent != "" {
			deps = append(deps, parent)
		}
		appendOp("upload-new", e.Path, "Selected new file; fake preview only.", nil, deps, e.Size, e.SHA256)
	}
	for _, r := range objects {
		if !localPaths[r.Path] {
			appendOp("skip", r.Path, "Remote-only object is preserved; absence never implies deletion.", &r, nil, 0, "")
		}
	}
	p.Summary.OperationCount = len(p.Operations)
	p.PlanDigest, err = p.ComputeDigest()
	if ctx.Err() != nil {
		return domain.Plan{}, domain.Fail("CANCELLED", "planning cancelled")
	}
	return p, err
}
