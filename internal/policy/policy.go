// Package policy separates allowlisted source mechanisms from dialect matching.
package policy

import (
	"context"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
)

const MaxRuleBytes = 1 << 20
const ProfileVersion = "ledgesync-offline-v1"

type Capability struct {
	Adapter        string   `json:"adapter"`
	Mechanism      string   `json:"mechanism"`
	Dialect        string   `json:"dialect"`
	ProfileVersion string   `json:"profileVersion"`
	Supported      bool     `json:"supported"`
	Limitations    []string `json:"limitations"`
}
type Material struct {
	GroupID        string `json:"groupId"`
	Adapter        string `json:"adapter"`
	Mechanism      string `json:"mechanism"`
	Dialect        string `json:"dialect"`
	ProfileVersion string `json:"profileVersion"`
	Source         string `json:"source"`
	Scope          string `json:"scope"`
	SelectorIndex  int    `json:"selectorIndex"`
	ContentHash    string `json:"contentHash"`
	Content        string `json:"-"`
}
type Snapshot struct {
	Materials []Material `json:"materials"`
	Digest    string     `json:"digest"`
}

func Capabilities() []Capability {
	list := []Capability{}
	for _, pair := range [][2]string{{"git", "gitignore"}, {"rclone", "rclone-filter"}, {"rclone", "rclone-include"}, {"rclone", "rclone-exclude"}, {"mercurial", "hgignore"}, {"svn", "svn-ignore"}, {"svn", "svn-global-ignores"}, {"perforce", "p4ignore"}, {"cvs", "cvsignore"}, {"bazaar", "bzrignore"}, {"fossil", "fossil-ignore-glob"}, {"docker", "dockerignore"}, {"npm", "npmignore"}, {"prettier", "prettierignore"}, {"helm", "helmignore"}} {
		c := Capability{pair[0], "file", pair[1], ProfileVersion, pair[0] == "git" || pair[0] == "rclone", []string{}}
		if c.Adapter == "svn" {
			c.Mechanism = "vcs-property"
		}
		if !c.Supported {
			c.Limitations = []string{"Not implemented; configured enabled groups fail closed."}
		} else if c.Adapter == "git" {
			c.Limitations = []string{"Patterns-only; no index or global excludes; malformed classes and dangling escapes fail closed; finite differential corpus is not full Git parity."}
		} else {
			c.Limitations = []string{"File filter profiles only; no metadata/hash filters, files-from, or rclone configuration import."}
		}
		list = append(list, c)
	}
	return list
}
func CapabilityFor(dialect string) (Capability, error) {
	for _, c := range Capabilities() {
		if c.Dialect == dialect {
			return c, nil
		}
	}
	return Capability{}, domain.Fail("CAPABILITY_UNSUPPORTED", "unknown policy dialect")
}
// ReadMaterial snapshots one rule file for a group. Links, special nodes,
// unreadable files and invalid text fail closed.
func ReadMaterial(ctx context.Context, g config.Group, cap Capability, index int, p string, tree *discovery.Tree) (Material, error) {
	content, err := tree.ReadFile(ctx, p, MaxRuleBytes)
	if err != nil {
		return Material{}, domain.Fail(domain.ErrorCode(err), "cannot snapshot rule source %s in group %s", p, g.ID)
	}
	if !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return Material{}, domain.Fail("RULE_PARSE_ERROR", "rule source %s is not valid UTF-8 text", p)
	}
	scope := path.Dir(p)
	if scope == "." || cap.Adapter == "rclone" {
		scope = ""
	}
	return Material{g.ID, cap.Adapter, cap.Mechanism, g.Dialect, cap.ProfileVersion, p, scope, index, domain.HashBytes(content), string(content)}, nil
}

// Selectors returns every configured recursive basename and root-relative file
// of enabled groups. Callers use them to detect policy sources that appear later.
func Selectors(c config.Config) (basenames, rootFiles []string) {
	seen := map[string]bool{}
	for _, g := range c.Filters.Groups {
		if !g.Enabled {
			continue
		}
		for _, source := range g.Sources {
			key := source.Type + "\x00" + source.Value
			if seen[key] {
				continue
			}
			seen[key] = true
			switch source.Type {
			case "recursive-basename":
				basenames = append(basenames, source.Value)
			case "root-file":
				rootFiles = append(rootFiles, source.Value)
			}
		}
	}
	sort.Strings(basenames)
	sort.Strings(rootFiles)
	return basenames, rootFiles
}

func Resolve(ctx context.Context, c config.Config, tree *discovery.Tree) (Snapshot, error) {
	s := Snapshot{Materials: []Material{}}
	entries := tree.Entries()
	for _, g := range c.Filters.Groups {
		if !g.Enabled {
			continue
		}
		cap, err := CapabilityFor(g.Dialect)
		if err != nil {
			return s, err
		}
		if !cap.Supported {
			return s, domain.Fail("CAPABILITY_UNSUPPORTED", "adapter %s / %s is not available", cap.Adapter, g.Dialect)
		}
		seen := map[string]bool{}
		for index, source := range g.Sources {
			if ctx.Err() != nil {
				return s, domain.Fail("CANCELLED", "policy discovery cancelled")
			}
			paths := []string{}
			if source.Type == "root-file" {
				if tree.Has(source.Value) {
					paths = append(paths, source.Value)
				}
			} else {
				for _, entry := range entries {
					if entry.Name == source.Value {
						paths = append(paths, entry.Path)
					}
				}
			}
			if len(paths) == 0 && source.Required {
				return s, domain.Fail("RULE_SOURCE_UNAVAILABLE", "required source %s in group %s is missing", source.Value, g.ID)
			}
			sort.Strings(paths)
			for _, p := range paths {
				if seen[p] {
					return s, domain.Fail("CONFIG_INVALID", "rule source %s resolves twice in group %s", p, g.ID)
				}
				seen[p] = true
				m, err := ReadMaterial(ctx, g, cap, index, p, tree)
				if err != nil {
					return s, err
				}
				s.Materials = append(s.Materials, m)
			}
		}
	}
	// Hierarchical Git order: ancestor before descendant, then configured selector order.
	sort.SliceStable(s.Materials, func(i, j int) bool {
		a, b := s.Materials[i], s.Materials[j]
		if a.GroupID != b.GroupID {
			return a.GroupID < b.GroupID
		}
		if a.Dialect != "gitignore" {
			return false
		}
		da, db := strings.Count(a.Scope, "/")+1, strings.Count(b.Scope, "/")+1
		if a.Scope == "" {
			da = 0
		}
		if b.Scope == "" {
			db = 0
		}
		if da != db {
			return da < db
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.SelectorIndex < b.SelectorIndex
	})
	d, err := domain.Digest(struct {
		Materials     []Material `json:"materials"`
		CaseSensitive bool       `json:"caseSensitive"`
		Composition   string     `json:"composition"`
		Version       string     `json:"version"`
	}{s.Materials, c.Source.CaseSensitive, c.Filters.Composition, ProfileVersion})
	s.Digest = d
	return s, err
}
