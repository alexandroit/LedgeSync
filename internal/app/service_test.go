package app

import (
	"context"
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestCancellationAtPlanningBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewService()
	s.now = func() time.Time { cancel(); return time.Unix(1000, 0) }
	if _, err := s.PreviewRoot(ctx, t.TempDir()); domain.ErrorCode(err) != "CANCELLED" {
		t.Fatalf("cancelled planning returned success: %v", err)
	}
}

func write(t *testing.T, root, p, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestSharedPreviewReadOnlyAndDeterminism(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".gitignore", "*.log\n")
	write(t, root, "a.log", "private ignored fixture")
	write(t, root, "a.txt", "fixture")
	s := NewService()
	s.now = func() time.Time { return time.Unix(1000, 0) }
	a, err := s.PreviewRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.PreviewRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("same snapshots and clock produced different preview")
	}
	if !a.Offline || a.Plan.DestinationIdentity != "fake:empty-offline" {
		t.Fatal("not a fake preview")
	}
	for _, e := range a.Entries {
		if e.Path == "a.log" && (e.Decision != "exclude" || e.SHA256 != "" || e.Explanation.Groups[0].Provenance.Source != ".gitignore") {
			t.Fatalf("ignored content or provenance: %+v", e)
		}
	}
	names, err := os.ReadDir(root)
	if err != nil || len(names) != 3 {
		t.Fatal("preview wrote source files")
	}
	if err = os.Remove(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewRoot(context.Background(), root); domain.ErrorCode(err) != "RULE_SOURCE_UNAVAILABLE" {
		t.Fatalf("lost known source accepted: %v", err)
	}
}
func TestFailClosedSources(t *testing.T) {
	for name, change := range map[string]func(*config.Config, string){"missing-required": func(c *config.Config, _ string) { c.Filters.Groups[0].Sources[0].Required = true }, "unsupported-svn": func(c *config.Config, _ string) {
		g := &c.Filters.Groups[0]
		g.Dialect = "svn-ignore"
		g.Sources = []config.RuleSource{{Type: "recursive-vcs-property", Value: "svn:ignore", Required: true}}
	}, "invalid-rule": func(c *config.Config, root string) { write(t, root, ".gitignore", "[invalid") }, "unreadable-rule": func(c *config.Config, root string) {
		write(t, root, ".gitignore", "*.log")
		if err := os.Chmod(filepath.Join(root, ".gitignore"), 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".gitignore"), 0600) })
	}, "duplicate-resolution": func(c *config.Config, root string) {
		write(t, root, ".gitignore", "*.log")
		c.Filters.Groups[0].Sources = append(c.Filters.Groups[0].Sources, config.RuleSource{Type: "root-file", Value: ".gitignore"})
	}} {
		t.Run(name, func(t *testing.T) {
			if name == "unreadable-rule" && runtime.GOOS == "windows" {
				t.Skip("POSIX mode 000 is not a Windows read-permission fixture; ACL denial needs a Windows-specific fixture")
			}
			root := t.TempDir()
			c := config.Default(root)
			change(&c, root)
			if _, err := NewService().Scan(context.Background(), c); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}
