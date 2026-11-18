//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package storage

import (
	"errors"
	"os"
)

func lockFile(_ *os.File) error {
	return errors.New("file locking is unavailable on this platform")
}
