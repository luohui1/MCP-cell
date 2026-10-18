package desktop

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WebView2 without WebView2Loader.dll: the installed Evergreen runtime ships
// EmbeddedBrowserWebView.dll, whose CreateWebViewEnvironmentWithOptionsInternal
// is what the loader calls. Vtable slots and IIDs are from WebView2.h
// (Microsoft.Web.WebView2 1.0.2903.40); the export's signature from
// jchv/OpenWebView2Loader.

const runtimeKey = `SOFTWARE\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// Vtable slots.
const (
	envCreateController = 3 // ICoreWebView2Environment

	ctrlPutIsVisible                      = 4 // ICoreWebView2Controller
	ctrlPutBounds                         = 6
	ctrlMoveFocus                         = 12
	ctrlNotifyParentWindowPositionChanged = 23
	ctrlClose                             = 24
	ctrlGetCoreWebView2                   = 25
	ctrl2PutDefaultBackgroundColor        = 27 // ICoreWebView2Controller2

	wvGetSettings           = 3 // ICoreWebView2
	wvNavigate              = 5
	wvAddNavigationStarting = 7
	wvAddNewWindowRequested = 44

	setPutIsStatusBarEnabled = 10 // ICoreWebView2Settings
	setPutAreDevToolsEnabled = 12

	navGetUri    = 3 // ICoreWebView2NavigationStartingEventArgs
	navPutCancel = 8

	nwGetUri     = 3 // ICoreWebView2NewWindowRequestedEventArgs
	nwPutHandled = 6
)

var (
	iidController2      = windows.GUID{Data1: 0xc979903e, Data2: 0xd4ca, Data3: 0x4228, Data4: [8]byte{0x92, 0xeb, 0x47, 0xee, 0x3f, 0xa9, 0x6e, 0xab}}
	iidEnvCompleted     = windows.GUID{Data1: 0x4e8a3389, Data2: 0xc9d8, Data3: 0x4bd2, Data4: [8]byte{0xb6, 0xb5, 0x12, 0x4f, 0xee, 0x6c, 0xc1, 0x4d}}
	iidCtrlCompleted    = windows.GUID{Data1: 0x6c4819f3, Data2: 0xc9b7, Data3: 0x4260, Data4: [8]byte{0x81, 0x27, 0xc9, 0xf5, 0xbd, 0xe7, 0xf6, 0x8c}}
	iidNewWindowHandler = windows.GUID{Data1: 0xd4c185fe, Data2: 0xc81c, Data3: 0x4989, Data4: [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51}}
	iidNavStartHandler  = windows.GUID{Data1: 0x9adbe429, Data2: 0xf36d, Data3: 0x432b, Data4: [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
)

// webview is the window's WebView2 state (UI thread only).
type webview struct {
	env, ctrl, view *comObj
	origin          string // the daemon's http://127.0.0.1:port

	envDone, ctrlDone, newWindow, navStarting *handler
}

// runtimeDLL finds EmbeddedBrowserWebView.dll of the installed runtime,
// per-user install first, as WebView2Loader does.
func runtimeDLL() (string, error) {
	arch := map[string]string{"amd64": "x64", "386": "x86", "arm64": "arm64"}[runtime.GOARCH]
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, runtimeKey, registry.QUERY_VALUE|registry.WOW64_32KEY)
		if err != nil {
			continue
		}
		dir, _, err := k.GetStringValue("EBWebView")
		k.Close()
		if err != nil || dir == "" {
			continue
		}
		dll := filepath.Join(dir, "EBWebView", arch, "EmbeddedBrowserWebView.dll")
		if _, err := os.Stat(dll); err == nil {
			return dll, nil
		}
	}
	return "", errors.New("没有找到 WebView2 运行时（Windows 11 自带；Windows 10 可从 https://go.microsoft.com/fwlink/p/?LinkId=2124703 安装）")
}

// Available reports whether the window can be shown.
func Available() error {
	_, err := runtimeDLL()
	return err
}

func createEnvironmentProc() (uintptr, error) {
	dll, err := runtimeDLL()
	if err != nil {
		return 0, err
	}
	h, err := windows.LoadLibrary(dll)
	if err != nil {
		return 0, fmt.Errorf("加载 %s 失败：%w", dll, err)
	}
	proc, err := windows.GetProcAddress(h, "CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return 0, fmt.Errorf("%s 缺少 CreateWebViewEnvironmentWithOptionsInternal：%w", dll, err)
	}
	return proc, nil
}

// createWebView starts filling the window; the rest happens in the
// completion callbacks, on this thread.
func (h *host) createWebView() error {
	h.envDone = newHandler(&iidEnvCompleted, h.onEnvironment)
	h.ctrlDone = newHandler(&iidCtrlCompleted, h.onController)
	h.newWindow = newHandler(&iidNewWindowHandler, func(_, args uintptr) uintptr {
		e := ptrOf[comObj](args)
		if uri := takeString(e, nwGetUri); uri != "" {
			openExternal(uri)
		}
		e.call(nwPutHandled, 1)
		return 0
	})
	h.navStarting = newHandler(&iidNavStartHandler, func(_, args uintptr) uintptr {
		e := ptrOf[comObj](args)
		uri := takeString(e, navGetUri)
		if h.origin != "" && uri != "" && uri != h.origin && !strings.HasPrefix(uri, h.origin+"/") && !strings.HasPrefix(uri, "about:") {
			e.call(navPutCancel, 1)
			openExternal(uri)
		}
		return 0
	})
	proc, err := createEnvironmentProc()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(h.opts.Home, "webview")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	r, _, _ := syscall.SyscallN(proc, 1, 0, uintptr(unsafe.Pointer(utf16(dataDir))), 0, uintptr(unsafe.Pointer(h.envDone)))
	if err := hresult(r); err != nil {
		return fmt.Errorf("创建 WebView2 环境失败：%w", err)
	}
	return nil
}

// takeString reads a string property (a CoTaskMem LPWSTR) at vtable slot i.
func takeString(o *comObj, i int) string {
	var p *uint16
	if hresult(o.call(i, uintptr(unsafe.Pointer(&p)))) != nil || p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func (h *host) onEnvironment(hr, env uintptr) uintptr {
	if err := hresult(hr); err != nil {
		h.fail(fmt.Errorf("创建 WebView2 环境失败：%w", err))
		return 0
	}
	if h.hwnd == 0 {
		return 0 // closed meanwhile
	}
	h.env = ptrOf[comObj](env)
	h.env.addRef()
	if err := hresult(h.env.call(envCreateController, uintptr(h.hwnd), uintptr(unsafe.Pointer(h.ctrlDone)))); err != nil {
		h.fail(fmt.Errorf("创建 WebView2 控件失败：%w", err))
	}
	return 0
}

func (h *host) onController(hr, c uintptr) uintptr {
	if err := hresult(hr); err != nil {
		h.fail(fmt.Errorf("创建 WebView2 控件失败：%w", err))
		return 0
	}
	ctrl := ptrOf[comObj](c)
	if h.hwnd == 0 {
		ctrl.call(ctrlClose)
		return 0
	}
	ctrl.addRef()
	h.ctrl = ctrl
	h.setBackground()
	h.resize()
	ctrl.call(ctrlPutIsVisible, 1)
	var view *comObj
	if err := hresult(ctrl.call(ctrlGetCoreWebView2, uintptr(unsafe.Pointer(&view)))); err != nil || view == nil {
		h.fail(fmt.Errorf("WebView2 没有返回页面对象：%v", err))
		return 0
	}
	h.view = view
	var settings *comObj
	if hresult(view.call(wvGetSettings, uintptr(unsafe.Pointer(&settings)))) == nil && settings != nil {
		devtools := uintptr(0)
		if os.Getenv("MCPCELL_DEVTOOLS") == "1" {
			devtools = 1
		}
		settings.call(setPutAreDevToolsEnabled, devtools)
		settings.call(setPutIsStatusBarEnabled, 0)
		settings.release()
	}
	var token int64
	view.call(wvAddNewWindowRequested, uintptr(unsafe.Pointer(h.newWindow)), uintptr(unsafe.Pointer(&token)))
	view.call(wvAddNavigationStarting, uintptr(unsafe.Pointer(h.navStarting)), uintptr(unsafe.Pointer(&token)))
	target := h.opts.URL
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		h.origin = u.Scheme + "://" + u.Host
	}
	if err := hresult(view.call(wvNavigate, uintptr(unsafe.Pointer(utf16(target))))); err != nil {
		h.fail(fmt.Errorf("打开管理页失败：%w", err))
		return 0
	}
	ctrl.call(ctrlMoveFocus, 0)
	return 0
}

func (h *host) resize() {
	if h.ctrl == nil || h.hwnd == 0 {
		return
	}
	r := h.clientRect()
	// RECT is 16 bytes, so the x64 ABI passes it by reference.
	h.ctrl.call(ctrlPutBounds, uintptr(unsafe.Pointer(&r)))
}

// setBackground paints the area the page has not drawn yet in the page's
// own background color instead of white.
func (h *host) setBackground() {
	if h.ctrl == nil {
		return
	}
	if c2 := h.ctrl.queryInterface(&iidController2); c2 != nil {
		_, color := background()
		c2.call(ctrl2PutDefaultBackgroundColor, uintptr(color))
		c2.release()
	}
}

func (h *host) releaseWebView() {
	if h.view != nil {
		h.view.release()
		h.view = nil
	}
	if h.ctrl != nil {
		h.ctrl.call(ctrlClose)
		h.ctrl.release()
		h.ctrl = nil
	}
	if h.env != nil {
		h.env.release()
		h.env = nil
	}
}
