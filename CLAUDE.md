# MCPcell

本机 MCP 运行时：一个常驻守护进程代替"每个 Agent 会话各起一套 MCP 进程"。设计见 `docs/architecture.md`，里程碑与验收见 `docs/roadmap.md`（M3a 接管、M3b 按会话隔离已完成）。

## 布局

- `cmd/mcpcell/` — 同一份代码构建两个二进制：`mcpcell.exe`（命令行）和 `mcpcelld.exe`（`-H windowsgui`，无窗口：开机自启的守护进程、桌面窗口进程，双击即打开窗口）。命令：`open`、`ui`、`start`、`stop`、`daemon`、`ps`、`clients`、`restore`、`autostart`、`shortcut`、`update`，以及守护进程内部用的 `window`。`rsrc_windows_amd64.syso` 是图标、清单（PerMonitorV2、通用控件 v6）和版本信息，由 `winres/winres.json` 生成
- `internal/jsonrpc` 原始报文 · `registry` 配置/token · `proc` 进程、Job Object、内存上限 · `instance` 单个上游的多会话路由（独占实例还转发服务端请求） · `pool` 实例生命周期、预算、扁平化；共享服务一个槽、按会话服务每个会话一个槽 · `catalog` 能力目录缓存 · `npmcache` npx 包缓存与版本锁定 · `clients` 客户端配置读取、接管与还原 · `autostart` 开机自启 · `desktop` 托盘图标（在守护进程里）、WebView2 窗口（独立进程，直接调 COM，不带 WebView2Loader.dll）、快捷方式 · `httpd` HTTP 入口、管理 API、内嵌状态页（`httpd/ui/`）
- `web/` — 管理页源码（React + Vite + Tailwind + lucide，风格参照 CC Switch，只做桌面）。`cd web && npm run build` 输出到 `internal/httpd/ui/dist`，由 Go embed 打进二进制；构建产物要一起提交，这样构建 Go 不需要 Node。客户端品牌图标来自 lobe-icons（MIT，见 `docs/icon-notices.md`）
- `internal/testutil/fakemcp` — 基于官方 Go SDK 的测试上游；测试二进制通过 `MaybeRun()` 自我 re-exec 成为它
- `tools/mcpprobe` — 并发 SDK 客户端，用于对真实服务做验收
- `tools/mkicon` — 用无头 Edge 把 Logo 渲染成 `internal/desktop/mcpcell.ico`
- `shim/` — `mcpcell-run`（Rust，只用标准库）：接管时包在 Agent 条目原命令前面的转发程序，每个会话、每个 MCP 一个，必须保持约 1 MB 私有提交（Go 运行时在 Windows 上的下限约 11 MB，所以不用 Go）；协议见 `internal/httpd/run.go`。守护进程按命令和参数把包装连接对到已配置的服务上
- 接管写的是包装格式（`clients/wrap.go`：Claude Code、Cursor 保留 JSON 键顺序，Codex 只替换 command、args 两行）；没有安装 `mcpcell-run` 时退回 HTTP 端点格式
- `legacy/python-0.5/` — 0.5.0a1 Python 原型，只读参考。M3 移植配置发现/改写时以它为准（`discovery.py`、`patching.py`、`management.py`）
- `scripts/measure-mcp.ps1` — 内存基线测量；`scripts/mcpbench.py`（内存与速度）、`scripts/mcpcompat.py`（真实 MCP 原生 vs 经 MCPcell 的差分测试）、`scripts/mcpconformance.py`（官方一致性套件，原生 / 共享 / 按会话对照）；`bench/` 存本机结果，已 git-ignore（仓库公开，结果会暴露本机装了哪些 MCP）

## 命令

```sh
go vet ./... && go test ./...
GOOS=linux go vet ./...                       # Unix 分支只能这样检查
cd web && npm install && npm run build && cd ..   # 改了管理页才需要
go build -ldflags "-s -w" -o bin/mcpcell.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcelld.exe ./cmd/mcpcell
MCPCELL_REAL_CLAUDE=1 go test ./internal/clients/ -run RealClaude   # 用真实 claude CLI，临时目录
cd shim && cargo test --release && cargo build --release && cd .. && cp shim/target/release/mcpcell-run.exe bin/
bin/mcpcell.exe install    # 装到 ~/.mcpcell/bin 并从那里重启守护进程；用户的 Agent 配置指向那里，改完代码要重新 install
MCPCELL_HOME=<临时目录> bin/mcpcell.exe daemon   # 试验时别动用户的 ~/.mcpcell；config.json 里换个 port
# 改了 Logo 或版本号才需要（版本号在 winres.json 里另写一份）：
go run ./tools/mkicon
cd cmd/mcpcell && go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64 && cd ../..
```

