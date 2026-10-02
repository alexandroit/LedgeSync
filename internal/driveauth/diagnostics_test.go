package driveauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise real service paths in a child test process so stdout, stderr and the
// standard logger are all observed without redirecting another test's streams.
func TestOAuthDiagnosticsDoNotExposeCredentials(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("test executable unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestOAuthDiagnosticsHelper$", "-test.v")
	command.Env = append(os.Environ(), "LEDGESYNC_TEST_AUTH_DIAGNOSTICS_CHILD=1")
	output, runErr := command.CombinedOutput()
	for _, marker := range diagnosticSecrets() {
		if strings.Contains(string(output), marker) {
			t.Fatal("credential fixture appeared in process diagnostics")
		}
	}
	if runErr != nil || !strings.Contains(string(output), "--- PASS: TestOAuthDiagnosticsHelper") {
		t.Fatal("isolated diagnostic checks failed")
	}
}

func diagnosticSecrets() []string {
	return []string{"fake-client-secret", "fake-access-token", "fake-refresh-token", "fake-code", "fake-account-1", "synthetic-private-provider-detail"}
}

func TestOAuthDiagnosticsHelper(t *testing.T) {
	if os.Getenv("LEDGESYNC_TEST_AUTH_DIAGNOSTICS_CHILD") != "1" {
		return
	}
	check := func(status Status, err error) {
		t.Helper()
		data, marshalErr := json.Marshal(status)
		if marshalErr != nil {
			t.Fatal("status serialization failed")
		}
		text := string(data)
		if err != nil {
			text += err.Error()
		}
		for _, marker := range diagnosticSecrets() {
			if strings.Contains(text, marker) {
				t.Fatal("credential fixture escaped the backend status boundary")
			}
		}
	}
	t.Run("success-refresh-disconnect", func(t *testing.T) {
		f := newBundledFixture(t)
		check(f.service.Connect(context.Background()))
		f.service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		check(f.service.Check(context.Background()))
		check(f.service.Disconnect(context.Background()))
	})
	t.Run("provider-response-and-revoked-grant", func(t *testing.T) {
		f := newBundledFixture(t)
		check(f.service.Connect(context.Background()))
		f.service.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		f.mu.Lock()
		f.tokenStatus = http.StatusBadRequest
		f.tokenReply = `{"error":"invalid_grant","error_description":"synthetic-private-provider-detail fake-refresh-token"}`
		f.mu.Unlock()
		check(f.service.Check(context.Background()))
	})
	t.Run("launcher-and-vault-errors", func(t *testing.T) {
		f := newBundledFixture(t)
		f.service.openURL = func(string) error { return errors.New("synthetic-private-provider-detail") }
		check(f.service.Connect(context.Background()))
		f.store.mu.Lock()
		f.store.getErr = errors.New("synthetic-private-provider-detail fake-refresh-token")
		f.store.mu.Unlock()
		check(f.service.Status(context.Background()))
		check(f.service.Check(context.Background()))
	})
	t.Run("revocation-error", func(t *testing.T) {
		f := newBundledFixture(t)
		status := f.connect()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "synthetic-private-provider-detail fake-refresh-token")
		}))
		defer server.Close()
		f.service.revokeURL = server.URL
		check(f.service.Revoke(context.Background(), status.Account.Reference, false))
		check(f.service.Revoke(context.Background(), status.Account.Reference, true))
	})
}
