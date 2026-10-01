package planner

import (
	"github.com/alexandroit/LedgeSync/internal/config"
	"github.com/alexandroit/LedgeSync/internal/domain"
	"reflect"
	"testing"
	"time"
)

func TestDeterministicPlanAndOwnership(t *testing.T) {
	c := config.Default("/fixture")
	hash := domain.HashBytes([]byte("fixture"))
	entries := []domain.Entry{{Path: "sub/new", Kind: "file", Decision: "include", Size: 7, SHA256: hash}, {Path: "excluded", Kind: "file", Decision: "exclude"}, {Path: "same", Kind: "file", Decision: "include", Size: 7, SHA256: hash}, {Path: "collision", Kind: "file", Decision: "include", Size: 7, SHA256: hash}}
	remote := domain.Inventory{Identity: "fake:test", Complete: true, Entries: []domain.RemoteEntry{{Path: "same", Kind: "file", ObjectID: "id1", Managed: true, VerifiedSHA256: hash}, {Path: "collision", Kind: "file", ObjectID: "id2", Managed: false, VerifiedSHA256: hash}, {Path: "remote-only", Kind: "file", ObjectID: "id3"}}}
	now := time.Unix(1000, 0)
	p, err := Build(c, hash, hash, entries, remote, now)
	if err != nil {
		t.Fatal(err)
	}
	reversed := append([]domain.Entry{}, entries...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	q, err := Build(c, hash, hash, reversed, remote, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, q) {
		t.Fatal("enumeration affected plan")
	}
	if err = p.ValidateDigest(); err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	ids := map[string]bool{}
	for _, o := range p.Operations {
		if ids[o.OperationID] {
			t.Fatal("duplicate operation ID")
		}
		ids[o.OperationID] = true
		types[o.RelativePath] = o.Type
		if o.Type == "trash-managed" || o.Type == "update-managed" {
			t.Fatal("destructive operation")
		}
		for _, dependency := range o.DependsOn {
			if !ids[dependency] {
				t.Fatal("dependency is not earlier in graph")
			}
		}
	}
	for path, want := range map[string]string{"sub": "create-directory", "sub/new": "upload-new", "same": "skip", "collision": "conflict", "excluded": "skip", "remote-only": "skip"} {
		if types[path] != want {
			t.Fatalf("%s: %s", path, types[path])
		}
	}
	if p.Summary.UploadBytes != 7 || p.Summary.TrashCount != 0 {
		t.Fatal(p.Summary)
	}
}
func TestDestinationFailureGates(t *testing.T) {
	c := config.Default("/fixture")
	hash := domain.HashBytes(nil)
	for name, inventory := range map[string]domain.Inventory{"incomplete": {Identity: "fake:test"}, "real-provider": {Identity: "google-drive:real", Complete: true}, "duplicate-path": {Identity: "fake:test", Complete: true, Entries: []domain.RemoteEntry{{Path: "a", ObjectID: "1"}, {Path: "a", ObjectID: "2"}}}, "duplicate-id": {Identity: "fake:test", Complete: true, Entries: []domain.RemoteEntry{{Path: "a", ObjectID: "1"}, {Path: "b", ObjectID: "1"}}}} {
		t.Run(name, func(t *testing.T) {
			if _, err := Build(c, hash, hash, nil, inventory, time.Unix(0, 0)); err == nil {
				t.Fatal("unsafe destination accepted")
			}
		})
	}
}
