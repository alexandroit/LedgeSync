package systembrowser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOnlyGoogleAuthorizationAndSetupPagesAreAccepted(t *testing.T) {
	auth := "https://accounts.google.com/o/oauth2/v2/auth?state=synthetic&client_id=fixture"
	for _, raw := range []string{SetupURL, auth} {
		called := false
		if err := open(context.Background(), raw, func(_ context.Context, got string) error {
			called = true
			if got != raw {
				t.Fatal("URL changed")
			}
			return nil
		}); err != nil || !called {
			t.Fatal("valid fixed destination was rejected")
		}
	}
	for _, raw := range []string{
		"https://example.test/", "http://accounts.google.com/o/oauth2/v2/auth?state=fixture",
		"https://accounts.google.com.attacker.invalid/o/oauth2/v2/auth?state=fixture",
		"https://attacker@accounts.google.com/o/oauth2/v2/auth?state=fixture",
		"https://accounts.google.com:443/o/oauth2/v2/auth?state=fixture",
		"https://accounts.google.com/o/oauth2/v2/auth?state=fixture#secret",
		"https://accounts.google.com/o/oauth2/v2/auth?state=fixture#",
		"https://accounts.google.com/o/oauth2/v2/auth", "https://accounts.google.com/o/oauth2/v2/%61uth?state=fixture",
		"https://accounts.google.com/other?state=fixture", SetupURL + "?redirect=attacker", SetupURL + "/",
		"file:///tmp/client.json", "javascript:alert(1)", "--help", strings.Repeat("a", 8193),
	} {
		if err := open(context.Background(), raw, func(context.Context, string) error { t.Fatal("untrusted destination reached launcher"); return nil }); !errors.Is(err, ErrURL) {
			t.Fatal("untrusted destination accepted")
		}
	}
}
func TestLauncherErrorsAreRedactedAndContextIsBounded(t *testing.T) {
	if err := open(context.Background(), SetupURL, func(context.Context, string) error { return errors.New("private-command-output") }); !errors.Is(err, ErrLaunch) || strings.Contains(err.Error(), "private-command-output") {
		t.Fatal("launcher error leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := open(ctx, SetupURL, func(context.Context, string) error { t.Fatal("canceled launcher ran"); return nil }); !errors.Is(err, ErrLaunch) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := open(ctx, SetupURL, func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }); !errors.Is(err, ErrLaunch) || time.Since(start) > time.Second {
		t.Fatal("launch deadline not honored")
	}
}
