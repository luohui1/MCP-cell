package httpd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// mode "per-session" (mcpcell takeover --per-session) runs a new service per
// session even when it could be shared; an existing service keeps its mode.
func TestAttachPerSessionMode(t *testing.T) {
	h, home := startWithHome(t)
	userHome := t.TempDir()
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	_ = os.MkdirAll(filepath.Dir(cursorFile), 0o700)
	_ = os.WriteFile(cursorFile, []byte(`{"mcpServers": {
  "context7": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"] },
  "fake": { "command": "fake" }
}}`), 0o600)
	store, _ := clients.OpenStore(home)
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, filepath.Join(home, "backups"))}
	h.srv.opts.Takeovers = store

	if code, _ := h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["context7"],"mode":"exclusive"}`); code != 400 {
		t.Fatalf("unknown mode accepted: %d", code)
	}
	_, body := h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["context7","fake"],"mode":"per-session"}`)
	if strings.Count(body, `"ok":true`) != 2 {
		t.Fatalf("attach: %s", body)
	}
	cfg, _ := registry.Load(home)
	if cfg.Services["context7"].Mode != registry.ModePerSession || cfg.Services["fake"].Mode != registry.ModeShared {
		t.Fatalf("modes: context7 %q, fake %q", cfg.Services["context7"].Mode, cfg.Services["fake"].Mode)
	}
}
