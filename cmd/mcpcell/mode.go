package main

import (
	"errors"
	"fmt"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// interfaceStatus is GET/POST /api/interface.
type interfaceStatus struct {
	Interface     string `json:"interface"`
	Tray          bool   `json:"tray"`
	TraySupported bool   `json:"traySupported"`
}

var interfaceName = map[string]string{registry.InterfaceGUI: "图形界面", registry.InterfaceCLI: "命令行"}

const cliCommands = `    mcpcell ps          服务、实例、会话与内存
    mcpcell clients     各 Agent 的 MCP 条目与托管状态
    mcpcell takeover    托管 Agent 的 MCP（先列出计划，确认后执行）
    mcpcell restore     还原托管
    mcpcell start/stop  启动、停止守护进程
    mcpcell open        临时打开 MCPcell 窗口
`

// runMode shows the interface choice, or switches between the GUI (tray
// icon) and the command line. A running daemon switches at once.
func runMode(args []string) error {
	switch {
	case len(args) == 0:
		return showMode()
	case len(args) > 1:
		return errors.New("用法: mcpcell mode [cli|gui]")
	}
	mode, err := registry.ParseInterface(args[0])
	if err != nil {
		return err
	}
	st, live, err := setInterface(mode)
	if err != nil {
		return err
	}
	fmt.Printf("已切换为%s。", interfaceName[mode])
	switch {
	case !live:
		fmt.Println("守护进程下次启动时生效（开机自启、mcpcell start 和 Agent 拉起它时都按这个方式）。")
	case !st.TraySupported:
		fmt.Println("这个守护进程不显示托盘图标（以 --no-tray 运行，或系统不支持）。")
	case st.Tray:
		fmt.Println("托盘图标已显示。")
	default:
		fmt.Println("托盘图标已隐藏，MCPcell 继续在后台运行。")
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
	var st interfaceStatus
	switch {
	case daemonCall("GET", "/api/interface", &st) == nil && st.Tray:
		fmt.Println("守护进程：运行中，显示托盘图标")
	case st.Interface != "":
		fmt.Println("守护进程：运行中，不显示托盘图标")
	case daemonCall("GET", "/api/status", &psStatus{}) == nil:
		fmt.Println("守护进程：运行中（旧版本，重启后按当前方式显示）")
	default:
		fmt.Println("守护进程：未运行")
	}
	fmt.Println("\n两种方式的内核完全一样：接管、按需启动、能力目录、断线重连都相同，只是操作入口不同。")
	if mode == registry.InterfaceGUI {
		fmt.Println("图形界面：托盘图标常驻，点击打开 MCPcell 窗口。")
		fmt.Println("改用命令行：mcpcell mode cli")
		return nil
	}
	fmt.Print("命令行：不显示托盘图标，用这些命令管理：\n" + cliCommands)
	fmt.Println("改用图形界面：mcpcell mode gui")
	return nil
}

// setInterface records the choice through the running daemon, which shows
// or hides its tray icon at once, or in config.json when none runs. live
// reports that a daemon applied it.
func setInterface(mode string) (st interfaceStatus, live bool, err error) {
	if daemonCall("GET", "/api/interface", &st) == nil {
		err = daemonSend("POST", "/api/interface", map[string]string{"interface": mode}, &st)
		return st, err == nil, err
	}
	home, err := registry.Home()
	if err != nil {
		return st, false, err
	}
	// An older daemon without the setting reads the file on its next start.
	return interfaceStatus{Interface: mode}, false, registry.SetInterface(home, mode)
}
