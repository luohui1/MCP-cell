# MCPcell

本机 MCP 运行时：一个常驻守护进程代替"每个 Agent 会话各起一套 MCP 进程"。设计见 `docs/architecture.md`，里程碑与验收见 `docs/roadmap.md`（M2 已完成，下一步 M3）。

## 布局

- `cmd/mcpcell/` — 同一份代码构建两个二进制：`mcpcell.exe`（命令行）和 `mcpcelld.exe`（`-H windowsgui`，开机自启用的无窗口守护进程）。命令：`start`、`stop`、`daemon`、`ps`、`ui`、`clients`、`restore`、`autostart`、`update`
- `internal/jsonrpc` 原始报文 · `registry` 配置/token · `proc` 进程、Job Object、内存上限 · `instance` 单个上游的多会话路由 · `pool` 实例生命周期、预算、扁平化 · `catalog` 能力目录缓存 · `npmcache` npx 包缓存与版本锁定 · `clients` 客户端配置读取、接管与还原 · `autostart` 开机自启 · `httpd` HTTP 入口、管理 API、内嵌状态页（`httpd/ui/`，无框架，只做桌面）
- `web/` — 管理页源码（React + Vite + Tailwind + lucide，风格参照 CC Switch，只做桌面）。`cd web && npm run build` 输出到 `internal/httpd/ui/dist`，由 Go embed 打进二进制；构建产物要一起提交，这样构建 Go 不需要 Node。客户端品牌图标来自 lobe-icons（MIT，见 `docs/icon-notices.md`）
- `internal/testutil/fakemcp` — 基于官方 Go SDK 的测试上游；测试二进制通过 `MaybeRun()` 自我 re-exec 成为它
- `tools/mcpprobe` — 并发 SDK 客户端，用于对真实服务做验收
- `legacy/python-0.5/` — 0.5.0a1 Python 原型，只读参考。M3 移植配置发现/改写时以它为准（`discovery.py`、`patching.py`、`management.py`）
- `scripts/measure-mcp.ps1` — 内存基线测量；`bench/` 存本机结果，已 git-ignore（仓库公开，结果会暴露本机装了哪些 MCP）

## 命令

```sh
go vet ./... && go test ./...
GOOS=linux go vet ./...                       # Unix 分支只能这样检查
cd web && npm install && npm run build && cd ..   # 改了管理页才需要
go build -ldflags "-s -w" -o bin/mcpcell.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcelld.exe ./cmd/mcpcell
MCPCELL_REAL_CLAUDE=1 go test ./internal/clients/ -run RealClaude   # 用真实 claude CLI，临时目录
MCPCELL_HOME=<临时目录> bin/mcpcell.exe daemon   # 试验时别动用户的 ~/.mcpcell
```

## 约定

- Go 1.26，运行时依赖只有标准库和 `golang.org/x/sys`。官方 `go-sdk` 只用于测试和 `tools/`（理由见架构 §13）。按原始 JSON-RPC 转发，未知字段原样透传。
- 协议有两版：2025-11-25 及以前带握手和会话，2026-07-28 无状态。当前端点只提供旧版，新版客户端靠 400 回退。改路由前先看 `docs/architecture.md` §6.3。
- 路由只允许改写 id、progressToken、cancelled 的 requestId，不改工具名（聚合 hub 模式除外）、不伪造能力、不重放调用。上游中途退出时返回 `CodeResultUnknown`，不自动重试。
- Windows 优先：每个上游进程放进 Job Object（KILL_ON_JOB_CLOSE）。内存一律报私有提交（private commit），不能只报 RSS/工作集。
- 任何链接了 `crypto/rand`（包括 `net/http`）的 Go 程序在 Windows 上会多出 32 MB 提交量（架构 §13）。每会话一份的组件（shim）不能链接它们。
- 只监听 loopback；校验 Host 和 Origin；MCP 端点只认 Bearer token，管理 API 认 token 或状态页 cookie，cookie 写操作需带 `X-MCPcell: 1`。日志不记录 params/result 和环境变量值，stderr 只在内存。
- 改写客户端配置前先备份，改动只限所选客户端与条目，必须可还原；还原必须逐字节恢复，条目被用户改过就拒绝覆盖。
- **测试绝不能碰真实客户端配置**：适配器会读 `CODEX_HOME`、`CLAUDE_CONFIG_DIR`，`internal/clients` 的 TestMain 会清掉它们，新测试要用临时目录并调用 `inTemp`。测试失败信息不要打印 Entry 的环境变量值。
- 代码注释用英文，文档和面向用户的提示用中文（与 legacy 一致）。
- `~/.mcpcell/` 下：`config.json`、`token`、`daemon.json`、`daemon.log`、`catalog/`（能力目录）、`pkgs/`（npm 包与 `pins.json`）、`takeover.json`（接管记录，含原始条目）、`backups/`（客户端配置整文件备份）。
- 结论以测量为准：用 `scripts/measure-mcp.ps1` 或 `mcpcell ps` 对比基线，不写未测过的节省比例。
