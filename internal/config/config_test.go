package config

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExampleContracts(t *testing.T) {
	for _, name := range []string{"default", "multiformat", "ordered", "vcs-policies"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile("../../examples/project." + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			c, err := Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			round, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Decode(bytes.NewReader(round)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStrictShape(t *testing.T) {
	raw, err := json.Marshal(Default("/fixture"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"missing-false": func(s string) string { return strings.Replace(s, `"enabled":false,`, "", 1) },
		"missing-zero":  func(s string) string { return strings.Replace(s, `,"maxDeleteCount":0`, "", 1) },
		"duplicate-key": func(s string) string {
			return strings.Replace(s, `"schemaVersion":"1.1"`, `"schemaVersion":"1.1","schemaVersion":"1.1"`, 1)
		},
		"unknown": func(s string) string {
			return strings.Replace(s, `"schemaVersion":"1.1"`, `"schemaVersion":"1.1","secret":"token"`, 1)
		},
		"wrong-case": func(s string) string { return strings.Replace(s, `"caseSensitive"`, `"CaseSensitive"`, 1) },
		"null":       func(s string) string { return strings.Replace(s, `"groups":[`, `"groups":null,"extra":[`, 1) },
		"fraction":   func(s string) string { return strings.Replace(s, `"priority":100`, `"priority":100.5`, 1) },
		"trailing":   func(s string) string { return s + ` {}` },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(mutate(string(raw)))); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
func TestSemanticSafety(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"future-schema":    func(c *Config) { c.SchemaVersion = "2.0" },
		"duplicates":       func(c *Config) { c.Filters.Groups = append(c.Filters.Groups, c.Filters.Groups[0]) },
		"unsafe-path":      func(c *Config) { c.Filters.Groups[0].Sources[0].Value = "../secrets" },
		"credential-file":  func(c *Config) { c.Filters.Groups[0].Sources[0].Value = "rclone.conf" },
		"nested-basename":  func(c *Config) { c.Filters.Groups[0].Sources[0].Value = "a/.gitignore" },
		"recursive-rclone": func(c *Config) { c.Filters.Groups[0].Dialect = "rclone-filter" },
		"fake-svn-file": func(c *Config) {
			c.Filters.Groups[0].Dialect = "svn-ignore"
			c.Filters.Groups[0].Sources[0].Value = ".svnignore"
		},
		"unsafe-automation": func(c *Config) {
			c.Automation.Enabled = true
			c.Automation.Trigger = "interval"
			c.Automation.IntervalSeconds = 60
		},
		"disabled-interval": func(c *Config) { c.Automation.IntervalSeconds = 60 },
		"mirror-preauth":    func(c *Config) { c.Sync.Mode = "mirror"; c.Sync.ApprovalPolicy = "copy-preauthorized" },
	} {
		t.Run(name, func(t *testing.T) {
			c := Default("/fixture")
			change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("unsafe config accepted")
			}
		})
	}
}
func FuzzConfigDecode(f *testing.F) {
	raw, _ := json.Marshal(Default("/fixture"))
	f.Add(raw)
	f.Add([]byte(`{"schemaVersion":null}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxConfigBytes {
			return
		}
		_, _ = Decode(bytes.NewReader(b))
	})
}
