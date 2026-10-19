package httpd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/jsonrpc"
)

// runClient speaks mcpcell-run's side of the wire format.
type runClient struct {
	t      *testing.T
	conn   net.Conn
	r      *bufio.Reader
	stderr []string
	nextID int
}

func dialRun(t *testing.T, h *harness, header map[string]any) *runClient {
	t.Helper()
	addr := strings.TrimPrefix(h.base, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	fmt.Fprintf(conn, "GET /run HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nConnection: Upgrade\r\nUpgrade: mcpcell-run\r\n\r\n", addr, token)
	r := bufio.NewReader(conn)
	status, _ := r.ReadString('\n')
	if !strings.Contains(status, " 101 ") {
		t.Fatalf("upgrade: %q", status)
	}
	for {
		l, _ := r.ReadString('\n')
		if strings.TrimSpace(l) == "" {
			break
		}
	}
	b, _ := json.Marshal(header)
	conn.Write(append(b, '\n'))
	return &runClient{t: t, conn: conn, r: r}
}

func (c *runClient) send(method string, params any) {
	c.nextID++
	m, _ := jsonrpc.NewRequest(json.RawMessage(fmt.Sprint(c.nextID)), method, params)
	b, _ := jsonrpc.Encode(m)
	c.conn.Write(append(b, '\n'))
}

// read returns the next JSON-RPC line, collecting stderr lines; a control
// line comes back as its text.
func (c *runClient) read() (jsonrpc.Message, string) {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("read: %v", err)
		}
		switch line[0] {
		case runStderr:
			c.stderr = append(c.stderr, strings.TrimSpace(line[1:]))
			continue
		case runControl:
			return jsonrpc.Message{}, strings.TrimSpace(line[1:])
		}
		msgs, _, err := jsonrpc.Parse([]byte(line))
		if err != nil {
			c.t.Fatalf("parse %q: %v", line, err)
		}
		return msgs[0], ""
	}
}

func (c *runClient) init() {
	c.t.Helper()
	c.send("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "run-test", "version": "1"}})
	if m, control := c.read(); control != "" || len(m.Error) > 0 {
		c.t.Fatalf("initialize: control %q error %s", control, m.Error)
	}
}

func (c *runClient) call(tool string, args map[string]any) string {
	c.t.Helper()
	c.send("tools/call", map[string]any{"name": tool, "arguments": args})
	m, _ := c.read()
	var res struct {
		Content []struct{ Text string } `json:"content"`
	}
	if len(m.Error) > 0 || json.Unmarshal(m.Result, &res) != nil || len(res.Content) == 0 {
		c.t.Fatalf("%s: %s %s", tool, m.Error, m.Result)
	}
	return res.Content[0].Text
}

func TestRunWrapper(t *testing.T) {
	h := startConfig(t, nil, `{"services":{}}`)
	env := map[string]string{"MCPCELL_FAKE_MCP": "1", "MCPCELL_FAKE_PAGE": "0", "RUN_MARKER": "from-the-agent"}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			if _, set := env[k]; !set {
				env[k] = v
			}
		}
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	argv := []string{"npx", "-y", "some-unknown-mcp"}

	// An unknown server runs per session, as the agent would have run it:
	// its own process, in the session's directory, with the session's env.
	a := dialRun(t, h, map[string]any{"argv": argv, "cwd": dirA, "env": env, "mode": "auto"})
	b := dialRun(t, h, map[string]any{"argv": argv, "cwd": dirB, "env": env, "mode": "auto"})
	a.init()
	b.init()
	if len(a.stderr) == 0 || !strings.Contains(a.stderr[0], "按会话") {
		t.Fatalf("notice for the agent's log: %q", a.stderr)
	}
	pa, pb := a.call("pid", nil), b.call("pid", nil)
	if pa == pb {
		t.Fatal("per-session sessions share a process")
	}
	same := func(x, y string) bool {
		ex, _ := filepath.EvalSymlinks(x)
		ey, _ := filepath.EvalSymlinks(y)
		return strings.EqualFold(ex, ey)
	}
	if got := a.call("cwd", nil); !same(got, dirA) {
		t.Fatalf("cwd %q, want the session's %q", got, dirA)
	}
	if got := b.call("getenv", map[string]any{"text": "RUN_MARKER"}); got != "from-the-agent" {
		t.Fatalf("env RUN_MARKER = %q", got)
	}
	services := func() int {
		n := 0
		for _, s := range h.status(t).Services {
			if strings.HasPrefix(s.Name, "run-some-unknown-mcp-") {
				n += len(s.Instances)
			}
		}
		return n
	}
	if n := services(); n != 2 {
		t.Fatalf("%d session instances, want 2", n)
	}
	// Closing the wrapper's stdin ends the session and its instance at once.
	a.conn.(*net.TCPConn).CloseWrite()
	if _, err := io.ReadAll(a.r); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); services() != 1; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("instance not released, %d left", services())
		}
	}

	// --shared: every session uses one process.
	s1 := dialRun(t, h, map[string]any{"argv": argv, "cwd": dirA, "env": env, "mode": "shared"})
	s2 := dialRun(t, h, map[string]any{"argv": argv, "cwd": dirB, "env": env, "mode": "shared"})
	s1.init()
	s2.init()
	if s1.call("pid", nil) != s2.call("pid", nil) {
		t.Fatal("shared sessions got different processes")
	}

	// Anything MCPcell cannot serve goes back to the wrapper.
	bad := dialRun(t, h, map[string]any{"argv": argv, "cwd": dirA, "env": env, "mode": "per-workspace"})
	if _, control := bad.read(); control != runDirect {
		t.Fatalf("control %q, want %q", control, runDirect)
	}
}

