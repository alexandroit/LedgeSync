// Package config implements the strict v1.1 project contract. It stores no secrets.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/alexandroit/LedgeSync/internal/domain"
)

type Project struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}
type Source struct {
	Root          string `json:"root"`
	CaseSensitive bool   `json:"caseSensitive"`
	Symlinks      string `json:"symlinks"`
}
type Destination struct {
	Provider      string `json:"provider"`
	AccountRef    string `json:"accountRef"`
	RootFolderID  string `json:"rootFolderId"`
	NamespaceMode string `json:"namespaceMode"`
}
type RuleSource struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Required bool   `json:"required"`
}
type Group struct {
	ID       string       `json:"id"`
	Priority int          `json:"priority"`
	Enabled  bool         `json:"enabled"`
	Dialect  string       `json:"dialect"`
	Scope    string       `json:"scope"`
	Sources  []RuleSource `json:"sources"`
}
type Filters struct {
	Composition     string  `json:"composition"`
	GitMode         string  `json:"gitMode"`
	DefaultDecision string  `json:"defaultDecision"`
	FailOnError     bool    `json:"failOnError"`
	Groups          []Group `json:"groups"`
}
type Sync struct {
	Mode             string  `json:"mode"`
	ApprovalPolicy   string  `json:"approvalPolicy"`
	ConflictPolicy   string  `json:"conflictPolicy"`
	OverwritePolicy  string  `json:"overwritePolicy"`
	Verification     string  `json:"verification"`
	MaxTransfers     int     `json:"maxTransfers"`
	MaxRetries       int     `json:"maxRetries"`
	MaxDeleteCount   int     `json:"maxDeleteCount"`
	MaxDeletePercent float64 `json:"maxDeletePercent"`
}
type Automation struct {
	Enabled         bool   `json:"enabled"`
	Trigger         string `json:"trigger"`
	IntervalSeconds int    `json:"intervalSeconds"`
}
type Config struct {
	SchemaVersion string      `json:"schemaVersion"`
	Project       Project     `json:"project"`
	Source        Source      `json:"source"`
	Destination   Destination `json:"destination"`
	Filters       Filters     `json:"filters"`
	Sync          Sync        `json:"sync"`
	Automation    Automation  `json:"automation"`
}

const MaxConfigBytes = 2 << 20

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func Default(root string) Config {
	return Config{"1.1", Project{"local-preview", "Local preview"}, Source{root, true, "reject"}, Destination{"google-drive", "OFFLINE_NOT_CONNECTED", "OFFLINE_NOT_CONNECTED", "app-managed"}, Filters{"conservative", "patterns-only", "include", true, []Group{{"git", 100, true, "gitignore", "project", []RuleSource{{"recursive-basename", ".gitignore", false}}}}}, Sync{"copy", "interactive", "keep-both", "deny", "content-hash", 4, 6, 0, 0}, Automation{false, "manual", 0}}
}
func Load(filename string) (Config, error) {
	if strings.EqualFold(filepath.Base(filename), "rclone.conf") {
		return Config{}, domain.Fail("CONFIG_INVALID", "rclone.conf is credential configuration, not a LedgeSync project")
	}
	f, err := os.Open(filename)
	if err != nil {
		return Config{}, domain.Fail("CONFIG_INVALID", "cannot read project configuration")
	}
	defer f.Close()
	c, err := Decode(f)
	if err != nil {
		return c, err
	}
	if !filepath.IsAbs(c.Source.Root) {
		c.Source.Root = filepath.Join(filepath.Dir(filename), c.Source.Root)
	}
	c.Source.Root, err = filepath.Abs(c.Source.Root)
	return c, err
}
func Decode(r io.Reader) (Config, error) {
	var c Config
	if err := DecodeJSON(r, &c, MaxConfigBytes); err != nil {
		return c, err
	}
	return c, c.Validate()
}

