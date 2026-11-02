# MCPcell

本机 MCP 运行时：一个常驻守护进程代替"每个 Agent 会话各起一套 MCP 进程"。设计见 `docs/architecture.md`，里程碑与验收见 `docs/roadmap.md`（M3a 接管、M3b 按会话隔离已完成）。

## 布局

- `cmd/mcpcell/` — 同一份代码构建两个二进制：`mcpcell.exe`（命令行）和 `mcpcelld.exe`（`-H windowsgui`，无窗口：开机自启的守护进程、WebView2 窗口进程，双击即打开窗口）。守护进程**没有任何界面**；每个命令先校验参数，`-h/--help` 只打印用法（`dispatch` 返回退出码，2 表示用法错误、什么也没做）。命令：`mcpcell help` 只列常用的 `open`、`status`、`takeover`、`restore`、`stop`、`version`（`commands` 前 `everyday` 个），`help --all` 再列 `ui`、`start`、`daemon`、`mode`、`ps`（同 status）、`clients`、`autostart`、`shortcut`、`install`、`update`；守护进程内部用的 `window` 不列出。界面方式（config.json 的 `interface`：`gui` 默认带托盘图标，`cli` 不带）只决定托盘进程启不启动（含开机自启的 `MCPcellTray` 一项），`mcpcell mode` 经 `/api/interface`（守护进程没运行时直接）启停它；`takeover` 调用和管理页「一键托管」相同的 `/api/mcp` 与 `/api/clients/{client}/attach`，不另写改配置的逻辑。`rsrc_windows_amd64.syso` 是图标、清单（PerMonitorV2、通用控件 v6）和版本信息，由 `winres/winres.json` 生成
- `cmd/mcpcell-tray/` — 托盘图标 `mcpcell-tray.exe`（`-H windowsgui`）：单独的小进程，看守护进程状态（自己用原始 TCP 发 HTTP/1.0）、打开窗口（启动 `mcpcelld open`），「退出」只关托盘和窗口。**不能链接 `net/http`、`crypto/rand`、`internal/registry`**（否则多 32 MB，架构 §13），找目录和端口用 `internal/home`；实测约 14 MB 私有提交。按 MCPcell home 区分实例（窗口属性 `MCPcellHome` + 命名互斥量），试验用的托盘和正式的互不干扰
- 自动托管（`internal/httpd/autotakeover.go`，config.json `autoTakeover` 默认开）：守护进程定期看 Agent 配置文件的修改时间，只对第一次看过之后**新出现**的条目走和一键托管相同的 `tryForTakeover` + `attachOne`；还原过的进排除名单；Agent 把条目改回原命令最多重新托管一次。测试用 `Options.AutoTakeoverEvery = 0`（不起后台循环）直接调 `autoTakeover()`；试验时 `MCPCELL_AUTO_TAKEOVER_SECS` 缩短间隔
- 所有应用同一套 MCP（`internal/httpd/sync.go`，config.json `syncApps` 默认开；设置页开关、`mcpcell takeover --sync on|off`）：某个应用经 MCPcell 用着的服务，加到还没有它的应用里（`unified` 给每行算 `syncTo`，`POST /api/sync` 照着加，复用「添加到应用」的 `addRow`）；一键托管（页面、引导、不带 `--client`/条目名的 `mcpcell takeover`）托管完就同步，自动托管接到新条目后也同步；跳过打不开的、只有 Claude Code 项目用的、远程的、应用里有同名别的 MCP 的，以及用户从某个应用里还原过（`Excluded`）或删掉 MCPcell 加的条目（记录还在）的
- `internal/jsonrpc` 原始报文 · `home` 数据目录、daemon.json、token（不链接 crypto/rand） · `registry` 配置/token · `proc` 进程、Job Object、内存上限（只有 Windows）；内存统计按平台分（`usage_*.go`：Windows 私有提交，Linux `/proc` 的 RssAnon，macOS 用 `ps` 加总进程组的常驻内存） · `instance` 单个上游的多会话路由（独占实例还转发服务端请求） · `pool` 实例生命周期、预算、扁平化；共享服务一个槽、按会话服务每个会话一个槽 · `catalog` 能力目录缓存 · `npmcache` npx 包缓存与版本锁定 · `clients` 客户端配置读取、接管与还原：Claude Code（`claude` CLI）、Codex（TOML）和 `jsonclient.go` 的 `jsonAgents`——Cursor、Claude Desktop、VS Code（`servers`）、Copilot CLI、Gemini CLI、Antigravity、Windsurf、Kiro、Cline、Roo Code、OpenCode（`mcp`，命令是数组；`opencode.jsonc` 在就用它，否则 `opencode.json`，每次用时再找，见 `firstOf`）都是"一个 JSON 文件里的一组 MCP"，只改那一项、保留注释和其他设置；路径一律从传入的 home 算（`appDir`：Windows `AppData\Roaming`、macOS `Library/Application Support`、其他 `.config`），所以测试和试验守护进程换了 home 就碰不到真实配置；条目里有 `${…}` 或 `envFile` 的标 `Hold`（变量由应用自己展开）；没托管的条目程序不在（`missing.go` 的 `Missing`：按 PATH 找命令、npm 垫片脚本、node/python 要跑的脚本）或上次试启动失败（`takeover.json` 的 `Failed`，按条目内容记，内容一变就失效，托管成功就清掉）的标 `EntryView.Dead`：一键托管、自动托管、`mcpcell takeover` 都跳过，界面算「无法启动」收进列表底部折叠的「已忽略」，详情里可以「重试」；已托管的服务打不开时，从没启动成功过（启动 0 次、没有工具列表）或程序不在的同样算「无法启动」收进已忽略（`model.ts` 的 `mcpView`），只有以前能用、现在坏了的才报红；httpd 测试的 TestMain 在 PATH 上放了 `fake`、`exits-mcp` 两个空程序，让测试条目不被当成缺程序；Claude Code 有自己 MCP 的项目各是一个子应用 `claude-code@<目录>`（`claude_project.go`，`Expand` 展开）：项目私有（local 作用域，`~/.claude.json` 的 projects 下）的用 `claude mcp -s local` 在项目目录里改；项目 `.mcp.json` 里的绝不改文件，加一条同名 local 条目覆盖它（`Record.Override`），还原时删掉；没批准的、用了 `${VAR}` 的标 `Entry.Hold` 不托管；批准记录要同时看项目 `.claude/settings.local.json`、`settings.json`、用户 `settings.json` 和 `~/.claude.json`（Claude Code 改 local 作用域时会把它从后者挪到前者） · `autostart` 开机自启（Windows 注册表 Run 键；macOS `~/Library/LaunchAgents/com.mcpcell.daemon.plist`；Linux XDG `autostart/mcpcell.desktop`；后两者没有托盘，下次登录生效） · `desktop` 托盘进程（`RunTray`）、WebView2 窗口（独立进程，直接调 COM，不带 WebView2Loader.dll，每个 home 一个）、启停与查找它们、快捷方式 · `httpd` HTTP 入口、管理 API、内嵌状态页（`httpd/ui/`）
- `web/` — 管理页源码（React + Vite + Tailwind + lucide，只做桌面）。**小窗口（默认 920×620，最小 760×520，`internal/desktop/window_windows.go`；页面最小宽 720）。风格参照 magpie 和 CC Switch：简洁但功能齐全、可视化好，不靠藏功能来简化；页面上不写说明文字，解释放进悬停提示（`ui.tsx` 的 `Tip`、`title`）**。顶部一条：名字、MCP / 内存 / 动态 三个标签、设置齿轮（`App.tsx` 的 `TopBar`）。「MCP」（`pages/Home.tsx`）：应用筛选（`#/?agent=codex`）、三个数字一条（`stats.tsx` 的 `StatStrip`：内存、运行中、对话，取 `/api/metrics?range=now`；没有"比各开各的省"这类无法核对的估算）、自动托管和配置被改动的一行提示、所有 MCP 在一张卡片里一行一个（名字、用途、哪些应用在用（品牌图标，彩色是已托管，灰色虚线点一下托管）、内存或状态、悬停才出现的启动/停止和更多、托管开关）。每行的应用图标超过 3 个时折叠成「+N」，点开再点箭头收起。一键托管（首页弹窗、引导第三步）用 `run.tsx` 的 `useTakeoverRun` 一个应用一个应用地托管、再一个应用一个应用地同步，逐行显示进度（正在进行的那行自动滚到眼前）。详情页里没有这个 MCP 的应用合成一行「未添加 N 个应用 · 全部添加」。「全部还原」（`?all=1`，`restore --all` 同样）不把同步加的条目记进排除名单，下次一键托管会再同步回来；单个还原的记进排除名单、不再加回。「一键托管」、托管开关、虚线图标、单个还原、添加到应用都是点了直接执行（`agents.tsx` 的 `useAgentActions`，改前照样备份），只有「全部还原」和「忽略」先确认。使用引导（`guide.tsx`，`App.tsx` 挂载）：盖住整个窗口的干净页面，介绍 → 一键检测（重读 `/api/mcp`，列出找到的应用和各自的 MCP 数、没找到的应用，以及可托管 / 已托管 / 打不开 / 远程或内置）→ 一键托管（按 MCP 计数）；第一次打开窗口时自动弹出（`localStorage` 的 `mcpcell.guide`），之后从首页「更多」菜单或设置页的「使用引导」打开。「内存」（`pages/Memory.tsx`：数字条、1 小时/24 小时曲线 `history.tsx`、每个 MCP 的内存表）、「动态」（`pages/Activity.tsx`：按天分组，可筛选）。详情 `pages/Mcp.tsx`（`#/mcp/<名>`：返回箭头、这个 MCP 的数字条和最近 1 小时内存曲线、哪些应用在用，技术设置在「高级」）。**用词简洁、不用口语句子**：状态和动作用短的标准词——已托管 / 未托管 / 配置被改动 / 远程 / 内置，待命 / 启动中 / 内存数 / 打不开：原因，启动 / 停止 / 重试 / 托管 / 还原 / 忽略 / 添加；不写"用 MCPcell 共用的这份""各开各的""打开一次试试"这类句子；会话说"对话"，不出现回收、能力目录、实例等内部词；原因由 `model.ts` 的 `plainReason` 从错误和 stderr 翻译，用途取 `purposes.ts`（常见包的中文说明）或 MCP 自带的 description / instructions，再不行列工具名。数据在 `data.tsx`（`/api/status` 2 秒、`/api/mcp` 4 秒轮询）。设置页（齿轮）：常规（开机自启、自动托管新 MCP、托盘图标、快捷方式）、后台服务（状态、地址、内存、令牌、配置、停止）、命令行都直接显示，每行只有名字和开关；手动接入和诊断（原始连接、`run-` 变体）折叠。`cd web && npm run build` 输出到 `internal/httpd/ui/dist`，由 Go embed 打进二进制；构建产物要一起提交，这样构建 Go 不需要 Node。客户端品牌图标来自 lobe-icons（MIT，见 `docs/icon-notices.md`）
- `internal/testutil/fakemcp` — 基于官方 Go SDK 的测试上游；测试二进制通过 `MaybeRun()` 自我 re-exec 成为它
- `tools/mcpprobe` — 并发 SDK 客户端，用于对真实服务做验收
- `tools/mkicon` — 用无头 Edge 把 Logo 渲染成 `internal/desktop/mcpcell.ico`
- `shim/` — `mcpcell-run`（Rust，只用标准库）：接管时包在 Agent 条目原命令前面的转发程序，每个会话、每个 MCP 一个，必须保持约 1 MB 私有提交（Go 运行时在 Windows 上的下限约 11 MB，所以不用 Go）；协议见 `internal/httpd/run.go`。守护进程按命令和参数把包装连接对到已配置的服务上
- 接管写的是包装格式（`clients/wrap.go`：Claude Code、Cursor 保留 JSON 键顺序，Codex 只替换 command、args 两行）；没有安装 `mcpcell-run` 时退回 HTTP 端点格式
- `legacy/python-0.5/` — 0.5.0a1 Python 原型，只读参考。M3 移植配置发现/改写时以它为准（`discovery.py`、`patching.py`、`management.py`）
- `scripts/measure-mcp.ps1` — 内存基线测量（MCPcell 管理窗口单独一行，不算作 MCP）；`scripts/mcpbench.py`（内存与速度；`--concurrent` 是多个对话同时调用多个 MCP，`scripts/mcpbench_concurrent_plot.py` 出一张方块对比图（内存、等多久能用、全部做完））、`scripts/mcpcompat.py`（真实 MCP 原生 vs 经 MCPcell 的差分测试）、`scripts/mcpconformance.py`（官方一致性套件，原生 / 共享 / 按会话对照）、`scripts/mcpreconnect.py`（守护进程重启、崩溃、退出时包装会话不断；先探测再握手的客户端；起不来的服务交回原生且不反复重试；端到端）、`scripts/mcptray.py`（托盘出现/退出、退出后包装会话仍走守护进程、mode 启停托盘、`stop --help` 不停服务、托盘私有提交；`--window` 还会短暂打开窗口）；`bench/` 存本机结果，已 git-ignore（仓库公开，结果会暴露本机装了哪些 MCP）

