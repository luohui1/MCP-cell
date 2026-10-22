package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Enable starts the daemon at logon, and the tray icon too when tray is set.
func Enable(exe string, tray bool) error {
	cmd, err := Command(exe)
	if err != nil {
		return err
	}
	if err := set(valueName, cmd); err != nil {
		return err
	}
	return SetTray(exe, tray)
}

// SetTray adds or removes only the tray icon's entry (switching between the
// graphical interface and the command line).
func SetTray(exe string, on bool) error {
	if !on {
		return remove(trayValue)
	}
	cmd, err := TrayCommand(exe)
	if err != nil {
		return err
	}
	return set(trayValue, cmd)
}

// Disable removes both entries.
func Disable() error {
	if err := remove(valueName); err != nil {
		return err
	}
	return remove(trayValue)
}

func Get() Status {
	st := Status{Supported: true}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer k.Close()
	if v, _, err := k.GetStringValue(valueName); err == nil {
		st.Enabled, st.Command = true, v
	}
	if v, _, err := k.GetStringValue(trayValue); err == nil {
		st.Tray, st.TrayCommand = true, v
	}
	return st
}

func set(name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func remove(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
