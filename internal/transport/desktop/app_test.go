package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alexandroit/LedgeSync/internal/app"
	"github.com/alexandroit/LedgeSync/internal/config"
)

func TestNativeFolderBridgeUsesSharedCoreAndRefreshFailsClosed(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{".gitignore": "*.log\n", "notes.md": "hello", "debug.log": "excluded"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := app.NewService()
	bridge := New(service, func() (string, error) { return root, nil }, nil)
	actual, err := bridge.OpenFolder()
	if err != nil {
		t.Fatal(err)
	}
	want, err := app.NewService().PreviewRoot(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual.Entries, want.Entries) || !reflect.DeepEqual(actual.Plan.Operations, want.Plan.Operations) {
		t.Fatal("desktop entries, explanations, or operations differ from shared service")
	}
	if actual.Plan.Summary.TrashCount != 0 || !actual.Offline {
		t.Fatal("desktop preview must be offline and non-destructive")
	}
	if err := os.Remove(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if result, err := bridge.Refresh(); err == nil || result != nil || !strings.Contains(err.Error(), "RULE_SOURCE_UNAVAILABLE") {
		t.Fatalf("missing observed rule must block refresh, got %v, %v", result, err)
	}
}

func TestConfigurationPickerAndCancelledPicker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config.Default(root))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	bridge := New(app.NewService(), func() (string, error) { return "", nil }, func() (string, error) { return configPath, nil })
	if result, err := bridge.OpenFolder(); result != nil || err != nil {
		t.Fatalf("cancelled picker should leave selection unchanged: %v %v", result, err)
	}
	result, err := bridge.OpenConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Path != "file.txt" {
		t.Fatalf("wrong configured root: %#v", result.Entries)
	}
	if _, err := bridge.Refresh(); err != nil {
		t.Fatal(err)
	}
}

type blockedService struct{ started chan struct{} }

func (s *blockedService) PreviewRoot(ctx context.Context, _ string) (app.Preview, error) {
	close(s.started)
	<-ctx.Done()
	return app.Preview{}, ctx.Err()
}
func (s *blockedService) Preview(ctx context.Context, path string) (app.Preview, error) {
	return s.PreviewRoot(ctx, path)
}

func TestCancellationAndConcurrentRequests(t *testing.T) {
	service := &blockedService{started: make(chan struct{})}
	bridge := New(service, func() (string, error) { return "/fixture", nil }, nil)
	done := make(chan error, 1)
	go func() { _, err := bridge.OpenFolder(); done <- err }()
	<-service.started
	if _, err := bridge.OpenFolder(); err == nil || !strings.Contains(err.Error(), "SCAN_BUSY") {
		t.Fatalf("concurrent scan should fail, got %v", err)
	}
	bridge.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel did not reach shared core: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not finish")
	}
	if _, err := bridge.Refresh(); err == nil || !strings.Contains(err.Error(), "ROOT_REQUIRED") {
		t.Fatalf("cancelled root must not become active, got %v", err)
	}
}
