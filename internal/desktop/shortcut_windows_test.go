package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Writes into a temp dir only; the real Desktop and Start menu are left
// alone.
func TestShortcutRoundTrip(t *testing.T) {
	// The long form of the path: TEMP may hold 8.3 names (C:\Users\RUNNER~1)
	// that the shortcut gives back in full.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "mcpcelld.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := shortcutTarget(filepath.Join(dir, "mcpcell.exe")); got != target {
		t.Fatalf("shortcutTarget = %q, want the windowless binary %q", got, target)
	}
	lnk := filepath.Join(dir, "目录 with space", ShortcutName)
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeShortcut(lnk, target, "open", "MCPcell — 本机 MCP 运行时"); err != nil {
		t.Fatal(err)
	}
	gotTarget, gotArgs, err := readShortcut(lnk)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(gotTarget, target) || gotArgs != "open" {
		t.Fatalf("shortcut = %q %q, want %q open", gotTarget, gotArgs, target)
	}
}
