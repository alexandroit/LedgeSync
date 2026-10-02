// Package app is the shared application boundary used by desktop and CLI.
package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/filters"
	"github.com/alexandroit/LedgeSync/internal/planner"
	"github.com/alexandroit/LedgeSync/internal/policy"
	"github.com/alexandroit/LedgeSync/internal/providers/fake"
)

// TraversalProfile identifies the selection traversal contract. Rule sources in
// directories that the applicable Gitignore policy excludes are not consulted.
const TraversalProfile = "ledgesync-traversal-v2"

// Entry decisions. Unsupported entries are selected by policy but are links or
// special nodes, which are never followed, read or copied.
const (
	DecisionInclude     = "include"
	DecisionExclude     = "exclude"
	DecisionUnsupported = "unsupported"
)

type Preview struct {
	ProjectName  string              `json:"projectName"`
	SourceRoot   string              `json:"sourceRoot"`
	Offline      bool                `json:"offline"`
	Entries      []domain.Entry      `json:"entries"`
	Plan         domain.Plan         `json:"plan"`
	Capabilities []policy.Capability `json:"capabilities"`
	RuleSources  []string            `json:"ruleSources"`
	Unsupported  int                 `json:"unsupported"`
	Pruned       []string            `json:"pruned"`
}

type hashRecord struct {
	info   os.FileInfo
	sha256 string
	hashed time.Time
}

type Service struct {
	mu       sync.Mutex
	observed map[string][]string
	hashes   map[string]map[string]hashRecord
	order    []string
	now      func() time.Time
}

// Racy window: a file whose modification time is this close to the moment it
// was hashed may change again within the same timestamp granularity.
const racyWindow = 3 * time.Second
const cachedRoots = 8

func NewService() *Service {
	return &Service{observed: map[string][]string{}, hashes: map[string]map[string]hashRecord{}, now: time.Now}
}
func (s *Service) PreviewRoot(ctx context.Context, root string) (Preview, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Preview{}, domain.Fail("CONFIG_INVALID", "invalid local root")
	}
	return s.Scan(ctx, config.Default(absolute))
}
func (s *Service) Preview(ctx context.Context, configPath string) (Preview, error) {
	c, err := config.Load(configPath)
	if err != nil {
		return Preview{}, err
	}
	return s.Scan(ctx, c)
}

// traversal decides, top-down, which directories the conservative Gitignore
// policy excludes completely. Their descendants cannot be selected by any group
// under conservative composition, so they are not listed or read.
type traversal struct {
	engine  *filters.Engine
	keep    map[string]bool
	sources []traversalSource
}
type traversalSource struct {
	group  config.Group
	cap    policy.Capability
	index  int
	source config.RuleSource
}

func newTraversal(c config.Config) (*traversal, error) {
	if c.Filters.Composition != "conservative" {
		return nil, nil
	}
	engine, err := filters.NewTraversal(c)
	if err != nil || engine.Groups() == 0 {
		return nil, err
	}
	t := &traversal{engine: engine, keep: map[string]bool{}}
	for _, g := range c.Filters.Groups {
		if !g.Enabled {
			continue
		}
		cap, err := policy.CapabilityFor(g.Dialect)
		if err != nil {
			return nil, err
		}
		for index, source := range g.Sources {
			if source.Type == "root-file" {
				// Explicit sources must always be found where configured.
				for dir := path.Dir(source.Value); dir != "." && dir != "/"; dir = path.Dir(dir) {
					t.keep[dir] = true
				}
			}
			if g.Dialect == "gitignore" {
				t.sources = append(t.sources, traversalSource{g, cap, index, source})
			}
		}
	}
	return t, nil
}

func (t *traversal) directory(ctx context.Context, tree *discovery.Tree, dir string) error {
	for _, s := range t.sources {
		p := ""
		switch s.source.Type {
		case "recursive-basename":
			p = s.source.Value
			if dir != "." {
				p = dir + "/" + s.source.Value
			}
		case "root-file":
			if path.Dir(s.source.Value) != dir {
				continue
			}
			p = s.source.Value
		default:
			continue
		}
		if !tree.Has(p) {
			continue
		}
		m, err := policy.ReadMaterial(ctx, s.group, s.cap, s.index, p, tree)
		if err != nil {
			return err
		}
		if err = t.engine.Add(m); err != nil {
			return err
		}
	}
	return nil
}

func (t *traversal) prune(ctx context.Context, _ *discovery.Tree, dir string) (bool, error) {
	if t.keep[dir] {
		return false, nil
	}
	x, err := t.engine.Explain(ctx, dir, "directory")
	if err != nil {
		return false, err
	}
	return x.Decision == DecisionExclude, nil
}

// ScanTree lists the configured source with policy-aware traversal. The caller
// owns the returned tree. No file contents except rule sources are read.
func ScanTree(ctx context.Context, c config.Config) (*discovery.Tree, error) {
	t, err := newTraversal(c)
	if err != nil {
		return nil, err
	}
	options := discovery.Options{}
	if t != nil {
		options.Directory, options.Prune = t.directory, t.prune
	}
	return discovery.ScanWith(ctx, c.Source.Root, options)
}

