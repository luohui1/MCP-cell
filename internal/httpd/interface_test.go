package httpd

import (
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/registry"
)

func (f *fakeDesktop) SetTray(bool) {}

// trayDesktop records SetTray calls.
type trayDesktop struct {
	fakeDesktop
	calls []bool
}

func (d *trayDesktop) SetTray(on bool) { d.calls = append(d.calls, on) }

func TestInterfaceSwitch(t *testing.T) {
	h, home := startWithHome(t)
	code, body := h.apiCall(t, "GET", "/api/interface", "")
	if want := `{"interface":"gui","tray":false,"traySupported":false}`; code != 200 || strings.TrimSpace(body) != want {
		t.Fatalf("GET = %d %s, want %s", code, body, want)
	}
	// No desktop shell (another OS): the choice is still saved.
	if code, body = h.apiCall(t, "POST", "/api/interface", `{"interface":"cli"}`); code != 200 || !strings.Contains(body, `"interface":"cli"`) {
		t.Fatalf("POST cli = %d %s", code, body)
	}
	if cfg, _ := registry.Load(home); !cfg.CLI() || cfg.Services["fake"] == nil {
		t.Fatalf("config after switching: %+v", cfg)
	}

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

	// --no-tray: the choice is saved, the icon stays hidden.
	h.srv.opts.NoTray = true
	if code, body = h.apiCall(t, "POST", "/api/interface", `{"interface":"gui"}`); code != 200 ||
		!strings.Contains(body, `"interface":"gui","tray":false,"traySupported":false`) {
		t.Fatalf("POST gui with --no-tray = %d %s", code, body)
	}
	if len(d.calls) != 2 {
		t.Fatalf("SetTray called despite --no-tray: %v", d.calls)
	}
	if code, _ = h.apiCall(t, "POST", "/api/interface", `{"interface":"tui"}`); code != 400 {
		t.Fatalf("unknown interface = %d", code)
	}
	if cfg, _ := registry.Load(home); cfg.Interface != registry.InterfaceGUI {
		t.Fatalf("interface = %q", cfg.Interface)
	}
}
