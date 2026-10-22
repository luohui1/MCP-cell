// Command mcpcell-tray is MCPcell's tray icon: a small process of its own
// that shows how the daemon is doing and opens the window. Quitting it
// closes the window and the icon only; the daemon and every MCP it runs keep
// going. It must not link net/http or crypto/rand, which would add a fixed
// 32 MB to its memory (docs/architecture.md §13), so it talks to the daemon
// over a plain socket.
package main

import (
	"fmt"
	"os"

	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/home"
)

const usage = `mcpcell-tray — MCPcell 的托盘图标

用法: mcpcell-tray

显示后台服务的状态，点击打开 MCPcell 窗口；退出只关闭托盘和窗口，
后台服务和正在用的 MCP 照常运行。图形界面方式下开机自动启动。`

func main() {
	if len(os.Args) > 1 {
		code := 2
		if a := os.Args[1]; a == "-h" || a == "--help" || a == "help" {
			code = 0
		}
		text := usage
		if code != 0 {
			text = fmt.Sprintf("未知参数 %q\n\n%s", os.Args[1], usage)
		}
		if desktop.HasConsole() {
			fmt.Fprintln(os.Stderr, text)
		} else {
			desktop.Alert("MCPcell", text)
		}
		os.Exit(code)
	}
	dir, err := home.Dir()
	if err == nil {
		exe, _ := os.Executable()
		err = desktop.RunTray(desktop.TrayOptions{Home: dir, Exe: exe, Status: func() []string { return status(dir) }})
	}
	if err != nil {
		desktop.Alert("MCPcell", "无法显示托盘图标："+err.Error())
		os.Exit(1)
	}
}
