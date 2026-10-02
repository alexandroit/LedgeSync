package transferstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestProcessLockSubprocess(t *testing.T) {
	directory := os.Getenv("LEDGESYNC_TEST_PROCESS_LOCK_DIR")
	if directory == "" {
		return
	}
	release, err := AcquireProcessLock(directory)
	if os.Getenv("LEDGESYNC_TEST_PROCESS_LOCK_BUSY") == "1" {
		if !errors.Is(err, ErrLockBusy) || release != nil {
			t.Fatal("child did not observe the parent's kernel lock")
		}
		return
	}
	if err != nil {
		t.Fatal("child could not acquire released lock", err)
	}
	release()
}

func TestProcessLockContendsAcrossProcessesWithoutOpeningSQLite(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "auth-lock")
	release, err := AcquireProcessLock(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	child := func(busy bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessLockSubprocess$")
		cmd.Env = append(os.Environ(), "LEDGESYNC_TEST_PROCESS_LOCK_DIR="+directory, "LEDGESYNC_TEST_PROCESS_LOCK_BUSY=0")
		if busy {
			cmd.Env[len(cmd.Env)-1] = "LEDGESYNC_TEST_PROCESS_LOCK_BUSY=1"
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child lock check: %v\n%s", err, out)
		}
	}
	child(true)
	release()
	release() // A duplicate release must not unlock a later holder's descriptor.
	child(false)
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "operation.lock" {
		t.Fatal("lock helper created unexpected state or SQLite files")
	}
	st, err := os.Stat(filepath.Join(directory, "operation.lock"))
	if err != nil || st.Size() != 0 {
		t.Fatal("lock helper persisted data")
	}
}

func TestProcessLockRejectsUnsafePaths(t *testing.T) {
	t.Run("not-directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		if release, err := AcquireProcessLock(filepath.Join(path, "auth-lock")); release != nil || !errors.Is(err, ErrLockUnavailable) {
			t.Fatal("file ancestor was accepted")
		}
	})
	t.Run("directory-symlink", func(t *testing.T) {
		base := t.TempDir()
		target, link := filepath.Join(base, "target"), filepath.Join(base, "link")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			if runtime.GOOS == "windows" {
				t.Skip("symlink creation requires an enabled native capability")
			}
			t.Fatal(err)
		}
		if release, err := AcquireProcessLock(filepath.Join(link, "auth-lock")); release != nil || !errors.Is(err, ErrLockUnavailable) {
			t.Fatal("symlink ancestor was accepted")
		}
		entries, _ := os.ReadDir(target)
		if len(entries) != 0 {
			t.Fatal("unsafe path was mutated")
		}
	})
	t.Run("lock-hardlink", func(t *testing.T) {
		base := t.TempDir()
		directory := filepath.Join(base, "auth-lock")
		release, err := AcquireProcessLock(directory)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if err := os.Link(filepath.Join(directory, "operation.lock"), filepath.Join(base, "alias")); err != nil {
			t.Fatal(err)
		}
		if release, err := AcquireProcessLock(directory); release != nil || !errors.Is(err, ErrLockUnavailable) {
			t.Fatal("hardlinked lock was accepted")
		}
	})
	t.Run("broad-posix-access", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX modes are not a Windows security boundary")
		}
		directory := filepath.Join(t.TempDir(), "auth-lock")
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if release, err := AcquireProcessLock(directory); release != nil || !errors.Is(err, ErrLockUnavailable) {
			t.Fatal("broad directory permissions were accepted")
		}
		st, _ := os.Stat(directory)
		if st.Mode().Perm() != 0755 {
			t.Fatal("existing permissions were silently changed")
		}
	})
}
