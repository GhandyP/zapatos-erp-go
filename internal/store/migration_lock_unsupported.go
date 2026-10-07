//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package store

import (
	"context"
	"fmt"
	"runtime"
)

func acquireHostLocalMigrationLock(_ context.Context, path string) (func() error, error) {
	return nil, fmt.Errorf("host-local migration locks are unsupported on %s (lock file %q)", runtime.GOOS, path)
}
