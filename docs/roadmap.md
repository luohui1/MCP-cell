# MCPcell 里程碑与验收

日期：2026-10-17。每个里程碑结束时用 `scripts/measure-mcp.ps1` 测量并与基线对比；未测量的项不算完成。

## 基线（改造前）

2026-10-17 本机：6 个 Claude Code、1 个 Codex、1 个 Grok 会话，共 **71 个 MCP 相关进程**（含 18 个 `cmd.exe` npx 中间层），**约 2.9 GB 私有提交**，工作集只有约 0.8 GB。系统提交 60.8 / 68.7 GB。同一服务在多个会话中重复启动；同一次会话里 5 个 npx 启动的服务触发了 Claude Code 的 30 秒连接超时。

## M0 地基 ✅

- Go 模块、单二进制入口、`internal/jsonrpc` 原始报文封装及测试
- `legacy/python-0.5/` 保存原型并通过 SHA256 清单校验
- 基线测量脚本与本机基线
- `docs/architecture.md`、本文件、`CLAUDE.md`

## M1 共享内核 ✅

目标：证明"多个会话共用一个上游进程"在协议上是正确的。

**实测结果（2026-10-17，Windows 11）**：

| 验收项 | 结果 |
|---|---|
| 多会话共享 | 6 个官方 SDK 客户端加 2 个真实 Claude Code 2.1.302 会话，连同一个真实 context7，只有 1 棵上游进程树：4 个进程，私有提交约 235 MB。原来的方式下每个会话都会起一棵同样的树 |
| 协议回退 | SDK 客户端和 Claude Code 都先用 2026-07-28 探测，收到 400 后回退到 2025-11-25，列出和调用工具均正常 |
| 连接耗时 | 热连接 9–12 ms；冷启动约 10–11 s，几乎全部耗在 npx 上（M2 解决） |
| 不串线 | 6 个会话 × 40 个相同 id 并发（instance 层），3 个会话 × 40 个相同 id 经 HTTP 并发，0 串线 |
| 取消与崩溃 | 一个会话取消请求，不影响其他会话的同 id 请求；上游崩溃时，在途请求收到"结果未知"，下一个请求换新进程，不重放 |
| 无孤儿进程 | 强杀守护进程后，上游进程树（4 个进程）在 133 ms 内全部被系统清理 |
| 守护进程自身 | 约 45–55 MB 私有提交，其中 32 MB 是 Go 1.26 的固定开销（架构 §13）；工作集约 17 MB |
| 测试 | 39 项在 Windows 上连续多轮全部通过；Linux 只做了 `go vet` 和交叉编译，没有实际运行；竞态检测需要 cgo，这台机器上没跑 |

另外发现：上游树里 npx 自身的 node 进程占 137 MB，真正的服务 node 只占 93 MB，所以 M2 的 npx 扁平化预计能把每个实例的内存降低约六成。

范围调整：shim 移到 M3。服务定义用 `~/.mcpcell/config.json`，格式与客户端的 `mcpServers` 条目相同。状态页（原计划在 M5）提前到 M1 交付，是守护进程自带的 `/ui/`。

范围：只做旧版协议（2025-11-25，带握手和会话）的下游和上游。新版客户端（如 Claude Code v2）按规范回退到旧版。

交付：
- 第一步，技术验证：官方 Go SDK 的 `Transport`/`Connection` 能否作为原始传输层（原样保留 params、result 和 id 拼写）。结论写进 `docs/architecture.md` §13，再决定自己实现还是采用 SDK
- `mcpcell daemon`：loopback Streamable HTTP 入口，按服务路径 `/s/<service>` 暴露
- stdio 上游：拉起、逐行 JSON-RPC 读写、stderr 有界环形缓冲
- 路由：下游 id ↔ 上游 id 重映射、progressToken 映射、取消映射、通知归属
- 共享模式：守护进程向上游做一次 `initialize`，各会话的 `initialize` 用上游结果应答
- Windows Job Object（KILL_ON_JOB_CLOSE），守护进程退出后不留孤儿
- `mcpcell ps`：实例、会话、在途请求数、私有提交
- 状态页 `/ui/`（`mcpcell ui` 用一次性链接打开）：服务、实例、会话、内存，一键复制三种客户端的接入配置
- 服务定义先手写在 `~/.mcpcell/config.json`，导入留到 M3

验收：
- 6 个并发下游会话（go-sdk 测试客户端 + 1 个真实 Claude Code）连同一个 context7，**只有 1 个上游服务进程**
- 真实 Claude Code 用新版请求探测后回退到旧版，并能正常列出和调用工具
- 并发压测：每个会话都用 id=1..N 发 1000 个请求，响应 0 串线
- 一个会话取消请求，不影响其他会话的同 id 请求
- 杀掉上游：在途请求收到"结果未知"错误，后续请求重新拉起，不重放
- 杀掉守护进程：上游进程在 1 秒内全部消失

## M2 按需启动、回收与进程瘦身 ✅

**实测结果（2026-10-17，Windows 11，从全新目录开始）**：

