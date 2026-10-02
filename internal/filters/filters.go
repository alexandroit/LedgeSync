// Package filters compiles immutable, separate dialect groups with provenance.
package filters

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
)

const MaxLineBytes = 16 << 10
const MaxRules = 100000

type rule struct {
	re            *regexp.Regexp
	allow         bool
	directoryOnly bool
	provenance    domain.Provenance
}
type group struct {
	id, dialect     string
	priority        int
	rules, dirRules []rule
}

type Engine struct {
	groups        []group
	composition   string
	caseSensitive bool
	count         int
}

func New(c config.Config, s policy.Snapshot) (*Engine, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	e := &Engine{composition: c.Filters.Composition, caseSensitive: c.Source.CaseSensitive}
	for _, g := range c.Filters.Groups {
		if !g.Enabled {
			continue
		}
		cap, err := policy.CapabilityFor(g.Dialect)
		if err != nil {
			return nil, err
		}
		if !cap.Supported {
			return nil, domain.Fail("CAPABILITY_UNSUPPORTED", "adapter %s is not implemented", cap.Adapter)
		}
		compiled := group{id: g.ID, dialect: g.Dialect, priority: g.Priority}
		for _, m := range s.Materials {
			if m.GroupID != g.ID {
				continue
			}
			if err := e.compile(&compiled, m); err != nil {
				return nil, err
			}
		}
		if g.Dialect == "rclone-include" {
			p := domain.Provenance{Adapter: "rclone", Mechanism: "file", Dialect: g.Dialect, ProfileVersion: policy.ProfileVersion, Source: "(implicit)", Pattern: "/**", Action: "deny"}
			if err := compiled.addRclone("/**", false, p, !c.Source.CaseSensitive); err != nil {
				return nil, err
			}
			e.count++
		}
		e.groups = append(e.groups, compiled)
		if e.count > MaxRules {
			return nil, domain.Fail("RULE_PARSE_ERROR", "expanded rule limit exceeded")
		}
	}
	sort.Slice(e.groups, func(i, j int) bool { return e.groups[i].priority > e.groups[j].priority })
	return e, nil
}

// NewTraversal returns a conservative engine holding only the enabled Gitignore
// groups, initially without rules. Hierarchical Gitignore rules can only affect
// paths below their own directory, so a top-down scan that adds every rule file
// before considering that directory's children evaluates each directory exactly
// as the complete engine would. Other dialects never prune traversal.
func NewTraversal(c config.Config) (*Engine, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	e := &Engine{composition: "conservative", caseSensitive: c.Source.CaseSensitive}
	for _, g := range c.Filters.Groups {
		if g.Enabled && g.Dialect == "gitignore" {
			e.groups = append(e.groups, group{id: g.ID, dialect: g.Dialect, priority: g.Priority})
		}
	}
	sort.Slice(e.groups, func(i, j int) bool { return e.groups[i].priority > e.groups[j].priority })
	return e, nil
}

// Add compiles one more rule file into its group. Callers add an ancestor
// directory's materials before any descendant directory's materials.
func (e *Engine) Add(m policy.Material) error {
	for i := range e.groups {
		if e.groups[i].id == m.GroupID {
			return e.compile(&e.groups[i], m)
		}
	}
	return nil
}

// Groups reports whether the engine can decide anything.
func (e *Engine) Groups() int { return len(e.groups) }

