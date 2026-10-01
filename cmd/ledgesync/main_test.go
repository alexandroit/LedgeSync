package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/alexandroit/LedgeSync/internal/app"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectStrictJSON(t *testing.T) {
	b, err := os.ReadFile("../../examples/plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"valid": b, "trailing": append(append([]byte{}, b...), []byte(" {}")...), "duplicate": []byte(strings.Replace(string(b), `"planId":`, `"planId":"duplicate","planId":`, 1))} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(p, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var out, errors bytes.Buffer
			code := run(context.Background(), []string{"plan", "inspect", "--plan", p}, &out, &errors)
			if name == "valid" && code != 0 {
				t.Fatalf("valid plan failed: %s", &errors)
			}
			if name != "valid" && code == 0 {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
}
func TestPlanOutputCaseAliasGuard(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "SourceCase")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "sourcecase")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	var out, errors bytes.Buffer
	code := run(context.Background(), []string{"plan", "--root", root, "--output", filepath.Join(alias, "plan.json")}, &out, &errors)
	if code != 6 {
		t.Fatalf("case alias write accepted: %d %s", code, &errors)
	}
	if _, err := os.Stat(filepath.Join(root, "plan.json")); !os.IsNotExist(err) {
		t.Fatal("source was written")
	}
}

func TestCLIUsesSharedPreview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errors bytes.Buffer
	code := run(context.Background(), []string{"browse", "--root", root, "--json"}, &out, &errors)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, &errors)
	}
	var p app.Preview
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Offline || len(p.Entries) != 1 || p.Entries[0].Path != "a.txt" {
		t.Fatal(p)
	}
	if err := p.Plan.ValidateDigest(); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"apply", "--plan", "anything"}, &out, &errors); code != 6 {
		t.Fatalf("apply exit %d", code)
	}
}
func TestPlanOutputSourceGuard(t *testing.T) {
	root := t.TempDir()
	var out, errors bytes.Buffer
	code := run(context.Background(), []string{"plan", "--root", root, "--output", filepath.Join(root, "plan.json")}, &out, &errors)
	if code != 6 {
		t.Fatalf("source write exit %d %s", code, &errors)
	}
	if _, err := os.Stat(filepath.Join(root, "plan.json")); !os.IsNotExist(err) {
		t.Fatal("plan written inside source")
	}
}
