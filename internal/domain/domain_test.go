package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPlanExampleCanonicalDigest(t *testing.T) {
	b, err := os.ReadFile("../../examples/plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Plan
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if err = p.ValidateDigest(); err != nil {
		t.Fatal(err)
	}
	p.Operations[0].Explanation += " tampered"
	if err = p.ValidateDigest(); err == nil {
		t.Fatal("tampering accepted")
	}
}
func TestCanonicalUnicode(t *testing.T) {
	b, err := CanonicalJSON(map[string]any{"z": "<&>\u2028é", "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{\"a\":1,\"z\":\"<&>\u2028é\"}" {
		t.Fatalf("unexpected canonical bytes: %s", b)
	}
}
func TestPathBoundary(t *testing.T) {
	for _, p := range []string{"", "/absolute", "../escape", "a/../b", "a//b", "a/./b", `C:\source`, `a\b`, "x\x00y", "x:y", string([]byte{255})} {
		if err := ValidatePath(p); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	for _, p := range []string{"a", "a/b", "café/猫.txt", "a trailing "} {
		if err := ValidatePath(p); err != nil {
			t.Fatal(err)
		}
	}
}
func FuzzPath(f *testing.F) {
	for _, s := range []string{"a/b", "../x", `C:\x`, "café"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		err := ValidatePath(s)
		if err == nil && (strings.HasPrefix(s, "/") || strings.Contains(s, "\x00")) {
			t.Fatal("unsafe path accepted")
		}
	})
}
