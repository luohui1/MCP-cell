package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/httpd"
	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/registry"
)

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	port := fs.Int("port", 0, "监听端口（默认取 config.json 的 port，未设置时为 47621）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	home, err := registry.Home()
	if err != nil {
		return err
	}
	cfg, err := registry.Load(home)
	if err != nil {
		return err
	}
	if *port != 0 {
		cfg.Port = *port
	}
	token, err := registry.Token(home)
	if err != nil {
		return fmt.Errorf("无法创建 token：%w", err)
	}
	l, err := httpd.Listen(cfg.Port)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	userHome, _ := os.UserHomeDir()
	pkgs, err := npmcache.Open(filepath.Join(home, "pkgs"), nil)
	if err != nil {
		return fmt.Errorf("无法打开 npm 包缓存：%w", err)
	}
	p := pool.New(cfg, pool.Options{Version: version, ParentEnv: os.Environ(), DefaultDir: userHome, Log: log,
		MaxInstances: cfg.Limits.MaxInstances, Packages: pkgs, Catalog: catalog.OpenStore(filepath.Join(home, "catalog"))})
	srv := httpd.New(p, httpd.Options{Port: cfg.Port, Token: token, Version: version,
		ConfigPath: registry.ConfigPath(home), Home: home, MaxInstances: cfg.Limits.MaxInstances, Log: log})

	started := time.Now()
	if err := registry.WriteDaemonInfo(home, registry.DaemonInfo{PID: os.Getpid(), Port: cfg.Port,
		Version: version, StartedAt: started}); err != nil {
		return err
	}
	defer registry.RemoveDaemonInfo(home)

	// Ctrl+C, and on Windows closing the console window, arrive here.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go p.Run(ctx, 15*time.Second)
	go srv.Run(ctx)

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if hs.Shutdown(sctx) != nil {
			_ = hs.Close() // SSE streams do not end on their own
		}
	}()

	names := cfg.Names()
	fmt.Fprintf(os.Stderr, "MCPcell %s 已启动：http://127.0.0.1:%d\n", version, cfg.Port)
	fmt.Fprintf(os.Stderr, "配置：%s（%d 个服务）\n", registry.ConfigPath(home), len(names))
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "还没有服务：在配置的 services 里添加后重启守护进程。")
	}
	fmt.Fprintln(os.Stderr, "管理页：另开终端运行 mcpcell ui；按 Ctrl+C 退出。")

	if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		p.Close()
		return err
	}
	p.Close()
	fmt.Fprintln(os.Stderr, "MCPcell 已退出，所有 MCP 服务进程已结束。")
	return nil
}
