// Package autostart registers MCPcell to start at user logon. Clients that
// were taken over depend on the daemon, so it should be running before they
// are.
//
// On Windows the per-user Run key starts mcpcelld.exe, the windowless build
// of the same program, so no console window appears at logon. With the
// graphical interface a second value starts mcpcell-tray.exe, the tray icon;
// the daemon itself has no user interface.
package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsupported = errors.New("自启动目前只支持 Windows")

const (
	valueName = "MCPcell"
	trayValue = "MCPcellTray"
)

// Command is what starts the daemon for exe (any program of the install).
func Command(exe string) (string, error) {
	gui := filepath.Join(filepath.Dir(exe), "mcpcelld.exe")
	if _, err := os.Stat(gui); err != nil {
		return "", errors.New("同目录下缺少 mcpcelld.exe（无窗口版守护进程），请先构建它")
	}
	return `"` + gui + `" daemon --log`, nil
}

// TrayCommand is what starts the tray icon for exe.
func TrayCommand(exe string) (string, error) {
	tray := filepath.Join(filepath.Dir(exe), "mcpcell-tray.exe")
	if _, err := os.Stat(tray); err != nil {
		return "", errors.New("同目录下缺少 mcpcell-tray.exe（托盘图标），请先构建它")
	}
	return `"` + tray + `"`, nil
}

type Status struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"` // the daemon starts at logon
	Command   string `json:"command,omitempty"`
	// Tray: the tray icon starts at logon too (graphical interface).
	Tray        bool   `json:"tray"`
	TrayCommand string `json:"trayCommand,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Ours reports whether the registered daemon entry starts a program next to
// exe: a switch of interface adjusts only its own installation's entries
// (and never a developer's while tests run from a temporary directory).
func (s Status) Ours(exe string) bool {
	if exe == "" {
		return false
	}
	dir := filepath.Clean(filepath.Dir(exe)) + string(filepath.Separator)
	return s.Enabled && strings.Contains(strings.ToLower(s.Command), strings.ToLower(dir))
}
