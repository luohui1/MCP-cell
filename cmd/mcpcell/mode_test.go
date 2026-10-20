package main

import (
	"testing"

	"github.com/luohui1/MCP-cell/internal/registry"
)

type modeDesktop struct{ calls []bool }

func (*modeDesktop) Show() error                   { return nil }
func (*modeDesktop) Available() error              { return nil }
func (*modeDesktop) Shortcuts() (bool, bool)       { return false, false }
func (*modeDesktop) SetShortcuts(_, _ *bool) error { return nil }
func (d *modeDesktop) SetTray(on bool)             { d.calls = append(d.calls, on) }

func TestModeCommand(t *testing.T) {
	// No daemon: the choice goes into config.json for its next start.
	home := t.TempDir()
	t.Setenv("MCPCELL_HOME", home)
	if err := runMode([]string{"cli"}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := registry.Load(home); !cfg.CLI() {
		t.Fatal("mode cli not saved")
	}
	if err := runMode(nil); err != nil {
		t.Fatal(err)
	}
	if runMode([]string{"tui"}) == nil || runMode([]string{"cli", "gui"}) == nil {
		t.Fatal("bad arguments accepted")
	}

	// A running daemon switches its tray icon at once.
	d := &modeDesktop{}
	home, _ = testDaemon(t, d)
	if err := runMode([]string{"cli"}); err != nil {
		t.Fatal(err)
	}
	if err := runMode([]string{"gui"}); err != nil {
		t.Fatal(err)
	}
	if len(d.calls) != 2 || d.calls[0] || !d.calls[1] {
		t.Fatalf("SetTray calls = %v, want [false true]", d.calls)
	}
	if cfg, _ := registry.Load(home); cfg.Interface != registry.InterfaceGUI {
		t.Fatalf("interface = %q", cfg.Interface)
	}
	if err := runMode(nil); err != nil {
		t.Fatal(err)
	}
}
