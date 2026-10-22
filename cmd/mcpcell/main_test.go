package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/httpd"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
	"github.com/luohui1/MCP-cell/internal/testutil/fakemcp"
)

func TestMain(m *testing.M) {
	fakemcp.MaybeRun()
	// Never read or write the user's real agent configurations or reach the
	// real daemon: every test runs against its own MCPCELL_HOME.
	os.Unsetenv("CODEX_HOME")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	home, err := os.MkdirTemp("", "mcpcell-cmd-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("MCPCELL_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

// inTemp fails the test unless path is under the test's temp directories.
func inTemp(t *testing.T, path string) {
	t.Helper()
	if !strings.HasPrefix(path, os.TempDir()) && !strings.Contains(path, filepath.Join("Temp", "")) {
		t.Fatalf("refusing to touch %s outside the temp dir", path)
	}
}

// testDaemon runs the daemon's HTTP side in this process, with MCPCELL_HOME
// and the agents' configurations in temp directories; every service is the
// fake MCP server.
func testDaemon(t *testing.T, desk httpd.Desktop) (home, userHome string) {
	t.Helper()
	home, userHome = t.TempDir(), t.TempDir()
	t.Setenv("MCPCELL_HOME", home)
	cfg, err := registry.Load(home)
	if err != nil {
		t.Fatal(err)
	}
	token, err := registry.Token(home)
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New(cfg, pool.Options{Version: "test", Catalog: catalog.OpenStore(filepath.Join(home, "catalog")),
		SpecFor: func(string, *registry.Service) proc.Spec { return fakemcp.Spec() }})
	store, err := clients.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(home, "backups")
	cs := []clients.Client{clients.NewCodex(userHome, backups), clients.NewCursor(userHome, backups)}
	for _, c := range cs {
		inTemp(t, c.Path())
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := httpd.New(p, httpd.Options{Port: port, Token: token, Version: "test", Home: home, ConfigPath: registry.ConfigPath(home),
		Clients: cs, Takeovers: store, Desktop: desk, Wrapper: filepath.Join(home, "bin", "mcpcell-run.exe")})
	hs := &http.Server{Handler: srv.Handler()}
	go hs.Serve(l)
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Run(ctx)
	if err := registry.WriteDaemonInfo(home, registry.DaemonInfo{PID: os.Getpid(), Port: port, Version: "test", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		hs.Close()
		p.Close()
	})
	return home, userHome
}