func TestSharedEnvIgnoresClientVariables(t *testing.T) {
	t.Setenv("RUN_SAME", "x")
	got := sharedEnv(map[string]string{"RUN_SAME": "x", "API_KEY": "k", "CLAUDECODE": "1", "CODEX_SANDBOX": "1",
		"TERM_PROGRAM": "vscode", "PATH": "elsewhere", "MCPCELL_HOME": "h", "CLAUDE_PROJECT_DIR": "d", "AI_AGENT": "claude",
		"MSYS_NO_PATHCONV": "1"})
	if len(got) != 1 || got["API_KEY"] != "k" {
		t.Fatalf("sharedEnv = %v", got)
	}
}

func TestRunUsesTheConfiguredService(t *testing.T) {
	h := startConfig(t, nil, `{"services":{"ctx":{"command":"npx","args":["-y","some-mcp"],"env":{"API_KEY":"k1"}},
		"ctx-other":{"command":"npx","args":["-y","some-mcp"],"env":{"API_KEY":"k2"}}}}`)
	env := map[string]string{"MCPCELL_FAKE_MCP": "1", "API_KEY": "k2"}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" && env[k] == "" {
			env[k] = v
		}
	}
	// Written differently (cmd /c), same server: the session joins the
	// configured service whose variables match the agent's.
	c := dialRun(t, h, map[string]any{"argv": []string{"cmd", "/c", "npx", "-y", "some-mcp"}, "cwd": t.TempDir(), "env": env})
	c.init()
	c.call("pid", nil)
	sessions := h.status(t).Sessions
	if len(sessions) != 1 || sessions[0].Service != "ctx-other" {
		t.Fatalf("sessions: %+v", sessions)
	}
}

func TestRunToldWhyTheDaemonLeaves(t *testing.T) {
	h := startConfig(t, nil, `{"services":{}}`)
	env := map[string]string{"MCPCELL_FAKE_MCP": "1"}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" && env[k] == "" {
			env[k] = v
		}
	}
	c := dialRun(t, h, map[string]any{"argv": []string{"npx", "-y", "some-unknown-mcp"}, "cwd": t.TempDir(), "env": env, "mode": "auto"})
	c.init()
	// install stops the daemon for a new one: wrappers are told to come back.
	h.srv.leave(runRestart)
	h.srv.closeRuns()
	if _, control := c.read(); control != runRestart {
		t.Fatalf("control before closing = %q", control)
	}
	if _, err := c.r.ReadString('\n'); err == nil {
		t.Fatal("connection still open")
	}
	// Until it is gone, wrappers that try again are turned away to retry.
	addr := strings.TrimPrefix(h.base, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /run HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nConnection: Upgrade\r\nUpgrade: mcpcell-run\r\n\r\n", addr, token)
	status, _ := bufio.NewReader(conn).ReadString('\n')
	if !strings.Contains(status, " 503 ") {
		t.Fatalf("while leaving: %q", status)
	}
}
