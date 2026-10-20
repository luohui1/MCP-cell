package httpd

import (
	"encoding/json"
	"net/http"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// The interface choice, GUI or command line, only decides whether the
// daemon shows a tray icon. Switching saves the choice and shows or hides the
// icon at once; nothing else about the daemon changes.

type interfaceStatus struct {
	Interface string `json:"interface"` // gui | cli
	// Tray: this daemon shows its tray icon now.
	Tray bool `json:"tray"`
	// TraySupported: this daemon can show one (Windows, not --no-tray).
	TraySupported bool `json:"traySupported"`
}

func (s *Server) traySupported() bool { return s.opts.Desktop != nil && !s.opts.NoTray }

func (s *Server) interfaceStatus() (interfaceStatus, error) {
	cfg, err := registry.Load(s.opts.Home)
	if err != nil {
		return interfaceStatus{}, err
	}
	st := interfaceStatus{Interface: registry.InterfaceGUI, TraySupported: s.traySupported()}
	if cfg.CLI() {
		st.Interface = registry.InterfaceCLI
	}
	s.mu.Lock()
	st.Tray = s.tray
	s.mu.Unlock()
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
		if s.traySupported() {
			on := mode == registry.InterfaceGUI
			s.opts.Desktop.SetTray(on)
			s.mu.Lock()
			s.tray = on
			s.mu.Unlock()
		}
		s.opts.Log.Info("interface changed", "interface", mode)
	}
	st, err := s.interfaceStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}
