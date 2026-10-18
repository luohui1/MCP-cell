package desktop

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/luohui1/MCP-cell/internal/autostart"
)

//go:embed mcpcell.ico
var iconData []byte

const (
	trayClass = "MCPcellTray"
	trayID    = 1

	menuOpen      = 1
	menuAutostart = 2
	menuQuit      = 3

	createNoWindow = 0x08000000
)

// The single App of this process; the tray window procedure reaches it here.
var current *App

// App is the daemon's side: the tray icon, and the window process it
// starts on demand. The window runs in its own process (RunWindow) so the
// WebView2 runtime never loads into the daemon: it adds nothing to the
// daemon's memory once closed, and cannot take the shared MCPs down with it.
type App struct {
	opts Options
	log  *slog.Logger
	done chan struct{} // closed when the UI thread has exited
	stop sync.Once

	mu        sync.Mutex
	child     *os.Process   // the window process while it runs
	childDone chan struct{} // closed when it has exited

	// UI thread only.
	tray           windows.HWND // hidden window that receives tray and app messages
	trayIcon       windows.Handle
	bigIcon        windows.Handle
	taskbarCreated uint32
	tipAt          time.Time
	closedOnce     bool
}

// Start runs the UI thread and, with opts.Tray, adds the tray icon. It
// returns once the thread is ready.
func Start(opts Options) (*App, error) {
	if current != nil {
		return nil, errors.New("desktop: already started")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	a := &App{opts: opts, log: opts.Log, done: make(chan struct{})}
	current = a
	ready := make(chan error, 1)
	go a.run(ready)
	if err := <-ready; err != nil {
		current = nil
		return nil, err
	}
	return a, nil
}

// Show opens the window, or brings it to the front if it is already open.
func (a *App) Show() error {
	if err := Available(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.child != nil {
		// Still starting if it has no window yet; it comes up by itself.
		focusWindowOf(a.child.Pid)
		return nil
	}
	url := ""
	if a.opts.LoginURL != nil {
		url = a.opts.LoginURL()
	}
	cmd := exec.Command(shortcutTarget(a.opts.Exe), "window", "--url", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动窗口进程：%w", err)
	}
	// Pass on the foreground right we got from the caller (mcpcell open)
	// or from the tray click.
	AllowForeground(cmd.Process.Pid)
	done := make(chan struct{})
	a.child, a.childDone = cmd.Process, done
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		if a.child == cmd.Process {
			a.child = nil
		}
		a.mu.Unlock()
		close(done)
		if err != nil {
			a.log.Warn("desktop: window process", "err", err)
		}
		postMessage(a.tray, wmWindowClosed, 0, 0)
	}()
	return nil
}

// Close closes the window, removes the tray icon and ends the UI thread.
func (a *App) Close() {
	a.stop.Do(func() {
		a.mu.Lock()
		child, done := a.child, a.childDone
		a.mu.Unlock()
		if child != nil {
			closeWindowOf(child.Pid)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = child.Kill()
			}
		}
		postMessage(a.tray, wmStop, 0, 0)
		select {
		case <-a.done:
		case <-time.After(5 * time.Second):
			a.log.Warn("desktop: UI thread did not stop")
		}
	})
}

func (a *App) Available() error { return Available() }

func (a *App) Shortcuts() (desktop, startMenu bool) { return Shortcuts() }

func (a *App) SetShortcuts(desktop, startMenu *bool) error {
	return SetShortcuts(a.opts.Exe, desktop, startMenu)
}

func (a *App) run(ready chan<- error) {
	lockThread() // never unlocked: the thread ends with this goroutine
	defer close(a.done)
	if procSetThreadDpiAwarenessContext.Find() == nil {
		procSetThreadDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	}
	dpi := dpiForSystem()
	a.bigIcon = loadIcon(int(systemMetric(smCxIcon, dpi)))
	a.trayIcon = loadIcon(int(systemMetric(smCxSmIcon, dpi)))
	hwnd, err := createHiddenWindow(trayClass, trayProc)
	if err != nil {
		ready <- err
		return
	}
	a.tray = hwnd
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	a.taskbarCreated = uint32(r)
	if a.opts.Tray {
		if err := a.addTrayIcon(); err != nil {
			// Explorer may not be up yet at logon; TaskbarCreated retries.
			a.log.Warn("desktop: tray icon not added yet", "err", err)
		}
	}
	ready <- nil
	messageLoop()
}

func (a *App) trayData() notifyIconData {
	d := notifyIconData{Wnd: a.tray, ID: trayID}
	d.Size = uint32(unsafe.Sizeof(d))
	return d
}

func (a *App) addTrayIcon() error {
	d := a.trayData()
	d.Flags = nifMessage | nifIcon | nifTip | nifShowTip
	d.CallbackMsg = wmTray
	d.Icon = a.trayIcon
	copyUTF16(d.Tip[:], a.tooltip())
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d))); r == 0 {
		return fmt.Errorf("Shell_NotifyIcon: %w", err)
	}
	d.Version = notifyIconV4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&d)))
	a.tipAt = time.Now()
	a.log.Info("tray icon added")
	return nil
}

