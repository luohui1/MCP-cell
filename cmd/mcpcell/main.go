// Command mcpcell is the single binary for the MCPcell runtime: the per-user
// daemon, the stdio shim for clients without HTTP support, and the CLI.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/luohui1/MCP-cell/internal/desktop"
)

const version = "0.6.0-dev"

type command struct {
	name, usage string
	run         func(args []string) error
}

// commands in the order the usage lists them. Every one checks its
// arguments before doing anything, and -h/--help anywhere only prints its
// usage: `mcpcell stop --help` once stopped a running daemon.
var commands = []command{
	{"open", "open                 打开 MCPcell 窗口（需要时先启动后台服务，图形界面方式下也显示托盘图标）；双击 mcpcelld.exe 同此", runOpen},
	{"ui", "ui [--browser]       同 open；--browser 在浏览器中打开管理页", runOpen},
	{"start", "start                在后台启动守护进程（无窗口，日志写到 ~/.mcpcell/daemon.log）；图形界面方式下同时显示托盘图标", noArgs(func([]string) error { return runStart() })},
	{"stop", "stop                 停止守护进程：正在用的 MCP 会各自改为直接运行，共享的在 MCPcell 恢复后自动回来", noArgs(func([]string) error { return runStop() })},
	{"daemon", "daemon [--port N] [--log] [--open]\n                       在前台运行守护进程（Ctrl+C 退出）", runDaemon},
	{"mode", "mode [cli|gui]       界面方式：gui 图形界面（托盘图标，默认），cli 命令行（不显示托盘图标）；\n                       不带参数显示当前方式。只决定托盘图标，后台服务和 MCP 不受影响", runMode},
	{"ps", "ps                   查看服务、实例、会话与内存（私有提交）", noArgs(func([]string) error { return runPS() })},
	{"clients", "clients              列出各客户端的 MCP 条目、托管状态与运行方式", noArgs(func([]string) error { return runClients() })},
	{"takeover", "takeover [条目...] [--client 客户端] [--shared|--per-session] [-y]\n                       托管 Agent 的 MCP（同管理页「一键托管」）：先列出计划，确认后执行", runTakeover},
	{"restore", "restore --all        还原所有被托管的客户端配置（不需要守护进程）\n  restore <客户端> [条目...]", runRestore},
	{"skills", "skills [名称...]     各 Agent 的 Skill 在哪些目录、各处是否一致（不需要守护进程）\n  skills sync <名称...>|--all [--to 位置,...] [--from 位置] [--copy] [-y]\n                       把 Skill 放进 MCPcell 的来源目录，各 Agent 的目录链接到它\n  skills restore <名称...>|--all\n                       逐字节还原同步时替换的内容", runSkills},
	{"autostart", "autostart [on|off]   开机自启（图形界面方式下托盘图标也随之启动）", runAutostart},
	{"shortcut", "shortcut [on|off]    桌面和开始菜单快捷方式", runShortcut},
	{"install", "install [--cli|--gui]\n                       把程序装到 ~/.mcpcell/bin，开机自启、快捷方式和 Agent 配置都指向那里；\n                       --cli/--gui 同时选择界面方式，不写则保持不变", runInstall},
	{"update", "update <服务>        把 npx 服务的 npm 包更新到最新版本并重新锁定", runUpdateArgs},
	{"version", "version              显示版本", noArgs(func([]string) error { fmt.Println("mcpcell", version); return nil })},
	// The window process `open` and the daemon start; not in the usage.
	{"window", "", runWindowArgs},
}

func usageText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "mcpcell %s — 本机 MCP 运行时：每个服务一份进程，所有 Agent 共用\n\n用法: mcpcell <命令> [参数]\n\n", version)
	for _, c := range commands {
		if c.usage != "" {
			b.WriteString("  " + c.usage + "\n")
		}
	}
	b.WriteString("\n每个命令都可以加 -h 查看用法。服务定义在 ~/.mcpcell/config.json（可用 MCPCELL_HOME 改目录）。\n")
	return b.String()
}

func commandUsage(name string) string {
	for _, c := range commands {
		if c.name == name && c.usage != "" {
			return "用法: mcpcell " + c.usage + "\n"
		}
	}
	return usageText()
}

// usageError is a wrong invocation: reported with the usage, exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func badUsage(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// noArgs wraps a command that takes no arguments.
func noArgs(run func([]string) error) func([]string) error {
	return func(args []string) error {
		if len(args) > 0 {
			return badUsage("多余的参数 %q", args[0])
		}
		return run(nil)
	}
}

// parseFlags parses a command's flags and rejects anything left over.
func parseFlags(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return badUsage("%v", err)
	}
	if fs.NArg() > 0 {
		return badUsage("多余的参数 %q", fs.Arg(0))
	}
	return nil
}

func isHelp(a string) bool { return a == "-h" || a == "--help" || a == "-help" || a == "/?" }

func main() {
	args := os.Args[1:]
	if len(args) == 0 && !desktop.HasConsole() {
		// mcpcelld.exe double-clicked or pinned: open the window.
		args = []string{"open"}
	}
	os.Exit(dispatch(args, os.Stdout, os.Stderr))
}

// dispatch runs one command line and returns the exit code: 0, 1 when the
// command failed, 2 for a wrong invocation (nothing was done).
func dispatch(argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprint(stderr, usageText())
		return 2
	}
	name, args := argv[0], argv[1:]
	switch {
	case name == "help" || isHelp(name):
		if len(args) > 0 {
			fmt.Fprint(stdout, commandUsage(args[0]))
		} else {
			fmt.Fprint(stdout, usageText())
		}
		return 0
	case name == "--version" || name == "-v":
		name = "version"
	}
	i := slices.IndexFunc(commands, func(c command) bool { return c.name == name })
	if i < 0 {
		return report(stderr, badUsage("未知命令 %q", name), usageText())
	}
	if slices.ContainsFunc(args, isHelp) {
		fmt.Fprint(stdout, commandUsage(name))
		return 0
	}
	err := commands[i].run(args)
	if err != nil && (name == "open" || name == "ui" || name == "window") && !desktop.HasConsole() {
		// mcpcelld.exe was opened by the user and has nowhere to print to.
		desktop.Alert("MCPcell", err.Error())
		return 1
	}
	return report(stderr, err, commandUsage(name))
}

func report(w io.Writer, err error, usage string) int {
	if err == nil {
		return 0
	}
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(w, "mcpcell: %s\n\n%s", ue.msg, usage)
		return 2
	}
	fmt.Fprintln(w, "mcpcell:", err)
	return 1
}

func runUpdateArgs(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return badUsage("需要一个服务名")
	}
	return runUpdate(args[0])
}

func runWindowArgs(args []string) error {
	err := runWindow(args)
	var ue usageError
	if err != nil && !errors.As(err, &ue) {
		return errors.New("无法打开 MCPcell 窗口：" + err.Error() + "\n\n可以运行 mcpcell ui --browser 在浏览器里打开管理页。")
	}
	return err
}
