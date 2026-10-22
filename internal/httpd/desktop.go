package httpd

import (
	"encoding/json"
	"net/http"
)

// Desktop opens the window and starts or stops the tray icon, which are
// processes of their own (the daemon has no user interface); nil where
// there is no desktop.
type Desktop interface {
	// Show opens the window or brings it to the front.
	Show() error
	// Available reports whether the window can be shown (WebView2 present).
	Available() error
	Shortcuts() (desktop, startMenu bool)
	// SetShortcuts creates (true) or removes (false) each shortcut; nil
	// leaves it unchanged.
	SetShortcuts(desktop, startMenu *bool) error
	// TrayShown reports whether the tray icon process runs.
	TrayShown() bool
	// SetTray starts or stops the tray icon process (and with it the
	// window); the daemon is not affected.
	SetTray(on bool) error
}

type desktopStatus struct {
	Supported         bool   `json:"supported"`
	Webview           bool   `json:"webview"`
	WebviewError      string `json:"webviewError,omitempty"`
	DesktopShortcut   bool   `json:"desktopShortcut"`
	StartMenuShortcut bool   `json:"startMenuShortcut"`
}

func (s *Server) desktopStatus() desktopStatus {
	d := s.opts.Desktop
	if d == nil {
		return desktopStatus{}
	}
	st := desktopStatus{Supported: true, Webview: true}
	if err := d.Available(); err != nil {
		st.Webview, st.WebviewError = false, err.Error()
	}
	st.DesktopShortcut, st.StartMenuShortcut = d.Shortcuts()
	return st
}

func (s *Server) handleDesktop(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if s.opts.Desktop == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "桌面应用目前只支持 Windows"})
			return
		}
		var req struct {
			DesktopShortcut   *bool `json:"desktopShortcut"`
			StartMenuShortcut *bool `json:"startMenuShortcut"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不对"})
			return
		}
		if err := s.opts.Desktop.SetShortcuts(req.DesktopShortcut, req.StartMenuShortcut); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "快捷方式设置失败：" + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, s.desktopStatus())
}

func (s *Server) handleWindow(w http.ResponseWriter, r *http.Request) {
	if s.opts.Desktop == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "这个守护进程没有桌面界面"})
		return
	}
	if err := s.opts.Desktop.Show(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