## 命令

`.github/workflows/build.yml`：每次推送在 macOS（Apple 芯片，顺带编译 Intel 版）、Linux、Windows 上跑 vet 和全部测试，编译 Rust 的 mcpcell-run，打成 `mcpcell-<系统>-<架构>.tar.gz`（tar 保留可执行权限）；`v*` 标签把包挂到 Release。本机只能交叉编译 Go：`GOOS=darwin GOARCH=arm64 go build ./cmd/mcpcell`；Unix 上的测试可以在 WSL 里跑（Go 解压到 WSL 的 `/tmp`，`GOCACHE`、`GOPATH` 也放 `/tmp`，不要加 `-mod=mod`）。测试里不要写死 Windows 路径或只有某个系统才有的命令：转发程序路径按系统给，`cmd /c`、`npx.cmd` 这类用例只在 Windows 上跑；httpd、cmd 的 TestMain 在 PATH 上放了测试条目要用的空程序。

```sh
go vet ./... && go test ./...
GOOS=linux go vet ./...                       # Unix 分支只能这样检查
cd web && npm install && npm run build && cd ..   # 改了管理页才需要
go build -ldflags "-s -w" -o bin/mcpcell.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcelld.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcell-tray.exe ./cmd/mcpcell-tray   # 托盘；rsrc_windows_amd64.syso 是 cmd/mcpcell 的那份拷贝
MCPCELL_REAL_CLAUDE=1 go test ./internal/clients/ -run RealClaude   # 用真实 claude CLI，临时目录（含项目 local 条目和 .mcp.json 覆盖）
cd shim && cargo test --release && cargo build --release && cd .. && cp shim/target/release/mcpcell-run.exe bin/
bin/mcpcell.exe install    # 装到 ~/.mcpcell/bin 并从那里重启守护进程；用户的 Agent 配置指向那里，改完代码要重新 install（--cli/--gui 同时选界面方式，不写不变）
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
- `mcpcell restore` 在守护进程运行时交给守护进程做（`/api/clients/{id}/restore`）：它内存里有接管记录，绕过它直接改文件，它会把变回原命令的条目当成应用自己改回去而重新托管。
- `MCPCELL_HOME` 只隔离 MCPcell 自己的数据，读写的客户端配置仍是真实的。试验守护进程要同时把 `CODEX_HOME`、`CLAUDE_CONFIG_DIR`、`USERPROFILE` 指到临时目录（JSON 类应用的配置都按 home 找），否则它的窗口和正式版一模一样，用户一点就会把真实配置接管到试验端口。截完图立刻关窗口，停掉前用 `MCPCELL_HOME=<临时目录> mcpcell restore --all` 收尾。
- 用真实 Codex 测试时用 `codex exec --ignore-user-config --ephemeral -c mcp_servers.<名>.url=...`：不读也不改 `config.toml`，鉴权仍用 `CODEX_HOME`，不需要复制登录凭据。Codex 只给 MCP 进程传 24 个固定变量加条目的 `env`（`MCPCELL_HOME` 要写进条目）；`codex exec` 会拦下需要审批的 MCP 工具，用 `default_tools_approval_mode="approve"` 放行；测内存时排除 `codex-code-mode-host.exe` 及其 node 子进程。
- 用 Git Bash 调 Windows 程序时 `/c` 这类参数会被改成路径，测试脚本要设 `MSYS_NO_PATHCONV=1`。
- **测试绝不能碰真实客户端配置**：适配器会读 `CODEX_HOME`、`CLAUDE_CONFIG_DIR`，`internal/clients` 的 TestMain 会清掉它们，新测试要用临时目录并调用 `inTemp`。测试失败信息不要打印 Entry 的环境变量值。
- 代码注释用英文，文档和面向用户的提示用中文（与 legacy 一致）。
- `~/.mcpcell/` 下：`bin/`（安装的 mcpcell、mcpcelld、mcpcell-run、mcpcell-tray；Agent 配置引用这里的 mcpcell-run）、`config.json`（服务、端口、界面方式）、`token`、`daemon.json`、`daemon.log`、`catalog/`（能力目录；文件里的 `failure` 是最近一次启动失败，重启后仍显示"打不开"，配置改了或启动成功就清掉）、`pkgs/`（npm 包与 `pins.json`）、`takeover.json`（接管记录，含原始条目；自动托管用的 `Seen`/`Baseline`（见过的条目）、`Excluded`（还原过、不再管、或 Agent 反复改回的，不再自动托管）、`Events`（最近的自动托管）、`Failed`（试启动失败的条目））、`backups/`（客户端配置整文件备份）、`events.json`（最近动态）、`webview/`（窗口的 WebView2 配置目录）、`window.json`（窗口位置）。
- 结论以测量为准：用 `scripts/measure-mcp.ps1` 或 `mcpcell ps` 对比基线，不写未测过的节省比例。
