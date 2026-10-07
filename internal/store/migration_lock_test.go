package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func requireMigrationLockSupport(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "linux", "netbsd", "openbsd":
	default:
		t.Skipf("flock-backed migration locks are unsupported on %s", runtime.GOOS)
	}
}

func TestWithMigrationLockSerializesCallbacksAndCreatesPrivateLockFile(t *testing.T) {
	requireMigrationLockSupport(t)
	path := filepath.Join(t.TempDir(), "migration.lock")
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(firstRelease) }) }
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithMigrationLock(context.Background(), path, func() error {
			close(firstEntered)
			<-firstRelease
			return nil
		})
	}()
	<-firstEntered
	defer releaseFirst()

	secondStarted := make(chan struct{})
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- WithMigrationLock(context.Background(), path, func() error {
			close(secondEntered)
			return nil
		})
	}()
	<-secondStarted
	select {
	case <-secondEntered:
		t.Fatal("second callback entered while the first held the lock")
	case <-time.After(100 * time.Millisecond):
	}

	releaseFirst()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first WithMigrationLock returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first callback did not finish")
	}
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second callback did not enter after the first released the lock")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second WithMigrationLock returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second callback did not finish")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat migration lock file: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("migration lock permissions = %04o, want private permissions no broader than 0600", info.Mode().Perm())
	}
}

func TestWithMigrationLockSerializesSubprocesses(t *testing.T) {
	requireMigrationLockSupport(t)
	const modeEnv = "GENTLE_STORE_MIGRATION_LOCK_TEST_MODE"
	if mode := os.Getenv(modeEnv); mode != "" {
		migrationLockSubprocessHelper(t, mode)
		return
	}

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "migration.lock")
	firstEntered := filepath.Join(dir, "first-entered")
	secondStarted := filepath.Join(dir, "second-started")
	secondEntered := filepath.Join(dir, "second-entered")
	releaseFirst := filepath.Join(dir, "release-first")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	startChild := func(mode string) (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		output := new(bytes.Buffer)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWithMigrationLockSerializesSubprocesses$")
		cmd.Env = []string{
			modeEnv + "=" + mode,
			"GENTLE_STORE_MIGRATION_LOCK_TEST_PATH=" + lockPath,
			"GENTLE_STORE_MIGRATION_LOCK_TEST_FIRST=" + firstEntered,
			"GENTLE_STORE_MIGRATION_LOCK_TEST_SECOND_STARTED=" + secondStarted,
			"GENTLE_STORE_MIGRATION_LOCK_TEST_SECOND=" + secondEntered,
			"GENTLE_STORE_MIGRATION_LOCK_TEST_RELEASE=" + releaseFirst,
		}
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			t.Fatalf("start %s lock subprocess: %v", mode, err)
		}
		return cmd, output
	}
	waitForMarker := func(path string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); err == nil {
				return
			} else if !os.IsNotExist(err) {
				t.Fatalf("inspect subprocess marker %q: %v", path, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for subprocess marker %q", path)
	}
	waitChild := func(mode string, cmd *exec.Cmd, output *bytes.Buffer) {
		t.Helper()
		if cmd.ProcessState != nil {
			return
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s lock subprocess failed: %v\n%s", mode, err, output.String())
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s lock subprocess\n%s", mode, output.String())
		}
	}

	first, firstOutput := startChild("first")
	released := false
	defer func() {
		if !released {
			_ = os.WriteFile(releaseFirst, []byte("release"), 0o600)
		}
		waitChild("first", first, firstOutput)
	}()
	waitForMarker(firstEntered)

	second, secondOutput := startChild("second")
	defer func() {
		if !released {
			_ = os.WriteFile(releaseFirst, []byte("release"), 0o600)
		}
		waitChild("second", second, secondOutput)
	}()
	waitForMarker(secondStarted)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(secondEntered); err == nil {
			t.Fatal("second process entered its callback while the first process held the migration lock")
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect second callback marker: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := os.WriteFile(releaseFirst, []byte("release"), 0o600); err != nil {
		t.Fatalf("release first migration-lock process: %v", err)
	}
	released = true
	waitForMarker(secondEntered)
	waitChild("first", first, firstOutput)
	waitChild("second", second, secondOutput)
}

func migrationLockSubprocessHelper(t *testing.T, mode string) {
	t.Helper()
	lockPath := os.Getenv("GENTLE_STORE_MIGRATION_LOCK_TEST_PATH")
	markerPath := os.Getenv("GENTLE_STORE_MIGRATION_LOCK_TEST_FIRST")
	if mode == "second" {
		markerPath = os.Getenv("GENTLE_STORE_MIGRATION_LOCK_TEST_SECOND")
		if err := os.WriteFile(os.Getenv("GENTLE_STORE_MIGRATION_LOCK_TEST_SECOND_STARTED"), []byte("started"), 0o600); err != nil {
			t.Fatalf("write second-started marker: %v", err)
		}
	}
	callback := func() error {
		if err := os.WriteFile(markerPath, []byte("entered"), 0o600); err != nil {
			return err
		}
		if mode == "first" {
			releasePath := os.Getenv("GENTLE_STORE_MIGRATION_LOCK_TEST_RELEASE")
			for {
				if _, err := os.Stat(releasePath); err == nil {
					return nil
				} else if !os.IsNotExist(err) {
					return err
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		return nil
	}
	if err := WithMigrationLock(context.Background(), lockPath, callback); err != nil {
		t.Fatalf("WithMigrationLock in %s subprocess: %v", mode, err)
	}
}

func TestWithMigrationLockHonorsCancellationWhileWaiting(t *testing.T) {
	requireMigrationLockSupport(t)
	path := filepath.Join(t.TempDir(), "migration.lock")
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(firstRelease) }) }
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- WithMigrationLock(context.Background(), path, func() error {
			close(firstEntered)
			<-firstRelease
			return nil
		})
	}()
	<-firstEntered
	defer releaseFirst()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	callbackEntered := make(chan struct{})
	go func() {
		close(secondStarted)
		secondDone <- WithMigrationLock(ctx, path, func() error {
			close(callbackEntered)
			return nil
		})
	}()
	<-secondStarted
	time.Sleep(75 * time.Millisecond)
	cancel()

	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second WithMigrationLock error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting WithMigrationLock did not honor cancellation")
	}
	select {
	case <-callbackEntered:
		t.Fatal("callback entered after its context was canceled while waiting")
	default:
	}
	releaseFirst()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first WithMigrationLock returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first callback did not finish")
	}
}

