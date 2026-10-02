package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

const syntheticClient = `{"installed":{"client_id":"123-cli-test.apps.googleusercontent.com","client_secret":"synthetic-client-marker","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost"]}}`

type statusVault struct {
	value         string
	err           error
	reads, writes int
}

func (v *statusVault) Get(string) (string, error) { v.reads++; return v.value, v.err }
func (v *statusVault) Set(string, string) error   { v.writes++; return errors.New("unexpected write") }
func (v *statusVault) Delete(string) error        { v.writes++; return errors.New("unexpected deletion") }

func TestCLIAuthStatusUsesSharedServiceWithoutWritesOrBrowser(t *testing.T) {
	for _, state := range []string{"connected", "reconnect_required", "client_changed", "storage_unavailable", "disconnected"} {
		t.Run(state, func(t *testing.T) {
			record := map[string]any{"version": 1,
				"client": map[string]string{"id": "123-cli-test.apps.googleusercontent.com", "secret": "synthetic-client-marker"},
				"credential": map[string]any{"refreshToken": "synthetic-refresh-marker", "reconnect": state == "reconnect_required",
					"account": map[string]string{"reference": "drive_" + strings.Repeat("a", 64), "displayName": "Test account", "email": "test@example.test"}}}
			if state == "client_changed" {
				record["client"].(map[string]string)["id"] = "456-other.apps.googleusercontent.com"
			}
			data, _ := json.Marshal(record)
			vault := &statusVault{value: string(data)}
			if state == "storage_unavailable" {
				vault.err = errors.New("synthetic-vault-secret-marker")
			}
			if state == "disconnected" {
				vault.value = ""
				vault.err = driveauth.ErrNotFound
			}
			browserCalls := 0
			service, err := driveauth.NewWithClient(vault, func(string) error { browserCalls++; return errors.New("unexpected browser") }, []byte(syntheticClient))
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := runAuthStatus(context.Background(), []string{"status"}, &out, &errOut, func() (authStatusService, error) { return service, nil })
			if vault.reads != 1 || vault.writes != 0 || browserCalls != 0 {
				t.Fatal("status caused side effects")
			}
			combined := out.String() + errOut.String()
			for _, marker := range []string{"synthetic-client-marker", "synthetic-refresh-marker", "synthetic-vault-secret-marker", "refreshToken", "access_token", "client_secret"} {
				if strings.Contains(combined, marker) {
					t.Fatal("secret reached CLI output")
				}
			}
			if state == "storage_unavailable" {
				if code != 6 || out.Len() != 0 || !strings.Contains(errOut.String(), "AUTH_STORAGE_UNAVAILABLE") {
					t.Fatal("vault failure did not fail closed")
				}
				return
			}
			var result struct {
				State          string `json:"state"`
				OnlineVerified *bool  `json:"onlineVerified"`
			}
			if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || result.State != state || result.OnlineVerified == nil || *result.OnlineVerified {
				t.Fatal("incorrect or misleading status")
			}
		})
	}
}

func TestCLIAuthRejectsMutationAndArgumentsBeforeCreatingService(t *testing.T) {
	for _, args := range [][]string{nil, {"connect"}, {"check"}, {"revoke"}, {"disconnect"}, {"status", "synthetic-private-argument"}} {
		var out, errOut bytes.Buffer
		code := runAuthStatus(context.Background(), args, &out, &errOut, func() (authStatusService, error) { t.Fatal("unexpected service construction"); return nil, nil })
		if code != 6 || out.Len() != 0 || !strings.Contains(errOut.String(), "AUTH_REQUIRED") || strings.Contains(errOut.String(), "synthetic-private-argument") {
			t.Fatal("headless action did not fail safely")
		}
	}
}

func TestCLIAuthCancellationAndBuildFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if runAuthStatus(ctx, []string{"status"}, &out, &errOut, func() (authStatusService, error) { t.Fatal("canceled request opened service"); return nil, nil }) != 130 {
		t.Fatal("wrong cancellation exit")
	}
	errOut.Reset()
	if runAuthStatus(context.Background(), []string{"status"}, &out, &errOut, func() (authStatusService, error) { return nil, errors.New("synthetic-build-secret-marker") }) != 6 || strings.Contains(errOut.String(), "synthetic-build-secret-marker") {
		t.Fatal("unsafe configuration error")
	}
}

func TestCLIStatusExternalLockContentionDoesNotReadVaultOrClaimConnection(t *testing.T) {
	vault := &statusVault{err: errors.New("must-not-read")}
	service, err := driveauth.NewWithClientAndLock(vault, nil, []byte(syntheticClient), func() (func(), error) { return nil, driveauth.ErrBusy })
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runAuthStatus(context.Background(), []string{"status"}, &out, &errOut, func() (authStatusService, error) { return service, nil })
	if code != 6 || out.Len() != 0 || vault.reads != 0 || !strings.Contains(errOut.String(), "AUTH_BUSY") {
		t.Fatalf("busy status unsafe: %d %s", code, &errOut)
	}
}
