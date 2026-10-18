package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/pool"
	"github.com/luohui1/MCP-cell/internal/proc"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// runWindow is the body of the window process the daemon starts; it is
// not in the usage text.
func runWindow(args []string) error {
	fs := flag.NewFlagSet("window", flag.ContinueOnError)
	url := fs.String("url", "", "首个页面（守护进程给的一次性登录链接）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	home, err := registry.Home()
	if err != nil {
		return err
	}
	return desktop.RunWindow(desktop.WindowOptions{Home: home, URL: *url})
}

// runOpen shows the MCPcell window, starting the daemon first if needed.
// Without a window (no WebView2, another OS, an older daemon) it opens the
// page in the browser instead.
func runOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	browser := fs.Bool("browser", false, "在浏览器中打开管理页")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, _, err := ensureDaemon()
	if err != nil {
		return err
	}
	if !*browser {
		var ds struct {
			Supported, Webview bool
		}
		if daemonCall("GET", "/api/desktop", &ds) == nil && ds.Supported && ds.Webview {
			// We were just started by the user, so we may hand the
			// foreground to the daemon's window.
			desktop.AllowForeground(st.PID)
			var ok map[string]bool
			if err := daemonCall("POST", "/api/window", &ok); err == nil {
				return nil
			}
		}
	}
	return openBrowser()
}

func openBrowser() error {
	var out struct{ URL string }
	if err := daemonCall("POST", "/api/login-code", &out); err != nil {
		return err
	}
	fmt.Println("管理页：" + out.URL + "（链接 1 分钟内有效，只能用一次）")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", out.URL)
	case "darwin":
		cmd = exec.Command("open", out.URL)
	default:
		cmd = exec.Command("xdg-open", out.URL)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "无法自动打开浏览器，请手动打开上面的链接。")
	}
	return nil
}

func runShortcut(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 1 && (args[0] == "on" || args[0] == "off"):
		want := args[0] == "on"
		if err := desktop.SetShortcuts(exe, &want, &want); err != nil {
			return err
		}
	case len(args) > 0:
		return errors.New("用法: mcpcell shortcut [on|off]")
	}
	d, s := desktop.Shortcuts()
	state := map[bool]string{true: "已创建", false: "未创建"}
	fmt.Printf("桌面快捷方式：%s\n开始菜单：%s\n", state[d], state[s])
	if !d && !s && len(args) == 0 {
		fmt.Println("创建：mcpcell shortcut on")
	}
	return nil
}

// trayStatus is the tray tooltip's second line.
func trayStatus(p *pool.Pool) func() string {
	return func() string {
		n, total := 0, proc.Self().PrivateBytes
		for _, s := range p.Status() {
			if s.Instance != nil {
				n++
				total += s.Instance.Usage.PrivateBytes
			}
		}
		if n == 0 {
			return "没有运行中的 MCP · " + mb(total)
		}
		return fmt.Sprintf("%d 个 MCP 运行中 · 共 %s", n, mb(total))
	}
}