func (s *Service) Scan(ctx context.Context, c config.Config) (Preview, error) {
	result := Preview{}
	if err := c.Validate(); err != nil {
		return result, err
	}
	if c.Sync.Mode != "copy" || c.Sync.OverwritePolicy != "deny" || c.Automation.Enabled {
		return result, domain.Fail("CAPABILITY_UNSUPPORTED", "this release supports manual copy previews with overwrite denied")
	}
	// Resolve capabilities before inspecting any source data.
	for _, g := range c.Filters.Groups {
		if g.Enabled {
			cap, err := policy.CapabilityFor(g.Dialect)
			if err != nil {
				return result, err
			}
			if !cap.Supported {
				return result, domain.Fail("CAPABILITY_UNSUPPORTED", "%s adapter is not implemented", cap.Adapter)
			}
		}
	}
	tree, err := ScanTree(ctx, c)
	if err != nil {
		return result, err
	}
	defer tree.Close()
	c.Source.Root = tree.RootPath()
	snapshot, err := policy.Resolve(ctx, c, tree)
	if err != nil {
		return result, err
	}
	engine, err := filters.New(c, snapshot)
	if err != nil {
		return result, err
	}
	key, err := domain.Digest(struct {
		Config  config.Config
		Profile string
	}{c, TraversalProfile})
	if err != nil {
		return result, err
	}
	s.mu.Lock()
	previous := append([]string{}, s.observed[key]...)
	s.mu.Unlock()
	current := map[string]bool{}
	sourcePaths := []string{}
	for _, m := range snapshot.Materials {
		current[m.Source] = true
		sourcePaths = append(sourcePaths, m.Source)
	}
	for _, p := range previous {
		if !current[p] {
			return result, domain.Fail("RULE_SOURCE_UNAVAILABLE", "previously observed rule source %s disappeared; restore it or explicitly change the configuration", p)
		}
	}
	cache := s.cacheFor(tree.Identity())
	fresh := map[string]hashRecord{}
	entries := tree.Entries()
	included := []string{}
	for i := range entries {
		entry := &entries[i]
		entry.Explanation, err = engine.Explain(ctx, entry.Path, entry.Kind)
		if err != nil {
			return result, err
		}
		entry.Decision = entry.Explanation.Decision
		switch {
		case entry.Decision == DecisionExclude:
			entry.Status = "Excluded"
		case tree.Pruned(entry.Path):
			// The traversal engine and the complete engine disagree: never
			// select a directory whose contents were not inspected.
			return result, domain.Fail("SCAN_INCOMPLETE", "traversal pruning disagreed with the complete policy for %s", entry.Path)
		case entry.Kind == discovery.KindSymlink:
			entry.Decision, entry.Status = DecisionUnsupported, "Not copied: symbolic link"
			result.Unsupported++
		case entry.Kind == discovery.KindSpecial:
			entry.Decision, entry.Status = DecisionUnsupported, "Not copied: special file"
			result.Unsupported++
		default:
			entry.Status = "Local only"
		}
		if entry.Decision == DecisionInclude && entry.Kind == discovery.KindFile {
			// Reuse a digest only for the identical, unchanged node, and only when
			// the file was not modified within the racy timestamp window.
			info, _ := tree.Info(entry.Path)
			r, ok := cache[entry.Path]
			if ok && discovery.Same(r.info, info) && info.ModTime().Before(r.hashed.Add(-racyWindow)) {
				entry.SHA256 = r.sha256
			} else {
				hashed := s.now()
				entry.SHA256, err = tree.HashFile(ctx, entry.Path)
				if err != nil {
					return result, err
				}
				r = hashRecord{info: info, sha256: entry.SHA256, hashed: hashed}
			}
			fresh[entry.Path] = r
			included = append(included, entry.Path)
		}
		if tree.Pruned(entry.Path) {
			result.Pruned = append(result.Pruned, entry.Path)
		}
	}
	// Included files must still match the observations that their digests and
	// selection were based on. Excluded churn does not invalidate the preview.
	if err = tree.RootUnchanged(); err != nil {
		return result, err
	}
	if err = tree.RevalidatePaths(append(included, sourcePaths...)); err != nil {
		return result, err
	}
	remote, err := fake.Empty().List(ctx)
	if err != nil {
		return result, err
	}
	plan, err := planner.BuildContext(ctx, c, tree.Identity(), snapshot.Digest, entries, remote, s.now())
	if err != nil {
		return result, err
	}
	s.mu.Lock()
	s.observed[key] = append([]string{}, sourcePaths...)
	s.storeCache(tree.Identity(), fresh)
	s.mu.Unlock()
	result.ProjectName, result.SourceRoot, result.Offline, result.Entries, result.Plan = c.Project.DisplayName, tree.RootPath(), true, entries, plan
	result.Capabilities, result.RuleSources = policy.Capabilities(), sourcePaths
	if result.Pruned == nil {
		result.Pruned = []string{}
	}
	return result, nil
}

func (s *Service) cacheFor(identity string) map[string]hashRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]hashRecord{}
	for k, v := range s.hashes[identity] {
		out[k] = v
	}
	return out
}

// storeCache keeps verified content digests for a bounded number of roots.
// Callers hold s.mu.
func (s *Service) storeCache(identity string, records map[string]hashRecord) {
	if _, ok := s.hashes[identity]; !ok {
		s.order = append(s.order, identity)
	}
	s.hashes[identity] = records
	for len(s.order) > cachedRoots {
		delete(s.hashes, s.order[0])
		s.order = s.order[1:]
	}
}

func (s *Service) Explain(ctx context.Context, configPath, p string) (domain.Explanation, error) {
	if err := domain.ValidatePath(p); err != nil {
		return domain.Explanation{}, err
	}
	preview, err := s.Preview(ctx, configPath)
	if err != nil {
		return domain.Explanation{}, err
	}
	for _, e := range preview.Entries {
		if e.Path == p {
			return e.Explanation, nil
		}
	}
	return domain.Explanation{}, domain.Fail("PATH_UNSAFE", "path is not present in the complete source inventory")
}
