package desktop

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The tray icon and the window are processes of their own. Their windows
// carry a property with a hash of the MCPcell home they serve, so they are
// found by class and home: a trial MCPcell (another MCPCELL_HOME) never
// mistakes the installed one's tray or window for its own.

const homeProp = "MCPcellHome"

func homeID(home string) uintptr {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(filepath.Clean(home))))
	return uintptr(h.Sum32()) | 1 // never 0, which GetProp returns for "none"
}

func markHome(hwnd windows.HWND, home string) {
	procSetPropW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16(homeProp))), homeID(home))
}

func unmarkHome(hwnd windows.HWND) {
	procRemovePropW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16(homeProp))))
}

// One EnumWindows callback for the life of the process (callbacks are a
// limited resource); the search state is guarded by findMu.
var (
	findMu    sync.Mutex
	findClass string
	findID    uintptr
	found     windows.HWND
	findProc  = sync.OnceValue(func() uintptr {
		return windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
			if className(h) != findClass {
				return 1
			}
			if r, _, _ := procGetPropW.Call(uintptr(h), uintptr(unsafe.Pointer(utf16(homeProp)))); r == findID {
				found = h
				return 0
			}
			return 1
		})
	})
)

// findWindow returns home's top-level window of class, or 0.
func findWindow(class, home string) windows.HWND {
	findMu.Lock()
	defer findMu.Unlock()
	findClass, findID, found = class, homeID(home), 0
	procEnumWindows.Call(findProc(), 0)
	return found
}

func waitFor(d time.Duration, done func() bool) bool {
	for deadline := time.Now().Add(d); ; time.Sleep(50 * time.Millisecond) {
		if done() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

// start runs a program of the installation for home, detached and without
// a console; the caller passes on its foreground right.
func start(path, home string, args ...string) (*os.Process, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), "MCPCELL_HOME="+home)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | detachedProcess}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	AllowForeground(cmd.Process.Pid)
	return cmd.Process, nil
}

// TrayPath is the tray program that belongs with exe (any program of the
// installation).
func TrayPath(exe string) string { return filepath.Join(filepath.Dir(exe), "mcpcell-tray.exe") }

// TrayRunning reports whether home's tray icon process runs.
func TrayRunning(home string) bool { return findWindow(trayClass, home) != 0 }

// StartTray starts the tray icon for home unless it runs, and waits until it
// is up.
func StartTray(exe, home string) error {
	if TrayRunning(home) {
		return nil
	}
	tray := TrayPath(exe)
	if _, err := os.Stat(tray); err != nil {
		return errors.New("同目录下缺少 mcpcell-tray.exe（托盘图标）")
	}
	p, err := start(tray, home)
	if err != nil {
		return fmt.Errorf("无法启动托盘图标：%w", err)
	}
	_ = p.Release()
	if !waitFor(5*time.Second, func() bool { return TrayRunning(home) }) {
		return errors.New("托盘图标没有在 5 秒内出现")
	}
	return nil
}

// StopTray asks home's tray process to quit, as its 退出 does (the window
// closes with it; the daemon keeps running), and waits until it is gone.
func StopTray(home string) bool {
	h := findWindow(trayClass, home)
	if h == 0 {
		return false
	}
	postMessage(h, wmTrayQuit, 0, 0)
	return waitFor(5*time.Second, func() bool { return !TrayRunning(home) })
}

// OpenWindow brings home's window to the front, or starts the window
// process (mcpcelld.exe window) next to exe with url, a one-time login link.
func OpenWindow(exe, home, url string) error {
	if h := findWindow(windowClass, home); h != 0 {
		focusWindow(h)
		return nil
	}
	p, err := start(shortcutTarget(exe), home, "window", "--url", url)
	if err != nil {
		return fmt.Errorf("无法启动窗口进程：%w", err)
	}
	return p.Release()
}

// WindowOpen reports whether home's window is open.
func WindowOpen(home string) bool { return findWindow(windowClass, home) != 0 }

// CloseWindow closes home's window if it is open.
func CloseWindow(home string) {
	if h := findWindow(windowClass, home); h != 0 {
		postMessage(h, wmClose, 0, 0)
	}
}

func focusWindow(h windows.HWND) {
	if r, _, _ := procIsIconic.Call(uintptr(h)); r != 0 {
		procShowWindow.Call(uintptr(h), swRestore)
	} else {
		procShowWindow.Call(uintptr(h), swShow)
	}
	procBringWindowToTop.Call(uintptr(h))
	procSetForegroundWindow.Call(uintptr(h))
}

// singleInstance holds a named mutex for name and home for the life of the
// process; false means another process already holds it.
func singleInstance(name, home string) bool {
	m, err := windows.CreateMutex(nil, false, utf16(fmt.Sprintf(`Local\%s-%08x`, name, homeID(home))))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(m)
		return false
	}
	return true // kept open until the process exits; without one, run anyway
}
