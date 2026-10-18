package desktop

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const windowClass = "MCPcellWindow"

// host is the window process's state (UI thread only).
type host struct {
	opts      WindowOptions
	log       *slog.Logger
	hwnd      windows.HWND
	bigIcon   windows.Handle
	smallIcon windows.Handle
	err       error // why the window had to close, if it was not the user
	webview
}

var theHost *host

// RunWindow shows the MCPcell window and returns once it is closed. It is
// the body of the window process the daemon starts (mcpcelld window); it
// must run on the main goroutine of that process.
func RunWindow(opts WindowOptions) error {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	lockThread()
	if err := windows.CoInitializeEx(0, coinitApartmentThreaded); err != nil && err != windows.Errno(1) {
		return fmt.Errorf("CoInitializeEx: %w", err)
	}
	defer windows.CoUninitialize()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		procSetThreadDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	}
	h := &host{opts: opts, log: opts.Log}
	theHost = h
	if err := h.open(); err != nil {
		return err
	}
	messageLoop()
	return h.err
}

func (h *host) open() error {
	b := defaultBounds()
	dpi := dpiForSystem()
	h.bigIcon = loadIcon(int(systemMetric(smCxIcon, dpi)))
	h.smallIcon = loadIcon(int(systemMetric(smCxSmIcon, dpi)))
	if err := registerClass(windowClass, windowProc, h.bigIcon, h.smallIcon); err != nil {
		return err
	}
	r, _, err := procCreateWindowExW.Call(wsExAppWindow, uintptr(unsafe.Pointer(utf16(windowClass))), uintptr(unsafe.Pointer(utf16("MCPcell"))),
		wsOverlappedWindow, uintptr(b.Left), uintptr(b.Top), uintptr(b.width()), uintptr(b.height()), 0, 0, moduleHandle(), 0)
	if r == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	h.hwnd = windows.HWND(r)
	h.applyTheme()
	if !h.restorePlacement() {
		procShowWindow.Call(uintptr(h.hwnd), swShowNormal)
	}
	procSetForegroundWindow.Call(uintptr(h.hwnd))
	if err := h.createWebView(); err != nil {
		procDestroyWindow.Call(uintptr(h.hwnd))
		return err
	}
	return nil
}

// fail closes the window because of err; RunWindow returns it.
func (h *host) fail(err error) {
	h.log.Error("desktop: webview", "err", err)
	if h.err == nil {
		h.err = err
	}
	if h.hwnd != 0 {
		procDestroyWindow.Call(uintptr(h.hwnd))
	}
}

func (h *host) clientRect() rect {
	var r rect
	procGetClientRect.Call(uintptr(h.hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

func windowProc(hwnd windows.HWND, m uint32, w, l uintptr) uintptr {
	h := theHost
	if h == nil || hwnd != h.hwnd {
		// Messages sent during CreateWindowEx, before h.hwnd is set.
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
		return r
	}
	switch m {
	case wmSize:
		h.resize()
		return 0
	case wmMove:
		if h.ctrl != nil {
			h.ctrl.call(ctrlNotifyParentWindowPositionChanged)
		}
	case wmSetFocus:
		if h.ctrl != nil {
			h.ctrl.call(ctrlMoveFocus, 0)
		}
		return 0
	case wmEraseBkgnd:
		cref, _ := background()
		brush, _, _ := procCreateSolidBrush.Call(uintptr(cref))
		r := h.clientRect()
		procFillRect.Call(w, uintptr(unsafe.Pointer(&r)), brush)
		procDeleteObject.Call(brush)
		return 1
	case wmGetMinMaxInfo:
		mmi := ptrOf[minMaxInfo](l)
		dpi := dpiForWindow(hwnd)
		mmi.MinTrackSize = point{scale(960, dpi), scale(640, dpi)}
		return 0
	case wmDpiChanged:
		s := ptrOf[rect](l)
		procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(s.Left), uintptr(s.Top), uintptr(s.width()), uintptr(s.height()),
			swpNoZOrder|swpNoActivate)
		return 0
	case wmSettingChange:
		if l != 0 && windows.UTF16PtrToString(ptrOf[uint16](l)) == "ImmersiveColorSet" {
			h.applyTheme()
			h.setBackground()
		}
	case wmClose:
		h.savePlacement()
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		h.releaseWebView()
		h.hwnd = 0
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(m), w, l)
	return r
}

// ---- placement and theme ----

// savedPlacement is window.json.
type savedPlacement struct {
	Left, Top, Right, Bottom int32
	Maximized                bool
}

func (h *host) placementPath() string { return filepath.Join(h.opts.Home, "window.json") }

func (h *host) savePlacement() {
	wp := windowPlacement{}
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(uintptr(h.hwnd), uintptr(unsafe.Pointer(&wp))); r == 0 {
		return
	}
	p := savedPlacement{wp.NormalRect.Left, wp.NormalRect.Top, wp.NormalRect.Right, wp.NormalRect.Bottom, wp.ShowCmd == swShowMaximized}
	if b, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(h.placementPath(), b, 0o600)
	}
}

// restorePlacement applies window.json if that rectangle is still on a
// monitor; it reports whether it did.
func (h *host) restorePlacement() bool {
	b, err := os.ReadFile(h.placementPath())
	if err != nil {
		return false
	}
	var p savedPlacement
	if json.Unmarshal(b, &p) != nil {
		return false
	}
	r := rect{p.Left, p.Top, p.Right, p.Bottom}
	if r.width() < 200 || r.height() < 150 {
		return false
	}
	if mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNull); mon == 0 {
		return false
	}
	wp := windowPlacement{ShowCmd: swShowNormal, NormalRect: r}
	if p.Maximized {
		wp.ShowCmd = swShowMaximized
	}
	wp.Length = uint32(unsafe.Sizeof(wp))
	ok, _, _ := procSetWindowPlacement.Call(uintptr(h.hwnd), uintptr(unsafe.Pointer(&wp)))
	return ok != 0
}

