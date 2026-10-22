package main

import (
	"fmt"
	"os"

	"github.com/luohui1/MCP-cell/internal/autostart"
	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// interfaceStatus is GET/POST /api/interface.
type interfaceStatus struct {
	Interface     string `json:"interface"`
	Tray          bool   `json:"tray"`
	TraySupported bool   `json:"traySupported"`
	Error         string `json:"error"`
}

var interfaceName = map[string]string{registry.InterfaceGUI: "图形界面", registry.InterfaceCLI: "命令行"}

const cliCommands = `    mcpcell ps          服务、实例、会话与内存
    mcpcell clients     各 Agent 的 MCP 条目与托管状态
    mcpcell takeover    托管 Agent 的 MCP（先列出计划，确认后执行）
    mcpcell restore     还原托管
    mcpcell start/stop  启动、停止守护进程
    mcpcell open        临时打开 MCPcell 窗口
`

// guiMode reports whether the graphical interface is chosen (and there is
// a desktop for it): the tray icon runs and starts at logon.
func guiMode() bool {
	home, err := registry.Home()
	if err != nil || desktop.Available() == desktop.ErrUnsupported {
		return false
	}
	cfg, err := registry.Load(home)
	return err == nil && !cfg.CLI()
}

// runMode shows the interface choice, or switches between the GUI (tray
// icon) and the command line. Only the tray icon changes: the daemon and
// the MCPs it runs are not affected.
func runMode(args []string) error {
	switch {
	case len(args) == 0:
		return showMode()
	case len(args) > 1:
		return badUsage("多余的参数 %q", args[1])
	}
	mode, err := registry.ParseInterface(args[0])
	if err != nil {
		return badUsage("%v", err)
	}
	st, err := setInterface(mode)
	if err != nil {
		return err
	}
	fmt.Printf("已切换为%s。", interfaceName[mode])
	switch {
	case !st.TraySupported:
		fmt.Println("这台电脑上没有托盘图标可显示。")
	case st.Error != "":
		fmt.Println("托盘图标：" + st.Error)
	case st.Tray:
		fmt.Println("托盘图标已显示。")
	default:
		fmt.Println("托盘图标已关闭，后台服务和 MCP 照常运行。")
	}
	if mode == registry.InterfaceCLI {
		fmt.Print("\n常用命令：\n" + cliCommands)
	}
	return nil
}

func showMode() error {
	home, err := registry.Home()
	if err != nil {
		return err
	}
	cfg, err := registry.Load(home)
	if err != nil {
		return err
	}
	mode := registry.InterfaceGUI
	if cfg.CLI() {
		mode = registry.InterfaceCLI
	}
	fmt.Printf("界面方式：%s（%s）\n", interfaceName[mode], mode)
	if desktop.TrayRunning(home) {
		fmt.Println("托盘图标：显示中")
	} else {
		fmt.Println("托盘图标：没有显示")
	}
	if daemonCall("GET", "/api/status", &psStatus{}) == nil {
		fmt.Println("后台服务：运行中")
	} else {
		fmt.Println("后台服务：未运行")
	}
	fmt.Println("\n两种方式的内核完全一样：接管、按需启动、能力目录、断线重连都相同，只是操作入口不同；")
	fmt.Println("托盘图标是单独的程序，退出它不影响后台服务。")
	if mode == registry.InterfaceGUI {
		fmt.Println("图形界面：托盘图标常驻，点击打开 MCPcell 窗口。")
		fmt.Println("改用命令行：mcpcell mode cli")
		return nil
	}
	fmt.Print("命令行：不显示托盘图标，用这些命令管理：\n" + cliCommands)
	fmt.Println("改用图形界面：mcpcell mode gui")
	return nil
}

// setInterface records the choice and starts or stops the tray icon (and
// its logon entry): through the running daemon, or directly when none runs.
func setInterface(mode string) (interfaceStatus, error) {
	var st interfaceStatus
	if daemonCall("GET", "/api/interface", &st) == nil {
		err := daemonSend("POST", "/api/interface", map[string]string{"interface": mode}, &st)
		return st, err
	}
	home, err := registry.Home()
	if err != nil {
		return st, err
	}
	if err := registry.SetInterface(home, mode); err != nil {
		return st, err
	}
	st = interfaceStatus{Interface: mode, TraySupported: desktop.Available() != desktop.ErrUnsupported}
	if !st.TraySupported {
		return st, nil
	}
	gui := mode == registry.InterfaceGUI
	exe, _ := os.Executable()
	if gui {
		if err := desktop.StartTray(exe, home); err != nil {
			st.Error = err.Error()
		}
	} else {
		desktop.StopTray(home)
	}
	if a := autostart.Get(); a.Ours(exe) && a.Tray != gui {
		if err := autostart.SetTray(exe, gui); err != nil && st.Error == "" {
			st.Error = err.Error()
		}
	}
	st.Tray = desktop.TrayRunning(home)
	return st, nil
}
