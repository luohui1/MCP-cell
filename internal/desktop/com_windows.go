package desktop

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// comObj is any COM interface pointer: its first word points at the vtable.
type comObj struct{ vtbl unsafe.Pointer }

// call invokes vtable method i with this as the first argument.
//
//go:uintptrescapes
func (o *comObj) call(i int, args ...uintptr) uintptr {
	fn := *(*uintptr)(unsafe.Add(o.vtbl, uintptr(i)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObj) addRef() { o.call(1) }

func (o *comObj) release() {
	if o != nil {
		o.call(2)
	}
}

// queryInterface returns the interface iid of o, or nil.
func (o *comObj) queryInterface(iid *windows.GUID) *comObj {
	var out *comObj
	if hresult(o.call(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))) != nil {
		return nil
	}
	return out
}

var iidIUnknown = windows.GUID{Data1: 0, Data2: 0, Data3: 0, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}

// handler is a COM callback object (the WebView2 *Handler interfaces): IUnknown
// plus Invoke(a, b). Handlers live as long as the App, so reference counts
// are not tracked; WebView2 never frees them.
type handler struct {
	vtbl *handlerVtbl
	iid  *windows.GUID
	fn   func(a, b uintptr) uintptr
}

type handlerVtbl struct{ queryInterface, addRef, release, invoke uintptr }

var sharedHandlerVtbl *handlerVtbl

func newHandler(iid *windows.GUID, fn func(a, b uintptr) uintptr) *handler {
	if sharedHandlerVtbl == nil {
		sharedHandlerVtbl = &handlerVtbl{
			queryInterface: syscall.NewCallback(func(h *handler, riid *windows.GUID, out *uintptr) uintptr {
				if *riid == iidIUnknown || *riid == *h.iid {
					*out = uintptr(unsafe.Pointer(h))
					return 0
				}
				*out = 0
				return 0x80004002 // E_NOINTERFACE
			}),
			addRef:  syscall.NewCallback(func(*handler) uintptr { return 1 }),
			release: syscall.NewCallback(func(*handler) uintptr { return 1 }),
			invoke: syscall.NewCallback(func(h *handler, a, b uintptr) uintptr {
				return h.fn(a, b)
			}),
		}
	}
	return &handler{vtbl: sharedHandlerVtbl, iid: iid, fn: fn}
}

func coCreateInstance(clsid, iid *windows.GUID) (*comObj, error) {
	var out *comObj
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := hresult(r); err != nil {
		return nil, err
	}
	return out, nil
}

// withCOM runs fn on a locked thread with COM initialized (STA).
func withCOM(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		// A fresh goroutine keeps the caller's thread state untouched.
		lockThread()
		err := windows.CoInitializeEx(0, coinitApartmentThreaded)
		switch err {
		case nil, syscall.Errno(1): // S_OK, S_FALSE
			defer windows.CoUninitialize()
		case syscall.Errno(0x80010106): // RPC_E_CHANGED_MODE: COM is usable as is
		default:
			done <- err
			return
		}
		done <- fn()
	}()
	return <-done
}
