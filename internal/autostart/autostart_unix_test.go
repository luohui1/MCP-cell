//go:build darwin || linux

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Enabling writes the login entry for the installation's mcpcell, Get reads
// it back, Disable removes it; all under a temporary home.
func TestEnableGetDisable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	bin := t.TempDir()
	exe := filepath.Join(bin, "mcpcell")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if st := Get(); !st.Supported || st.Enabled {
		t.Fatalf("before: %+v", st)
	}
	if err := Enable(exe, true); err != nil {
		t.Fatal(err)
	}
	st := Get()
	if !st.Enabled || !strings.Contains(st.Command, exe) || !strings.Contains(st.Command, "daemon") || !st.Ours(exe) || st.Tray {
		t.Fatalf("after enable: %+v", st)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if st := Get(); st.Enabled {
		t.Fatalf("after disable: %+v", st)
	}
	if err := Enable(filepath.Join(t.TempDir(), "mcpcell"), false); err == nil {
		t.Fatal("enabled without the program next to it")
	}
}
