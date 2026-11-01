package autostart

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// entryPath is the XDG autostart entry: $XDG_CONFIG_HOME/autostart (by
// default ~/.config/autostart), which desktop sessions start at login.
func entryPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "mcpcell.desktop"), nil
}

// Enable writes an autostart entry that starts the daemon at login. There is
// no tray icon on Linux.
func Enable(exe string, _ bool) error {
	cmd, err := Command(exe)
	if err != nil {
		return err
	}
	path, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=MCPcell\nComment=MCPcell daemon\nExec=" + cmd +
		"\nNoDisplay=true\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(path, []byte(entry), 0o644)
}

// SetTray has nothing to do: there is no tray icon on Linux.
func SetTray(string, bool) error { return nil }

func Disable() error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func Get() Status {
	st := Status{Supported: true}
	path, err := entryPath()
	if err != nil {
		st.Error = err.Error()
		return st
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return st
	}
	if err != nil {
		st.Error = err.Error()
		return st
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if exec, ok := strings.CutPrefix(sc.Text(), "Exec="); ok {
			st.Enabled, st.Command = true, exec
		}
	}
	return st
}
