package httpd

import (
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/registry"
)

func (f *fakeDesktop) TrayShown() bool    { return false }
func (f *fakeDesktop) SetTray(bool) error { return nil }

// trayDesktop records SetTray calls and keeps the tray's state.
type trayDesktop struct {
	fakeDesktop
	calls []bool
	on    bool
}

func (d *trayDesktop) TrayShown() bool { return d.on }
func (d *trayDesktop) SetTray(on bool) error {
	d.calls = append(d.calls, on)
	d.on = on
	return nil
}

func TestInterfaceSwitch(t *testing.T) {
	h, home := startWithHome(t)
	code, body := h.apiCall(t, "GET", "/api/interface", "")
	if want := `{"interface":"gui","tray":false,"traySupported":false}`; code != 200 || strings.TrimSpace(body) != want {
		t.Fatalf("GET = %d %s, want %s", code, body, want)
	}
	// No desktop (another OS): the choice is still saved.
	if code, body = h.apiCall(t, "POST", "/api/interface", `{"interface":"cli"}`); code != 200 || !strings.Contains(body, `"interface":"cli"`) {
		t.Fatalf("POST cli = %d %s", code, body)
	}
	if cfg, _ := registry.Load(home); !cfg.CLI() || cfg.Services["fake"] == nil {
		t.Fatalf("config after switching: %+v", cfg)
	}

	// The tray is a process of its own: switching starts and stops it.
	d := &trayDesktop{}
	h.srv.opts.Desktop = d
	if code, body = h.apiCall(t, "POST", "/api/interface", `{"interface":"gui"}`); code != 200 ||
		!strings.Contains(body, `"tray":true,"traySupported":true`) {
		t.Fatalf("POST gui = %d %s", code, body)
	}
	if code, body = h.apiCall(t, "POST", "/api/interface", `{"interface":"cli"}`); code != 200 || !strings.Contains(body, `"tray":false`) {
		t.Fatalf("POST cli = %d %s", code, body)
	}
	if len(d.calls) != 2 || !d.calls[0] || d.calls[1] {
		t.Fatalf("SetTray calls = %v, want [true false]", d.calls)
	}
	if code, _ = h.apiCall(t, "POST", "/api/interface", `{"interface":"tui"}`); code != 400 {
		t.Fatalf("unknown interface = %d", code)
	}
	if cfg, _ := registry.Load(home); cfg.Interface != registry.InterfaceCLI {
		t.Fatalf("interface = %q", cfg.Interface)
	}
}
