//go:build !windows

package skills

import (
	"errors"
	"syscall"
)

func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