| 验收项 | 结果 |
|---|---|
| 连接不启动进程 | 能力目录建好后，6 个会话连接并列出工具：连接 12–14 ms，守护进程下 **0 个上游进程**。真实 Claude Code 2.1.302 的会话也由目录应答（`fromCatalog=true`） |
| 调用时启动 | 第一次 `tools/call` 才拉起进程。冷调用总耗时 3.67 s，其中约 1.15 s 是 context7 自身的网络 API 时间（同一调用热执行为 1.15 s），**启动开销约 2.5 s**；原来走 npx 冷启动超过 10 s。按"3 秒内"的原定写法，总耗时没达标，启动开销达标 |
| npx 扁平化 | context7 从 4 个进程、约 235 MB，降到 **2 个进程（node + conhost）、约 84 MB**，减少约 64%。首次安装 npm 包并锁定版本（4.4.2）是一次性的，含启动共 20 s |
| 空闲回收 | shadcn-ui 设 `idleTimeout: 30s`，空闲 36 s 后回收到 0 个进程（回收检查每 15 s 一次）；目录仍在，下次调用再启动 |
| 实例上限 | 满额时停掉最久未用的空闲实例腾位置；全部忙碌时拒绝并说明原因（单元测试） |
| 内存上限 | Job 内存上限生效，超限后 **0.22 s 内终止整棵进程树**，在途调用收到"超出内存上限"。只设上限不够：实测进程会卡在上限上既不退出也不响应，所以加了 Job 完成端口监听，收到超限通知立即终止 |
| 测试 | 9 个包全部通过（新增 catalog、npmcache 两个包，pool、httpd、proc 增加了 M2 的测试） |

范围调整：
- 上游协议识别（`server/discover` 探测、支持只说新版协议的上游）移到 M3，和"双版本端点"一起做。本机全部服务都是旧版，只支持新版的服务目前很少；而且两者都是新旧版翻译，放在一起实现更合理。
- 每个 Job 都加了 `DIE_ON_UNHANDLED_EXCEPTION`，崩溃的服务直接退出，不会卡在 Windows 错误报告上。

交付：
- 目录缓存：`initialize`、`tools/list`、`prompts/list`、`resources/list` 用缓存应答，按配置指纹失效，收到 `list_changed` 时刷新并广播
- 调用时才获取实例；单次创建合并（并发冷启动只起一个）
- 空闲回收（默认 10 分钟，M3a 后改为 3 分钟）；崩溃退避；连续崩溃进入隔离状态，不无限重启
- npx 扁平化：首次导入时把包装进 MCPcell 的包缓存并锁定版本，之后直接 `node <bin.js>` 启动，去掉 `cmd.exe` 和 npx 自身的 node 进程，也不再每次启动都去查 `@latest`
- 预算：全局实例上限、按实例的 Job 内存上限（可选），超限时排队或拒绝，先驱逐空闲实例
- ~~上游协议识别~~ → M3

验收（原定）：
- 会话连上后、第一次调用前，上游进程数为 0
- 空闲超时后上游进程数回到 0；下一次调用自动冷启动
- context7 从每会话 3 个进程变为全机 1 个进程
- 已缓存的包：冷启动到首个 `tools/call` 返回的时间低于 Claude Code 的 30 秒超时（目标 < 3 秒，以实测为准）

## M3 接管、还原与隔离模式

分三步做，先拿到真实机器上的节省：

**M3a 接管与还原 ✅**

真机验收（2026-10-17，Windows 11，作者的开发机）：接管 Claude Code 的 3 个可共享条目（context7、shadcn-ui、一个 npm 的 API 包装服务）和 Codex 的 2 个，开启自启，预热后用 `scripts/bench-sessions.ps1` 做 A/B——同一台机器、同一份配置，各起 3 个全新的 Claude Code 会话，45 秒后测量：

| | 接管前 | 接管后 |
|---|---|---|
| 每个会话的 MCP 私有提交 | 约 632 MB（6–7 个进程） | **约 314 MB**（7 个进程，全是尚未接管的有状态服务） |
| 3 个会话 + MCPcell | 1,946 MB | **997 MB** |
| MCPcell 内的上游进程 | — | **0**（会话连接和列工具都由能力目录应答） |

- 接管前，context7、shadcn-ui 和一个 ssh 服务在新会话里**根本没连上**：npx 冷启动超过 Claude Code 的 30 秒连接超时被杀掉，占了启动开销却不可用。接管后它们即时可用
- 真实 Claude Code 会话（不加任何参数，用接管后的 `~/.claude.json`）调用 context7 成功；MCPcell 此时才拉起 context7（2 个进程），没被调用的服务保持 0 进程
- 被调用时每个服务只在全机起一份：实测 context7 约 81–116 MB、shadcn-ui 约 45 MB、API 包装服务约 218 MB，空闲 10 分钟回收
- 剩下的每会话开销来自有状态服务（浏览器调试、桌面应用桥接等），由 M3b 的按会话隔离处理
- 测量方法：会话用 `--input-format stream-json` 并保持 stdin 打开，不发任何消息，完全不调用模型；测量只计会话自己拉起的 MCP 进程树（排除 shell），以及创建时间晚于父进程的子进程（Windows 不会清除已退出父进程的 PID）

**性能对比**（同日，只加载被接管的 3 个服务：context7、shadcn-ui、一个 npm 的 API 包装服务；`--strict-mcp-config` 排除其他 MCP；"原生"使用接管前的原始条目）

内存与进程（私有提交，测量时刻为会话启动后 30–45 秒）：

