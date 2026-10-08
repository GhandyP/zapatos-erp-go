//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package app

import "errors"

func setPrivateFileCreationMask() error {
	return errors.New("private SQLite file modes are unsupported on this platform")
}