func (e *Engine) compile(compiled *group, m policy.Material) error {
	lines := strings.Split(m.Content, "\n")
	for i, line := range lines {
		if len(line) > MaxLineBytes {
			return domain.Fail("RULE_PARSE_ERROR", "%s:%d exceeds line limit", m.Source, i+1)
		}
		line = strings.TrimSuffix(line, "\r")
		if i == 0 && compiled.dialect == "gitignore" {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		p := domain.Provenance{Adapter: m.Adapter, Mechanism: m.Mechanism, Dialect: m.Dialect, ProfileVersion: m.ProfileVersion, Source: m.Source, Line: i + 1, Pattern: line, Scope: m.Scope}
		if compiled.dialect == "gitignore" {
			r, skip, err := parseGit(line, p, e.caseSensitive)
			if err != nil {
				return domain.Fail("RULE_PARSE_ERROR", "%s:%d: %s", m.Source, i+1, err)
			}
			if !skip {
				compiled.rules = append(compiled.rules, r)
				e.count++
			}
		} else {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			if compiled.dialect == "rclone-filter" && line == "!" {
				e.count -= len(compiled.rules) + len(compiled.dirRules)
				compiled.rules = nil
				compiled.dirRules = nil
				continue
			}
			allow := compiled.dialect == "rclone-include"
			pattern := line
			if compiled.dialect == "rclone-filter" {
				if strings.HasPrefix(line, "+ ") {
					allow = true
				} else if !strings.HasPrefix(line, "- ") {
					return domain.Fail("RULE_PARSE_ERROR", "%s:%d: expected + pattern, - pattern, or !", m.Source, i+1)
				}
				pattern = line[2:]
			}
			before := len(compiled.rules) + len(compiled.dirRules)
			if err := compiled.addRclone(pattern, allow, p, !e.caseSensitive); err != nil {
				return domain.Fail("RULE_PARSE_ERROR", "%s:%d: %s", m.Source, i+1, err)
			}
			e.count += len(compiled.rules) + len(compiled.dirRules) - before
		}
		if e.count > MaxRules {
			return domain.Fail("RULE_PARSE_ERROR", "compiled rule limit exceeded")
		}
	}
	return nil
}

func (e *Engine) Explain(ctx context.Context, p, kind string) (domain.Explanation, error) {
	x := domain.Explanation{Path: p, Kind: kind, Composition: e.composition, Groups: []domain.GroupDecision{}}
	if err := domain.ValidatePath(p); err != nil {
		return x, err
	}
	match := kind
	switch kind {
	case "file", "directory":
	case "symlink", "special":
		// Ignore rules match links and special nodes like files: a trailing
		// slash pattern never matches them and they are never traversed.
		match = "file"
	default:
		return x, domain.Fail("NODE_UNSUPPORTED", "unsupported node type")
	}
	for _, g := range e.groups {
		if ctx.Err() != nil {
			return x, domain.Fail("CANCELLED", "filter evaluation cancelled")
		}
		decision := g.evaluate(p, match, e.caseSensitive)
		x.Groups = append(x.Groups, decision)
	}
	x.Decision, x.Reason = Compose(e.composition, x.Groups)
	return x, nil
}
func Compose(mode string, groups []domain.GroupDecision) (string, string) {
	g := append([]domain.GroupDecision{}, groups...)
	sort.Slice(g, func(i, j int) bool { return g[i].Priority > g[j].Priority })
	for _, d := range g {
		if d.Decision == "ERROR" {
			return "error", "group " + d.GroupID + " failed"
		}
	}
	if mode == "conservative" {
		for _, d := range g {
			if d.Decision == "DENY" {
				return "exclude", "denied by group " + d.GroupID
			}
		}
		return "include", "no group denies this path"
	}
	for _, d := range g {
		if d.Decision == "DENY" {
			return "exclude", "first decisive group: " + d.GroupID
		}
		if d.Decision == "ALLOW" {
			return "include", "first decisive group: " + d.GroupID
		}
	}
	return "include", "default include; every group passed"
}
func (g group) evaluate(p, kind string, caseSensitive bool) domain.GroupDecision {
	d := domain.GroupDecision{GroupID: g.id, Priority: g.priority, Decision: "PASS"}
	parts := strings.Split(p, "/")
	for i := 1; i < len(parts); i++ {
		ancestor := strings.Join(parts[:i], "/")
		if r := g.match(ancestor, "directory", caseSensitive); r != nil && !r.allow {
			d.Decision = "DENY"
			prov := r.provenance
			d.Provenance = &prov
			d.AncestorBlocker = ancestor
			return d
		}
	}
	if r := g.match(p, kind, caseSensitive); r != nil {
		d.Decision = "DENY"
		if r.allow {
			d.Decision = "ALLOW"
		}
		prov := r.provenance
		d.Provenance = &prov
	}
	return d
}
func (g group) match(p, kind string, caseSensitive bool) *rule {
	var winner *rule
	rules := g.rules
	if g.dialect != "gitignore" && kind == "directory" {
		rules = g.dirRules
		p += "/"
	}
	for i := range rules {
		r := &rules[i]
		candidate := p
		if g.dialect == "gitignore" {
			scope := r.provenance.Scope
			if scope != "" {
				if !strings.HasPrefix(p, scope+"/") {
					continue
				}
				candidate = strings.TrimPrefix(p, scope+"/")
			}
			if r.directoryOnly && kind != "directory" {
				continue
			}
			candidate = gitBytes(candidate, caseSensitive)
		}
		if r.re.MatchString(candidate) {
			winner = r
			if g.dialect != "gitignore" {
				return winner
			}
		}
	}
	return winner
}

// Git's wildmatch works on bytes. Encoding each byte as a rune preserves UTF-8
// literal spelling while '?' still matches one byte, as the reference does.
func gitBytes(s string, caseSensitive bool) string {
	var b strings.Builder
	for _, v := range []byte(s) {
		if !caseSensitive && v >= 'A' && v <= 'Z' {
			v += 32
		}
		b.WriteRune(rune(v))
	}
	return b.String()
}
func parseGit(raw string, p domain.Provenance, caseSensitive bool) (rule, bool, error) {
	r := rule{provenance: p}
	line := raw
	for strings.HasSuffix(line, " ") {
		n := 0
		for i := len(line) - 2; i >= 0 && line[i] == '\\'; i-- {
			n++
		}
		if n%2 == 1 {
			break
		}
		line = line[:len(line)-1]
	}
	if line == "" || line[0] == '#' {
		return r, true, nil
	}
	if line[0] == '!' {
		r.allow = true
		line = line[1:]
	}
	if line == "" {
		return r, true, nil
	}
	r.provenance.Action = "deny"
	if r.allow {
		r.provenance.Action = "allow"
	}
	r.directoryOnly = strings.HasSuffix(line, "/")
	if r.directoryOnly {
		line = strings.TrimSuffix(line, "/")
	}
	anchored := strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")
	anchored = anchored || strings.Contains(line, "/")
	if line == "" {
		return r, true, nil
	}
	pattern, err := gitGlob(gitBytes(line, caseSensitive))
	if err != nil {
		return r, false, err
	}
	prefix := "(^|/)"
	if anchored {
		prefix = "^"
	}
	r.re, err = regexp.Compile(prefix + pattern + "$")
	return r, false, err
}
func gitGlob(s string) (string, error) {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '\\':
			i++
			if i >= len(runes) {
				return "", fmt.Errorf("dangling escapes are not supported")
			}
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		case '?':
			b.WriteString(`[^/]`)
		case '*':
			j := i
			for j+1 < len(runes) && runes[j+1] == '*' {
				j++
			}
			double := j > i && (i == 0 || runes[i-1] == '/') && (j+1 == len(runes) || runes[j+1] == '/')
			if double {
				if j+1 < len(runes) {
					b.WriteString(`(?:[^/]+/)*`)
					j++
				} else {
					b.WriteString(`(?s:.*)`)
				}
			} else {
				b.WriteString(`[^/]*`)
			}
			i = j
		case '[':
			j := i + 1
			if j < len(runes) && (runes[j] == '!' || runes[j] == '^') {
				j++
			}
			if j < len(runes) && runes[j] == ']' {
				j++
			}
			for j < len(runes) && runes[j] != ']' {
				if runes[j] == '[' && j+1 < len(runes) && runes[j+1] == ':' {
					k := j + 2
					for k+1 < len(runes) && !(runes[k] == ':' && runes[k+1] == ']') {
						k++
					}
					if k+1 >= len(runes) {
						return "", fmt.Errorf("unclosed POSIX character class")
					}
					j = k + 2
					continue
				}
				j++
			}
			if j >= len(runes) {
				return "", fmt.Errorf("unclosed character class")
			}
			class := string(runes[i : j+1])
			if strings.HasPrefix(class, "[!") {
				class = "[^" + class[2:]
			}
			if strings.Contains(class, "/") {
				return "", fmt.Errorf("slash-containing classes are unsupported")
			}
			compiled, err := regexp.Compile("^" + class + "$")
			if err != nil {
				return "", err
			}
			var allowed strings.Builder
			allowed.WriteByte('[')
			matches := 0
			for v := 1; v <= 255; v++ {
				if v != 47 && compiled.MatchString(string(rune(v))) {
					fmt.Fprintf(&allowed, "\\x{%x}", v)
					matches++
				}
			}
			allowed.WriteByte(']')
			if matches == 0 {
				b.WriteString(`[^\x00-\x{ff}]`)
			} else {
				b.WriteString(allowed.String())
			}
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), nil
}
func (g *group) addRclone(pattern string, allow bool, p domain.Provenance, ignoreCase bool) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}
	p.Action = "deny"
	if allow {
		p.Action = "allow"
	}
	isDir := strings.HasSuffix(pattern, "/")
	isFile := !isDir
	if isDir && !allow {
		pattern += "**"
	}
	if strings.Contains(pattern, "**") {
		isDir = true
		isFile = true
	}
	re, err := GlobPathToRegexp(pattern, ignoreCase)
	if err != nil {
		return err
	}
	r := rule{re: re, allow: allow, provenance: p}
	if isFile {
		g.rules = append(g.rules, r)
		if allow || pattern == "*" {
			for _, dirGlob := range globToDirGlobs(pattern) {
				if dirGlob == "/" {
					continue
				}
				dirRe, err := GlobPathToRegexp(dirGlob, ignoreCase)
				if err != nil {
					return err
				}
				g.dirRules = append(g.dirRules, rule{re: dirRe, allow: allow, provenance: p})
			}
		}
	}
	if isDir {
		g.dirRules = append(g.dirRules, r)
	}
	return nil
}
