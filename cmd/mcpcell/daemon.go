package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luohui1/MCP-cell/internal/catalog"
	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/httpd"
	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/registry"
)

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	port := fs.Int("port", 0, "监听端口（默认取 config.json 的 port，未设置时为 47621）")
	logToFile := fs.Bool("log", false, "日志写到 ~/.mcpcell/daemon.log（无窗口运行时使用）")
	noTray := fs.Bool("no-tray", false, "不显示托盘图标")
	openWindow := fs.Bool("open", false, "启动后打开 MCPcell 窗口")
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
	var logOut io.Writer = os.Stderr
	if *logToFile {
		f, err := openLog(home)
		if err != nil {
			return err
		}
		defer f.Close()
		logOut = f
	}
	log := slog.New(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelInfo}))
	userHome, _ := os.UserHomeDir()
	pkgs, err := npmcache.Open(filepath.Join(home, "pkgs"), nil)
	if err != nil {
		return fmt.Errorf("无法打开 npm 包缓存：%w", err)
	}
	p := pool.New(cfg, pool.Options{Version: version, ParentEnv: os.Environ(), DefaultDir: userHome, Log: log,
		MaxInstances: cfg.Limits.MaxInstances, Packages: pkgs, Catalog: catalog.OpenStore(filepath.Join(home, "catalog"))})
	takeovers, err := clients.OpenStore(home)
	if err != nil {
		return fmt.Errorf("无法读取接管记录：%w", err)
	}
	exe, _ := os.Executable()
	// Ctrl+C, closing the console window, and mcpcell stop all end up here.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The tray icon and window (Windows only). LoginURL is only called when
	// the window opens, after srv exists.
	var srv *httpd.Server
	var shell httpd.Desktop
	app, err := desktop.Start(desktop.Options{Home: home, Exe: exe, Tray: !*noTray, Log: log,
		LoginURL: func() string { return srv.LoginURL() }, Status: trayStatus(p), Quit: stop})
	switch {
	case err == nil:
		defer app.Close()
		shell = app
	case !errors.Is(err, desktop.ErrUnsupported):
		log.Warn("desktop shell unavailable", "err", err)
	}
	srv = httpd.New(p, httpd.Options{Port: cfg.Port, Token: token, Version: version,
		ConfigPath: registry.ConfigPath(home), Home: home, MaxInstances: cfg.Limits.MaxInstances, Log: log,
		Clients: clients.All(userHome, filepath.Join(home, "backups")), Takeovers: takeovers, Exe: exe, Shutdown: stop,
		Desktop: shell})

	started := time.Now()
	if err := registry.WriteDaemonInfo(home, registry.DaemonInfo{PID: os.Getpid(), Port: cfg.Port,
		Version: version, StartedAt: started}); err != nil {
		return err
	}
	defer registry.RemoveDaemonInfo(home)

	go p.Run(ctx, 15*time.Second)
	go srv.Run(ctx)
	if *openWindow && shell != nil {
		if err := shell.Show(); err != nil {
			log.Warn("cannot open window", "err", err)
		}
	}

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if hs.Shutdown(sctx) != nil {
			_ = hs.Close() // SSE streams do not end on their own
		}
	}()

	log.Info("daemon started", "port", cfg.Port, "pid", os.Getpid(), "services", len(cfg.Names()))
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
	log.Info("daemon stopped")
	fmt.Fprintln(os.Stderr, "MCPcell 已退出，所有 MCP 服务进程已结束。")
	return nil
}

// openLog appends to ~/.mcpcell/daemon.log, starting over past 5 MB.
func openLog(home string) (*os.File, error) {
	path := filepath.Join(home, "daemon.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
