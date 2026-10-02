//go:build linux

package credentialvault

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func syntheticBusListener(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	// Abstract names avoid filesystem socket path limits when test caches and
	// temporary directories live under a long SSD path. No session bus is used.
	path := "\x00ledgesync-vault-test-" + rand.Text()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: path})
	if err != nil {
		t.Fatal("create synthetic bus listener")
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal("bound synthetic bus listener")
	}
	return listener, path
}

func TestUserBusPeerAllowsEffectiveUID(t *testing.T) {
	listener, path := syntheticBusListener(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	socket, err := dialUserBus(ctx, path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal("same-user synthetic bus rejected")
	}
	defer socket.Close()
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal("accept same-user connection")
	}
	defer peer.Close()
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal("bound same-user read")
	}
	if err := socket.Close(); err != nil {
		t.Fatal("close same-user connection")
	}
	var data [1]byte
	if n, err := peer.Read(data[:]); n != 0 || err != io.EOF {
		t.Fatal("peer verification wrote protocol bytes")
	}
}

func TestUserBusPeerRejectsUnexpectedUIDBeforeWriting(t *testing.T) {
	listener, path := syntheticBusListener(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Use the real kernel credentials of an owned listener, but require a
	// different UID. This exercises rejection without changing process identity.
	socket, err := dialUserBus(ctx, path, uint32(os.Geteuid())^1)
	if socket != nil {
		_ = socket.Close()
		t.Fatal("unexpected-user synthetic bus accepted")
	}
	if err != ErrUnavailable {
		t.Fatal("peer rejection was not redacted")
	}
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal("accept rejected connection")
	}
	defer peer.Close()
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal("bound rejected connection read")
	}
	var data [1]byte
	if n, err := peer.Read(data[:]); n != 0 || err != io.EOF {
		t.Fatal("rejected connection was not closed before protocol bytes")
	}
}

func TestUserBusPeerRejectsUninspectableSocket(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if err := verifyBusPeer(left, uint32(os.Geteuid())); err != ErrUnavailable {
		t.Fatal("non-Unix connection accepted or exposed inspection error")
	}
	listener, path := syntheticBusListener(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	socket, err := dialUserBus(ctx, path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal("connect synthetic bus")
	}
	peer, err := listener.AcceptUnix()
	if err != nil {
		_ = socket.Close()
		t.Fatal("accept synthetic connection")
	}
	defer peer.Close()
	if err := socket.Close(); err != nil {
		t.Fatal("close synthetic socket")
	}
	if err := verifyBusPeer(socket, uint32(os.Geteuid())); err != ErrUnavailable {
		t.Fatal("closed socket accepted or exposed inspection error")
	}
}

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
