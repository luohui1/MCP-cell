package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/registry"
)

func TestParseTakeover(t *testing.T) {
	o, yes, err := parseTakeover([]string{"context7", "-y", "--client", "codex,cursor", "ssh", "--per-session"})
	if err != nil || !yes || o.mode != registry.ModePerSession {
		t.Fatalf("parse: %+v %v %v", o, yes, err)
	}
	if strings.Join(o.clients, ",") != "codex,cursor" || len(o.entries) != 2 || !o.entries["context7"] || !o.entries["ssh"] {
		t.Fatalf("filters: %+v", o)
	}
	if _, _, err := parseTakeover([]string{"--shared", "--per-session"}); err == nil {
		t.Error("--shared with --per-session accepted")
	}
	if o, yes, _ = parseTakeover(nil); yes || o.mode != "" || o.entries != nil || o.clients != nil {
		t.Errorf("defaults: %+v %v", o, yes)
	}
	agents := []agentView{{ID: "claude-code", Name: "Claude Code"}, {ID: "codex", Name: "Codex"}}
	if ids, err := resolveClients(agents, []string{"Claude Code", "CODEX"}); err != nil || strings.Join(ids, ",") != "claude-code,codex" {
		t.Errorf("resolve: %v %v", ids, err)
	}
	if _, err := resolveClients(agents, []string{"vim"}); err == nil {
		t.Error("unknown client accepted")
	}
}

func planView() mcpView {
	cc := agentView{ID: "claude-code", Name: "Claude Code", Path: "claude.json", Found: true}
	cx := agentView{ID: "codex", Name: "Codex", Path: "config.toml", Found: true}
	cu := agentView{ID: "cursor", Name: "Cursor"}
	native := func(entry, class string) cellView {
		return cellView{State: "native", Entry: entry, Class: class, Takeable: true, Reason: class + " reason"}
	}
	return mcpView{Agents: []agentView{cc, cx, cu}, Rows: []mcpRowView{
		{Name: "existing", Mode: registry.ModeShared, Service: "existing", Agents: map[string]cellView{
			"claude-code": {State: "managed", Entry: "existing"}, "codex": native("existing", "unknown")}},
		{Name: "c7", Proposed: "c7", Agents: map[string]cellView{"claude-code": native("c7", "shared"), "codex": native("c7", "shared")}},
		{Name: "chrome", Proposed: "chrome", Agents: map[string]cellView{"claude-code": native("chrome", "stateful")}},
		{Name: "mine", Proposed: "mine", Agents: map[string]cellView{"claude-code": native("mine", "unknown")}},
		{Name: "env", Agents: map[string]cellView{"codex": {State: "native", Entry: "env", Class: "unknown", Reason: "通过 env_vars 读取"}}},
		{Name: "remote", Agents: map[string]cellView{"claude-code": {State: "remote", Entry: "remote", Reason: "已经是 HTTP 服务"}}},
		{Name: "edited", Service: "edited", Agents: map[string]cellView{"codex": {State: "changed", Entry: "edited"}}},
	}}
}

func stepsOf(p takeoverPlan) map[string]takeStep {
	out := map[string]takeStep{}
	for _, s := range p.steps {
		out[s.agent.ID+"/"+s.entry] = s
	}
	return out
}

