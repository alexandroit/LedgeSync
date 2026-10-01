package filters

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/discovery"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/policy"
)

type fixture struct {
	ID               string
	Dialect          string
	RuleFiles        map[string]string
	RuleStreams      []string
	Files            []string
	ExpectedExcluded []string
	TrackedFiles     []string
}
type corpus struct {
	GitCases         []fixture
	RcloneCases      []fixture
	CompositionCases []struct {
		ID, Composition, Expected string
		GroupDecisions            []domain.GroupDecision
	}
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()
	b, err := os.ReadFile("../../tests/filter-conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var c corpus
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func put(t *testing.T, root, p, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixtureEngine(t *testing.T, f fixture) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	for _, p := range f.Files {
		put(t, root, p, "fixture\n")
	}
	for p, c := range f.RuleFiles {
		put(t, root, p, c)
	}
	c := config.Default(root)
	if f.Dialect != "" {
		c.Filters.Groups[0].Dialect = f.Dialect
		c.Filters.Groups[0].Sources = nil
		for i, content := range f.RuleStreams {
			name := "rules-" + string(rune('a'+i))
			put(t, root, name, content)
			c.Filters.Groups[0].Sources = append(c.Filters.Groups[0].Sources, config.RuleSource{Type: "root-file", Value: name, Required: true})
		}
	}
	tree, err := discovery.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	s, err := policy.Resolve(context.Background(), c, tree)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	return e, root
}
func excluded(t *testing.T, e *Engine, paths []string) []string {
	t.Helper()
	out := []string{}
	for _, p := range paths {
		x, err := e.Explain(context.Background(), p, "file")
		if err != nil {
			t.Fatal(err)
		}
		if x.Decision == "exclude" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
func TestSpecificationCorpus(t *testing.T) {
	c := loadCorpus(t)
	for _, f := range append(c.GitCases, c.RcloneCases...) {
		t.Run(f.ID, func(t *testing.T) {
			e, _ := fixtureEngine(t, f)
			actual := excluded(t, e, f.Files)
			expected := append([]string{}, f.ExpectedExcluded...)
			sort.Strings(expected)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("excluded %q; expected %q", actual, expected)
			}
		})
	}
	for _, f := range c.CompositionCases {
		t.Run(f.ID, func(t *testing.T) {
			actual, _ := Compose(f.Composition, f.GroupDecisions)
			if actual != f.Expected {
				t.Fatalf("got %s want %s", actual, f.Expected)
			}
		})
	}
}

func gitOracle(t *testing.T, root string, f fixture) []string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git reference executable unavailable")
	}
	home := t.TempDir()
	empty := filepath.Join(home, "empty-config")
	if err = os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	template := filepath.Join(home, "template")
	if err = os.Mkdir(template, 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + empty, "GIT_CONFIG_SYSTEM=" + empty, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	base := []string{"-c", "core.ignoreCase=false", "-c", "core.excludesFile=" + empty, "-c", "core.hooksPath=" + template}
	run := func(input string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, git, append(append([]string{}, base...), args...)...)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		return cmd.Output()
	}
	if _, err := run("", "init", "--quiet", "--template="+template); err != nil {
		t.Fatal(err)
	}
	if len(f.TrackedFiles) > 0 {
		if _, err := run("", append([]string{"add", "--force", "--"}, f.TrackedFiles...)...); err != nil {
			t.Fatal(err)
		}
	}
	result, err := run(strings.Join(f.Files, "\x00")+"\x00", "check-ignore", "--no-index", "--stdin", "-z")
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatal(err)
		}
	}
	out := []string{}
	for _, p := range strings.Split(string(result), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
func TestGitDifferential(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	version, err := exec.Command(git, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(string(version)))
	if expected := os.Getenv("LEDGESYNC_GIT_EXPECT_VERSION"); expected != "" && strings.TrimSpace(string(version)) != expected {
		t.Fatalf("reference version differs: %s", version)
	}
	cases := loadCorpus(t).GitCases
	cases = append(cases,
		fixture{ID: "GIT-029-class-separator", RuleFiles: map[string]string{".gitignore": "foo[!a]bar\nfoo[[:punct:]]baz\n"}, Files: []string{"foo/bar", "fooZbar", "foo/baz", "foo-baz"}},
		fixture{ID: "GIT-030-byte-wildcards", RuleFiles: map[string]string{".gitignore": "caf?\ncaf??\n"}, Files: []string{"café", "cafa", "caf猫"}},
		fixture{ID: "GIT-031-multiple-stars", RuleFiles: map[string]string{".gitignore": "a/***/b\na**b\n"}, Files: []string{"a/b", "a/x/b", "a/x/y/b", "axb", "a/x/y/z"}},
		fixture{ID: "GIT-032-blocked-nested", RuleFiles: map[string]string{".gitignore": "build/\n", "build/.gitignore": "!keep.txt\n"}, Files: []string{"build/keep.txt", "build/drop.txt"}},
		fixture{ID: "GIT-033-escaped-space-markers", RuleFiles: map[string]string{".gitignore": "\\!keep\n\\#tag\nx\\  \n"}, Files: []string{"!keep", "#tag", "x ", "x"}},
		fixture{ID: "GIT-034-posix-negation", RuleFiles: map[string]string{".gitignore": "file[![:digit:]].txt\n"}, Files: []string{"filea.txt", "file1.txt", "file/.txt"}},
		fixture{ID: "GIT-035-newline-double-star", RuleFiles: map[string]string{".gitignore": "a/**\n"}, Files: []string{"a/x\ny", "b/x\ny"}},
	)
	for _, f := range cases {
		t.Run(f.ID, func(t *testing.T) {
			if f.ID == "GIT-035-newline-double-star" && runtime.GOOS == "windows" {
				t.Skip("Windows filenames cannot contain newline characters")
			}
			e, root := fixtureEngine(t, f)
			actual := excluded(t, e, f.Files)
			reference := gitOracle(t, root, f)
			if !reflect.DeepEqual(actual, reference) {
				t.Fatalf("product %q; Git %q", actual, reference)
			}
		})
	}
}
func TestMultiSourceAndComposition(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "*.log\nbuild/\n")
	put(t, root, ".ignore", "!keep.log\n")
	put(t, root, "rules", "+ build/keep.txt\n- *.log\n")
	put(t, root, "build/.gitignore", "!keep.txt\n")
	put(t, root, "build/keep.txt", "fixture")
	c := config.Default(root)
	c.Filters.Groups[0].Sources = append(c.Filters.Groups[0].Sources, config.RuleSource{Type: "recursive-basename", Value: ".ignore"})
	c.Filters.Groups = append(c.Filters.Groups, config.Group{ID: "transfer", Priority: 200, Enabled: true, Dialect: "rclone-filter", Scope: "project", Sources: []config.RuleSource{{Type: "root-file", Value: "rules", Required: true}}})
	tree, err := discovery.Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	s, err := policy.Resolve(context.Background(), c, tree)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"conservative", "ordered"} {
		c.Filters.Composition = mode
		e, err := New(c, s)
		if err != nil {
			t.Fatal(err)
		}
		x, err := e.Explain(context.Background(), "build/keep.txt", "file")
		if err != nil {
			t.Fatal(err)
		}
		want := "exclude"
		if mode == "ordered" {
			want = "include"
		}
		if x.Decision != want {
			t.Fatalf("%s: %+v", mode, x)
		}
		if x.Groups[1].AncestorBlocker != "build" || x.Groups[1].Provenance.Source != ".gitignore" {
			t.Fatalf("blocked nested source became active: %+v", x.Groups)
		}
	}
}
func TestCaseAndParserFailures(t *testing.T) {
	c := config.Default(t.TempDir())
	c.Source.CaseSensitive = false
	s := policy.Snapshot{Materials: []policy.Material{{GroupID: "git", Adapter: "git", Mechanism: "file", Dialect: "gitignore", Source: ".gitignore", Content: "*.LOG"}}}
	e, err := New(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if got := excluded(t, e, []string{"a.log", "a.LOG"}); len(got) != 2 {
		t.Fatal(got)
	}
	for _, pattern := range []string{"[unterminated", "dangling\\", strings.Repeat("x", MaxLineBytes+1)} {
		s.Materials[0].Content = pattern
		if _, err = New(c, s); err == nil {
			t.Fatalf("accepted invalid %q", pattern[:min(len(pattern), 20)])
		}
	}
}
func FuzzGitParser(f *testing.F) {
	for _, s := range []string{"*.log", "foo[!a]bar", "a/**/b", "\\#tag", "x\\ ", "[[:digit:]]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxLineBytes {
			return
		}
		_, _, _ = parseGit(s, domain.Provenance{}, true)
	})
}
func FuzzRcloneGlob(f *testing.F) {
	for _, s := range []string{"*.{log,tmp}", "{{[^/]*\\.log}}", "/foo/**", "{a,{b,c}}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxLineBytes {
			return
		}
		_, _ = GlobPathToRegexp(s, false)
	})
}
