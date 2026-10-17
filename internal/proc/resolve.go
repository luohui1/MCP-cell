package proc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ResolveArgv turns a client-style command into something CreateProcess can run
// without cmd.exe: `cmd /c X ...` is unwrapped, bare names are looked up on the
// target env's PATH, and npm .cmd shims are replaced by node + their script.
// Batch files are never run through cmd.exe, whose argument parsing is unsafe.
func ResolveArgv(argv []string, env []string, dir string) ([]string, error) {
	argv = UnwrapCmd(argv)
	if len(argv) == 0 || argv[0] == "" {
		return nil, errors.New("command 为空")
	}
	exe, err := lookPath(argv[0], lookupEnv(env, "PATH"), lookupEnv(env, "PATHEXT"), dir)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(exe)) {
		case ".cmd", ".bat":
			return resolveShim(exe, argv[1:], env, dir)
		}
	}
	return append([]string{exe}, argv[1:]...), nil
}

// UnwrapCmd strips a leading `cmd [/d] [/s] /c`, the usual Windows MCP config.
func UnwrapCmd(argv []string) []string {
	if len(argv) < 3 {
		return argv
	}
	switch strings.ToLower(filepath.Base(argv[0])) {
	case "cmd", "cmd.exe":
	default:
		return argv
	}
	for i := 1; i < len(argv); i++ {
		switch strings.ToLower(argv[i]) {
		case "/d", "/s", "/q":
			continue
		case "/c", "/k":
			return argv[i+1:]
		}
		break
	}
	return argv
}

func lookPath(name, pathEnv, pathext, dir string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		if p, ok := findExecutable(name, pathext); ok {
			return p, nil
		}
		return "", fmt.Errorf("找不到启动程序 %s", name)
	}
	for _, d := range filepath.SplitList(pathEnv) {
		if d == "" {
			continue
		}
		if p, ok := findExecutable(filepath.Join(d, name), pathext); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("在 PATH 中找不到启动程序 %q", name)
}

func findExecutable(path, pathext string) (string, bool) {
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		return path, err == nil && !st.IsDir() && st.Mode()&0o111 != 0
	}
	exts := strings.Split(strings.ToLower(pathext), ";")
	if pathext == "" {
		exts = []string{".com", ".exe", ".bat", ".cmd"}
	}
	candidates := []string{}
	if ext := strings.ToLower(filepath.Ext(path)); ext != "" && contains(exts, ext) {
		candidates = append(candidates, path)
	} else {
		for _, e := range exts {
			if e != "" {
				candidates = append(candidates, path+e)
			}
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// npm's cmd-shim launches `"%dp0%\node_modules\pkg\bin.js" %*` (older npm used %~dp0).
var shimScript = regexp.MustCompile(`"%~?dp0%?\\([^"%]+\.[cm]?js)"`)

func resolveShim(shim string, args []string, env []string, dir string) ([]string, error) {
	base := filepath.Dir(shim)
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(shim), filepath.Ext(shim)))
	var script string
	if stem == "npx" || stem == "npm" {
		script = filepath.Join(base, "node_modules", "npm", "bin", stem+"-cli.js")
	} else if m := shimScript.FindSubmatch(readHead(shim)); m != nil {
		script = filepath.Join(base, string(m[1]))
	}
	if script == "" {
		return nil, fmt.Errorf("不支持直接运行批处理文件 %s：请把 command 改成可执行文件或 node 脚本", filepath.Base(shim))
	}
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("%s 指向的脚本不存在：%s", filepath.Base(shim), script)
	}
	node := filepath.Join(base, "node.exe")
	if _, err := os.Stat(node); err != nil {
		if node, err = lookPath("node", lookupEnv(env, "PATH"), lookupEnv(env, "PATHEXT"), dir); err != nil {
			return nil, fmt.Errorf("找不到 node，无法运行 %s", filepath.Base(shim))
		}
	}
	return append([]string{node, script}, args...), nil
}

func readHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	return b
}
