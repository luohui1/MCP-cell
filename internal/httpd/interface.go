package httpd

import (
	"encoding/json"
	"net/http"

	"github.com/luohui1/MCP-cell/internal/autostart"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// The interface choice, GUI or command line, only decides whether the tray
// icon (mcpcell-tray.exe, a process of its own) runs and starts at logon.
// Switching saves the choice and starts or stops the tray at once; the
// daemon and the MCPs it runs are not affected either way.

type interfaceStatus struct {
	Interface string `json:"interface"` // gui | cli
	// Tray: the tray icon process runs now.
	Tray bool `json:"tray"`
	// TraySupported: there is a desktop to show one on (Windows).
	TraySupported bool `json:"traySupported"`
	// Error is why the tray did not start or stop, if it did not.
	Error string `json:"error,omitempty"`
}

func (s *Server) interfaceStatus() (interfaceStatus, error) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return interfaceStatus{}, err
	}
	st := interfaceStatus{Interface: registry.InterfaceGUI, TraySupported: s.opts.Desktop != nil}
	if cfg.CLI() {
		st.Interface = registry.InterfaceCLI
	}
	st.Tray = st.TraySupported && s.opts.Desktop.TrayShown()
	return st, nil
}

func (s *Server) handleInterface(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct{ Interface string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
			return
		}
		mode, err := registry.ParseInterface(req.Interface)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.editConfig(func(cfg *registry.Config) error { cfg.Interface = mode; return nil }); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var trayErr error
		if s.opts.Desktop != nil {
			gui := mode == registry.InterfaceGUI
			trayErr = s.opts.Desktop.SetTray(gui)
			if a := autostart.Get(); a.Ours(s.opts.Exe) && a.Tray != gui {
				if err := autostart.SetTray(s.opts.Exe, gui); err != nil && trayErr == nil {
					trayErr = err
				}
			}
		}
		s.opts.Log.Info("interface changed", "interface", mode)
		st, err := s.interfaceStatus()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if trayErr != nil {
			st.Error = trayErr.Error()
		}
		writeJSON(w, http.StatusOK, st)
		return
	}
	st, err := s.interfaceStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}
