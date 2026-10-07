//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const migrationLockPollInterval = 25 * time.Millisecond

func acquireHostLocalMigrationLock(ctx context.Context, path string) (func() error, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open migration lock file %q: %w", path, err)
	}

	closeOnError := func(cause error) (func() error, error) {
		if closeErr := file.Close(); closeErr != nil {
			cause = fmt.Errorf("%w (close migration lock file: %v)", cause, closeErr)
		}
		return nil, cause
	}

	for {
		if err := ctx.Err(); err != nil {
			return closeOnError(fmt.Errorf("wait for migration lock %q: %w", path, err))
		}

		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				closeErr := file.Close()
				cause := fmt.Errorf("acquire migration lock %q: %w", path, ctxErr)
				if unlockErr != nil || closeErr != nil {
					cause = fmt.Errorf("%w (release canceled migration lock: %v)", cause, errors.Join(unlockErr, closeErr))
				}
				return nil, cause
			}
			return func() error {
				unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				closeErr := file.Close()
				return errors.Join(unlockErr, closeErr)
			}, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return closeOnError(fmt.Errorf("lock migration file %q: %w", path, err))
		}

		timer := time.NewTimer(migrationLockPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return closeOnError(fmt.Errorf("wait for migration lock %q: %w", path, ctx.Err()))
		case <-timer.C:
		}
	}
}
