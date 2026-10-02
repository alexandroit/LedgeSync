package transfer

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
	"github.com/alexandroit/LedgeSync/internal/providers/drive"
	"github.com/alexandroit/LedgeSync/internal/transferstate"
)

const folderMIME = "application/vnd.google-apps.folder"

func sameParents(a, b []string) bool {
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}
func (s *Service) checkDestination(ctx context.Context, d Destination) error {
	f, err := s.provider.GetFolder(ctx, d.AccountReference, d.ID)
	if err != nil {
		return err
	}
	if f.ID != d.ID || f.Name != d.Name || !f.CanAddChildren || !sameParents(f.Parents, d.Parents) {
		return domain.Fail("DESTINATION_CHANGED", "The Drive destination changed or is no longer writable. Select it again and create a new preview.")
	}
	return nil
}
func verify(n transferstate.Node, o drive.Object) error {
	if o.ID != n.ID || o.Name != n.Name || o.Trashed || len(o.Parents) != 1 || o.Parents[0] != n.ParentID || o.AppProperties["ledgesyncOperation"] != n.OperationID {
		return domain.Fail("REMOTE_CHANGED", "A recorded Drive object changed. Review the destination before continuing.")
	}
	if n.Kind == "directory" {
		if o.MimeType != folderMIME {
			return domain.Fail("REMOTE_CHANGED", "A recorded Drive folder changed type.")
		}
	} else if o.MimeType == folderMIME || o.Size != n.Size || n.MD5 == "" || o.MD5 != n.MD5 {
		return domain.Fail("REMOTE_CHANGED", "A Drive file does not match the approved content checksum and size.")
	}
	return nil
}

// guard rechecks rule contents and configuration before every externally visible
// mutation. The pinned tree also catches new/deleted paths and identity changes.
func guard(ctx context.Context, a *approved, tree *discovery.Tree) error {
	if ctx.Err() != nil {
		return domain.Fail("CANCELLED", "Upload cancelled.")
	}
	if tree.Identity() != a.Preview.Plan.SourceIdentity {
		return domain.Fail("SOURCE_CHANGED", "The local root identity changed.")
	}
	if err := tree.RevalidateStructure(); err != nil {
		return err
	}
	c := config.Default(a.Preview.SourceRoot)
	if a.IsConfig {
		var err error
		c, err = config.Load(a.Source)
		if err != nil {
			return err
		}
	}
	digest, err := domain.Digest(c)
	if err != nil {
		return err
	}
	if digest != a.Preview.Plan.ConfigDigest {
		return domain.Fail("CONFIG_CHANGED", "The project configuration changed; approve a new preview.")
	}
	snapshot, err := policy.Resolve(ctx, c, tree)
	if err != nil {
		return err
	}
	if snapshot.Digest != a.Preview.Plan.RulesDigest {
		return domain.Fail("RULES_CHANGED", "Ignore rules changed; approve a new preview.")
	}
	return nil
}

