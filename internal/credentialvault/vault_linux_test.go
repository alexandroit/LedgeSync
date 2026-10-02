//go:build linux

package credentialvault

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSecretServiceRejectsNonLocalAddresses(t *testing.T) {
	for _, address := range []string{"", "autolaunch:", "tcp:host=localhost,port=1234", "unix:path=/tmp/a;tcp:host=example.test", "unix:path=relative", "unix:path=/tmp/%00foo", "unix:abstract=", "unix:path=/a,path=/b", "unix:dir=/tmp"} {
		if _, err := unixBusAddress(address); err != ErrUnavailable {
			t.Fatalf("accepted unsafe bus address %q", address)
		}
	}
	for address, want := range map[string]string{"unix:path=/run/user/1000/bus": "/run/user/1000/bus", "unix:path=/tmp/a%20b,guid=1234": "/tmp/a b", "unix:abstract=session": "\x00session"} {
		got, err := unixBusAddress(address)
		if err != nil || got != want {
			t.Fatalf("valid bus address rejected: %q", address)
		}
	}
}

func TestMissingSessionBusFailsClosed(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "absent-bus"))
	start := time.Now()
	store := New()
	if _, err := store.Get("synthetic"); err != ErrUnavailable {
		t.Fatal("missing bus did not fail closed")
	}
	if err := store.Set("synthetic", "synthetic"); err != ErrUnavailable {
		t.Fatal("missing bus accepted secret")
	}
	if err := store.Delete("synthetic"); err != ErrUnavailable {
		t.Fatal("missing bus accepted deletion")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("missing bus did not fail promptly")
	}
}