| 同时会话数 | 原生 | MCPcell：只连接 | MCPcell：3 个服务都在运行 |
|---|---|---|---|
| 1 | 12 个进程，1,264 MB | 0 个进程，守护进程 53 MB | 6 个进程，406 MB |
| 3 | 36 个进程，3,825 MB | 0 个进程，守护进程 53 MB | 406 MB |
| 6 | 约 72 个进程、约 7.6 GB（按 1、3 会话线性推算；机器提交余量只剩约 7 GB，没有实测） | 0 个进程，守护进程 56 MB | 406 MB |

会话就绪时间（启动到 Claude Code 报告 MCP 全部连上）：

| 同时会话数 | 原生 | MCPcell |
|---|---|---|
| 1 | 15.6 s | 2.2 s |
| 3 | 平均 23.5 s，最慢 24.1 s | 平均 3.1 s，最慢 3.5 s |
| 6 | 未测 | 平均 3.8 s，最慢 4.2 s |

原生就绪时间随并发上升，几个 npx 同时启动互相抢资源；再多一些就会撞上 30 秒连接超时，此前测量中已经出现过服务被超时杀掉。

工具调用（`tools/mcpprobe`，各 3 次取中位数）：

| | 原生（每个新会话都是冷启动） | MCPcell 冷启动（空闲回收后第一次调用） | MCPcell 热调用 |
|---|---|---|---|
| context7 连接 + 调用 | 11.7 s + 2.3 s = 14.5 s | 0.006 s + 5.3 s = 5.4 s | 1.5 s |
| shadcn-ui 连接 | 8.8 s | 0.007 s | 0.007 s |

context7 的调用本身要访问外部 API（热调用约 1.1–1.5 s），两种方式都有这部分。shadcn-ui 的原生调用在测试机上因环境中一个失效的 `GITHUB_PERSONAL_ACCESS_TOKEN` 认证失败，没有可比的调用耗时；MCPcell 只向服务传白名单环境变量，因此不受影响。

**M3a 交付内容**
- `internal/clients`：读取 Claude Code（`~/.claude.json` 用户级）、Codex（`config.toml`）、Cursor（`mcp.json`）的 MCP 条目；按启动包、参数、环境变量值识别"同一个服务"，Claude Code 和 Codex 的 context7 映射到同一个 MCPcell 服务，凭据不同则分开
- 分类：可共享（已知无状态清单）/ 需要会话隔离 / 未知（可强制共享）/ 远程 HTTP / 客户端内置（Codex 的 codex、node_repl、cua_repl）/ 已连接 MCPcell
- 接管：Claude Code 走 `claude mcp remove/add-json -s user`（不和运行中的会话抢写文件，失败时把原条目放回）；Codex 只替换该条目的表块，保留 enabled、超时等客户端策略；Cursor 只替换该条目的 JSON 片段。改前整文件备份到 `~/.mcpcell/backups/`，接管记录在 `takeover.json`
- 还原：按记录逐字节放回；条目在接管后被改过就拒绝覆盖，可以"忘记记录"保留用户修改。`mcpcell restore --all` 不需要守护进程
- 开机自启：HKCU Run 键启动无窗口的 `mcpcelld.exe daemon --log`；`mcpcell start/stop`
- 管理页「客户端接入」改为接管中心；设置页加自启开关
- 测试：接管→还原后文件逐字节一致（含注释、CRLF）；被改过的条目拒绝还原；真实 `claude` CLI 的接管→还原（临时 `CLAUDE_CONFIG_DIR`，`MCPCELL_REAL_CLAUDE=1` 时运行）

**统一管理页**（M3a 之后）
- `GET /api/mcp` 把 MCPcell 的服务和各 Agent 的条目合并成一张表：同一个 MCP 在几个 Agent 里配置就是一行，每个 Agent 一格（托管 / 原生 / 已改动 / 远程 / 内置 / 未配置）；同名但参数不同的行显示参数，疑似密钥的参数值隐藏
- 管理页只剩 MCP、Agents、会话、设置四页：MCP 页逐格点击即可托管、还原、强制共享或加入其他 Agent（`POST /api/agents/{client}/add`，必要时先从另一个 Agent 的条目建服务）；Agents 页按 Agent 批量托管和还原，其余客户端仍给手动配置
- 加入 Agent 的条目在还原时整条删除，文件其余部分逐字节不变（Codex 表块、Cursor JSON 成员都有测试）
- Node 服务默认加 `--max-semi-space-size=1 --optimize-for-size`（服务级 `lean: false` 可关）；`--lite-mode`、`--jitless` 会关掉 WebAssembly 导致 fetch 失败，没有采用

**桌面应用**
- 守护进程带托盘图标：单击打开窗口；右键菜单「打开 MCPcell / 开机自启 / 退出 MCPcell」，退出前确认（退出后已托管的 MCP 不可用）；悬停显示运行中的 MCP 数和总内存；资源管理器重启后自动补回图标，退出时移除
- 窗口用系统自带的 WebView2 显示管理页，直接调 COM（`EmbeddedBrowserWebView.dll` 的 `CreateWebViewEnvironmentWithOptionsInternal`），不带 WebView2Loader.dll、不加依赖。窗口跑在守护进程拉起的独立进程（`mcpcelld window`）里，关窗口进程就退出：WebView2 不进守护进程，崩了也不影响共享的 MCP。外部链接交给默认浏览器；记住窗口位置；跟随系统深浅色；按显示器 DPI 缩放
- `mcpcell open`（双击 `mcpcelld.exe` 同此）：需要时先启动守护进程，再打开或聚焦窗口；没有 WebView2 时退回浏览器。`mcpcell shortcut on|off` 和设置页管理桌面、开始菜单快捷方式（`GET/POST /api/desktop`，`POST /api/window`）
- 实测（150% 缩放，空服务列表的试验守护进程，私有提交）：

