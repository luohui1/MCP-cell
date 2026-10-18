package clients

import (
	"path/filepath"
	"strings"

	"github.com/luohui1/MCP-cell/internal/npmcache"
	"github.com/luohui1/MCP-cell/internal/proc"
)

type Class string

const (
	// ClassShared: known to hold no per-client state; one instance serves all.
	ClassShared Class = "shared"
	// ClassStateful: holds browser, connection or app state per client; taken
	// over with one instance per session.
	ClassStateful Class = "stateful"
	// ClassUnknown: not in the known list; runs per session unless the user
	// confirms it can be shared.
	ClassUnknown  Class = "unknown"
	ClassRemote   Class = "remote"   // already an HTTP server: no local process to save
	ClassManaged  Class = "managed"  // already points at MCPcell
	ClassInternal Class = "internal" // part of the client app itself
)

// Known servers by launch package. Lists are conservative: anything unsure is
// left out and stays "unknown".
var knownShared = map[string]bool{
	"@upstash/context7-mcp":                     true,
	"@jpisnice/shadcn-ui-mcp-server":            true,
	"@modelcontextprotocol/server-memory":       true,
	"@modelcontextprotocol/server-github":       true,
	"@modelcontextprotocol/server-brave-search": true,
	"@larksuiteoapi/lark-mcp":                   true,
	"@brave/brave-search-mcp-server":            true,
	"@notionhq/notion-mcp-server":               true,
	"tavily-mcp":                                true,
	"exa-mcp-server":                            true,
	"firecrawl-mcp":                             true,
	"mcp-server-fetch":                          true,
	"mcp-server-time":                           true,
}

var knownStateful = map[string]string{
	"chrome-devtools-mcp":                              "控制一个浏览器，页面状态属于会话",
	"@modelcontextprotocol/server-sequential-thinking": "思考历史和分支保存在进程内存里，返回结果也带着它们",
	"@modelcontextprotocol/server-filesystem":          "向客户端要 roots，用会话的工作目录限定能访问的范围",
	"@modelcontextprotocol/server-everything":          "向客户端发 roots、sampling、elicitation 请求，工具列表随客户端能力变化",
	"@playwright/mcp":                                  "控制一个浏览器，页面状态属于会话",
	"@executeautomation/playwright-mcp-server":         "控制一个浏览器，页面状态属于会话",
	"@modelcontextprotocol/server-puppeteer":           "控制一个浏览器，页面状态属于会话",
	"@browsermcp/mcp":                                  "控制一个浏览器，页面状态属于会话",
	"@fangjunjie/ssh-mcp-server":                       "持有 SSH 连接",
	"ssh-mcp":                                          "持有 SSH 连接",
	"@wonderwhy-er/desktop-commander":                  "持有终端会话",
	"serena":                                           "绑定一个项目目录",
	"mcp-server-windows-x64":                           "连接正在运行的桌面应用",
	"mcp-for-blender":                                  "连接正在运行的 Blender",
	"blender-mcp":                                      "连接正在运行的 Blender",
}

// LaunchPackage names what an entry runs: the npm package for npx, the
// package for uvx, the script for node/python, otherwise the executable.
func LaunchPackage(e Entry) string {
	e, _ = Unwrap(e)
	argv := proc.UnwrapCmd(append([]string{e.Command}, e.Args...))
	if spec, _, ok := npmcache.Detect(argv); ok {
		name, _ := npmcache.SplitSpec(spec)
		return name
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0])))
	switch {
	case base == "uvx" && len(argv) > 1:
		pkg, _, _ := strings.Cut(argv[1], "==")
		pkg, _, _ = strings.Cut(pkg, "@")
		return strings.ToLower(pkg)
	case (strings.HasPrefix(base, "python") || base == "node" || base == "bun" || base == "deno") && len(argv) > 1:
		return filepath.Base(argv[1])
	}
	return base
}

// Classify decides whether an entry can be taken over now.
func Classify(e Entry, baseURL string) (Class, string) {
	if _, ok := Unwrap(e); ok {
		return ClassManaged, "经 mcpcell-run 运行"
	}
	switch {
	case e.URL != "" && baseURL != "" && strings.HasPrefix(e.URL, baseURL+"/s/"):
		return ClassManaged, "已经连接 MCPcell"
	case e.URL != "":
		return ClassRemote, "已经是 HTTP 服务，本机没有它的进程"
	case e.Kind != "stdio":
		return ClassUnknown, "无法识别的配置写法"
	case e.Client == "codex" && (e.Name == "codex" || e.Name == "node_repl" || e.Name == "cua_repl"):
		return ClassInternal, "Codex 应用自带的组件，和应用进程绑定"
	case len(e.EnvVars) > 0:
		return ClassUnknown, "通过 env_vars 读取客户端自己的环境变量，MCPcell 拿不到这些值"
	}
	pkg := LaunchPackage(e)
	if knownShared[pkg] {
		return ClassShared, "无会话状态，可以由所有会话共用"
	}
	if why, ok := knownStateful[pkg]; ok {
		return ClassStateful, why + "；托管后每个会话一个实例"
	}
	return ClassUnknown, "不在已知清单里：托管后默认每个会话一个实例，确认它没有会话状态后可以改为共享"
}
