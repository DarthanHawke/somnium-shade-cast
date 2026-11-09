package api

import (
	"errors"
	"syscall"
)

func isConnectionResetError(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET)
}
