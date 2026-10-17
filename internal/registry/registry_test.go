package registry

import (
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`{"services":{
		"context7":{"command":"npx","args":["-y","@upstash/context7-mcp"]},
		"keep":{"command":"x","idleTimeout":"0"},
		"fast":{"command":"x","idleTimeout":"90s"},
		"off":{"command":"x","disabled":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("port = %d", cfg.Port)
	}
	c7 := cfg.Services["context7"]
	if c7.Mode != ModeShared || c7.IdleTimeout.Duration != DefaultIdleTimeout {
		t.Errorf("context7 defaults: %+v", c7)
	}
	if d := cfg.Services["keep"].IdleTimeout; d.Duration != 0 || !d.Set {
		t.Errorf(`"0" must disable reaping, got %+v`, d)
	}
	if cfg.Services["fast"].IdleTimeout.Duration != 90*time.Second {
		t.Error("90s")
	}
	if got := strings.Join(cfg.Names(), ","); got != "context7,fast,keep" {
		t.Errorf("Names = %s", got)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		`{"services":{"bad name":{"command":"x"}}}`,
		`{"services":{"a":{"args":["x"]}}}`,
		`{"services":{"a":{"command":"x","mode":"per-session"}}}`,
		`{"services":{"a":{"command":"x","idleTimeout":"soon"}}}`,
		`{"services":{"a":{"command":"x","typo":1}}}`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
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
