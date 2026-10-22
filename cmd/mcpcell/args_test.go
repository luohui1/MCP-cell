package main

import (
	"bytes"
	"strings"
	"testing"
)

// Every command checks its arguments before it does anything: -h/--help
// only prints the usage, and a stray argument is refused. `mcpcell stop
// --help` once stopped a running daemon.
func TestArgumentsAreCheckedFirst(t *testing.T) {
	testDaemon(t, nil)
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := dispatch(args, &out, &errOut)
		return code, out.String() + errOut.String()
	}
	for _, args := range [][]string{
		{"stop", "--help"}, {"stop", "-h"}, {"update", "--help"}, {"install", "-h"}, {"restore", "--all", "--help"},
		{"takeover", "-y", "--help"}, {"mode", "cli", "-h"}, {"daemon", "--help"}, {"help", "stop"},
	} {
		code, out := run(args...)
		if code != 0 || !strings.Contains(out, "用法: mcpcell") {
			t.Errorf("%v = %d %q, want usage only", args, code, out)
		}
	}
	for _, args := range [][]string{
		{"stop", "now"}, {"start", "x"}, {"ps", "--all"}, {"update"}, {"update", "--force"}, {"mode", "cli", "gui"},
		{"mode", "tui"}, {"restore"}, {"restore", "--all", "x"}, {"restore", "codex", "--forget"}, {"install", "--cli", "--gui"},
		{"install", "extra"}, {"open", "--bogus"}, {"autostart", "maybe"}, {"shortcut", "x"}, {"nosuch"}, {"version", "x"},
	} {
		if code, out := run(args...); code != 2 {
			t.Errorf("%v = %d %q, want 2 (refused)", args, code, out)
		}
	}
	// The daemon is still there.
	var st psStatus
	if err := daemonCall("GET", "/api/status", &st); err != nil {
		t.Fatalf("daemon gone after the checks: %v", err)
	}
}