func TestPlanTakeover(t *testing.T) {
	v := planView()
	p := planTakeover(v, takeoverOptions{})
	got := stepsOf(p)
	want := map[string]string{
		"claude-code/c7": registry.ModeShared, "claude-code/chrome": registry.ModePerSession, "claude-code/mine": registry.ModePerSession,
		"codex/c7": registry.ModeShared, "codex/existing": registry.ModeShared,
	}
	if len(got) != len(want) {
		t.Fatalf("steps: %+v", p.steps)
	}
	for k, mode := range want {
		if got[k].mode != mode {
			t.Errorf("%s: mode %q, want %q", k, got[k].mode, mode)
		}
	}
	if s := got["codex/existing"]; s.isNew || s.service != "existing" || !got["claude-code/c7"].isNew {
		t.Errorf("an existing service is reused, a missing one created: %+v", p.steps)
	}
	if strings.Join(p.managed["claude-code"], ",") != "existing" {
		t.Errorf("managed: %v", p.managed)
	}
	var skipped []string
	for _, s := range p.skipped {
		skipped = append(skipped, s.agent.ID+"/"+s.entry)
	}
	if strings.Join(skipped, ",") != "claude-code/remote,codex/env,codex/edited,cursor/" {
		t.Errorf("skipped: %v", skipped)
	}

	// --shared shares the unverified, never the stateful; an existing
	// service keeps its mode.
	got = stepsOf(planTakeover(v, takeoverOptions{mode: registry.ModeShared}))
	if got["claude-code/mine"].mode != registry.ModeShared || got["claude-code/chrome"].mode != registry.ModePerSession {
		t.Errorf("--shared: %+v", got)
	}
	got = stepsOf(planTakeover(v, takeoverOptions{mode: registry.ModePerSession}))
	if got["claude-code/c7"].mode != registry.ModePerSession || got["codex/existing"].mode != registry.ModeShared {
		t.Errorf("--per-session: %+v", got)
	}

	// Filters: by client, and by entry or service name.
	p = planTakeover(v, takeoverOptions{clients: []string{"codex"}, entries: map[string]bool{"c7": true, "nope": true}})
	if got = stepsOf(p); len(got) != 1 || got["codex/c7"].entry == "" || strings.Join(p.unmatched, ",") != "nope" || len(p.skipped) != 0 {
		t.Errorf("filtered: %+v unmatched %v skipped %v", p.steps, p.unmatched, p.skipped)
	}

	var out strings.Builder
	printPlan(&out, planTakeover(v, takeoverOptions{}))
	for _, want := range []string{"托管计划：5 个条目", "新建服务 c7", "服务 c7", "沿用 MCPcell 里已有的服务", "已托管，不再处理：Claude Code：existing",
		"Cursor", "没有找到配置", "将修改：\n  claude.json\n  config.toml\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan output lacks %q:\n%s", want, out.String())
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	inTemp(t, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTakeoverCommand(t *testing.T) {
	home, userHome := testDaemon(t, nil)
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	codexFile := filepath.Join(userHome, ".codex", "config.toml")
	cursorOrig := `{
  "mcpServers": {
    "context7": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"] },
    "chrome": { "command": "npx", "args": ["-y", "chrome-devtools-mcp@latest"] },
    "mine": { "command": "python", "args": ["my_server.py"] },
    "remote": { "url": "https://example.com/mcp" }
  }
}
`
	codexOrig := `[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.time]
command = "uvx"
args = ["mcp-server-time"]
`
	writeFile(t, cursorFile, cursorOrig)
	writeFile(t, codexFile, codexOrig)
	run := func(answer string, args ...string) (string, error) {
		var out strings.Builder
		err := takeoverWith(&out, strings.NewReader(answer), args)
		return out.String(), err
	}

	// The plan comes first; declining changes nothing.
	out, err := run("n\n")
	if err != nil || !strings.Contains(out, "托管计划：5 个条目") || !strings.Contains(out, "已取消，没有改动") ||
		!strings.Contains(out, "已经是 HTTP 服务") {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if readFile(t, cursorFile) != cursorOrig || readFile(t, codexFile) != codexOrig {
		t.Fatal("declining changed a configuration")
	}

	// One entry of one client, confirmed at the prompt.
	if out, err = run("y\n", "--client", "cursor", "context7"); err != nil || !strings.Contains(out, "✓ Cursor / context7 → 新建服务 context7") {
		t.Fatalf("cursor context7: %v\n%s", err, out)
	}
	if b := readFile(t, cursorFile); !strings.Contains(b, "mcpcell-run.exe") || !strings.Contains(b, `"remote": { "url"`) || !strings.Contains(b, `"python"`) {
		t.Fatalf("cursor config after taking over context7:\n%s", b)
	}
	if readFile(t, codexFile) != codexOrig {
		t.Fatal("--client cursor changed Codex")
	}

	// The rest of Cursor with --shared: the unverified server is shared, the
	// browser one still runs per session.
	if out, err = run("", "--client", "cursor", "--shared", "-y"); err != nil || strings.Count(out, "✓") != 2 || !strings.Contains(out, "已托管，不再处理：Cursor：context7") {
		t.Fatalf("cursor --shared: %v\n%s", err, out)
	}
	cfg, _ := registry.Load(home)
	if cfg.Services["context7"].Mode != registry.ModeShared || cfg.Services["mine"].Mode != registry.ModeShared ||
		cfg.Services["chrome"].Mode != registry.ModePerSession {
		t.Fatalf("modes after cursor: %+v", cfg.Services)
	}

	// Codex with --per-session: a new service runs per session even though
	// it could be shared; the existing context7 keeps its mode.
	if out, err = run("", "--client", "codex", "--per-session", "-y"); err != nil || !strings.Contains(out, "✓ Codex / context7 → 服务 context7") ||
		!strings.Contains(out, "✓ Codex / time → 新建服务 time") {
		t.Fatalf("codex --per-session: %v\n%s", err, out)
	}
	cfg, _ = registry.Load(home)
	if cfg.Services["time"].Mode != registry.ModePerSession || cfg.Services["context7"].Mode != registry.ModeShared {
		t.Fatalf("modes after codex: time %q, context7 %q", cfg.Services["time"].Mode, cfg.Services["context7"].Mode)
	}
	if b := readFile(t, codexFile); !strings.Contains(b, "mcpcell-run.exe") {
		t.Fatalf("codex config:\n%s", b)
	}

	// Nothing left; a name that matches nothing is an error.
	if out, err = run("", "-y"); err != nil || !strings.Contains(out, "没有需要托管的条目") {
		t.Fatalf("again: %v\n%s", err, out)
	}
	if _, err = run("", "nosuch", "-y"); err == nil {
		t.Fatal("an unknown entry name was not an error")
	}
	if _, err = run("", "--client", "vim"); err == nil {
		t.Fatal("an unknown client was not an error")
	}
}
