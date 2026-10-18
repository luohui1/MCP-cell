package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// IShellLinkW and IPersistFile vtable slots (ShObjIdl_core.h, ObjIdl.h).
const (
	slSetDescription      = 7
	slSetWorkingDirectory = 9
	slGetArguments        = 10
	slSetArguments        = 11
	slSetIconLocation     = 17
	slSetPath             = 20
	slGetPath             = 3
	pfLoad                = 5
	pfSave                = 6
)

func lockThread() { runtime.LockOSThread() }

// shortcutTarget is the windowless binary next to exe, which the shortcuts
// start with "open".
func shortcutTarget(exe string) string {
	gui := filepath.Join(filepath.Dir(exe), "mcpcelld.exe")
	if _, err := os.Stat(gui); err == nil {
		return gui
	}
	return exe
}

func shortcutPaths() (desktop, startMenu string, err error) {
	d, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		return "", "", err
	}
	p, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(d, ShortcutName), filepath.Join(p, ShortcutName), nil
}

// Shortcuts reports whether the desktop and Start menu shortcuts exist.
func Shortcuts() (desktop, startMenu bool) {
	d, s, err := shortcutPaths()
	if err != nil {
		return false, false
	}
	_, de := os.Stat(d)
	_, se := os.Stat(s)
	return de == nil, se == nil
}

// ShortcutsInto reports which existing shortcuts start a program in dir.
func ShortcutsInto(dir string) (desktop, startMenu bool) {
	d, s, err := shortcutPaths()
	if err != nil {
		return false, false
	}
	in := func(lnk string) bool {
		target, _, err := readShortcut(lnk)
		return err == nil && strings.EqualFold(filepath.Dir(target), filepath.Clean(dir))
	}
	return in(d), in(s)
}

// SetShortcuts creates (true) or removes (false) each shortcut; nil leaves
// it as is.
func SetShortcuts(exe string, desktop, startMenu *bool) error {
	d, s, err := shortcutPaths()
	if err != nil {
		return err
	}
	target := shortcutTarget(exe)
	var errs []error
	for _, c := range []struct {
		want *bool
		path string
	}{{desktop, d}, {startMenu, s}} {
		switch {
		case c.want == nil:
		case *c.want:
			errs = append(errs, writeShortcut(c.path, target, "open", "MCPcell — 本机 MCP 运行时"))
		default:
			if err := os.Remove(c.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// writeShortcut saves a .lnk that starts target with args, using target's
// own icon.
func writeShortcut(lnk, target, args, description string) error {
	return withCOM(func() error {
		link, err := coCreateInstance(&clsidShellLink, &iidShellLinkW)
		if err != nil {
			return err
		}
		defer link.release()
		for _, step := range []error{
			hresult(link.call(slSetPath, uintptr(unsafe.Pointer(utf16(target))))),
			hresult(link.call(slSetArguments, uintptr(unsafe.Pointer(utf16(args))))),
			hresult(link.call(slSetWorkingDirectory, uintptr(unsafe.Pointer(utf16(filepath.Dir(target)))))),
			hresult(link.call(slSetIconLocation, uintptr(unsafe.Pointer(utf16(target))), 0)),
			hresult(link.call(slSetDescription, uintptr(unsafe.Pointer(utf16(description))))),
		} {
			if step != nil {
				return step
			}
		}
		file := link.queryInterface(&iidPersistFile)
		if file == nil {
			return errors.New("IShellLink 不支持 IPersistFile")
		}
		defer file.release()
		return hresult(file.call(pfSave, uintptr(unsafe.Pointer(utf16(lnk))), 1))
	})
}

// readShortcut loads a .lnk and returns its target and arguments.
func readShortcut(lnk string) (target, args string, err error) {
	err = withCOM(func() error {
		link, err := coCreateInstance(&clsidShellLink, &iidShellLinkW)
		if err != nil {
			return err
		}
		defer link.release()
		file := link.queryInterface(&iidPersistFile)
		if file == nil {
			return errors.New("IShellLink 不支持 IPersistFile")
		}
		defer file.release()
		if err := hresult(file.call(pfLoad, uintptr(unsafe.Pointer(utf16(lnk))), 0)); err != nil {
			return err
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		if err := hresult(link.call(slGetPath, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)); err != nil {
			return err
		}
		target = windows.UTF16ToString(buf)
		if err := hresult(link.call(slGetArguments, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))); err != nil {
			return err
		}
		args = windows.UTF16ToString(buf)
		return nil
	})
	return target, args, err
}
