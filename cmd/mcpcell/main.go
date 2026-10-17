// Command mcpcell is the single binary for the MCPcell runtime: the per-user
// daemon, the stdio shim for clients without HTTP support, and the CLI.
package main

import (
	"fmt"
	"os"
)

const version = "0.6.0-dev"

const usage = `mcpcell %s — 本机 MCP 运行时：每个服务一份进程，所有 Agent 共用

用法: mcpcell <命令> [参数]

  start                在后台启动守护进程（无窗口，日志写到 ~/.mcpcell/daemon.log）
  stop                 停止守护进程
  daemon [--port N]    在前台运行守护进程（Ctrl+C 退出）
  ps                   查看服务、实例、会话与内存（私有提交）
  ui                   在浏览器中打开管理页
  clients              列出各客户端的 MCP 条目与接管状态
  restore --all        还原所有被接管的客户端配置（不需要守护进程）
  restore <客户端> [条目...]
  autostart [on|off]   开机自启
  update <服务>        把 npx 服务的 npm 包更新到最新版本并重新锁定
  version              显示版本

服务定义在 ~/.mcpcell/config.json（可用 MCPCELL_HOME 改目录）。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "daemon":
		err = runDaemon(os.Args[2:])
	case "ps":
		err = runPS()
	case "ui":
		err = runUI()
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
