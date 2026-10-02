package driveauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAccountScopeLossStopsFurtherChecksWithoutRefreshing(t *testing.T) {
	for name, body := range map[string]string{
		"legacy":     `{"error":{"errors":[{"domain":"global","reason":"insufficientPermissions"}],"message":"synthetic-private-provider-text"}}`,
		"error-info": `{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"googleapis.com","reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}],"message":"synthetic-private-provider-text"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newBundledFixture(t)
			f.connect()
			f.aboutHandler = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, body)
			}
			st, err := f.service.Check(context.Background())
			if !errors.Is(err, ErrScope) || st.State != "reconnect_required" {
				t.Fatal("scope loss did not require reconnection")
			}
			encoded, _ := json.Marshal(st)
			if strings.Contains(string(encoded)+err.Error(), "synthetic-private-provider-text") {
				t.Fatal("provider text escaped the backend")
			}
			before := f.aboutCalls
			if _, err := f.service.Check(context.Background()); !errors.Is(err, ErrReconnect) || f.aboutCalls != before || f.tokenCalls != 1 {
				t.Fatal("scope loss caused a refresh or repeated provider request")
			}
			// The reconnect requirement survives a process restart in the vault.
			restarted, err := NewWithClient(f.store, nil, []byte(clientJSON))
			if err != nil {
				t.Fatal(err)
			}
			st, err = restarted.Status(context.Background())
			if err != nil || st.State != "reconnect_required" {
				t.Fatal("scope loss was not persisted")
			}
		})
	}
}

func TestOtherForbiddenResponsesDoNotInvalidateTheGrant(t *testing.T) {
	for name, body := range map[string]string{
		"quota":        `{"error":{"errors":[{"domain":"usageLimits","reason":"dailyLimitExceeded"}]}}`,
		"file-acl":     `{"error":{"errors":[{"domain":"global","reason":"insufficientFilePermissions"}]}}`,
		"disabled-api": `{"error":{"errors":[{"domain":"usageLimits","reason":"accessNotConfigured"}]}}`,
		"wrong-domain": `{"error":{"errors":[{"domain":"other","reason":"insufficientPermissions"}]}}`,
		"ambiguous":    `{"error":{"errors":[{"domain":"global","reason":"other","Reason":"insufficientPermissions"}]}}`,
		"malformed":    `{"error":`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newBundledFixture(t)
			f.connect()
			f.aboutHandler = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403); _, _ = io.WriteString(w, body) }
			st, err := f.service.Check(context.Background())
			if !errors.Is(err, ErrProvider) || st.State != "connected" || f.tokenCalls != 1 {
				t.Fatal("unrelated failure invalidated the grant or refreshed it")
			}
		})
	}
}
