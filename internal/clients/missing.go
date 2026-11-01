package clients

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/luohui1/MCP-cell/internal/proc"
)

// scriptExt are the scripts node, bun, deno and python run.
var scriptExt = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".py": true}

// Missing says why an entry's program is not on this machine, or "" when it
// is: the command (looked up on PATH like MCPcell starts it), an npm shim's
// script, or the script node or python would run. Such an entry runs
// nowhere, so takeover leaves it alone; once the program is back, so is the
// entry.
func Missing(e Entry) string {
	env := os.Environ()
	for k, v := range e.Env {
		env = append(env, k+"="+v) // the entry's own win
	}
	argv := append([]string{e.Command}, e.Args...)
	if _, err := proc.ResolveArgv(argv, env, e.Cwd); errors.Is(err, proc.ErrNotFound) {
		return err.Error()
	}
	argv = proc.UnwrapCmd(argv)
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0])))
	runner := base == "node" || base == "bun" || base == "deno" || strings.HasPrefix(base, "python")
	if !runner || len(argv) < 2 || !scriptExt[strings.ToLower(filepath.Ext(argv[1]))] {
		return ""
	}
	script := argv[1]
	if !filepath.IsAbs(script) {
		if e.Cwd == "" {
			return "" // relative to the app's own directory, unknown here
		}
		script = filepath.Join(e.Cwd, script)
	}
	if _, err := os.Stat(script); errors.Is(err, os.ErrNotExist) {
		return "脚本不存在：" + script
	}
	return ""
}