| | 守护进程 | 窗口进程 | WebView2 进程 | 合计 |
|---|---|---|---|---|
| 只有托盘 | 47.5–48.3 MB | — | — | 约 48 MB |
| 窗口打开 | 53.6 MB | 48.7 MB | 6 个，约 283 MB | 385 MB |
| 关窗口后 2 s | 54.3 MB | 已退出 | 全部退出 | 54 MB |

  关窗后守护进程比空闲多的约 6 MB 来自管理页的 API 轮询，不开窗口、只用 curl 调 15 次 `/api/status` 和 `/api/mcp` 也是 47.5 → 54.5 MB；开关三轮不增长。最初把 WebView2 放在守护进程里时，关窗后 57 MB 且每轮涨约 1 MB，运行时 DLL 也一直不肯卸载（`DllCanUnloadNow` 始终说还在用），所以改成独立进程。窗口进程 48.7 MB 里约 32 MB 是同一个二进制链接 `net/http` 带来的固定提交（架构 §13），单独做一个只含窗口的小二进制能省下，暂未做

**一键托管与管理页改版**
- MCP 页按「已托管 / 可以托管 / 暂不支持 / 远程与内置」分组，每行一个托管开关；Agent 用图标表示状态，点图标给出能做的操作；顶部是托管数、实例、内存、会话
- 「一键托管」一次处理所有 Agent：已知可共享的默认勾选，未验证的默认不勾，需要会话隔离的列出但不托管；改哪些文件、哪些条目在确认框里写明。Agents 页的按 Agent 托管用同一个对话框
- 沙箱验证（临时 `CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`USERPROFILE`）：一键托管 4 个 MCP、共 3 个 Agent 的 6 个条目，再「全部还原」后 Codex、Cursor 配置逐字节一致，Claude Code 的 `mcpServers` 内容一致

**Codex 沙盒实测**（2026-10-17，Codex CLI 0.161.0）
- 本机自用的 MCP 在这里用化名：local-search（原生程序，代码检索）、local-py-a、local-py-b（Python 脚本）
- 方法：独立的 MCPcell（单独 home 和端口），沙盒 `CODEX_HOME` 里放 context7、local-search、sequential-thinking 三个原生条目，用接管接口转成 HTTP 条目（local-search 未验证，强制共享）。真实 Codex 用 `codex exec --ignore-user-config --ephemeral -c mcp_servers.*` 指向沙盒，不读不改用户的 `config.toml`，鉴权照旧。两个会话并发，分别在两个带不同标记的项目里，要求只用 MCP 工具找出项目标记并用 context7 查 React 的库 ID
- Codex 0.161 以 2025-11-25 协议连接，握手和工具列表来自能力目录

| 两个并发会话 | Codex 自己启动（stdio） | 经 MCPcell |
|---|---|---|
| 会话下的 MCP 进程 | 10 个（local-search ×2、cmd ×4、node ×4），峰值私有提交约 663 MB（含 Codex 代码模式的 node 进程，偏高，见下方「包装方式原型」的复测） | 0 个；全机 3 个服务实例约 122 MB + 守护进程 54 MB，和会话数无关 |
| context7 | 两个会话都没拿到：模型查找工具时它的工具还不在列表里，回答 UNAVAILABLE（单会话重跑也一样） | 两轮都答对 `/reactjs/react.dev` |
| local-search | 正确 | 正确：两个项目共用一个进程，各自拿到自己的标记 |

- local-search 共享后，不传 `path` 的调用以 MCPcell 的工作目录（用户目录）为准：一次 `glob` 扫了整个用户目录，用时 32 s；原生时是会话所在项目。它的说明要求传绝对路径，Codex 实际也传了绝对路径，所以能用；这类依赖当前目录的工具是 M3b「按项目一份」的候选
- 协议层（`tools/mcpprobe`）：local-search 绝对路径调用 21–36 ms，3 个客户端并发正常；context7 调用约 2.5 s（外部 API）；sequential-thinking 7 ms。崩溃的会话没有断开连接，留下的会话在 10 分钟无活动后清理

**M3b 按会话隔离** ✅（2026-10-17）
- 服务新增 `mode: "per-session"`：每个客户端会话一个独立实例，**第一次调用工具时才启动**，会话结束（DELETE，或 GET 流断开 2 分钟无活动）就回收；空闲回收默认 30 分钟。握手和工具列表仍由能力目录回答，连接本身不启动进程
- 按会话的实例用该会话自己的 `initialize` 参数（clientInfo、capabilities）握手，服务端请求（roots/list、sampling、elicitation）转发给所属会话：优先走它正在等待的 POST 流，否则走 GET 流；客户端通知（roots/list_changed 等）原样转发，`logging/setLevel` 只在实例已运行时转发。共享实例行为不变，仍然拒绝服务端请求
- 实例池按「槽」管理：共享服务一个槽，按会话服务每个会话一个槽；名额满时先回收空闲的共享实例，再动会话实例
- 接管规则：已知无状态的共享；有状态的和未知的默认按会话（和原生一样每个会话一份，但用时才启动、不留孤儿进程），未知的可以选择共享。一键托管默认全选
- 新建、修改或接管服务后在后台预热一次（启动、缓存工具列表、停止），否则缓存失效时第一批会话会各起一个实例来握手
- 客户端声明的能力（会话日志里记录名字）：Codex CLI 0.161 只有 `elicitation`；Claude Code 2.1.302 有 `elicitation`、`roots`
- 测试：池（各会话独立实例、复用、释放只停自己的、握手身份透传、roots 转发、预算先回收共享、空闲回收）；HTTP 端到端用官方 Go SDK 客户端（两个客户端各自的 roots 经 HTTP 转发并由 SDK 应答，断开一个只回收它的实例）

沙盒实测（独立 MCPcell + 沙盒 `CODEX_HOME`；chrome-devtools 用 `--headless --isolated`，local-py-a 照搬真实配置）：
- 两个 Codex 会话并发：A 用 chrome-devtools 打开 example.com 读标题并查 context7，B 只查 context7。经 MCPcell：两个会话下 0 个 MCP 进程；只有 A 起了 chrome-devtools 实例（含无头 Chrome，峰值 404 MB），B 没起；没人调用的 local-py-a 0 个实例；context7 共享一份 88 MB；A、B 都答对，A 的实例在会话结束时回收
- 同样两个会话让 Codex 自己启动：10 个进程、峰值 527 MB（每个会话都把 chrome-devtools、context7、local-py-a 全部启动；这个数含 Codex 代码模式的 node 进程，偏高，复测见下方「包装方式原型」），而 chrome-devtools 和 context7 在模型查工具时还没就绪，两个会话都没用上
- Claude Code（`claude -p --strict-mcp-config`）经 MCPcell 调按会话的 chrome-devtools 成功；`claude -p` 退出不发 DELETE，它的实例在退出后约 2 分钟回收
- Codex 的 `codex exec` 默认拦下需要审批的 MCP 工具（chrome-devtools 的 new_page），测试时用 `-c mcp_servers.<名>.default_tools_approval_mode="approve"` 放行

**包装方式：mcpcell-run** ✅（2026-10-17 接入，用 Rust 写）

接管时不再把 Agent 条目换成 HTTP 地址，而是保留原命令、加一个前缀：`"command": "mcpcell-run", "args": ["--", "npx", "-y", "@upstash/context7-mcp"]`。Agent 配置仍然写着实际运行的是什么，去掉前缀就是原生。

- 转发程序把会话交给守护进程（`GET /run` 升级后的一条连接，协议见 `internal/httpd/run.go`），发过去原命令、工作目录、完整环境变量。守护进程按命令在内存里找到或建立服务，不写 `config.json`：Agent 条目是唯一的定义
- 按会话的实例用会话自己的工作目录和环境变量启动，和原生一致；共享实例按「Agent 额外设置的环境变量」区分，Agent 和 shell 自带的变量（CLAUDE*、AI_AGENT、CODEX*、TERM、MSYS…）不参与
- 连接即会话：Agent 关掉转发程序的输入，会话和它的实例立即结束，不靠超时判断
- 兜底：守护进程没运行就拉起它（放在同目录的 `mcpcelld.exe`）；守护进程拒绝（版本旧、令牌错）或起不来，就直接运行原命令；守护进程在握手前也可以让它直接运行（比如 MCPcell 起不来的服务）。`--direct` 直接运行，`--shared` / `--per-session` 指定方式
- 启动提示和错误经 stderr 进入 Agent 的 MCP 日志

转发程序每个会话、每个 MCP 一个，所以它自己的大小决定了收益（私有提交，像 Agent 一样用管道、隐藏窗口启动）：

| | 私有提交 | 工作集 | 进程 | 就绪（工具列表来自缓存） |
|---|---|---|---|---|
| Rust 版（`shim/`，只用标准库，253 KB） | 0.9–1.0 MB | 4.7–5.2 MB | 1 | 0.03–0.8 s |
| Go 版（原型，已删除；不链接 net/http） | 12.4–13.2 MB | 9 MB | 1 | 0.03–0.08 s |
| Go 版编成控制台程序 | 约 14 MB | 15 MB | 2（多一个 conhost） | 0.07–0.09 s |
| 原生 npx context7 | 194–383 MB | 231–395 MB | 5 | 8.7–8.9 s |

Go 程序在 Windows 上的下限就是约 11.3–11.9 MB（只读标准输入的空程序），所以转发程序用 Rust。

真实 Agent（沙盒 MCPcell；Codex 两个会话并发：A 用 local-search 不传路径列 md 文件、chrome-devtools 读标题、context7 查 React，B 只查 context7；采样排除 Codex 代码模式的进程）：

| | 包装（Rust） | 原生 |
|---|---|---|
| 会话下的 MCP 相关进程 | 8 个 mcpcell-run，共 7 MB | cmd、node、python、local-search，共 1088 MB |
| 守护进程侧 | chrome-devtools 只为 A 起了一个（含无头 Chrome 416 MB）；local-search 只为 A 起了一个（2 MB）；context7 共享 85 MB；local-py-a 没人用，0 个 | — |
| 结果 | A、B 全对；A 的 local-search 返回会话自己项目里的 README.md | 只有 local-search 成功，chrome-devtools、context7 没就绪 |

- Claude Code 同样全对，local-search 返回它所在项目的文件；Claude 和 Codex 同时用 context7 时共用一个共享实例
- 守护进程没运行时由转发程序自动拉起；令牌错、守护进程起不来、`--direct` 三种情况都直接运行原命令并正常工作，不会多拉起守护进程
- Codex 只把 24 个变量（PATH、USERPROFILE、APPDATA……）加上条目里的 `env` 传给 MCP 进程，自定义变量和 `MCPCELL_HOME` 都传不过去；转发程序转交的正是原生 MCP 会拿到的那一份
- 测量时要排除 Codex 代码模式（`codex-code-mode-host.exe` 和它的 node 子进程），它们不是 MCP

接入：
- 接管默认写包装格式：Claude Code 经 `claude mcp add-json` 写入（保留 JSON 键顺序），Cursor 只换 command、args 两个成员，Codex 只换 command、args 两行（多行 args 合成一行），env、enabled、超时、注释都不动；新增条目也写包装格式；还原逐字节不变（Codex、Cursor 有 CRLF 用例）
- 接管时仍在 `config.json` 建服务，守护进程按命令和参数（加上服务设置的环境变量值）把包装连接对到这个服务：模式、空闲回收等设置和 HTTP 客户端共用同一个实例；用户手动加前缀的条目落到内存里的临时服务
- `mcpcell install` 把三个程序装到 `~/.mcpcell/bin`，Agent 配置引用那里的 mcpcell-run，开机自启和快捷方式只在原本指向旧位置时才改过去（它们是用户级设置，不随 MCPCELL_HOME 变化）
- 沙盒全流程：install → 接管（Codex 4 个、Claude Code 3 个，包括真实 `claude` CLI 写入）→ 真实 Claude Code 和 Codex 直接用写出的条目，全部答对，会话落在已配置的服务上，两个 Agent 共用 context7 → 全部还原后 Codex 配置逐字节一致，Claude Code 条目一致
- 本机迁移：11 个 HTTP 方式的托管全部改成包装格式；真实配置下 Claude Code、Codex（含正在运行的桌面版）都经转发程序连上守护进程，没有退回直接运行

多会话、多 MCP 实测（2026-10-17，沙盒 MCPcell，Codex CLI 0.161；3 个会话并发，每个会话配 9 个 MCP：context7、shadcn-ui、sequential-thinking、memory、filesystem、local-search、chrome-devtools、local-py-a、local-py-b；每个会话只用其中 2 个，chrome-devtools、local-py-a、local-py-b 谁都不用；启动超时都设 60 秒；各跑一轮预热后计数）：

| | 原生 | 包装（mcpcell-run） |
|---|---|---|
| 会话下的进程 | 72 个（node ×18、cmd ×18、conhost ×27、python ×6、local-search ×3） | 27 个 mcpcell-run，共 23 MB |
| MCP 内存峰值（私有提交） | 4308 MB | 453 MB（含守护进程；共享实例 context7、shadcn-ui、filesystem、memory 各一份，按会话的只为用到它的会话启动，没人用的 3 个为 0） |
| 6 次工具调用 | 只有 local-search 成功；npx 启动的 5 个 MCP 在模型查工具时都没就绪 | 全部成功，答案全对 |

- sequential-thinking 按会话运行时返回的历史长度是 1，和原生一致；共享的话会把别的会话的思考算进去，所以它从共享清单移到了有状态清单
- 首次使用（能力目录还没缓存、npm 包还没装进 MCPcell 的缓存）时，包装方式也有同样的就绪问题，而且按会话的服务会在每个会话连接时各起一个实例来握手。正式接管会在后台预热，所以只影响用户自己手写前缀、从没运行过的服务；可以改成第一个会话预热、其余会话等目录

**内存与速度基准**（2026-10-17，`scripts/mcpbench.py`，不经过模型；9 个 MCP，原生与 MCPcell 交替，结果与图在本机 `bench/`）：
- 并发空闲会话（已连接、已列出工具）：原生每个会话约 +1.7 GB、+37 个进程，4 个会话 6.6 GB / 148 个进程；MCPcell 每个会话约 +9 MB（9 个转发程序），4 个会话 93 MB / 37 个进程（含守护进程）。n = 3
- 全部 MCP 就绪：原生 15.8 s（1 个会话）到 35.5 s（4 个会话）；MCPcell 0.1–0.9 s。单个 npx 类 MCP 原生就绪 13–16 s，经 MCPcell 0.07–0.1 s（能力目录已缓存）。n = 5
- 从启动到第一个工具结果（npx 类，含进程启动）：原生 11–14 s；MCPcell 实例未运行时中位数约 1.2 s（本地工具）到 3.5–4.6 s（联网工具），共享实例已在运行时 0.04 s（本地）
- 每次调用多一跳：本地工具中位数 +0.3–0.5 ms（约 1 ms → 1.4 ms），本身需要十几毫秒或联网的工具看不出差别
- 真实 Codex（3 会话 × 9 MCP，n = 3 轮）：内存峰值 4.35 GB → 0.38 GB（−91%），会话下进程 72 → 27，要求的 6 个 MCP 成功用上 1 → 6

**泛化测试**（2026-10-18，结果在本机 `bench/2026-10-18/`）：
- 官方一致性套件（`@modelcontextprotocol/conformance` 0.1.16 的 32 个服务端场景，`scripts/mcpconformance.py`）：原生 40/40；修复前 MCPcell 按会话 40/40、共享 28/32（sampling、elicitation 的 4 个场景）；修复后两种方式都是 40/40
- 差分测试（`scripts/mcpcompat.py`）：50 个能在本机原生启动的 MCP，同一套客户端动作原生跑一遍、经 mcpcell-run 跑 5 种情况（首次使用、目录已缓存、两个会话并发、换工作目录、客户端请求旧协议版本 2025-03-26），比对握手结果、全部列表、工具调用结果、服务端发给客户端的请求与通知。覆盖 Node（npx 的 `cmd /c`、直接写、`npx.cmd` 全路径、`bun x` 四种写法）34 个、Python（uvx）11 个，Go、.NET、Rust、本机原生程序、mcp-remote 远程桥各 1 个；另有 8 个原生就起不来（假密钥被拒或包本身坏了），不计入
- 修复前：47/50 只差在旧协议版本（缓存的握手结果不看客户端要的版本），3 个被判共享的服务（server-everything、server-filesystem 带参数与不带参数）在共享时和原生不同——服务按客户端能力注册工具，或向客户端要 roots；filesystem 共享时能读到原生会拒绝的目录
- 修复：握手按客户端请求的协议版本进行，共享实例按版本分开；能力目录按客户端画像缓存；两个服务移出共享清单；加了共享资格检测，以及共享实例收到服务端请求时自动改为按会话（架构 §8）
- 修复后：50/50 在全部情况下与原生一致。剩下的差别都是预期的：换目录时用的是会话自己的目录；MCPcell 丢掉服务端写到 stdout 的非 JSON 行；多发一次列表变更通知
- 客户端配置：24 种写法（Codex TOML 16 种、Cursor JSONC 8 种）接管后语义只改 command/args，还原逐字节一致（`internal/clients/edge_test.go`）
- 首次启动超过 2 分钟的（要先下载浏览器或大依赖），守护进程会把这次交回包装程序原生运行，功能不受影响
- 未覆盖：Docker 启动的 MCP（本机 Docker 引擎未运行）、Java 写的 MCP

**命令行或图形界面**
- `config.json` 的 `interface`（`gui` 默认，`cli`）只决定托盘图标。守护进程启动时读取，开机自启、包装程序拉起、`mcpcell start` 都自动遵守；`--no-tray` 仍然优先
- `mcpcell mode [cli|gui]` 和设置页的「界面方式」经 `POST /api/interface` 即时切换：守护进程写回配置，在 UI 线程上添加或移除托盘图标，不重启；命令行方式下 `mcpcell open` 仍能打开窗口；`mcpcell install --cli|--gui` 安装时选择，不写则不变
- `mcpcell takeover`：命令行的一键托管。读 `/api/mcp` 列出计划（每个 Agent 的条目、共享还是按会话、新建还是沿用服务、要改的文件、跳过的条目和原因），确认后调用管理页用的同一个 `/api/clients/{client}/attach`；`-y` 直接执行，可按 `--client` 和条目名筛选；`--shared` 让未验证的也共享，`--per-session`（接口新增 `mode` 字段）让新建的服务一律按会话
- 实测（试验守护进程，没有服务，启动后 5 秒，两轮）：命令行 47.3–47.4 MB 私有提交、15.8 MB 工作集；图形界面 47.8–48.0 MB、18.2–18.3 MB。托盘多 0.4–0.6 MB 私有提交、约 2.5 MB 工作集。运行中切换后用 `Shell_NotifyIconGetRect` 确认图标立即出现、消失
- 试验环境（临时 `MCPCELL_HOME`、`CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`USERPROFILE`）全流程：列出计划 → 拒绝后三个配置文件逐字节不变 → 托管 Claude Code（真实 `claude` CLI）和 Codex 的 4 个条目，启动不了的 1 个保持原样并给出原因 → `--per-session` 把可共享的 shadcn 建成按会话 → `restore --all` 后 Codex 逐字节一致、Claude Code 条目一致