func (s *Service) execute(ctx context.Context, a *approved) (result error) {
	st, err := transferstate.Open(s.stateDir, a.Preview.SourceRoot)
	if err != nil {
		return err
	}
	defer st.Close()
	state, err := st.Load(a.Key)
	if err != nil {
		return err
	}
	digest, _ := domain.Digest(state)
	if digest != a.StateDigest {
		return domain.Fail("PLAN_STALE", "Transfer history changed after preview. Create a new preview.")
	}
	if _, err = s.account(ctx, a.Destination.AccountReference); err != nil {
		return err
	}
	if err = s.checkDestination(ctx, a.Destination); err != nil {
		return err
	}
	current, err := s.scan(ctx, a.Source, a.IsConfig)
	if err != nil {
		return err
	}
	if err = st.ObserveRules(current.Plan.SourceIdentity, current.Plan.ConfigDigest, current.RuleSources); err != nil {
		return err
	}
	fp, err := fingerprint(current)
	if err != nil {
		return err
	}
	if fp != a.Fingerprint {
		return domain.Fail("SOURCE_CHANGED", "The source or ignore rules changed after preview. Approve a new preview.")
	}
	remote := map[string]drive.Object{}
	for _, n := range a.Nodes {
		if n.ID == "" {
			continue
		}
		o, e := s.provider.GetObject(ctx, a.Destination.AccountReference, n.ID)
		if e != nil {
			if domain.ErrorCode(e) != "DRIVE_NOT_FOUND" || n.Status == "verified" {
				return e
			}
			continue
		}
		if err = verify(n, o); err != nil {
			return err
		}
		remote[n.OperationID] = o
	}
	digest, _ = domain.Digest(remote)
	if digest != a.RemoteDigest {
		return domain.Fail("PLAN_STALE", "Drive changed after preview. Create a new preview to reconcile the transfer.")
	}
	tree, err := discovery.Scan(ctx, current.SourceRoot)
	if err != nil {
		return err
	}
	defer tree.Close()
	if err = guard(ctx, a, tree); err != nil {
		return err
	}
	state.SourceIdentity, state.AccountReference, state.DestinationID = a.Preview.Plan.SourceIdentity, a.Destination.AccountReference, a.Destination.ID
	if err = st.SaveProject(state); err != nil {
		return err
	}
	run := runID()
	if err = st.BeginRun(run, a.Key, a); err != nil {
		return err
	}
	defer func() {
		outcome := "succeeded"
		if result != nil {
			outcome = "needs_review"
			if ctx.Err() != nil {
				outcome = "cancelled"
			}
		}
		if e := st.FinishRun(run, outcome); e != nil {
			result = e
		}
	}()
	completed := map[string]transferstate.Node{}
	for _, planned := range a.Nodes {
		n := planned
		if err = guard(ctx, a, tree); err != nil {
			return err
		}
		if err = s.checkDestination(ctx, a.Destination); err != nil {
			return err
		}
		parent := a.Destination.ID
		if n.Path != "" {
			ancestor := path.Dir(n.Path)
			if ancestor == "." {
				ancestor = ""
			}
			parentNode, ok := completed[ancestor]
			if !ok {
				return domain.Fail("PLAN_INVALID", "An included entry has no approved parent folder.")
			}
			parent = parentNode.ID
			for {
				an, ok := completed[ancestor]
				if !ok {
					return domain.Fail("PLAN_INVALID", "Missing parent folder in upload plan.")
				}
				o, e := s.provider.GetObject(ctx, a.Destination.AccountReference, an.ID)
				if e != nil {
					return e
				}
				if e = verify(an, o); e != nil {
					return e
				}
				if ancestor == "" {
					break
				}
				ancestor = path.Dir(ancestor)
				if ancestor == "." {
					ancestor = ""
				}
			}
		}
		if n.ParentID != "" && n.ParentID != parent {
			return domain.Fail("REMOTE_CHANGED", "The managed destination hierarchy no longer matches the approved plan.")
		}
		n.ParentID = parent
		s.change(func(v *Status) {
			v.CurrentPath = n.Path
			v.Message = "Uploading and verifying the approved folder contents."
			v.State = "uploading"
		})
		// Reconcile every saved ID immediately before a retry. A verified mapping
		// that disappears is never interpreted as permission to recreate it.
		var existing *drive.Object
		if n.ID != "" {
			o, e := s.provider.GetObject(ctx, a.Destination.AccountReference, n.ID)
			if e == nil {
				if e = verify(n, o); e != nil {
					return e
				}
				existing = &o
			} else if domain.ErrorCode(e) != "DRIVE_NOT_FOUND" || n.Status == "verified" {
				return e
			}
		}
		if existing == nil {
			var source *discovery.UploadFile
			if n.Kind == "file" {
				source, err = tree.OpenUpload(ctx, n.Path, n.SHA256)
				if err != nil {
					return err
				}
				n.MD5 = source.MD5
			}
			if n.ID == "" {
				ids, e := s.provider.GenerateIDs(ctx, a.Destination.AccountReference, 1)
				if e != nil {
					if source != nil {
						source.Close()
					}
					return e
				}
				if len(ids) != 1 || ids[0] == "" {
					if source != nil {
						source.Close()
					}
					return domain.Fail("DRIVE_INVALID_RESPONSE", "Drive could not reserve an upload identity.")
				}
				n.ID = ids[0]
			}
			n.Status = "intent"
			if err = st.SaveNode(a.Key, n); err != nil {
				if source != nil {
					source.Close()
				}
				return err
			}
			var o drive.Object
			if source != nil {
				o, err = s.provider.Upload(ctx, a.Destination.AccountReference, n.ID, n.ParentID, n.Name, n.OperationID, source, n.Size, n.MD5)
				if err == nil {
					err = source.Verify()
				}
				source.Close()
			} else {
				o, err = s.provider.CreateFolder(ctx, a.Destination.AccountReference, n.ID, n.ParentID, n.Name, n.OperationID)
			}
			if err != nil {
				return err
			}
			if err = verify(n, o); err != nil {
				return err
			}
			// Persist the acknowledgement separately: a crash before verification
			// leaves an explicit reconciliation obligation, never a blind recreate.
			n.Status = "acknowledged"
			if err = st.SaveNode(a.Key, n); err != nil {
				return err
			}
		}
		s.change(func(v *Status) {
			v.State = "verifying"
			v.Message = "Verifying the Drive object by identity and content checksum."
		})
		o, err := s.provider.GetObject(ctx, a.Destination.AccountReference, n.ID)
		if err != nil {
			return err
		}
		if err = verify(n, o); err != nil {
			return err
		}
		if err = guard(ctx, a, tree); err != nil {
			return err
		}
		n.Status = "verified"
		if err = st.SaveNode(a.Key, n); err != nil {
			return err
		}
		completed[n.Path] = n
		s.change(func(v *Status) {
			if n.Path == "" {
				v.RemoteFolderID = n.ID
			}
			if n.Kind == "file" {
				v.CompletedFiles++
				v.UploadedBytes += n.Size
			}
		})
	}
	// A final full content/rule inventory detects edits made while other files
	// were uploading. Never report a complete current snapshot after such edits.
	final, err := s.scan(ctx, a.Source, a.IsConfig)
	if err != nil {
		return err
	}
	finalDigest, err := fingerprint(final)
	if err != nil {
		return err
	}
	if finalDigest != a.Fingerprint {
		return domain.Fail("SOURCE_CHANGED", "The source changed during upload. Completed copies remain; preview the new state before continuing.")
	}
	return nil
}
