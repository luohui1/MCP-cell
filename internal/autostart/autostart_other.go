//go:build !windows

package autostart

func Enable(string, bool) error  { return ErrUnsupported }
func SetTray(string, bool) error { return ErrUnsupported }
func Disable() error             { return ErrUnsupported }
func Get() Status                { return Status{Error: ErrUnsupported.Error()} }