**M3c**：Claude Desktop 的 shim、shared 端点双版本协议、新版协议上游识别

原 M3 交付清单：
- `mcpcell import`：从各客户端读取 MCP 配置，合并为服务定义（命令、参数、环境变量值都相同才合并，否则按凭据域分开）
- `mcpcell attach <client>` / `restore <client>`：先支持 Claude Code、Codex、Cursor（本机实际在用的），优先用客户端自带的 CLI 修改，没有时按 legacy 的局部改写逻辑并备份
- 隔离模式 `per-session`、`per-workspace` 与内置的已知服务清单；未知服务默认 `per-session`
- 独占实例的服务端请求（sampling、elicitation、roots/list）转发给所属会话
- 上游协议识别（从 M2 移来）：stdio 上先用 `server/discover` 探测，支持只说新版协议的上游；新版结果的 `ttlMs`/`cacheScope` 进入缓存策略
- 守护进程自启动（Windows 登录任务）；shim 子命令兼容只支持 stdio 的客户端（Claude Desktop 只能这样接入）
- shared 端点同时提供两版协议：新版客户端连旧版上游时，合成 `server/discover` 应答并补上 `resultType`、`ttlMs`、`cacheScope`；per-session 端点只提供旧版，用会话 ID 分区（架构 §6.3）

