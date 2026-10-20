//go:build !windows

package desktop

type App struct{}

func Start(Options) (*App, error)                        { return nil, ErrUnsupported }
func (*App) Show() error                                 { return ErrUnsupported }
func (*App) Close()                                      {}
func (*App) SetTray(bool)                                {}
func (*App) Available() error                            { return ErrUnsupported }
func (*App) Shortcuts() (desktop, startMenu bool)        { return false, false }
func (*App) SetShortcuts(desktop, startMenu *bool) error { return ErrUnsupported }

func Available() error                                        { return ErrUnsupported }
func Shortcuts() (desktop, startMenu bool)                    { return false, false }
func ShortcutsInto(dir string) (desktop, startMenu bool)      { return false, false }
func SetShortcuts(exe string, desktop, startMenu *bool) error { return ErrUnsupported }
func Alert(title, text string)                                {}
func AllowForeground(pid int)                                 {}
func HasConsole() bool                                        { return true }
func RunWindow(WindowOptions) error                           { return ErrUnsupported }
