package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// WithMigrationLock runs callback while holding an exclusive host-local
// inter-process lock associated with lockFilePath. The lock file's parent
// directory must already exist. Cancellation is honored while waiting.
func WithMigrationLock(ctx context.Context, lockFilePath string, callback func() error) (err error) {
	if ctx == nil {
		return errors.New("migration lock context must not be nil")
	}
	if strings.TrimSpace(lockFilePath) == "" || strings.IndexByte(lockFilePath, 0) >= 0 {
		return errors.New("migration lock file path must not be empty or contain NUL")
	}
	if callback == nil {
		return errors.New("migration lock callback must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}

	release, err := acquireHostLocalMigrationLock(ctx, lockFilePath)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			if err == nil {
				err = fmt.Errorf("release migration lock: %w", releaseErr)
			} else {
				err = fmt.Errorf("%w (release migration lock: %v)", err, releaseErr)
			}
		}
	}()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	return callback()
}
