// Command mcpcell is the single binary for the MCPcell runtime: the per-user
// daemon, the stdio shim for clients without HTTP support, and the CLI.
package main

import (
	"fmt"
	"os"

	"github.com/luohui1/MCP-cell/internal/desktop"
)

const version = "0.6.0-dev"

const usage = `mcpcell %s — 本机 MCP 运行时：每个服务一份进程，所有 Agent 共用

用法: mcpcell <命令> [参数]

  open                 打开 MCPcell 窗口（需要时先启动守护进程）；双击 mcpcelld.exe 同此
  ui [--browser]       同 open；--browser 在浏览器中打开管理页
  start                在后台启动守护进程（无窗口，日志写到 ~/.mcpcell/daemon.log）
  stop                 停止守护进程
  daemon [--port N] [--no-tray] [--open]
                       在前台运行守护进程（Ctrl+C 退出）
  mode [cli|gui]       界面方式：gui 图形界面（托盘图标，默认），cli 命令行（不显示托盘图标）；
                       不带参数显示当前方式。运行中切换立即生效，内核完全一样
  ps                   查看服务、实例、会话与内存（私有提交）
  clients              列出各客户端的 MCP 条目、托管状态与运行方式
  takeover [条目...] [--client 客户端] [--shared|--per-session] [-y]
                       托管 Agent 的 MCP（同管理页「一键托管」）：先列出计划，确认后执行
  restore --all        还原所有被托管的客户端配置（不需要守护进程）
  restore <客户端> [条目...]
  autostart [on|off]   开机自启
  shortcut [on|off]    桌面和开始菜单快捷方式
  install [--cli|--gui]
                       把程序装到 ~/.mcpcell/bin，开机自启、快捷方式和 Agent 配置都指向那里；
                       --cli/--gui 同时选择界面方式，不写则保持不变
  update <服务>        把 npx 服务的 npm 包更新到最新版本并重新锁定
  version              显示版本

服务定义在 ~/.mcpcell/config.json（可用 MCPCELL_HOME 改目录）。
`

func main() {
	if len(os.Args) < 2 {
		if !desktop.HasConsole() {
			// mcpcelld.exe double-clicked or pinned: open the window.
			os.Args = append(os.Args, "open")
		} else {
			fmt.Fprintf(os.Stderr, usage, version)
			os.Exit(2)
		}
	}
	var err error
	switch os.Args[1] {
	case "daemon":
		err = runDaemon(os.Args[2:])
	case "ps":
		err = runPS()
	case "open", "ui":
		err = runOpen(os.Args[2:])
		if err != nil && !desktop.HasConsole() {
			desktop.Alert("MCPcell", err.Error())
		}
	case "shortcut":
		err = runShortcut(os.Args[2:])
	case "install":
		err = runInstall(os.Args[2:])
	case "mode":
		err = runMode(os.Args[2:])
	case "takeover":
		err = runTakeover(os.Args[2:])
	case "window":
		if err = runWindow(os.Args[2:]); err != nil {
			desktop.Alert("MCPcell", "无法打开 MCPcell 窗口："+err.Error()+"\n\n可以运行 mcpcell ui --browser 在浏览器里打开管理页。")
		}
	case "start":
		err = runStart()
	case "stop":
		err = runStop()
	case "clients":
		err = runClients()
	case "restore":
		err = runRestore(os.Args[2:])
	case "autostart":
		err = runAutostart(os.Args[2:])
	case "update":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "用法: mcpcell update <服务>")
			os.Exit(2)
		}
		err = runUpdate(os.Args[2])
	case "version", "--version", "-v":
		fmt.Println("mcpcell", version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		fmt.Fprintf(os.Stderr, "mcpcell: 未知命令 %q\n\n", os.Args[1])
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpcell:", err)
		os.Exit(1)
	}
}
