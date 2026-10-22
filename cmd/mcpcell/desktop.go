package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// runWindow is the body of the window process the daemon starts; it is
// not in the usage text.
func runWindow(args []string) error {
	fs := flag.NewFlagSet("window", flag.ContinueOnError)
	url := fs.String("url", "", "首个页面（守护进程给的一次性登录链接）")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	home, err := registry.Home()
	if err != nil {
		return err
	}
	return desktop.RunWindow(desktop.WindowOptions{Home: home, URL: *url})
}

// runOpen shows the MCPcell window, starting the daemon first if needed,
// and with the graphical interface the tray icon too. Without a window (no
// WebView2, another OS) it opens the page in the browser instead.
func runOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	browser := fs.Bool("browser", false, "在浏览器中打开管理页")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if _, _, err := ensureDaemon(); err != nil {
		return err
	}
	home, err := registry.Home()
	if err != nil {
		return err
	}
	ensureTray(home)
	if !*browser && desktop.Available() == nil {
		var out struct{ URL string }
		if err := daemonCall("POST", "/api/login-code", &out); err == nil {
			// We were just started by the user, so the window we start may
			// come to the front.
			exe, _ := os.Executable()
			if err := desktop.OpenWindow(exe, home, out.URL); err == nil {
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
		return badUsage("参数只能是 on 或 off")
	}
	d, s := desktop.Shortcuts()
	state := map[bool]string{true: "已创建", false: "未创建"}
	fmt.Printf("桌面快捷方式：%s\n开始菜单：%s\n", state[d], state[s])
	if !d && !s && len(args) == 0 {
		fmt.Println("创建：mcpcell shortcut on")
	}
	return nil
}

// desktopShell is httpd.Desktop for the daemon: it starts the window and
// tray processes, which have the user interface the daemon does not.
type desktopShell struct {
	home, exe string
	loginURL  func() string
}

func (d *desktopShell) Show() error {
	if err := desktop.Available(); err != nil {
		return err
	}
	return desktop.OpenWindow(d.exe, d.home, d.loginURL())
}

func (d *desktopShell) Available() error                { return desktop.Available() }
func (d *desktopShell) Shortcuts() (dt, sm bool)        { return desktop.Shortcuts() }
func (d *desktopShell) SetShortcuts(dt, sm *bool) error { return desktop.SetShortcuts(d.exe, dt, sm) }
func (d *desktopShell) TrayShown() bool                 { return desktop.TrayRunning(d.home) }

func (d *desktopShell) SetTray(on bool) error {
	if on {
		return desktop.StartTray(d.exe, d.home)
	}
	desktop.StopTray(d.home)
	return nil
}

// ensureTray starts the tray icon with the graphical interface, where
// there is a desktop; it reports whether the icon is up.
func ensureTray(home string) bool {
	cfg, err := registry.Load(home)
	if err != nil || cfg.CLI() || desktop.Available() == desktop.ErrUnsupported {
		return false
	}
	exe, _ := os.Executable()
	if err := desktop.StartTray(exe, home); err != nil {
		fmt.Fprintln(os.Stderr, "托盘图标没有显示：", err)
		return false
	}
	return true
}
