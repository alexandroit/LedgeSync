package filters

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// The reference is an explicitly supplied, pinned development executable.
// It is never discovered or invoked by production application code.
func TestRcloneDifferential(t *testing.T) {
	binary := os.Getenv("LEDGESYNC_RCLONE_REFERENCE")
	if binary == "" {
		t.Skip("set LEDGESYNC_RCLONE_REFERENCE to the audited pinned local-only rclone lsf oracle")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("reference executable must be an absolute path")
	}
	cases := loadCorpus(t).RcloneCases
	cases = append(cases,
		fixture{ID: "RCL-013-directory-barrier", Dialect: "rclone-filter", RuleStreams: []string{"- /cache/\n+ /cache/keep.txt\n"}, Files: []string{"cache/keep.txt", "cache/drop.txt", "app/cache/keep.txt"}},
		fixture{ID: "RCL-014-inferred-parent", Dialect: "rclone-include", RuleStreams: []string{"/a/deep/*.txt\n"}, Files: []string{"a/deep/a.txt", "a/deep/a.bin", "a/elsewhere/b.txt", "b.txt"}},
		fixture{ID: "RCL-015-nested-alternatives", Dialect: "rclone-include", RuleStreams: []string{"/{a,{b,c}}/x.txt\n"}, Files: []string{"a/x.txt", "b/x.txt", "c/x.txt", "d/x.txt"}},
		fixture{ID: "RCL-016-unicode-escape", Dialect: "rclone-exclude", RuleStreams: []string{"café.txt\n\\[literal\\]\n"}, Files: []string{"café.txt", "d/café.txt", "cafe.txt", "[literal]"}},
		fixture{ID: "RCL-017-brace-path-fallback", Dialect: "rclone-include", RuleStreams: []string{"/{a/x,b/y}/file.txt\n"}, Files: []string{"a/x/file.txt", "b/y/file.txt", "a/z/file.txt"}},
		fixture{ID: "RCL-018-reset-directory-rules", Dialect: "rclone-filter", RuleStreams: []string{"- /cache/\n", "!\n+ /cache/a.txt\n- /**\n"}, Files: []string{"cache/a.txt", "cache/b.txt", "else.txt"}},
	)
	for _, f := range cases {
		t.Run(f.ID, func(t *testing.T) {
			e, root := fixtureEngine(t, f)
			home := t.TempDir()
			empty := filepath.Join(home, "empty.conf")
			if err := os.WriteFile(empty, nil, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"lsf", "--config", empty, "--recursive"}
			flag := "--filter-from"
			if f.Dialect == "rclone-include" {
				flag = "--include-from"
			} else if f.Dialect == "rclone-exclude" {
				flag = "--exclude-from"
			}
			for i := range f.RuleStreams {
				args = append(args, flag, filepath.Join(root, "rules-"+string(rune('a'+i))))
			}
			run := func(kind string) []string {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, append(append([]string{}, args...), kind, root)...)
				cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home, "RCLONE_CONFIG=" + empty, "LC_ALL=C"}
				output, err := cmd.Output()
				if err != nil {
					if exit, ok := err.(*exec.ExitError); ok {
						t.Fatalf("reference failed: %s", exit.Stderr)
					}
					t.Fatal(err)
				}
				paths := []string{}
				for _, p := range strings.Split(string(output), "\n") {
					if p != "" {
						paths = append(paths, strings.TrimSuffix(p, "/"))
					}
				}
				sort.Strings(paths)
				return paths
			}
			selected := map[string]bool{}
			for _, p := range run("--files-only") {
				selected[p] = true
			}
			referenceExcluded := []string{}
			for _, p := range f.Files {
				if !selected[p] {
					referenceExcluded = append(referenceExcluded, p)
				}
			}
			sort.Strings(referenceExcluded)
			actual := excluded(t, e, f.Files)
			if !reflect.DeepEqual(actual, referenceExcluded) {
				t.Fatalf("product %q; pinned rclone %q", actual, referenceExcluded)
			}
			allDirs := map[string]bool{}
			for _, file := range f.Files {
				parts := strings.Split(file, "/")
				for i := 1; i < len(parts); i++ {
					allDirs[strings.Join(parts[:i], "/")] = true
				}
			}
			referenceDirs := map[string]bool{}
			for _, p := range run("--dirs-only") {
				referenceDirs[p] = true
			}
			for p := range allDirs {
				x, err := e.Explain(context.Background(), p, "directory")
				if err != nil {
					t.Fatal(err)
				}
				if (x.Decision == "include") != referenceDirs[p] {
					t.Fatalf("directory %q: product=%s referenceIncluded=%t", p, x.Decision, referenceDirs[p])
				}
			}
		})
	}
}
