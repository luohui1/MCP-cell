package main

import (
	"os/exec"
	"syscall"
)

// detach starts the daemon outside this console: DETACHED_PROCESS |
// CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200 | 0x08000000}
}
