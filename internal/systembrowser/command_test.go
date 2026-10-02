//go:build darwin || linux

package systembrowser

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestBrowserCommandHelper(t *testing.T) {
	// The parent starts only this test with a synthetic marker argument.
	args := os.Args
	if len(args) < 2 || args[len(args)-1] != "browser-launch-helper" {
		return
	}
	fmt.Fprintln(os.Stdout, "synthetic-authorization-output")
	fmt.Fprintln(os.Stderr, "synthetic-authorization-output")
	time.Sleep(10 * time.Second)
	os.Exit(0)
}
func TestLauncherProcessDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("test executable unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = command(ctx, executable, "-test.run=^TestBrowserCommandHelper$", "--", "browser-launch-helper")
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("launcher process escaped deadline")
	}
}
