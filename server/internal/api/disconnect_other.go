//go:build !windows

package api

import (
	"errors"
	"syscall"
)

func isConnectionResetError(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}