验收：
- 本机 CC + Codex 的 MCP 全部接到 MCPcell 后，复现基线场景（6 CC + Codex），测量并公布私有提交与进程数对比
- 同一场景下用 1MCP 作对照组，一起测量
- 只接管 Cursor 时，CC 的配置不变（沿用 0.5 的作用域修复）
- `restore` 后配置内容与备份一致；外部改过的条目报冲突，不强制覆盖
- 工具全名不变，客户端原有的允许/拒绝规则继续生效

## Skill 统一管理 ✅（2026-10-20）

设计见 [skills.md](skills.md)。

交付：
- 核实各 Agent 的用户级 Skill 目录（Claude Code、Codex、Gemini CLI、Antigravity、Cursor、OpenCode、GitHub Copilot），`~/.agents/skills` 的使用方
- `mcpcell skills`：每个 Skill 在哪些位置、按内容哈希是否一致、哪份较新、具体哪些文件不同；只在多处有实体时才读文件
- `mcpcell skills sync`：来源目录 `~/.mcpcell/skills`，各位置换成 junction（失败时复制）；内容不同的副本不覆盖，`--from` 选版本；其他工具的链接默认不动
- `mcpcell skills restore`：移回原目录、重建原链接，内容哈希核对；同步后被改过的拒绝
- 管理页「Skill」页：分组列表、各位置的状态标签、逐处差异，同步与还原先显示计划再执行
- 记录在 `~/.mcpcell/skills.json`，备份在 `skill-backups/`，命令行不需要守护进程