## 约定

- Go 1.26，运行时依赖只有标准库和 `golang.org/x/sys`。官方 `go-sdk` 只用于测试和 `tools/`（理由见架构 §13）。按原始 JSON-RPC 转发，未知字段原样透传。
- 协议有两版：2025-11-25 及以前带握手和会话，2026-07-28 无状态。当前端点只提供旧版，新版客户端靠 400 回退。改路由前先看 `docs/architecture.md` §6.3。
- 路由只允许改写 id、progressToken、cancelled 的 requestId，不改工具名（聚合 hub 模式除外）、不伪造能力、不重放调用。上游中途退出时返回 `CodeResultUnknown`，不自动重试。
- 服务端的回答随客户端变化：握手用客户端请求的协议版本（共享实例按版本分开），按会话的实例还用客户端的能力；能力目录按这个"客户端画像"分别缓存，不能拿一种客户端的目录回答另一种。共享服务由资格检测和"收到服务端请求"自动改为按会话（架构 §8）。
- Windows 优先：每个上游进程放进 Job Object（KILL_ON_JOB_CLOSE）。内存一律报私有提交（private commit），不能只报 RSS/工作集。
- 任何链接了 `crypto/rand`（包括 `net/http`）的 Go 程序在 Windows 上会多出 32 MB 提交量（架构 §13）。每会话一份的组件（shim）不能链接它们。
- 只监听 loopback；校验 Host 和 Origin；MCP 端点只认 Bearer token，管理 API 认 token 或状态页 cookie，cookie 写操作需带 `X-MCPcell: 1`。日志不记录 params/result 和环境变量值，stderr 只在内存。
- 改写客户端配置前先备份，改动只限所选客户端与条目，必须可还原；还原必须逐字节恢复，条目被用户改过就拒绝覆盖。
- `MCPCELL_HOME` 只隔离 MCPcell 自己的数据，读写的客户端配置仍是真实的。试验守护进程要同时把 `CODEX_HOME`、`CLAUDE_CONFIG_DIR` 指到临时目录（Cursor 读 `%USERPROFILE%\.cursor`，不要对它做写操作），否则它的窗口和正式版一模一样，用户一点就会把真实配置接管到试验端口。截完图立刻关窗口，停掉前用 `MCPCELL_HOME=<临时目录> mcpcell restore --all` 收尾。
- 用真实 Codex 测试时用 `codex exec --ignore-user-config --ephemeral -c mcp_servers.<名>.url=...`：不读也不改 `config.toml`，鉴权仍用 `CODEX_HOME`，不需要复制登录凭据。Codex 只给 MCP 进程传 24 个固定变量加条目的 `env`（`MCPCELL_HOME` 要写进条目）；`codex exec` 会拦下需要审批的 MCP 工具，用 `default_tools_approval_mode="approve"` 放行；测内存时排除 `codex-code-mode-host.exe` 及其 node 子进程。
- 用 Git Bash 调 Windows 程序时 `/c` 这类参数会被改成路径，测试脚本要设 `MSYS_NO_PATHCONV=1`。
- **测试绝不能碰真实客户端配置**：适配器会读 `CODEX_HOME`、`CLAUDE_CONFIG_DIR`，`internal/clients` 的 TestMain 会清掉它们，新测试要用临时目录并调用 `inTemp`。测试失败信息不要打印 Entry 的环境变量值。
- 代码注释用英文，文档和面向用户的提示用中文（与 legacy 一致）。
- `~/.mcpcell/` 下：`bin/`（安装的 mcpcell、mcpcelld、mcpcell-run；Agent 配置引用这里的 mcpcell-run）、`config.json`、`token`、`daemon.json`、`daemon.log`、`catalog/`（能力目录）、`pkgs/`（npm 包与 `pins.json`）、`takeover.json`（接管记录，含原始条目）、`backups/`（客户端配置整文件备份）、`webview/`（窗口的 WebView2 配置目录）、`window.json`（窗口位置）。
- 结论以测量为准：用 `scripts/measure-mcp.ps1` 或 `mcpcell ps` 对比基线，不写未测过的节省比例。
