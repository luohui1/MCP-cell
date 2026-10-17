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
- 空闲回收（默认 10 分钟）；崩溃退避；连续崩溃进入隔离状态，不无限重启
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

**M3a 交付内容**
- `internal/clients`：读取 Claude Code（`~/.claude.json` 用户级）、Codex（`config.toml`）、Cursor（`mcp.json`）的 MCP 条目；按启动包、参数、环境变量值识别"同一个服务"，Claude Code 和 Codex 的 context7 映射到同一个 MCPcell 服务，凭据不同则分开
- 分类：可共享（已知无状态清单）/ 需要会话隔离 / 未知（可强制共享）/ 远程 HTTP / 客户端内置（Codex 的 codex、node_repl、cua_repl）/ 已连接 MCPcell
- 接管：Claude Code 走 `claude mcp remove/add-json -s user`（不和运行中的会话抢写文件，失败时把原条目放回）；Codex 只替换该条目的表块，保留 enabled、超时等客户端策略；Cursor 只替换该条目的 JSON 片段。改前整文件备份到 `~/.mcpcell/backups/`，接管记录在 `takeover.json`
- 还原：按记录逐字节放回；条目在接管后被改过就拒绝覆盖，可以"忘记记录"保留用户修改。`mcpcell restore --all` 不需要守护进程
- 开机自启：HKCU Run 键启动无窗口的 `mcpcelld.exe daemon --log`；`mcpcell start/stop`
- 管理页「客户端接入」改为接管中心；设置页加自启开关
- 测试：接管→还原后文件逐字节一致（含注释、CRLF）；被改过的条目拒绝还原；真实 `claude` CLI 的接管→还原（临时 `CLAUDE_CONFIG_DIR`，`MCPCELL_REAL_CLAUDE=1` 时运行）

**M3b 按会话隔离**：chrome-devtools、ssh、pencil、playwright 等有状态服务每个会话独立实例，服务端请求转发给所属会话；之后它们也能接管

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
