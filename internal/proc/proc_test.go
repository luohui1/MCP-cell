package proc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the child process; see helper().
const helperEnv = "MCPCELL_PROC_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		helper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(mode string) {
	switch mode {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "sleep":
		time.Sleep(time.Minute)
	case "spawn":
		child, err := Start(helperSpec("sleep"))
		if err != nil {
			fmt.Println("error", err)
			return
		}
		fmt.Println(child.Pid)
		time.Sleep(time.Minute)
	}
}

func helperSpec(mode string) Spec {
	self, _ := os.Executable()
	env := BuildEnv(os.Environ(), map[string]string{helperEnv: mode})
	return Spec{Argv: []string{self}, Env: env, Dir: os.TempDir()}
}

func TestEchoAndKill(t *testing.T) {
	p, err := Start(helperSpec("echo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(p.Stdin, `{"jsonrpc":"2.0","method":"x"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(p.Stdout).ReadString('\n')
	if err != nil || !strings.Contains(line, `"method":"x"`) {
		t.Fatalf("read %q %v", line, err)
	}
	p.Kill()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Kill")
	}
	p.Kill() // idempotent
}

func TestKillTakesDescendants(t *testing.T) {
	p, err := Start(helperSpec("spawn"))
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(p.Stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("helper said %q", line)
	}
	if runtime.GOOS == "windows" {
		if u, err := p.Usage(); err != nil || u.Processes < 2 {
			t.Fatalf("job should contain parent and child: %+v %v", u, err)
		}
	}
	p.Kill()
	if !waitExited(child, 5*time.Second) {
		t.Fatalf("grandchild %d survived Kill", child)
	}
}

func TestUsage(t *testing.T) {
	p, err := Start(helperSpec("sleep"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	u, err := p.Usage()
	if err != nil || u.Processes < 1 || u.PrivateBytes == 0 {
		t.Fatalf("usage %+v %v", u, err)
	}
	if s := Self(); s.PrivateBytes == 0 {
		t.Fatalf("self usage %+v", s)
	}
}

func TestUnwrapCmd(t *testing.T) {
	cases := map[string]string{
		"cmd /c npx -y pkg":      "npx -y pkg",
		"cmd.exe /d /s /c npx a": "npx a",
		"CMD /C node x.js":       "node x.js",
		"cmd /x npx":             "cmd /x npx",
		"node server.js":         "node server.js",
	}
	for in, want := range cases {
		if got := strings.Join(unwrapCmd(strings.Fields(in)), " "); got != want {
			t.Errorf("unwrapCmd(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveNpmShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd shims are Windows-only")
	}
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("node.exe", "")
	write("npx.cmd", "@ECHO off\r\n")
	write(`node_modules\npm\bin\npx-cli.js`, "")
	write("context7-mcp.cmd", "@ECHO off\r\n\"%_prog%\"  \"%dp0%\\node_modules\\@upstash\\context7-mcp\\dist\\index.js\" %*\r\n")
	write(`node_modules\@upstash\context7-mcp\dist\index.js`, "")
	write("other.bat", "@echo hi\r\n")
	env := []string{"PATH=" + dir, "PATHEXT=.COM;.EXE;.BAT;.CMD"}

	got, err := ResolveArgv([]string{"cmd", "/c", "npx", "-y", "@upstash/context7-mcp"}, env, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "node.exe"), filepath.Join(dir, `node_modules\npm\bin\npx-cli.js`), "-y", "@upstash/context7-mcp"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("npx resolved to %q", got)
	}
	got, err = ResolveArgv([]string{"context7-mcp", "--port", "1"}, env, dir)
	if err != nil || got[1] != filepath.Join(dir, `node_modules\@upstash\context7-mcp\dist\index.js`) || got[2] != "--port" {
		t.Errorf("cmd-shim resolved to %q %v", got, err)
	}
	if _, err := ResolveArgv([]string{"other"}, env, dir); err == nil {
		t.Error("an arbitrary batch file must not be run")
	}
	if _, err := ResolveArgv([]string{"missing-tool"}, env, dir); err == nil {
		t.Error("expected not found")
	}
}

func TestBuildEnv(t *testing.T) {
	parent := []string{"PATH=/bin", "GITHUB_TOKEN=secret", "HOME=/h", "https_proxy=http://p"}
	env := BuildEnv(parent, map[string]string{"API_KEY": "k", "HOME": "/override"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "secret") {
		t.Error("daemon credentials leaked into the upstream env")
	}
	for _, want := range []string{"PATH=/bin", "API_KEY=k", "HOME=/override", "https_proxy=http://p"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, env)
		}
	}
}
