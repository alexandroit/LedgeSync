package connections

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/alexandroit/LedgeSync/internal/domain"
	"github.com/alexandroit/LedgeSync/internal/driveauth"
)

func TestPublicErrorsAreTypedAndRedacted(t *testing.T) {
	home, _ := os.UserHomeDir()
	secret := "ya29.synthetic-access-token https://www.googleapis.com/upload/drive/v3/files?upload_id=session"
	cases := []struct {
		err  error
		code string
	}{
		{domain.Fail("SOURCE_CHANGED", "This file changed: %s/private/a.txt", home), "SOURCE_CHANGED"},
		{fmt.Errorf("wrapped: %w", driveauth.ErrReconnect), "AUTH_REQUIRED"},
		{fmt.Errorf("%s: %w", secret, driveauth.ErrBusy), "AUTH_BUSY"},
		{context.Canceled, "CANCELLED"},
		{errors.New(secret), "INTERNAL_ERROR"},
		{driveauth.ErrCallback, "AUTH_CALLBACK_INVALID"},
		{fmt.Errorf("%s: %w", secret, driveauth.CallbackFailure("repeated parameter")), "AUTH_CALLBACK_INVALID"},
	}
	for _, c := range cases {
		got := PublicError(c.err)
		if domain.ErrorCode(got) != c.code {
			t.Fatalf("%v mapped to %v", c.err, got)
		}
		message := got.Error()
		if strings.Contains(message, "ya29") || strings.Contains(message, "upload_id") || (len(home) > 1 && strings.Contains(message, home)) {
			t.Fatalf("public error leaked private data: %q", message)
		}
	}
	if PublicError(nil) != nil {
		t.Fatal("nil error became a failure")
	}
}

func TestCallbackFailureKeepsItsReason(t *testing.T) {
	got := PublicError(driveauth.CallbackFailure("unexpected issuer"))
	if domain.ErrorCode(got) != "AUTH_CALLBACK_INVALID" || !strings.Contains(got.Error(), "(unexpected issuer)") || !errors.Is(got, driveauth.ErrCallback) {
		t.Fatalf("callback failure lost its reason or cause: %v", got)
	}
}

func TestPublicErrorsStillMatchTheirCause(t *testing.T) {
	busy := domain.Fail("TRANSFER_BUSY", "Wait.")
	if !errors.Is(PublicError(busy), busy) || !errors.Is(PublicError(fmt.Errorf("x: %w", driveauth.ErrIdentity)), driveauth.ErrIdentity) {
		t.Fatal("public error lost its cause for errors.Is")
	}
}
