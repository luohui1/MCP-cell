// Package desktop is MCPcell's desktop shell on Windows: the tray icon
// (RunTray, the body of mcpcell-tray.exe) and a native window that shows the
// management page in WebView2 (RunWindow, the body of `mcpcelld window`).
//
// Both run in processes of their own; the daemon has no user interface. The
// tray only looks at the daemon and opens the window, so quitting it (or
// closing the window) leaves the daemon and every MCP it runs alone. Neither
// the daemon nor the tray ever loads WebView2: a closed window costs them
// nothing, and a browser crash cannot take the shared MCP servers down.
// WebView2 is driven through its COM interfaces directly, without
// WebView2Loader.dll. The tray must not link net/http or crypto/rand, which
// cost a Go process 32 MB on Windows (docs/architecture.md §13).
//
// Win32 work runs on locked OS threads.
package desktop

import (
	"errors"
	"log/slog"
)

var ErrUnsupported = errors.New("桌面应用目前只支持 Windows")

// TrayOptions configures RunTray.
type TrayOptions struct {
	// Home is the MCPcell home the tray belongs to.
	Home string
	// Exe is mcpcell-tray.exe; mcpcelld.exe next to it opens the window.
	Exe string
	// Status returns a few lines about the daemon for the menu; the first
	// is also the tooltip's second line. Called on hover and menu open.
	Status func() []string
	Log    *slog.Logger
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
