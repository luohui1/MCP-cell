package desktop

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	shcore   = windows.NewLazySystemDLL("shcore.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW             = user32.NewProc("RegisterClassExW")
	procCreateWindowExW              = user32.NewProc("CreateWindowExW")
	procDefWindowProcW               = user32.NewProc("DefWindowProcW")
	procDestroyWindow                = user32.NewProc("DestroyWindow")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procSetForegroundWindow          = user32.NewProc("SetForegroundWindow")
	procGetMessageW                  = user32.NewProc("GetMessageW")
	procTranslateMessage             = user32.NewProc("TranslateMessage")
	procDispatchMessageW             = user32.NewProc("DispatchMessageW")
	procPostMessageW                 = user32.NewProc("PostMessageW")
	procSendMessageW                 = user32.NewProc("SendMessageW")
	procPostQuitMessage              = user32.NewProc("PostQuitMessage")
	procLoadCursorW                  = user32.NewProc("LoadCursorW")
	procGetClientRect                = user32.NewProc("GetClientRect")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procGetWindowPlacement           = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement           = user32.NewProc("SetWindowPlacement")
	procIsIconic                     = user32.NewProc("IsIconic")
	procRegisterWindowMessageW       = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu              = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                  = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem           = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenuEx             = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu                  = user32.NewProc("DestroyMenu")
	procGetCursorPos                 = user32.NewProc("GetCursorPos")
	procMonitorFromPoint             = user32.NewProc("MonitorFromPoint")
	procMonitorFromRect              = user32.NewProc("MonitorFromRect")
	procGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow              = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem              = user32.NewProc("GetDpiForSystem")
	procGetSystemMetricsForDpi       = user32.NewProc("GetSystemMetricsForDpi")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procCreateIconFromResourceEx     = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon                  = user32.NewProc("DestroyIcon")
	procFillRect                     = user32.NewProc("FillRect")
	procAllowSetForegroundWindow     = user32.NewProc("AllowSetForegroundWindow")
	procBringWindowToTop             = user32.NewProc("BringWindowToTop")
	procEnumWindows                  = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId     = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW                = user32.NewProc("GetClassNameW")
	procShellNotifyIconW             = shell32.NewProc("Shell_NotifyIconW")
	procGetDpiForMonitor             = shcore.NewProc("GetDpiForMonitor")
	procCreateSolidBrush             = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procCoCreateInstance             = ole32.NewProc("CoCreateInstance")
	procGetModuleHandleW             = kernel32.NewProc("GetModuleHandleW")
	procGetConsoleWindow             = kernel32.NewProc("GetConsoleWindow")
)

const (
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmSettingChange = 0x001A
	wmGetMinMaxInfo = 0x0024
	wmContextMenu   = 0x007B
	wmSetIcon       = 0x0080
	wmMouseMove     = 0x0200
	wmLButtonDblClk = 0x0203
	wmDpiChanged    = 0x02E0
	wmUser          = 0x0400
	wmApp           = 0x8000

	wmTray         = wmApp + 1 // notification-area icon callback
	wmWindowClosed = wmApp + 2 // the window process exited
	wmStop         = wmApp + 3 // remove the tray icon and leave the loop

	ninSelect    = wmUser + 0
	ninKeySelect = wmUser + 1

	wsOverlappedWindow = 0x00CF0000
	wsExAppWindow      = 0x00040000
	cwUseDefault       = 0x80000000

	swShowNormal    = 1
	swShowMaximized = 3
	swShow          = 5
	swRestore       = 9

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	monitorDefaultToNull    = 0
	monitorDefaultToPrimary = 1
	monitorDefaultToNearest = 2

	smCxIcon   = 11
	smCxSmIcon = 49
	iconSmall  = 0
	iconBig    = 1

	idcArrow = 32512

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	nifShowTip    = 0x80
	niifUser      = 0x04
	niifNoSound   = 0x10
	niifLargeIcon = 0x20
	notifyIconV4  = 4

	mfString    = 0x0000
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	mbOK            = 0x00000000
	mbYesNo         = 0x00000004
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbDefButton2    = 0x00000100
	mbSetForeground = 0x00010000
	idYes           = 6

	dwmwaUseImmersiveDarkMode = 20

	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
)

// dpiAwarenessPerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4).
var dpiAwarenessPerMonitorV2 = ^uintptr(3)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type windowPlacement struct {
	Length     uint32
	Flags      uint32
	ShowCmd    uint32
	MinPos     point
	MaxPos     point
	NormalRect rect
}

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}

// notifyIconData is NOTIFYICONDATAW (976 bytes on amd64).
type notifyIconData struct {
	Size        uint32
	Wnd         windows.HWND
	ID          uint32
	Flags       uint32
	CallbackMsg uint32
	Icon        windows.Handle
	Tip         [128]uint16
	State       uint32
	StateMask   uint32
	Info        [256]uint16
	Version     uint32 // union with uTimeout
	InfoTitle   [64]uint16
	InfoFlags   uint32
	GUIDItem    windows.GUID
	BalloonIcon windows.Handle
}

// ptrOf reinterprets a pointer that Windows handed over as an integer
// (window message parameters, COM callback arguments).
func ptrOf[T any](u uintptr) *T { return *(**T)(unsafe.Pointer(&u)) }

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// copyUTF16 fills a fixed-size WCHAR field, truncating and NUL-terminating.
func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	clear(dst)
	copy(dst, u)
}

func loword(u uintptr) uint32 { return uint32(u & 0xffff) }

func postMessage(hwnd windows.HWND, m uint32, w, l uintptr) bool {
	r, _, _ := procPostMessageW.Call(uintptr(hwnd), uintptr(m), w, l)
	return r != 0
}

func messageBox(owner windows.HWND, text, title string, flags uint32) int32 {
	r, _ := windows.MessageBox(owner, utf16(text), utf16(title), flags)
	return r
}

// Alert shows an error dialog; the windowless binary has no console to
// print to.
func Alert(title, text string) {
	messageBox(0, text, title, mbOK|mbIconError|mbSetForeground)
}

// AllowForeground lets process pid bring its window to the front. The
// caller must own the foreground (e.g. it was just started by the user).
func AllowForeground(pid int) {
	procAllowSetForegroundWindow.Call(uintptr(pid))
}

// HasConsole reports whether the process has a console to print to.
func HasConsole() bool {
	r, _, _ := procGetConsoleWindow.Call()
	return r != 0
}

func dpiForSystem() uint32 {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	r, _, _ := procGetDpiForSystem.Call()
	if r == 0 {
		return 96
	}
	return uint32(r)
}

func dpiForWindow(hwnd windows.HWND) uint32 {
	r, _, _ := procGetDpiForWindow.Call(uintptr(hwnd))
	if r == 0 {
		return dpiForSystem()
	}
	return uint32(r)
}

func systemMetric(index int, dpi uint32) int32 {
	r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
	return int32(r)
}

func scale(v int32, dpi uint32) int32 { return int32(int64(v) * int64(dpi) / 96) }

// hresult checks a COM return value; the upper half of the register is
// undefined for a 32-bit HRESULT.
func hresult(r uintptr) error {
	if hr := int32(uint32(r)); hr < 0 {
		return syscall.Errno(uint32(hr))
	}
	return nil
}
