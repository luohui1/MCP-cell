package desktop

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed mcpcell.ico
var iconData []byte

const (
	trayClass = "MCPcellTray"
	trayID    = 1

	menuOpen = 1
	menuQuit = 2

	createNoWindow  = 0x08000000
	detachedProcess = 0x00000008
)

// tray is the tray process's state (UI thread only).
type tray struct {
	opts           TrayOptions
	log            *slog.Logger
	hwnd           windows.HWND // hidden window that receives tray messages
	icon           windows.Handle
	taskbarCreated uint32
	tipAt          time.Time
	shown          bool // the icon was added (Explorer may not be up yet at logon)
}

var theTray *tray

// RunTray is the body of mcpcell-tray.exe: the notification-area icon of
// one MCPcell home, until 退出. It only looks at the daemon and opens the
// window; quitting it leaves the daemon and every MCP it runs alone. It
// returns at once when that home's tray already runs.
func RunTray(opts TrayOptions) error {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if !singleInstance("MCPcellTray", opts.Home) {
		return nil
	}
	lockThread()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		procSetThreadDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	}
	t := &tray{opts: opts, log: opts.Log}
	theTray = t
	t.icon = loadIcon(int(systemMetric(smCxSmIcon, dpiForSystem())))
	hwnd, err := createHiddenWindow(trayClass, trayProc)
	if err != nil {
		return err
	}
	t.hwnd = hwnd
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	t.taskbarCreated = uint32(r)
	if err := t.add(); err != nil {
		// Explorer may not be up yet at logon; TaskbarCreated adds it then.
		t.log.Warn("tray icon not added yet", "err", err)
	}
	// Findable (TrayRunning, StartTray) only once the icon is there.
	markHome(hwnd, opts.Home)
	messageLoop()
	return nil
}

func (t *tray) data() notifyIconData {
	d := notifyIconData{Wnd: t.hwnd, ID: trayID}
	d.Size = uint32(unsafe.Sizeof(d))
	return d
}

func (t *tray) add() error {
	d := t.data()
	d.Flags = nifMessage | nifIcon | nifTip | nifShowTip
	d.CallbackMsg = wmTray
	d.Icon = t.icon
	copyUTF16(d.Tip[:], t.tooltip())
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d))); r == 0 {
		return fmt.Errorf("Shell_NotifyIcon: %w", err)
	}
	d.Version = notifyIconV4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&d)))
	t.tipAt, t.shown = time.Now(), true
	return nil
}

func (t *tray) remove() {
	if !t.shown {
		return
	}
	d := t.data()
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	t.shown = false
}

func (t *tray) status() []string {
	if t.opts.Status == nil {
		return nil
	}
	return t.opts.Status()
}

func (t *tray) tooltip() string {
	tip := "MCPcell"
	if lines := t.status(); len(lines) > 0 {
		tip += "\n" + lines[0]
	}
	return tip
}

// refreshTooltip runs on hover, at most once a second.
func (t *tray) refreshTooltip() {
	if time.Since(t.tipAt) < time.Second {
		return
	}
	t.tipAt = time.Now()
	d := t.data()
	d.Flags = nifTip | nifShowTip
	copyUTF16(d.Tip[:], t.tooltip())
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

// open runs `mcpcelld open`, which starts the daemon if it is not running
// and opens the window or brings it to the front. Doing that in a short-lived
// process keeps the HTTP client (and its fixed memory) out of the tray.
func (t *tray) open() {
	exe := filepath.Join(filepath.Dir(t.opts.Exe), "mcpcelld.exe")
	if _, err := os.Stat(exe); err != nil {
		messageBox(t.hwnd, "同目录下缺少 mcpcelld.exe，无法打开 MCPcell。", "MCPcell", mbOK|mbIconError|mbSetForeground)
		return
	}
	p, err := start(exe, t.opts.Home, "open")
	if err != nil {
		messageBox(t.hwnd, "无法打开 MCPcell："+err.Error(), "MCPcell", mbOK|mbIconError|mbSetForeground)
		return
	}
	_ = p.Release()
}

// quit closes the window and the tray. The daemon and the MCPs it runs stay.
func (t *tray) quit() {
	CloseWindow(t.opts.Home)
	t.remove()
	unmarkHome(t.hwnd)
	procDestroyWindow.Call(uintptr(t.hwnd))
	procPostQuitMessage.Call(0)
}

func (t *tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	add := func(flags, id uintptr, text string) {
		procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(utf16(text))))
	}
	add(mfString, menuOpen, "打开 MCPcell")
	procSetMenuDefaultItem.Call(menu, menuOpen, 0)
	if lines := t.status(); len(lines) > 0 {
		add(mfSeparator, 0, "")
		for _, l := range lines {
			add(mfString|mfGrayed, 0, l)
		}
	}
	add(mfSeparator, 0, "")
	add(mfString, menuQuit, "退出（MCP 照常可用）")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu does not close when the user clicks elsewhere.
	procSetForegroundWindow.Call(uintptr(t.hwnd))
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), uintptr(t.hwnd), 0)
	postMessage(t.hwnd, wmNull, 0, 0)
	switch cmd {
	case menuOpen:
		t.open()
	case menuQuit:
		t.quit()
	}
}

func trayProc(hwnd windows.HWND, m uint32, w, l uintptr) uintptr {
	t := theTray
	if t == nil || hwnd != t.hwnd {
		// Messages sent during CreateWindowEx, before t.hwnd is set.
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
		return r
	}
	switch m {
	case wmTray:
		switch loword(l) {
		case ninSelect, ninKeySelect, wmLButtonDblClk:
			t.open()
		case wmContextMenu:
			t.showMenu()
		case wmMouseMove:
			t.refreshTooltip()
		}
		return 0
	case wmTrayQuit, wmClose:
		// mcpcell mode cli, or the settings page switching to the command line.
		t.quit()
		return 0
	case wmEndSession:
		if w != 0 {
			t.remove()
		}
		return 0
	}
	if m == t.taskbarCreated && t.taskbarCreated != 0 {
		t.shown = false
		if err := t.add(); err != nil {
			t.log.Warn("re-adding tray icon", "err", err)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
	return r
}
