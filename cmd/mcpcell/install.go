package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/luohui1/MCP-cell/internal/autostart"
	"github.com/luohui1/MCP-cell/internal/desktop"
	"github.com/luohui1/MCP-cell/internal/registry"
)

// programs are what an installation consists of: the CLI, the windowless
// daemon and the wrapper agent entries launch.
func programs() []string {
	if runtime.GOOS == "windows" {
		return []string{"mcpcell.exe", "mcpcelld.exe", "mcpcell-run.exe"}
	}
	return []string{"mcpcell", "mcpcell-run"}
}

// wrapperNextTo returns the mcpcell-run that sits next to exe, or "".
func wrapperNextTo(exe string) string {
	name := "mcpcell-run"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// runInstall copies the programs into <home>/bin, the stable place agent
// entries, autostart and shortcuts point at, so rebuilding or moving the
// source never breaks a running agent. A program in use (a wrapper serving an
// agent session) is renamed aside rather than overwritten.
func runInstall() error {
	home, err := registry.Home()
	if err != nil {
		return err
	}
	dest := filepath.Join(home, "bin")
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	src := filepath.Dir(exe)
	if same(src, dest) {
		return errors.New("已经在从安装目录运行：" + dest)
	}
	for _, p := range programs() {
		if _, err := os.Stat(filepath.Join(src, p)); err != nil {
			return fmt.Errorf("%s 下缺少 %s，请先构建（见 CLAUDE.md）", src, p)
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	var st psStatus
	wasRunning := daemonCall("GET", "/api/status", &st) == nil
	if wasRunning {
		if err := restartDaemon(); err != nil {
			return err
		}
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if daemonCall("GET", "/api/status", &st) != nil {
				break
			}
		}
	}
	for _, p := range programs() {
		if err := installFile(filepath.Join(src, p), filepath.Join(dest, p)); err != nil {
			return err
		}
	}
	sweepAside(dest)
	// Autostart and shortcuts move along only when they start the programs
	// being replaced: they are per user, not per MCPcell home.
	installed := filepath.Join(dest, programs()[0])
	if a := autostart.Get(); a.Enabled && strings.Contains(strings.ToLower(a.Command), strings.ToLower(src+string(filepath.Separator))) {
		if err := autostart.Enable(installed); err != nil {
			return fmt.Errorf("已复制，但开机自启没有改到新位置：%w", err)
		}
		fmt.Println("开机自启已改为", dest)
	}
	if d, s := desktop.ShortcutsInto(src); d || s {
		yes := true
		var dp, sp *bool
		if d {
			dp = &yes
		}
		if s {
			sp = &yes
		}
		if err := desktop.SetShortcuts(installed, dp, sp); err != nil {
			fmt.Fprintln(os.Stderr, "快捷方式没有改到新位置：", err)
		}
	}
	fmt.Println("已安装到", dest)
	if wasRunning {
		cmd := exec.Command(installed, "start")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	return nil
}

// installFile copies src over dst. Windows will not overwrite a running
// program but will rename it, so a busy dst moves aside first.
func installFile(src, dst string) error {
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		aside := dst + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := os.Rename(dst, aside); err != nil {
			return fmt.Errorf("无法替换 %s：%w", dst, err)
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// sweepAside deletes programs moved aside earlier that are no longer running.
func sweepAside(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".old-") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func same(a, b string) bool {
	ra, _ := filepath.Abs(a)
	rb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
}