验收（临时目录，`USERPROFILE`、`HOME`、`CODEX_HOME`、`CLAUDE_CONFIG_DIR`、`MCPCELL_HOME` 都指向沙箱）：
- 内容不同的两份不带 `--from` 同步：退出码 1，什么都没改
- `--from` 同步、相同副本同步、外链跳过；通过一个 Agent 的链接编辑，其他 Agent 立即看到
- 一个链接被换成用户自己的目录后还原：这一项被拒绝，其余还原；用户删掉后再还原，37 个条目（哈希、修改时间、链接目标）与同步前完全一致
- 管理页上的同步与全部还原走同一套接口，结果一致
- 本机真实目录只做了只读扫描：60 多个 Skill 冷扫描约 0.4 秒

## 管理页信息结构 ✅（2026-10-21）

原来按技术概念分成 MCP、Skill、Agents、会话四页，互相重叠，要处理的问题和正常条目混在一起。改为三层，每层只回答一个问题：
- 概览（默认首页）：一句话状态（N 个连接共用 M 个进程、内存）；「需要你处理」只在有事时出现，每条一个按钮，点了进到对应详情；各 Agent 一行，带托管与全部还原
- MCP、Skill 列表：按状态分组（次要分组默认折叠、带数量），Agent 做筛选；每行只有名字和一句状态，不放徽章、版本、协议、会话编号，`run-` 变体并到所属服务下
- 详情：在列表右侧并排，面包屑，地址可直达、浏览器后退可用；原 ServiceForm 成为 MCP 详情里的「设置」
- 导航只剩概览、MCP、Skill 和设置；原始连接列表移到设置的「诊断」，去掉侧栏的状态卡
- 接口补两项：`/api/status` 的服务带 `failures`（连续启动失败次数，成功一次清零，用来判断"起不来"）和 `of`（`run-` 变体属于哪个服务）

