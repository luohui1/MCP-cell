// Package autostart registers the daemon to start at user logon. Clients
// that were taken over depend on it, so it should be running before they are.
//
// On Windows the per-user Run key starts mcpcelld.exe, the windowless build
// of the same program, so no console window appears at logon.
package autostart

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrUnsupported = errors.New("自启动目前只支持 Windows")

const valueName = "MCPcell"

// Command is what gets registered for exe (the running mcpcell binary).
func Command(exe string) (string, error) {
	gui := filepath.Join(filepath.Dir(exe), "mcpcelld.exe")
	if _, err := os.Stat(gui); err != nil {
		return "", errors.New("同目录下缺少 mcpcelld.exe（无窗口版守护进程），请先构建它")
	}
	return `"` + gui + `" daemon --log`, nil
}

type Status struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Command   string `json:"command,omitempty"`
	Error     string `json:"error,omitempty"`
}
