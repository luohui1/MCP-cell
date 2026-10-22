//go:build !windows

package desktop

func Available() error                                        { return ErrUnsupported }
func Shortcuts() (desktop, startMenu bool)                    { return false, false }
func ShortcutsInto(dir string) (desktop, startMenu bool)      { return false, false }
func SetShortcuts(exe string, desktop, startMenu *bool) error { return ErrUnsupported }
func Alert(title, text string)                                {}
func AllowForeground(pid int)                                 {}
func HasConsole() bool                                        { return true }
func RunWindow(WindowOptions) error                           { return ErrUnsupported }
func RunTray(TrayOptions) error                               { return ErrUnsupported }
func TrayPath(exe string) string                              { return "" }
func TrayRunning(home string) bool                            { return false }
func StartTray(exe, home string) error                        { return ErrUnsupported }
func StopTray(home string) bool                               { return false }
func OpenWindow(exe, home, url string) error                  { return ErrUnsupported }
func WindowOpen(home string) bool                             { return false }
func CloseWindow(home string)                                 {}
