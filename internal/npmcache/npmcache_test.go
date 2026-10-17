package npmcache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		in, spec, args string
		ok             bool
	}{
		{"npx -y @upstash/context7-mcp", "@upstash/context7-mcp", "", true},
		{"npx chrome-devtools-mcp@latest --headless", "chrome-devtools-mcp@latest", "--headless", true},
		{`D:\APPS\nodejs\npx.cmd @larksuiteoapi/lark-mcp mcp -a x`, "@larksuiteoapi/lark-mcp", "mcp -a x", true},
		{"npx --yes pkg@^1.2.0", "pkg@^1.2.0", "", true},
		{"npx -p pkg cmd", "", "", false},
		{"npx ./local/server.js", "", "", false},
		{"npx github:owner/repo", "", "", false},
		{"npx", "", "", false},
		{"node server.js", "", "", false},
	}
	for _, c := range cases {
		spec, args, ok := Detect(strings.Fields(c.in))
		if ok != c.ok || spec != c.spec || strings.Join(args, " ") != c.args {
			t.Errorf("Detect(%q) = %q %q %v", c.in, spec, args, ok)
		}
	}
}

func TestSplitSpec(t *testing.T) {
	for in, want := range map[string]string{
		"@upstash/context7-mcp":        "@upstash/context7-mcp|",
		"@upstash/context7-mcp@1.0.14": "@upstash/context7-mcp|1.0.14",
		"pkg@latest":                   "pkg|latest",
		"pkg":                          "pkg|",
	} {
		n, v := SplitSpec(in)
		if n+"|"+v != want {
			t.Errorf("SplitSpec(%q) = %s|%s", in, n, v)
		}
	}
}

// fakeInstaller lays out what npm install would produce, at the given version.
func fakeInstaller(version string, bin any, calls *atomic.Int32) Installer {
	return func(ctx context.Context, spec, prefix string, env []string) error {
		calls.Add(1)
		name, _ := SplitSpec(spec)
		dir := filepath.Join(prefix, "node_modules", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
			return err
		}
		pkg, _ := json.Marshal(map[string]any{"name": name, "version": version, "bin": bin})
		_ = os.WriteFile(filepath.Join(dir, "dist", "index.js"), []byte("// server"), 0o644)
		return os.WriteFile(filepath.Join(dir, "package.json"), pkg, 0o644)
	}
}

func TestEnsureInstallsOnceAndPins(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	c, err := Open(dir, fakeInstaller("1.2.3", map[string]string{"context7-mcp": "dist/index.js", "other": "x.js"}, &calls))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := c.Ensure(context.Background(), "@upstash/context7-mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version != "1.2.3" || !strings.HasSuffix(filepath.ToSlash(pin.Script()), "@upstash+context7-mcp@1.2.3/node_modules/@upstash/context7-mcp/dist/index.js") {
		t.Fatalf("pin %+v script %s", pin, pin.Script())
	}
	if _, err := os.Stat(pin.Script()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Ensure(context.Background(), "@upstash/context7-mcp", nil); err != nil || calls.Load() != 1 {
		t.Fatalf("second Ensure reinstalled (calls=%d) %v", calls.Load(), err)
	}
	// Pins survive a restart.
	c2, _ := Open(dir, fakeInstaller("9.9.9", "dist/index.js", &calls))
	if p := c2.Pinned("@upstash/context7-mcp"); p == nil || p.Version != "1.2.3" {
		t.Fatalf("pin not persisted: %+v", p)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("leftover temp dir %s", e.Name())
		}
	}
}

func TestUpdateRepins(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	c, _ := Open(dir, fakeInstaller("1.0.0", "dist/index.js", &calls))
	old, _ := c.Ensure(context.Background(), "pkg", nil)
	c.install = fakeInstaller("1.1.0", "dist/index.js", &calls)
	now, before, err := c.Update(context.Background(), "pkg", nil)
	if err != nil || before.Version != "1.0.0" || now.Version != "1.1.0" {
		t.Fatalf("update: %+v %+v %v", now, before, err)
	}
	if _, err := os.Stat(old.Dir); !os.IsNotExist(err) {
		t.Error("old version directory kept after update")
	}
}

func TestMissingBin(t *testing.T) {
	var calls atomic.Int32
	c, _ := Open(t.TempDir(), fakeInstaller("1.0.0", map[string]string{"a": "a.js", "b": "b.js"}, &calls))
	if _, err := c.Ensure(context.Background(), "pkg", nil); err == nil {
		t.Fatal("ambiguous bins must not guess")
	}
}
