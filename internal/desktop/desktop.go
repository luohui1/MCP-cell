// Package desktop is MCPcell's desktop shell on Windows: a notification-area
// icon owned by the daemon (Start), and a native window that shows the
// management page in WebView2 (RunWindow).
//
// The window runs in a process of its own that the daemon starts on demand
// and that exits when the window closes. The WebView2 runtime therefore never
// loads into the daemon: a closed window costs it nothing, and a browser
// crash cannot take the shared MCP servers down. WebView2 is driven through
// its COM interfaces directly, without WebView2Loader.dll.
//
// Win32 work runs on locked OS threads; App's methods may be called from any
// goroutine.
package desktop

import (
	"errors"
	"log/slog"
)

var ErrUnsupported = errors.New("桌面应用目前只支持 Windows")

// Options configures the tray icon and the window.
type Options struct {
	Home string
	// Exe is the running binary; the window process, autostart and
	// shortcuts use mcpcelld.exe next to it.
	Exe string
	// Tray shows the notification-area icon.
	Tray bool
	// LoginURL returns a fresh one-time login link for every window open.
	LoginURL func() string
	// Status is the second tooltip line, e.g. "2 个实例 · 180 MB".
	Status func() string
	// Quit is called after the user confirms 退出 in the tray menu.
	Quit func()
	Log  *slog.Logger
}

// WindowOptions configures RunWindow.
type WindowOptions struct {
	// Home is ~/.mcpcell: webview/ holds the WebView2 profile and
	// window.json the last window bounds.
	Home string
	// URL is the first page, a one-time login link from the daemon.
	URL string
	Log *slog.Logger
}

// ShortcutName is the file name of the desktop and Start menu shortcuts.
const ShortcutName = "MCPcell.lnk"
