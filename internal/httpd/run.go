package httpd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/jsonrpc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// The run endpoint serves mcpcell-run, the wrapper an agent's MCP entry
// launches in place of the server: `mcpcell-run -- npx -y some-mcp`. The
// agent's entry keeps the real command, so it stays the one definition of
// the server, and the wrapper falls back to running it directly whenever
// MCPcell cannot serve it.
//
// Wire format: the wrapper sends `GET /run` with the bearer token and
// `Upgrade: mcpcell-run`; after 101 it sends one JSON header line (runHeader)
// and then the agent's stdin verbatim. The daemon answers with the stdio
// stream: newline-delimited JSON-RPC, plus lines starting with runStderr
// (text for the agent's stderr) and runControl (instructions for the
// wrapper).
const (
	runStderr  = '\x1e'
	runControl = '\x1f'
	// runDirect tells the wrapper to run the command itself and replay what
	// it already sent. Only ever sent before the reply to initialize.
	runDirect = "direct"
)

type runHeader struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
	Mode string            `json:"mode"` // auto | shared | per-session
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if !s.bearerOK(r) {
		http.Error(w, "缺少或错误的 MCPcell token", http.StatusUnauthorized)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "mcpcell-run") {
		http.Error(w, "需要 Upgrade: mcpcell-run", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "连接不支持升级", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: mcpcell-run\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	s.serveRun(conn, rw.Reader)
}

// serveRun is one agent session over a wrapper connection: the same session
// as the HTTP endpoint's, with the connection as its stream. The session
// ends when the connection does, as a stdio server ends with its stdin.
func (s *Server) serveRun(conn net.Conn, br *bufio.Reader) {
	out := &runWriter{w: conn}
	line, err := readRunLine(br)
	if err != nil {
		return
	}
	var h runHeader
	if json.Unmarshal(line, &h) != nil || len(h.Argv) == 0 {
		out.stderr("[mcpcell] 无法识别 mcpcell-run 发来的命令")
		return
	}
	name, mode, err := s.runService(h)
	if err != nil {
		s.opts.Log.Warn("run: handing back to the wrapper", "argv0", filepath.Base(h.Argv[0]), "err", err)
		out.control(runDirect)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	var ss *session
	var gen int
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
		switch {
		case ss != nil && ss.version != "":
			s.endSession(ss)
		case ss != nil:
			ss.closeStream(gen)
		}
	}()
	for {
		line, err := readRunLine(br)
		if err != nil {
			return // the agent closed the wrapper's stdin or killed it
		}
		msgs, _, err := jsonrpc.Parse(line)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			switch m.Kind() {
			case jsonrpc.Request:
				if ss != nil {
					wg.Add(1)
					go func() {
						defer wg.Done()
						out.message(s.handle(ctx, ss, m, out.message))
					}()
					continue
				}
				if m.Method != "initialize" {
					out.message(jsonrpc.NewError(m.ID, jsonrpc.CodeInvalidRequest, "请先 initialize"))
					continue
				}
				ss = newSession(name, m.Params)
				// Used only if the session gets an instance of its own.
				ss.spawnEnv, ss.spawnDir = envList(h.Env), h.Cwd
				ch := make(chan jsonrpc.Message, 64)
				_, gen = ss.openStream(ch)
				go func() {
					for m := range ch {
						out.message(m)
					}
				}()
				reply, _ := s.openSession(ctx, ss, m)
				if len(reply.Error) > 0 {
					// MCPcell could not start it; the agent might. Let the
					// wrapper run it natively rather than fail the session.
					s.opts.Log.Warn("run: handing back to the wrapper", "service", name, "err", string(reply.Error))
					out.control(runDirect)
					return
				}
				if s.pool.PerSession(name) {
					mode = registry.ModePerSession // a shareability check may have moved it
				}
				out.stderr(runNotice(h.Argv, mode))
				out.message(reply)
			case jsonrpc.Notification:
				if ss != nil {
					s.clientNotification(ss, m)
				}
			case jsonrpc.Response:
				if ss != nil {
					ss.answer(m)
				}
			}
		}
	}
}

func runNotice(argv []string, mode string) string {
	what := clients.LaunchPackage(clients.Entry{Kind: "stdio", Command: argv[0], Args: argv[1:]})
	if mode == registry.ModePerSession {
		return "[mcpcell] " + what + " 经 MCPcell 按会话运行：用这个会话的工作目录和环境变量，第一次调用时启动，会话结束即回收"
	}
	return "[mcpcell] " + what + " 经 MCPcell 共享运行：全机一份进程，空闲后回收"
}

