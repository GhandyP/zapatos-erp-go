//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package app

import "syscall"

func setPrivateFileCreationMask() error {
	syscall.Umask(0o077)
	return nil
}