// defaultBounds is 1280×840 (logical) centered on the monitor under the
// cursor, shrunk to fit its work area.
func defaultBounds() rect {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// POINT is 8 bytes and travels in one register.
	mon, _, _ := procMonitorFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&pt))), monitorDefaultToPrimary)
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	dpi := uint32(96)
	if procGetDpiForMonitor.Find() == nil {
		var x, y uint32
		if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r == 0 && x != 0 {
			dpi = x
		}
	}
	work := mi.Work
	w, h := min(scale(1280, dpi), work.width()-scale(32, dpi)), min(scale(840, dpi), work.height()-scale(32, dpi))
	x, y := work.Left+(work.width()-w)/2, work.Top+(work.height()-h)/2
	return rect{x, y, x + w, y + h}
}

func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// background matches the page: zinc-50 light, zinc-950 dark. colorref is
// 0x00BBGGRR for GDI; webview is a COREWEBVIEW2_COLOR, the bytes {A, R, G, B}
// read as a little-endian uint32 (it is passed by value).
func background() (colorref uint32, webview uint32) {
	if systemDark() {
		return 0x0b0909, 0x0b0909ff // #09090b
	}
	return 0xfafafa, 0xfafafaff // #fafafa
}

func (h *host) applyTheme() {
	dark := uint32(0)
	if systemDark() {
		dark = 1
	}
	_ = windows.DwmSetWindowAttribute(h.hwnd, dwmwaUseImmersiveDarkMode, unsafe.Pointer(&dark), 4)
}

// openExternal opens http(s) links in the default browser; the window only
// shows MCPcell's own page.
func openExternal(uri string) {
	if !strings.HasPrefix(uri, "https://") && !strings.HasPrefix(uri, "http://") {
		return
	}
	_ = windows.ShellExecute(0, utf16("open"), utf16(uri), nil, nil, swShowNormal)
}

// ---- shared Win32 helpers ----

func moduleHandle() uintptr {
	r, _, _ := procGetModuleHandleW.Call(0)
	return r
}

func registerClass(name string, proc any, icon, iconSm windows.Handle) error {
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{WndProc: windows.NewCallback(proc), Instance: windows.Handle(moduleHandle()),
		Cursor: windows.Handle(cursor), Icon: icon, IconSm: iconSm, ClassName: utf16(name)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx %s: %w", name, err)
	}
	return nil
}

// createHiddenWindow makes a top-level window that is never shown; unlike a
// message-only window it receives broadcasts such as TaskbarCreated.
func createHiddenWindow(class string, proc any) (windows.HWND, error) {
	if err := registerClass(class, proc, 0, 0); err != nil {
		return 0, err
	}
	r, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16("MCPcell"))),
		0, 0, 0, 0, 0, 0, 0, moduleHandle(), 0)
	if r == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %w", err)
	}
	return windows.HWND(r), nil
}

// messageLoop pumps messages until WM_QUIT.
func messageLoop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func className(h windows.HWND) string {
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

// loadIcon makes an HICON of about size pixels from the embedded .ico.
func loadIcon(size int) windows.Handle {
	img, err := pickIcon(iconData, size)
	if err != nil {
		return 0
	}
	r, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, 0x00030000,
		uintptr(size), uintptr(size), 0)
	return windows.Handle(r)
}