func (a *App) removeTrayIcon() {
	d := a.trayData()
	if r, _, err := procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d))); r == 0 {
		a.log.Warn("desktop: removing tray icon", "err", err)
	} else {
		a.log.Info("tray icon removed")
	}
}

func (a *App) tooltip() string {
	tip := "MCPcell"
	if a.opts.Status != nil {
		if s := a.opts.Status(); s != "" {
			tip += "\n" + s
		}
	}
	return tip
}

// refreshTooltip runs on hover, at most once a second.
func (a *App) refreshTooltip() {
	if time.Since(a.tipAt) < time.Second {
		return
	}
	a.tipAt = time.Now()
	d := a.trayData()
	d.Flags = nifTip | nifShowTip
	copyUTF16(d.Tip[:], a.tooltip())
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

func (a *App) balloon(title, text string) {
	if !a.opts.Tray {
		return
	}
	d := a.trayData()
	d.Flags = nifInfo
	copyUTF16(d.InfoTitle[:], title)
	copyUTF16(d.Info[:], text)
	d.InfoFlags = niifUser | niifLargeIcon | niifNoSound
	d.BalloonIcon = a.bigIcon
	if r, _, err := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d))); r == 0 {
		a.log.Warn("desktop: tray notification", "err", err)
	}
}

func (a *App) open() {
	if err := a.Show(); err != nil {
		messageBox(a.tray, "无法打开 MCPcell 窗口："+err.Error(), "MCPcell", mbOK|mbIconError|mbSetForeground)
	}
}

func (a *App) showMenu() {
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
	add(mfSeparator, 0, "")
	auto := uintptr(mfString)
	if autostart.Get().Enabled {
		auto |= mfChecked
	}
	add(auto, menuAutostart, "开机自启")
	add(mfSeparator, 0, "")
	add(mfString, menuQuit, "退出 MCPcell")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu does not close when the user clicks elsewhere.
	procSetForegroundWindow.Call(uintptr(a.tray))
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), uintptr(a.tray), 0)
	postMessage(a.tray, wmNull, 0, 0)
	switch cmd {
	case menuOpen:
		a.open()
	case menuAutostart:
		var err error
		if autostart.Get().Enabled {
			err = autostart.Disable()
		} else {
			err = autostart.Enable(a.opts.Exe)
		}
		if err != nil {
			messageBox(a.tray, "开机自启设置失败："+err.Error(), "MCPcell", mbOK|mbIconError)
		}
	case menuQuit:
		if messageBox(a.tray, "退出后，已托管的 MCP 在各个 Agent 里会不可用，直到 MCPcell 再次启动。确定退出？", "退出 MCPcell",
			mbYesNo|mbIconQuestion|mbDefButton2|mbSetForeground) == idYes && a.opts.Quit != nil {
			a.opts.Quit()
		}
	}
}

func trayProc(hwnd windows.HWND, m uint32, w, l uintptr) uintptr {
	a := current
	if a == nil || hwnd != a.tray {
		// Messages sent during CreateWindowEx, before a.tray is set.
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
		return r
	}
	switch m {
	case wmTray:
		switch loword(l) {
		case ninSelect, ninKeySelect, wmLButtonDblClk:
			a.open()
		case wmContextMenu:
			a.showMenu()
		case wmMouseMove:
			a.refreshTooltip()
		}
		return 0
	case wmWindowClosed:
		if !a.closedOnce {
			a.closedOnce = true
			a.balloon("MCPcell 仍在后台运行", "点托盘图标可以重新打开")
		}
		return 0
	case wmStop:
		if a.opts.Tray {
			a.removeTrayIcon()
		}
		procDestroyWindow.Call(uintptr(a.tray))
		procPostQuitMessage.Call(0)
		return 0
	}
	if m == a.taskbarCreated && a.taskbarCreated != 0 && a.opts.Tray {
		if err := a.addTrayIcon(); err != nil {
			a.log.Warn("desktop: re-adding tray icon", "err", err)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
	return r
}

// One EnumWindows callback for the life of the process (callbacks are a
// limited resource); the search state is guarded by enumMu.
var (
	enumMu    sync.Mutex
	enumPID   uint32
	enumFound windows.HWND
	enumProc  = sync.OnceValue(func() uintptr {
		return windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
			var p uint32
			procGetWindowThreadProcessId.Call(uintptr(h), uintptr(unsafe.Pointer(&p)))
			if p == enumPID && className(h) == windowClass {
				enumFound = h
				return 0
			}
			return 1
		})
	})
)

// windowOf finds the window process's main window.
func windowOf(pid int) windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPID, enumFound = uint32(pid), 0
	procEnumWindows.Call(enumProc(), 0)
	return enumFound
}

func focusWindowOf(pid int) {
	h := windowOf(pid)
	if h == 0 {
		return
	}
	if r, _, _ := procIsIconic.Call(uintptr(h)); r != 0 {
		procShowWindow.Call(uintptr(h), swRestore)
	} else {
		procShowWindow.Call(uintptr(h), swShow)
	}
	procBringWindowToTop.Call(uintptr(h))
	procSetForegroundWindow.Call(uintptr(h))
}

func closeWindowOf(pid int) {
	if h := windowOf(pid); h != 0 {
		postMessage(h, wmClose, 0, 0)
	}
}