func TestWithMigrationLockReleasesAfterCallbackError(t *testing.T) {
	requireMigrationLockSupport(t)
	path := filepath.Join(t.TempDir(), "migration.lock")
	callbackErr := errors.New("callback failed")
	if err := WithMigrationLock(context.Background(), path, func() error { return callbackErr }); !errors.Is(err, callbackErr) {
		t.Fatalf("WithMigrationLock error = %v, want callback error", err)
	}

	entered := false
	if err := WithMigrationLock(context.Background(), path, func() error {
		entered = true
		return nil
	}); err != nil {
		t.Fatalf("WithMigrationLock after callback error returned error: %v", err)
	}
	if !entered {
		t.Fatal("callback did not run after previous callback error")
	}
}

func TestWithMigrationLockRejectsInvalidInputsAndMissingParent(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  context.Context
		path string
		fn   func() error
	}{
		{name: "nil context", path: filepath.Join(t.TempDir(), "lock"), fn: func() error { return nil }},
		{name: "empty path", ctx: context.Background(), fn: func() error { return nil }},
		{name: "nil callback", ctx: context.Background(), path: filepath.Join(t.TempDir(), "lock")},
		{name: "missing parent", ctx: context.Background(), path: filepath.Join(t.TempDir(), "missing", "lock"), fn: func() error { return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := WithMigrationLock(test.ctx, test.path, test.fn); err == nil {
				t.Fatal("WithMigrationLock returned nil error for invalid input")
			}
		})
	}
}
