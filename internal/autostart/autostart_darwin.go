package autostart

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// label names the LaunchAgent; its file is ~/Library/LaunchAgents/<label>.plist.
const label = "com.mcpcell.daemon"

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// Enable writes a LaunchAgent that starts the daemon at login. It takes
// effect from the next login: loading it now would start a second daemon
// next to the running one. There is no tray icon on macOS.
func Enable(exe string, _ bool) error {
	prog, err := daemonProgram(exe)
	if err != nil {
		return err
	}
	path, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + label + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range []string{prog, "daemon", "--log"} {
		b.WriteString("\t\t<string>")
		_ = xml.EscapeText(&b, []byte(a))
		b.WriteString("</string>\n")
	}
	b.WriteString("\t</array>\n\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n</dict>\n</plist>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// SetTray has nothing to do: there is no tray icon on macOS.
func SetTray(string, bool) error { return nil }

func Disable() error {
	path, err := agentPath()
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
	path, err := agentPath()
	if err != nil {
		st.Error = err.Error()
		return st
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st
	}
	if err != nil {
		st.Error = err.Error()
		return st
	}
	var doc struct {
		Strings []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil || len(doc.Strings) == 0 {
		st.Error = "LaunchAgent 文件格式不对：" + path
		return st
	}
	st.Enabled = true
	st.Command = `"` + doc.Strings[0] + `" ` + strings.Join(doc.Strings[1:], " ")
	return st
}
