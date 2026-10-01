// Package app is the shared application boundary used by desktop and CLI.
package app

import (
	"context"
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

type Preview struct {
	ProjectName  string              `json:"projectName"`
	SourceRoot   string              `json:"sourceRoot"`
	Offline      bool                `json:"offline"`
	Entries      []domain.Entry      `json:"entries"`
	Plan         domain.Plan         `json:"plan"`
	Capabilities []policy.Capability `json:"capabilities"`
}
type Service struct {
	mu       sync.Mutex
	observed map[string][]string
	now      func() time.Time
}

func NewService() *Service { return &Service{observed: map[string][]string{}, now: time.Now} }
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
func (s *Service) Scan(ctx context.Context, c config.Config) (Preview, error) {
	result := Preview{}
	if err := c.Validate(); err != nil {
		return result, err
	}
	if c.Sync.Mode != "copy" || c.Sync.OverwritePolicy != "deny" || c.Automation.Enabled {
		return result, domain.Fail("CAPABILITY_UNSUPPORTED", "offline alpha supports manual copy previews with overwrite denied")
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
	tree, err := discovery.Scan(ctx, c.Source.Root)
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
	key, err := domain.Digest(c)
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
	entries := tree.Entries()
	for i := range entries {
		entry := &entries[i]
		entry.Explanation, err = engine.Explain(ctx, entry.Path, entry.Kind)
		if err != nil {
			return result, err
		}
		entry.Decision = entry.Explanation.Decision
		entry.Status = "Local only"
		if entry.Decision == "exclude" {
			entry.Status = "Excluded"
		} else if entry.Kind == "file" {
			entry.SHA256, err = tree.HashFile(ctx, entry.Path)
			if err != nil {
				return result, err
			}
		}
	}
	if err = tree.Revalidate(); err != nil {
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
	s.mu.Unlock()
	return Preview{c.Project.DisplayName, tree.RootPath(), true, entries, plan, policy.Capabilities()}, nil
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
