package skills

import (
	"errors"

	"golang.org/x/sys/windows"
)

func crossDevice(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }
