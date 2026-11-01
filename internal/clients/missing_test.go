package clients

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// An entry whose program, npm shim script or node script is gone is
// missing; one whose script is relative to the app's own directory cannot
// be judged and is not.
func TestMissing(t *testing.T) {
	bin := t.TempDir()
	node := "node"
	if runtime.GOOS == "windows" {
		node += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, node), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": bin}
	script := filepath.Join(t.TempDir(), "server.js")
	cases := []struct {
		name    string
		e       Entry
		missing bool
	}{
		{"gone program", Entry{Command: filepath.Join(bin, "gone.exe")}, true},
		{"not on PATH", Entry{Command: "no-such-program-xyz", Env: env}, true},
		{"gone script", Entry{Command: "node", Args: []string{script}, Env: env}, true},
		{"relative script", Entry{Command: "node", Args: []string{"server.js"}, Env: env}, false},
		{"program there", Entry{Command: "node", Args: []string{"--version"}, Env: env}, false},
	}
	for _, c := range cases {
		if got := Missing(c.e); (got != "") != c.missing {
			t.Errorf("%s: Missing = %q", c.name, got)
		}
	}
	if err := os.WriteFile(script, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Missing(cases[2].e); got != "" {
		t.Fatalf("script back, still missing: %q", got)
	}
}