// DecodeJSON strictly decodes a bounded versioned envelope: duplicate, unknown,
// missing, null non-pointer fields, invalid UTF-8 and trailing values are errors.
func DecodeJSON(r io.Reader, target any, maxBytes int64) error {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return domain.Fail("CONFIG_INVALID", "cannot read JSON envelope")
	}
	if int64(len(b)) > maxBytes || !utf8.Valid(b) {
		return domain.Fail("CONFIG_INVALID", "JSON exceeds limit or is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := uniqueValue(d, 0); err != nil {
		return domain.Fail("CONFIG_INVALID", "%s", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return domain.Fail("CONFIG_INVALID", "trailing JSON data")
	}
	targetType := reflect.TypeOf(target)
	if targetType == nil || targetType.Kind() != reflect.Pointer || targetType.Elem().Kind() != reflect.Struct {
		return domain.Fail("CONFIG_INVALID", "decoder target must be a struct pointer")
	}
	if err := shape(b, targetType.Elem(), "envelope"); err != nil {
		return domain.Fail("CONFIG_INVALID", "%s", err)
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return domain.Fail("CONFIG_INVALID", "%s", err)
	}
	return nil
}
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting exceeds limit")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := k.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if seen[s] {
				return fmt.Errorf("duplicate field %q", s)
			}
			seen[s] = true
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case '[':
		for d.More() {
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter")
	}
}
func shape(raw json.RawMessage, t reflect.Type, where string) error {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return shape(raw, t.Elem(), where)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s must not be null", where)
	}
	switch t.Kind() {
	case reflect.Struct:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("%s must be an object", where)
		}
		allowed := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := f.Tag.Get("json")
			allowed[name] = true
			v, ok := m[name]
			if !ok {
				return fmt.Errorf("missing %s.%s", where, name)
			}
			if err := shape(v, f.Type, where+"."+name); err != nil {
				return err
			}
		}
		for k := range m {
			if !allowed[k] {
				return fmt.Errorf("unknown field %s.%s", where, k)
			}
		}
	case reflect.Slice:
		var a []json.RawMessage
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("%s must be an array", where)
		}
		for i, v := range a {
			if err := shape(v, t.Elem(), fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
func member(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func (c Config) Validate() error {
	bad := func(s string) error { return domain.Fail("CONFIG_INVALID", "%s", s) }
	if c.SchemaVersion != "1.1" {
		return bad("only schemaVersion 1.1 is supported; migrations are not yet available")
	}
	if !idPattern.MatchString(c.Project.ID) || strings.TrimSpace(c.Project.DisplayName) == "" {
		return bad("invalid project identity")
	}
	if c.Source.Root == "" || !utf8.ValidString(c.Source.Root) || strings.ContainsRune(c.Source.Root, 0) || c.Source.Symlinks != "reject" {
		return bad("invalid source root or symlink policy")
	}
	if c.Destination.Provider != "google-drive" || c.Destination.AccountRef == "" || c.Destination.RootFolderID == "" || c.Destination.NamespaceMode != "app-managed" {
		return bad("invalid destination contract")
	}
	if !member(c.Filters.Composition, "conservative", "ordered") || c.Filters.GitMode != "patterns-only" || c.Filters.DefaultDecision != "include" || !c.Filters.FailOnError || len(c.Filters.Groups) == 0 {
		return bad("invalid filters contract")
	}
	ids := map[string]bool{}
	priorities := map[int]bool{}
	for _, g := range c.Filters.Groups {
		if !idPattern.MatchString(g.ID) || ids[g.ID] || priorities[g.Priority] || g.Priority < 0 || g.Priority > 1000000 || g.Scope != "project" || len(g.Sources) == 0 {
			return bad("invalid or duplicate group ID, priority, scope, or empty sources")
		}
		ids[g.ID] = true
		priorities[g.Priority] = true
		if !member(g.Dialect, "gitignore", "rclone-filter", "rclone-include", "rclone-exclude", "hgignore", "p4ignore", "cvsignore", "bzrignore", "fossil-ignore-glob", "svn-ignore", "svn-global-ignores", "dockerignore", "npmignore", "prettierignore", "helmignore") {
			return bad("unknown dialect: " + g.Dialect)
		}
		seen := map[string]bool{}
		for _, s := range g.Sources {
			key := s.Type + ":" + s.Value
			if seen[key] {
				return bad("duplicate source in group " + g.ID)
			}
			seen[key] = true
			if s.Type == "recursive-vcs-property" {
				expected := "svn:ignore"
				if g.Dialect == "svn-global-ignores" {
					expected = "svn:global-ignores"
				}
				if !member(g.Dialect, "svn-ignore", "svn-global-ignores") || s.Value != expected {
					return bad("invalid metadata property/dialect combination")
				}
				continue
			}
			if !member(s.Type, "recursive-basename", "root-file") || member(g.Dialect, "svn-ignore", "svn-global-ignores") {
				return bad("invalid source mechanism/dialect combination")
			}
			if err := domain.ValidatePath(s.Value); err != nil {
				return bad("unsafe rule source path")
			}
			if strings.EqualFold(filepath.Base(s.Value), "rclone.conf") {
				return bad("rclone.conf cannot be used as a rule source")
			}
			if s.Type == "recursive-basename" && (strings.Contains(s.Value, "/") || !member(g.Dialect, "gitignore", "p4ignore", "cvsignore", "npmignore")) {
				return bad("recursive-basename is unavailable for this dialect or value")
			}
		}
	}
	if !member(c.Sync.Mode, "copy", "mirror") || !member(c.Sync.ApprovalPolicy, "interactive", "copy-preauthorized") || !member(c.Sync.ConflictPolicy, "keep-both", "pause") || !member(c.Sync.OverwritePolicy, "deny", "recover-managed") || c.Sync.Verification != "content-hash" || c.Sync.MaxTransfers < 1 || c.Sync.MaxTransfers > 32 || c.Sync.MaxRetries < 0 || c.Sync.MaxRetries > 20 || c.Sync.MaxDeleteCount < 0 || c.Sync.MaxDeletePercent < 0 || c.Sync.MaxDeletePercent > 100 {
		return bad("invalid sync safety settings")
	}
	if c.Automation.Enabled {
		if !member(c.Automation.Trigger, "interval", "watch") || c.Automation.IntervalSeconds < 60 || c.Automation.IntervalSeconds > 604800 || c.Sync.Mode != "copy" || c.Sync.ApprovalPolicy != "copy-preauthorized" || c.Sync.OverwritePolicy != "deny" {
			return bad("invalid automation safety settings")
		}
	} else if c.Automation.Trigger != "manual" || c.Automation.IntervalSeconds != 0 {
		return bad("disabled automation must be manual with zero interval")
	}
	if c.Sync.Mode == "mirror" && (c.Automation.Enabled || c.Sync.ApprovalPolicy != "interactive") {
		return bad("mirror requires manual interactive approval")
	}
	return nil
}
