package registry

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{"services":{
		"context7":{"command":"npx","args":["-y","@upstash/context7-mcp"]},
		"keep":{"command":"x","idleTimeout":"0"},
		"fast":{"command":"x","idleTimeout":"90s"},
		"off":{"command":"x","disabled":true},
		"capped":{"command":"x","memoryLimit":"1.5GB","flatten":false,"catalog":"off"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("port = %d", cfg.Port)
	}
	c7 := cfg.Services["context7"]
	if c7.Mode != ModeShared || c7.Idle() != DefaultIdleTimeout || c7.IdleTimeout.Set {
		t.Errorf("context7 defaults: %+v", c7)
	}
	if d := cfg.Services["keep"].IdleTimeout; d.Duration != 0 || !d.Set {
		t.Errorf(`"0" must disable reaping, got %+v`, d)
	}
	if cfg.Services["fast"].Idle() != 90*time.Second || cfg.Services["keep"].Idle() != 0 {
		t.Error("90s")
	}
	if c := cfg.Services["capped"]; c.MemoryLimit != Size(1.5*(1<<30)) || c.FlattenEnabled() || c.CatalogEnabled() {
		t.Errorf("capped: %+v", c)
	}
	if !c7.FlattenEnabled() || !c7.CatalogEnabled() || cfg.Limits.MaxInstances != DefaultMaxInstances {
		t.Errorf("defaults: flatten/catalog/maxInstances")
	}
	if got := strings.Join(cfg.Names(), ","); got != "capped,context7,fast,keep" {
		t.Errorf("Names = %s", got)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		`{"services":{"bad name":{"command":"x"}}}`,
		`{"services":{"a":{"args":["x"]}}}`,
		`{"services":{"a":{"command":"x","mode":"per-workspace"}}}`,
		`{"services":{"a":{"command":"x","mode":"exclusive"}}}`,
		`{"services":{"a":{"command":"x","idleTimeout":"soon"}}}`,
		`{"services":{"a":{"command":"x","typo":1}}}`,
		`{"services":{"a":{"command":"x","memoryLimit":"lots"}}}`,
		`{"services":{"a":{"command":"x","memoryLimit":"1MB"}}}`,
		`{"services":{"a":{"command":"x","catalog":"live"}}}`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func TestPerSessionIdleDefault(t *testing.T) {
	cfg, err := Parse([]byte(`{"services":{"a":{"command":"x","mode":"per-session"},"b":{"command":"x","mode":"per-session","idleTimeout":"5m"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.Services["a"]; !a.PerSession() || a.Idle() != DefaultSessionIdle {
		t.Errorf("a: per-session %v, idle %s", a.PerSession(), a.Idle())
	}
	if b := cfg.Services["b"]; b.Idle() != 5*time.Minute {
		t.Errorf("b: idle %s", b.Idle())
	}
}

func TestTokenIsStable(t *testing.T) {
	home := t.TempDir()
	a, err := Token(home)
	if err != nil || len(a) != 64 {
		t.Fatalf("token %q %v", a, err)
	}
	if b, _ := Token(home); a != b {
		t.Fatal("token changed between reads")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	home := t.TempDir()
	cfg, err := Parse([]byte(`{"port":1234,"limits":{"maxInstances":4},"services":{
		"a":{"command":"npx","args":["-y","x"],"idleTimeout":"90s","memoryLimit":"1GB"},
		"b":{"command":"y","env":{"K":"v"},"flatten":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(home, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	a, b := again.Services["a"], again.Services["b"]
	if again.Port != 1234 || again.Limits.MaxInstances != 4 || a.Idle() != 90*time.Second ||
		a.MemoryLimit != 1<<30 || b.Env["K"] != "v" || b.FlattenEnabled() || b.IdleTimeout.Set {
		t.Fatalf("round trip lost data: %+v %+v %+v", again, a, b)
	}
	raw, _ := os.ReadFile(ConfigPath(home))
	if strings.Contains(string(raw), "idleTimeout\": \"3m") {
		t.Fatalf("defaults were written back: %s", raw)
	}
}

func TestInterface(t *testing.T) {
	cfg, err := Parse([]byte(`{"services":{}}`))
	if err != nil || cfg.CLI() {
		t.Fatalf("the default must be the GUI: %+v %v", cfg, err)
	}
	if _, err := Parse([]byte(`{"interface":"tui","services":{}}`)); err == nil {
		t.Error("unknown interface accepted")
	}
	home := t.TempDir()
	if err := os.WriteFile(ConfigPath(home), []byte(`{"port":1234,"services":{"a":{"command":"x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetInterface(home, "tui"); err == nil {
		t.Error("SetInterface accepted tui")
	}
	if err := SetInterface(home, InterfaceCLI); err != nil {
		t.Fatal(err)
	}
	again, err := Load(home)
	if err != nil || !again.CLI() || again.Port != 1234 || again.Services["a"] == nil {
		t.Fatalf("after SetInterface(cli): %+v %v", again, err)
	}
	if err := SetInterface(home, InterfaceGUI); err != nil {
		t.Fatal(err)
	}
	if again, _ = Load(home); again.CLI() || again.Interface != InterfaceGUI {
		t.Fatalf("after SetInterface(gui): %q", again.Interface)
	}
}

func TestParseService(t *testing.T) {
	if _, err := ParseService("ok", []byte(`{"command":"x","idleTimeout":"5m"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseService("bad name", []byte(`{"command":"x"}`)); err == nil {
		t.Fatal("bad name accepted")
	}
}
