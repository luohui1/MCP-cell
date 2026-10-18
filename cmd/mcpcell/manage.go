package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/luohui1/MCP-cell/internal/autostart"
	"github.com/luohui1/MCP-cell/internal/clients"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// runStart launches the windowless daemon in the background.
func runStart() error {
	st, started, err := ensureDaemon()
	switch {
	case err != nil:
		return err
	case started:
		home, _ := registry.Home()
		fmt.Printf("MCPcell 已启动（PID %d，端口 %d），日志在 %s\n", st.PID, st.Port, filepath.Join(home, "daemon.log"))
	default:
		fmt.Printf("MCPcell 已在运行（PID %d，端口 %d）\n", st.PID, st.Port)
	}
	return nil
}

// ensureDaemon starts the windowless daemon unless one is running, and
// waits until it answers.
func ensureDaemon() (st psStatus, started bool, err error) {
	if daemonCall("GET", "/api/status", &st) == nil {
		return st, false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return st, false, err
	}
	bin := exe
	if gui := filepath.Join(filepath.Dir(exe), "mcpcelld.exe"); runtime.GOOS == "windows" {
		if _, err := os.Stat(gui); err == nil {
			bin = gui
		}
	}
	cmd := exec.Command(bin, "daemon", "--log")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return st, false, err
	}
	_ = cmd.Process.Release()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if daemonCall("GET", "/api/status", &st) == nil {
			return st, true, nil
		}
	}
	return st, false, errors.New("守护进程没有在 10 秒内响应，请查看 ~/.mcpcell/daemon.log")
}

func runStop() error {
	var out map[string]bool
	if err := daemonCall("POST", "/api/shutdown", &out); err != nil {
		return err
	}
	fmt.Println("MCPcell 正在退出，所有 MCP 服务进程会一起结束。")
	return nil
}

func openTakeovers() ([]clients.Client, *clients.Store, error) {
	home, err := registry.Home()
	if err != nil {
		return nil, nil, err
	}
	userHome, _ := os.UserHomeDir()
	store, err := clients.OpenStore(home)
	if err != nil {
		return nil, nil, err
	}
	return clients.All(userHome, filepath.Join(home, "backups")), store, nil
}

// runRestore undoes takeovers straight from takeover.json; the daemon does
// not need to be running, so this always works as the way back.
func runRestore(args []string) error {
	cs, store, err := openTakeovers()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("用法: mcpcell restore --all | mcpcell restore <客户端> [条目...]")
	}
	all := args[0] == "--all"
	var client string
	names := map[string]bool{}
	if !all {
		client = args[0]
		for _, n := range args[1:] {
			names[n] = true
		}
	}
	recs := store.All()
	done, failed := 0, 0
	for _, rec := range recs {
		if !all && (rec.Client != client || len(names) > 0 && !names[rec.Name]) {
			continue
		}
		c := clients.FindClient(cs, rec.Client)
		if c == nil {
			continue
		}
		if err := c.Restore(rec); err != nil {
			failed++
			fmt.Printf("  ✗ %s / %s：%v（原始配置备份：%s）\n", c.Name(), rec.Name, err, rec.Backup)
			continue
		}
		_ = store.Delete(rec.Client, rec.Name)
		done++
		fmt.Printf("  ✓ %s / %s 已还原\n", c.Name(), rec.Name)
	}
	if done+failed == 0 {
		fmt.Println("没有需要还原的接管记录。")
		return nil
	}
	fmt.Printf("还原 %d 个，失败 %d 个。改动后请重启对应客户端。\n", done, failed)
	if failed > 0 {
		return errors.New("部分条目没有还原")
	}
	return nil
}

// runClients prints what each client has and what MCPcell would do.
func runClients() error {
	cs, store, err := openTakeovers()
	if err != nil {
		return err
	}
	home, _ := registry.Home()
	cfg, err := registry.Load(home)
	if err != nil {
		return err
	}
	label := map[clients.Class]string{clients.ClassShared: "可共享", clients.ClassStateful: "需要会话隔离",
		clients.ClassUnknown: "未知", clients.ClassRemote: "远程 HTTP", clients.ClassManaged: "已接管", clients.ClassInternal: "客户端内置"}
	for _, v := range clients.Overview(cs, store, cfg.Services, fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)) {
		switch {
		case v.Error != "":
			fmt.Printf("%s（%s）：%s\n\n", v.Name, v.Path, v.Error)
			continue
		case !v.Found:
			fmt.Printf("%s：没有找到配置\n\n", v.Name)
			continue
		}
		fmt.Printf("%s  %s\n", v.Name, v.Path)
		rows := [][]string{{"条目", "类别", "启动", "MCPcell 服务"}}
		for _, e := range v.Entries {
			what := e.Package
			if e.Kind == "http" {
				what = e.URL
			}
			rows = append(rows, []string{e.Name, label[e.Class], what, e.Service})
		}
		printTable(rows)
		fmt.Println()
	}
	return nil
}

func runAutostart(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "on":
		exe, _ := os.Executable()
		if err := autostart.Enable(exe); err != nil {
			return err
		}
	case len(args) == 1 && args[0] == "off":
		if err := autostart.Disable(); err != nil {
			return err
		}
	case len(args) > 0:
		return errors.New("用法: mcpcell autostart [on|off]")
	}
	st := autostart.Get()
	switch {
	case !st.Supported:
		fmt.Println(st.Error)
	case st.Enabled:
		fmt.Println("开机自启：已开启\n  " + st.Command)
	default:
		fmt.Println("开机自启：未开启（mcpcell autostart on）")
	}
	return nil
}
