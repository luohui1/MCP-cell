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

const version = "0.6.2"

type command struct {
	name, usage string
	run         func(args []string) error
}

// commands in the order `mcpcell help --all` lists them; plain `help` lists
// only the everyday ones at the top. Every one checks its arguments before
// doing anything, and -h/--help anywhere only prints its usage: `mcpcell
// stop --help` once stopped a running daemon.
var commands = []command{
	{"open", "open                 打开 MCPcell 窗口（需要时先启动后台服务）；双击 mcpcelld.exe 同此", runOpen},
	{"status", "status               各个 MCP 开着几份、连着的对话、占多少内存（私有提交）", noArgs(func([]string) error { return runPS() })},
	{"takeover", "takeover [条目...] [--client 客户端] [--shared|--per-session] [-y]\n                       让 Agent 的 MCP 改用 MCPcell 共用的这份（同窗口里的「一键托管」）：先列出计划，确认后执行\n  takeover --auto on|off\n                       自动托管 Agent 里新加的 MCP（默认开）", runTakeover},
	{"restore", "restore --all        全部改回原样（不需要后台服务）\n  restore <客户端> [条目...]", runRestore},
	{"stop", "stop                 停止后台服务：正在用的 MCP 会各自改为直接运行，共享的在 MCPcell 恢复后自动回来", noArgs(func([]string) error { return runStop() })},
	{"version", "version              显示版本", noArgs(func([]string) error { fmt.Println("mcpcell", version); return nil })},
	{"ui", "ui [--browser]       同 open；--browser 在浏览器中打开管理页", runOpen},
	{"start", "start                在后台启动守护进程（无窗口，日志写到 ~/.mcpcell/daemon.log）；图形界面方式下同时显示托盘图标", noArgs(func([]string) error { return runStart() })},
	{"daemon", "daemon [--port N] [--log] [--open]\n                       在前台运行守护进程（Ctrl+C 退出）", runDaemon},
	{"mode", "mode [cli|gui]       托盘图标：gui 显示（默认），cli 不显示；不带参数显示当前方式。后台服务和 MCP 不受影响", runMode},
	{"ps", "ps                   同 status", noArgs(func([]string) error { return runPS() })},
	{"clients", "clients              列出各客户端的 MCP 条目、托管状态与运行方式", noArgs(func([]string) error { return runClients() })},
	{"autostart", "autostart [on|off]   开机自启（图形界面方式下托盘图标也随之启动）", runAutostart},
	{"shortcut", "shortcut [on|off]    桌面和开始菜单快捷方式", runShortcut},
	{"install", "install [--cli|--gui]\n                       把程序装到 ~/.mcpcell/bin，开机自启、快捷方式和 Agent 配置都指向那里；\n                       --cli/--gui 同时选择界面方式，不写则保持不变", runInstall},
	{"update", "update <服务>        把 npx 服务的 npm 包更新到最新版本并重新锁定", runUpdateArgs},
	// The window process `open` and the daemon start; not in the usage.
	{"window", "", runWindowArgs},
}

// everyday is how many commands, from the top, plain `help` lists.
const everyday = 6

// usageText lists the everyday commands, or with all every command.
func usageText(all bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mcpcell %s — 本机 MCP 运行时：每个 MCP 全机只开一份，所有 Agent 共用\n\n用法: mcpcell <命令> [参数]\n\n", version)
	list := commands
	if !all {
		list = commands[:everyday]
	}
	for _, c := range list {
		if c.usage != "" {
			b.WriteString("  " + c.usage + "\n")
		}
	}
	if all {
		b.WriteString("\n每个命令都可以加 -h 查看用法。服务定义在 ~/.mcpcell/config.json（可用 MCPCELL_HOME 改目录）。\n")
	} else {
		b.WriteString("\n每个命令都可以加 -h 查看用法；mcpcell help --all 列出全部命令。\n")
	}
	return b.String()
}

func commandUsage(name string) string {
	for _, c := range commands {
		if c.name == name && c.usage != "" {
			return "用法: mcpcell " + c.usage + "\n"
		}
	}
	return usageText(false)
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
		fmt.Fprint(stderr, usageText(false))
		return 2
	}
	name, args := argv[0], argv[1:]
	switch {
	case name == "help" || isHelp(name):
		if len(args) > 0 && args[0] != "--all" {
			fmt.Fprint(stdout, commandUsage(args[0]))
		} else {
			fmt.Fprint(stdout, usageText(len(args) > 0))
		}
		return 0
	case name == "--version" || name == "-v":
		name = "version"
	}
	i := slices.IndexFunc(commands, func(c command) bool { return c.name == name })
	if i < 0 {
		return report(stderr, badUsage("未知命令 %q", name), usageText(false))
	}
	if slices.ContainsFunc(args, isHelp) {
		fmt.Fprint(stdout, commandUsage(name))
		return 0
	}
	err := commands[i].run(args)
	var ue usageError
	if err != nil && !errors.As(err, &ue) && (name == "open" || name == "ui" || name == "window") && !desktop.HasConsole() {
		// mcpcelld.exe was opened by the user and has nowhere to print to.
		// A wrong invocation is not the user's double click, so it is only
		// reported as usual (a test or CI run without a console would wait
		// on the box forever).
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
