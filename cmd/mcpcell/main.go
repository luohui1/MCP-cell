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

  daemon [--port N]    启动守护进程（前台运行，Ctrl+C 退出）
  ps                   查看服务、实例、会话与内存（私有提交）
  ui                   在浏览器中打开管理页
  version              显示版本

服务定义在 ~/.mcpcell/config.json（可用 MCPCELL_HOME 改目录）。
规划中的 import / attach / restore / shim 见 docs/roadmap.md。
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