验收（沙箱守护进程，临时 `MCPCELL_HOME`、`USERPROFILE`、`CODEX_HOME`、`CLAUDE_CONFIG_DIR`，另一个端口）：概览的一键托管、详情里的托管 / 还原 / 忘记记录、设置保存、立即启动与回收、添加与删除服务、Skill 合并 / 选版本 / 还原都在页面上点过一遍并核对配置文件；旧地址（`#/services/…`、`#/agents`、`#/sessions`）跳到新位置。

## M4 Hub 聚合与上下文瘦身

优先级已下调：Claude Code、Cursor、Codex 已经内置工具检索，这一步只面向没有工具检索的客户端。新版客户端连旧版上游、又确实需要 sampling 或 elicitation 时的 MRTR 双向翻译，也放在这一阶段，等遇到实际需要的服务再做。

交付：
- `/hub` 聚合端点：按服务 ID 加命名空间，持久别名映射
- 瘦身模式：只暴露 `search` / `describe` / `call` 等元工具，按需展开具体工具定义
- 守护进程侧的调用策略（允许 / 询问 / 拒绝），因为瘦身后客户端只看到一个 `call` 工具，原有的逐工具审批会失效

验收：
- 同一组服务下，工具定义占用的 token 数对比全量暴露（以客户端实际注入的上下文为准）
- 策略不会因聚合而放大：旧规则无法等价转换时阻断并要求重新批准

## M5 体验与跨平台

- `mcpcell doctor`：端口、自启动、各客户端连通性
- 状态页（复用 legacy 的界面风格，只读为主）
- 其余客户端适配器（VS Code、Gemini CLI、Windsurf、Cline、Roo Code、OpenCode、Kiro）
- Linux（cgroup v2）与 macOS

## 冻结

0.3–0.5 的 Agent 终端工作台（ConPTY、会话选择器、xterm）不迁移到新内核，等核心价值验证后再评估是否需要。