// runService finds or creates the in-memory service that runs a wrapped
// command. It is never written to config.json: the agent's entry stays the
// only definition, and the same command from any agent maps to one service.
func (s *Server) runService(h runHeader) (name, mode string, err error) {
	e := clients.Entry{Kind: "stdio", Command: h.Argv[0], Args: h.Argv[1:]}
	if base := strings.ToLower(filepath.Base(e.Command)); strings.HasPrefix(base, "mcpcell") {
		return "", "", fmt.Errorf("不能用 mcpcell-run 包装 %s", base)
	}
	if name, mode, ok := s.configuredService(h); ok {
		return name, mode, nil
	}
	switch h.Mode {
	case registry.ModeShared, registry.ModePerSession:
		mode = h.Mode
	case "", "auto":
		class, _ := clients.Classify(e, "")
		mode = clients.ModeFor(class, false)
	default:
		return "", "", fmt.Errorf("未知的运行方式 %q", h.Mode)
	}
	var env map[string]string
	if mode == registry.ModeShared {
		env = sharedEnv(h.Env)
	}
	key := clients.ServiceKey(e.Command, e.Args, env, "") + "\x00" + mode
	name = "run-" + slug(clients.LaunchPackage(e)) + "-" + shortHash(key)[:6]
	if !s.pool.Has(name) {
		s.pool.Put(name, &registry.Service{Command: e.Command, Args: e.Args, Env: env, Mode: mode})
		s.opts.Log.Info("run: new service", "service", name, "mode", mode, "envNames", strings.Join(sortedKeys(env), ","))
	}
	return name, mode, nil
}

// configuredService finds the configured service a wrapped command belongs
// to: same command and arguments, and every variable the service sets has the
// same value in the agent's environment. A takeover creates that service, so
// wrapped sessions and HTTP sessions share its settings and instances. The
// most specific match (most variables) wins.
func (s *Server) configuredService(h runHeader) (name, mode string, ok bool) {
	want := clients.ServiceKey(h.Argv[0], h.Argv[1:], nil, "")
	best := -1
	for n, def := range s.pool.Definitions() {
		if strings.HasPrefix(n, "run-") || clients.ServiceKey(def.Command, def.Args, nil, "") != want {
			continue
		}
		if (h.Mode == registry.ModeShared || h.Mode == registry.ModePerSession) && def.Mode != h.Mode {
			continue
		}
		matches := true
		for k, v := range def.Env {
			if h.Env[k] != v {
				matches = false
				break
			}
		}
		if matches && len(def.Env) > best {
			name, mode, ok, best = n, def.Mode, true, len(def.Env)
		}
	}
	return name, mode, ok
}

// clientOnly are prefixes of variables that describe the agent or its shell
// rather than the server; they differ between agents and sessions and must
// not split shared instances. Missing one only splits an instance in two:
// shared mode is for servers known not to read such variables, and a
// per-session instance always gets the full environment.
var clientOnly = []string{"CLAUDE", "AI_AGENT", "CODEX", "MCPCELL_", "TERM", "COLORTERM", "VSCODE_", "ELECTRON_",
	"NODE_CHANNEL", "MSYS", "MINGW", "ORIGINAL_", "CHERE_", "EXEPATH", "GIT_", "PWD", "OLDPWD", "SHLVL", "SHELL",
	"_", "PROMPT", "SESSIONNAME", "PATH", "HOME", "LOGNAME", "HOSTNAME", "FORCE_COLOR", "NO_COLOR"}

// sharedEnv keeps what a shared instance is keyed and started with: the
// variables the agent's entry set beyond the daemon's own environment.
func sharedEnv(env map[string]string) map[string]string {
	own := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			own[strings.ToUpper(k)] = v
		}
	}
	out := map[string]string{}
	for k, v := range env {
		uk := strings.ToUpper(k)
		if v2, ok := own[uk]; ok && v2 == v {
			continue
		}
		skip := false
		for _, p := range clientOnly {
			if strings.HasPrefix(uk, p) {
				skip = true
				break
			}
		}
		if !skip {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	if out == "" {
		out = "mcp"
	}
	return out
}

func readRunLine(br *bufio.Reader) ([]byte, error) {
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			return t, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// runWriter serializes lines to the wrapper.
type runWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (o *runWriter) line(b []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.w.Write(append(b, '\n'))
}

func (o *runWriter) message(m jsonrpc.Message) {
	if b, err := jsonrpc.Encode(m); err == nil {
		o.line(b)
	}
}

func (o *runWriter) stderr(text string) { o.line([]byte(string(rune(runStderr)) + text)) }
func (o *runWriter) control(c string)   { o.line([]byte(string(rune(runControl)) + c)) }
