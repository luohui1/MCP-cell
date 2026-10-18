# MCPcell 架构 · 0.6 新内核

日期：2026-10-17。状态：M0–M2 已完成（实测结果见 roadmap），下一步 M3。里程碑与验收见 [roadmap.md](roadmap.md)。

一句话：**一台机器上，每个 MCP 服务只跑必要的那几份进程，所有 Agent 共用；不调用就不占内存；对客户端透明，随时可还原。**

## 1. 问题（本机实测）

| 现象 | 数据（2026-10-17，8 个 Agent 会话） |
|---|---|
| 进程数 = 会话数 × 服务数 | 71 个 MCP 相关进程，含 18 个 `cmd.exe` 中间层 |
| 内存被低估 | 私有提交约 2.9 GB，工作集只显示约 0.8 GB；系统提交 60.8 / 68.7 GB |
| Windows npx 链 | 每个 npx 服务 = `cmd.exe` → npx 的 node → 服务的 node，3 个进程 |
| 冷启动慢、易超时 | 同一次会话里 5 个 npx 服务撞上 Claude Code 的 30 秒连接超时；`@latest` 每次启动都查 npm 仓库 |
| 闲置常驻 | 会话开几个小时，MCP 一次没调用也一直占着 |
| 上下文膨胀 | 每个会话都把全部工具定义注入上下文 |
| 配置分散 | Claude Code 9 个、Codex 十余个、Cursor 1 个，各自维护 |

Windows 会压缩闲置进程的工作集，但它们的提交内存仍然计入系统提交上限。所以本项目所有内存数字一律报**私有提交**。

## 2. 目标与非目标

目标：
- **G1 共享**：同一凭据域内的无状态服务，全机只跑一份进程
- **G2 按需**：会话连上时上游进程数为 0，调用时才启动，闲置后回收
- **G3 透明**：默认保持工具全名和客户端审批规则不变，一条命令还原
- **G4 可度量、可约束**：按实例统计私有提交，可设 Job 内存上限
- **G5 上下文瘦身**（可选）：聚合端点只暴露检索和调用用的元工具

非目标：
- 不做 IDE 或 Agent 终端；0.3–0.5 的终端工作台冻结
- 不是安全沙箱：只做进程归属和资源约束，不限制服务的文件或网络访问（§11）
- 不管远程 MCP 服务内部的资源
- 不做多用户或云端部署

## 3. 总体结构

```
 Claude Code ×N ─┐
 Codex ──────────┤  Streamable HTTP（loopback + 每绑定 token）
 Cursor ─────────┼──────────────► mcpcell daemon（每用户一个）
 Gemini/Grok … ──┤                  ├─ 入口：/s/<service>（逐服务）  /hub（聚合，M4）
 仅 stdio 的客户端 ─ mcpcell shim ───┘  ├─ 会话表 ─ 路由（id / progressToken / 取消 / 通知归属）
                                     ├─ 目录缓存（initialize、*/list）
                                     └─ 实例池 ─┬─ shared：context7 ×1、shadcn ×1 …
                                                ├─ per-workspace：按工作区分区
                                                ├─ per-session：chrome-devtools、ssh …
                                                └─ 每个实例一个 Job Object
```

一个二进制 `mcpcell.exe`，三种角色：
- `mcpcell daemon`：常驻，空闲时预计十几 MB（M1 实测）
- `mcpcell shim <service>`：给只支持 stdio 的客户端用，在 stdio 和守护进程之间转发，也能在守护进程不在时把它拉起来
- CLI：`ps`、`import`、`attach`、`restore`、`doctor`

## 4. 组件

| 包 | 职责 | 里程碑 |
|---|---|---|
| `internal/jsonrpc` ✅ | 原始报文封装：只解析 id/method，params/result 原样保留；id 保持原始拼写；归属字段（progressToken、requestId）的改写工具 | M0/M1 |
| `internal/registry` ✅ | `~/.mcpcell/config.json`、token、`daemon.json` | M1 |
| `internal/proc` ✅ | 进程创建：Windows 挂起创建 → 加入 Job → 恢复；POSIX 进程组；`cmd /c` 拆包、npm shim 解析；环境变量白名单；私有提交度量 | M1（npx/uvx 扁平化 M2） |
| `internal/instance` ✅ | 一个上游 stdio 进程：旧版握手、id/progressToken/取消的映射、通知按归属分发、订阅引用计数、stderr 环形缓冲。原计划的 upstream 和 router 合并在这里 | M1 |
| `internal/pool` ✅ | 实例生命周期：单次创建合并、空闲回收、启动失败退避、连续崩溃暂停、实例上限与 LRU 驱逐、npx 扁平化、目录刷新 | M1/M2 |
| `internal/npmcache` ✅ | `npx pkg` 识别、安装到 `~/.mcpcell/pkgs` 并锁定版本、解析 bin 入口、`update` | M2 |
| `internal/httpd` ✅ | Streamable HTTP（旧版，POST/GET/DELETE、会话、SSE）、Origin/Host 校验、token 鉴权、管理 API、内嵌状态页 | M1 |
| `internal/testutil/fakemcp` ✅ | 基于官方 SDK 的测试用上游 | M1 |
| `internal/catalog` ✅ | 能力目录：拉取并合并分页、按配置指纹判断有效、按客户端画像（协议版本，按会话时加能力）分别缓存、`list_changed` 时先标记过期再刷新、记录共享资格检测结果，存到 `~/.mcpcell/catalog` | M2 |
| `internal/clients` | 各客户端配置的发现、改写、备份、还原（移植自 legacy） | M3 |
| `internal/hub` | 聚合命名空间、元工具、调用策略 | M4 |

## 5. 数据模型

沿用 legacy 统一方案里"定义与绑定分离"的思路，做了简化：

- **ServiceDefinition**：稳定 ID、启动方式（解析后的 argv 或远程 URL）、环境变量、cwd、锁定的包版本、隔离模式、空闲超时。环境变量值属于凭据，存储时用 DPAPI 保护。
- **Binding**：哪个客户端、哪个配置文件里的哪一条、原始内容的备份、该绑定专用的 token。一个定义可以有多个绑定。
- **InstanceKey**：`服务ID + 配置指纹 + 分区`。配置指纹是 argv、环境变量值和 cwd 的哈希，所以凭据不同就自然分开。分区：shared 为空，per-workspace 为工作区 ID，per-session 为会话 ID。
- **Instance**：一个上游进程，带状态、代次、在途请求数、已附着的会话。
- **Session**：一个下游 MCP 会话（Streamable HTTP 的 `Mcp-Session-Id`），记录协商的协议版本、客户端能力、订阅、日志级别和所属绑定。

合并规则：两个客户端的配置只有在命令、参数、环境变量值都相同时才合并为一个定义。同名不代表可以合并，不同凭据绝不共用实例。

## 6. 请求处理

6.1、6.2 描述旧版协议（2025-11-25 及以前，带 `initialize` 握手和会话）下的路由。现有的本地 stdio 服务几乎都是这一版。新版见 6.3。

### 6.1 下游到上游

| 方法 | 处理方式 |
|---|---|
| `initialize` | 守护进程用目录缓存里的 capabilities、serverInfo、instructions 应答。没有缓存时冷启动一次探测并写入缓存 |
| `notifications/initialized` | shared：吞掉（守护进程已经和上游握过手）；per-session：转发 |
| `ping` | 守护进程直接应答 |
| `tools/list` `prompts/list` `resources/list` `resources/templates/list` | 目录缓存应答（M2）；动态目录的服务标记为 `catalog = live`，每次转发 |
| `tools/call` `prompts/get` `resources/read` `completion/complete` | 获取实例 → 改写 id → 转发 |
| `resources/subscribe` / `unsubscribe` | 转发并按 URI 记录订阅，引用计数为 0 时才向上游退订 |
| `logging/setLevel` | per-session：转发；shared：记录在会话上，由守护进程过滤日志通知 |
| `notifications/cancelled` | 把 requestId 映射为上游 id 再转发，只取消本会话的那个请求 |
| 未知方法 | 走需要实例的路径转发（保守处理） |

上游 id 由守护进程按实例自增生成，映射表保存 `上游id → (会话, 原始id)`。原始 id 的类型和拼写原样还原，因为客户端用 `1` 和 `"1"` 时是两个不同的请求。`_meta.progressToken` 同样替换为实例内唯一的值。

### 6.2 上游到下游

| 消息 | 去向 |
|---|---|
| 响应 | 按映射表还原 id，发回原会话 |
| `notifications/progress` | 还原 progressToken，发回原会话 |
| `notifications/message` | 发给该实例的会话，按各会话的日志级别过滤 |
| `notifications/*/list_changed` | 刷新目录缓存，广播给该服务的所有会话 |
| `notifications/resources/updated` | 只发给订阅了该 URI 的会话 |
| 服务端请求（`sampling/createMessage`、`elicitation/create`、`roots/list`） | per-session：转发给所属会话。shared：旧版 stdio 线路上没有任何字段把服务端请求关联到引发它的客户端请求（TS SDK 的 `relatedRequestId` 只是内部选项），只有恰好一个会话有在途请求时才能确定归属并转发；否则返回 `CodeNotShareable`，并在 `ps` 中提示改为 per-session |

### 6.3 两版协议并存

MCP 当前版本是 **2026-07-28**（"modern"）。它去掉了 `initialize`/`initialized`、`Mcp-Session-Id`、GET 流、DELETE、`ping`、`logging/setLevel`；每个请求在 `_meta` 里自带协议版本、客户端能力和客户端信息；新增必需的 `server/discover`，每个结果必须带 `resultType`；服务端不再主动发请求，sampling、elicitation 改为在结果里返回 `InputRequiredResult`，由客户端带着答复重试（MRTR）。2025-11-25 是最后一个带握手的版本（"legacy"）。Claude Code 的 v2 运行时已经在协商新版，而本机的 stdio 服务基本都是旧版。

**下游识别**：同一个端点可以同时服务两版。第一条消息是 `initialize` 就是旧版；带 `_meta` 协议字段就是新版。

**上游识别**：在 stdio 上先发 `server/discover` 探测。只要返回的不是可识别的新版错误，或者超时，就按旧版处理；不要把回退条件绑定到某一个错误码。结果按进程缓存。

**上游是新版**时，共享几乎没有额外成本：只需映射 JSON-RPC id（以及 stdio 上 `notifications/cancelled` 里的 id）。每个客户端的能力随它自己的请求传递；规范要求 `tools/list` 不能因连接而不同，所以一份缓存可以服务所有客户端。

**上游是旧版**时，一个进程只握手一次，守护进程必须替所有下游选定一组客户端能力，并为每个下游合成初始化结果。roots、日志级别、资源订阅都是进程级状态；服务只要依赖其中任何一项，就只能分区。

**端点的协议版本跟随隔离模式**，这是本设计的关键简化：
- per-session 端点只提供旧版。新版客户端用新版请求探测时，守护进程返回不带新版错误体的 400，客户端按规范回退到旧版。这样每个会话有 `Mcp-Session-Id`，可以作为分区键
- shared 端点同时提供两版。新版客户端连旧版上游时，守护进程用缓存的初始化结果合成 `server/discover` 的应答，并补上 `resultType`、`ttlMs`、`cacheScope`。shared 模式本来就拒绝服务端请求，所以不需要 MRTR 翻译
- 新版客户端连旧版上游、又确实需要 sampling 或 elicitation 时，才做双向翻译：把上游的服务端请求转成 `InputRequiredResult`，挂起上游请求，等客户端重试时再答复。这一项放到最后，只有遇到实际需要的服务才做

**缓存**：新版的 `server/discover`、各个 `*/list`、`resources/read` 结果必须带 `ttlMs` 和 `cacheScope`。`public` 可以跨用户复用，`private` 只能在同一授权上下文内复用。带 `inputResponses` 或 `requestState` 的重试结果不缓存。旧版没有 TTL，靠配置指纹和 `list_changed` 失效。

**Streamable HTTP 要点**：两版都必须校验 `Origin`（不合法返回 403）。旧版：POST 和 GET；会话结束后返回 404，客户端应重新初始化；缺少 `MCP-Protocol-Version` 头时按 2025-03-26 处理；同一条消息不能在多个流上重复发送。新版：只有 POST，GET 和 DELETE 返回 405；`MCP-Protocol-Version`、`Mcp-Method`、`Mcp-Name` 头必须与报文一致，否则返回 400 和 `HeaderMismatch`（-32020）；中间层必须转发 `Mcp-Param-*` 头；关闭某个请求的 SSE 流等同于取消该请求。

### 6.4 失败处理

- 上游退出：该实例所有在途请求返回 `CodeResultUnknown`，不重放；实例代次加一，下一个请求重新拉起
- 60 秒内崩溃 3 次：实例进入隔离状态，请求直接报错，`ps` 显示最近的 stderr
- 下游断开：只释放这个会话的租约和订阅；它在途的请求向上游发取消通知

## 7. 实例生命周期与资源

```
cold ──acquire──► starting ──握手+能力校验──► ready ⇄ busy
  ▲                  │失败                     │在途=0
  │                  ▼                         ▼
  └──────回收────── stopping ◄──空闲超时──── idle
                     │连续崩溃
                     ▼
                 quarantined
```

- **单次创建合并**：同一个 InstanceKey 的并发 acquire 只触发一次启动，其他请求等它就绪
- **空闲回收**：shared 默认 3 分钟（工具列表有缓存，回收后再调用只多一次冷启动）；per-session 默认在会话结束或空闲 30 分钟后回收，因为回收会丢掉浏览器页面、SSH 连接这类状态
- **预算**（M2 已实现）：全局实例上限 `limits.maxInstances`（默认 32），满额时先停掉最久未用的空闲实例，全部忙碌就拒绝。每个服务可设 `memoryLimit`，对应整棵进程树的 Job 内存上限（`JOB_OBJECT_LIMIT_JOB_MEMORY`）。实测发现，进程到达上限后只是分配失败，会卡在那里既不退出也不响应，所以 Job 绑定了 I/O 完成端口，收到 `JOB_OBJECT_MSG_JOB_MEMORY_LIMIT` 后立即终止整棵树，并报告"超出内存上限"。总私有提交预算尚未实现
- **进程归属**：Windows 挂起创建 → 加入 Job（KILL_ON_JOB_CLOSE、禁止脱离）→ 恢复线程，沿用 legacy `processes.py` 的做法。守护进程无论怎么退出，Job 句柄关闭后整棵进程树都会被系统清理
- **npx 扁平化**（M2 已实现）：第一次启动时把包 `npm install` 到 `~/.mcpcell/pkgs/<name>@<version>/`，从安装结果读出实际版本并锁定，按 `package.json` 的 `bin` 找入口，之后直接 `node <bin> <args>` 启动。安装失败（如断网）时退回原 npx 命令，并在状态里说明原因。`npm_config_*` 环境变量会放行，以支持镜像源。`mcpcell update <服务>` 才升级。服务可设 `"flatten": false` 保留 npx。uvx 尚未处理
- **度量**：枚举 Job 内的进程，逐个取 `PrivateUsage` 求和；同时报峰值

## 8. 隔离模式

| 模式 | 一个实例服务于 | 适用 |
|---|---|---|
| `shared` | 同一配置指纹的所有会话 | 无状态的 API 或文档查询包装 |
| `per-workspace` | 同一工作区的会话 | 读写项目文件、依赖 cwd 的服务 |
| `per-session` | 单个会话 | 有会话状态、会回调客户端，或状态不明的服务 |

未知服务默认 `per-session`：仍然能获得按需启动和空闲回收，只是不共享。共享需要内置已知服务清单（随版本发布，带验证过的版本范围）或用户显式标记。工具上的"只读"注解只是提示，不能作为可以共享的证明。

**服务端看到的客户端**（2026-10-18 兼容性测试后改）：同一个服务对不同客户端的回答可能不同——协议版本按客户端请求的协商，有的服务按客户端声明的能力注册工具（server-everything 没有 roots 能力就不注册 `get-roots-list`）。所以：

- 共享实例按客户端请求的协议版本分开，每个实例用该版本、不带能力握手；能力目录按"协议版本"缓存
- 按会话的实例用客户端自己的 initialize 参数握手；能力目录按"协议版本 + 能力"缓存，`Warm` 按最近见过的几种客户端分别预热
- 共享资格检测：某种能力组合的客户端第一次连上共享服务时，后台用它的参数另起一个实例，服务端若向客户端发请求（roots、sampling、elicitation），或握手结果、列表和共享目录不同，就把服务改为按会话运行。共享实例在调用中收到服务端请求时同样处理；此时如果只有一个会话有进行中的调用，请求转给它，否则拒绝。结果和能力目录存在一起，配置或包版本变了才重新检测

工作区怎么识别：HTTP 客户端不会告诉守护进程自己的 cwd。优先向下游会话请求 `roots/list`；客户端不支持 roots 时，项目级配置里的绑定 URL 带上工作区 ID。两种都拿不到就退回 per-session。

常见服务的初步分类（M3 前逐个验证）：

| 服务 | 初步建议 | 理由 |
|---|---|---|
| context7 | shared | 只读文档查询 |
| shadcn-ui | shared | 只读组件文档 |
| lark-mcp（飞书） | shared（同凭据） | API 包装，凭据在环境变量里，凭据不同自然分区 |
| 异步任务类（spawn/status/wait） | 待验证 | 任务状态在进程里，要确认任务 ID 跨会话可见是否可接受 |
| 连接桌面应用的 MCP | 待验证 | 连接单例桌面应用，实例本身可能可以共享 |
| chrome-devtools | per-session | 每个实例会启动自己的 Chrome，页面状态属于会话 |
| ssh | per-session | 持有连接状态 |
| 远程 HTTP 服务 | 不接管 | 已经是 HTTP 服务，没有本地每会话进程 |

## 9. 接入方式与客户端

### 9.1 三种入口

1. **逐服务端点（默认）**：`http://127.0.0.1:<port>/s/<service>`。客户端里的条目名不变，例如 `context7` 从 `cmd /c npx @upstash/context7-mcp` 改为指向这个 URL，Claude Code 里的工具全名仍然是 `mcp__context7__*`，已有的允许规则继续生效
2. **shim**：`mcpcell shim context7`，给只支持 stdio 的客户端用。Go 二进制，比 legacy 的 Python 桥小一个数量级，以实测为准
3. **hub 聚合（M4）**：`/hub` 一个端点暴露所有服务，见 §10

### 9.2 接管与还原

`mcpcell attach <client>` 优先调用客户端自带的 CLI 修改配置（例如 `claude mcp add --transport http …`），因为 `~/.claude.json` 会被 Claude Code 自己频繁改写，外部直接写文件容易冲突。没有 CLI 时用 legacy 的局部改写逻辑：先备份、只改目标条目、写后校验、可逐条还原。改完需要重启对应客户端。

### 9.3 守护进程自启动

HTTP 客户端无法拉起守护进程，所以自启动是必需项：Windows 用当前用户的登录计划任务，失败时自动重启。守护进程不在时客户端会显示连接失败，`mcpcell restore --all` 一条命令回退到原始配置。

### 9.4 客户端支持矩阵

2026-10-17 调研（官方文档、changelog 与 issue）。标"未核实"的项在 M3 真机测试时确认。

| 客户端 | 本地 HTTP | 写法要点 | 请求头 / 变量 | 需要注意 |
|---|---|---|---|---|
| Claude Code | ✅ Streamable HTTP | `~/.claude.json` / `.mcp.json`：`type:"http"`、`url`、`headers` | `${VAR}`、`${VAR:-默认}` | 不写 `type` 会被当成 stdio 跳过；HTTP 调用 5 分钟无响应也无进度会被中止；v2 运行时协商 2026-07-28 新版协议；每个会话各起一套 stdio（#15973）；默认开启工具检索 |
| Claude Desktop | ❌ 只有 stdio | `claude_desktop_config.json`：`command` | — | 写 `url` 会清空整个 `mcpServers`（#37286）；必须用 shim |
| Codex（CLI / IDE / 桌面版共用配置） | ✅ Streamable HTTP，不支持 SSE | `config.toml`：`[mcp_servers.x] url` | `bearer_token_env_var`、`http_headers`、`env_http_headers` | 桌面版每个线程起一整套服务且不停止（#30408，未修复）；默认启动超时 10 秒；v0.142.2 起默认开启 MCP 工具检索 |
| Cursor | ✅ | `mcp.json`：`url`、`headers` | `${env:NAME}` | 2.2 起会周期性 POST 探测工具；localhost 上的 OAuth 不稳定，用静态请求头 |
| VS Code Copilot | ✅ `http`（会回退 SSE） | `mcp.json`：`servers.x.type/url/headers` | `${input:id}`；`${env:}` 未核实 | 每次请求最多 128 个工具 |
| Antigravity CLI（取代 Gemini CLI） | ✅ | `~/.gemini/config/mcp_config.json`：`serverUrl`、`headers` | 未核实 | 不支持 `url`/`httpUrl` 写法；旧 Gemini CLI 用 `httpUrl`，收到 401 会尝试 OAuth |
| Devin Desktop（原 Windsurf） | ✅ | `mcp_config.json`：`serverUrl`、`headers` | `${env:VAR}`；未设置时为空串 | 配置路径在文档与 FAQ 之间有冲突；最多 100 个工具 |
| Cline | ✅ | `type:"streamableHttp"`、`url`、`headers` | 未写明 | 不写 `type` 默认当旧版 SSE |
| OpenCode | ✅ | `mcp.x.type:"remote"`、`url`、`headers`、`oauth:false` | `{env:VAR}` | 收到 401 会自动 OAuth，要设 `oauth:false`；会探测 SSE，服务端最好提供 GET 流 |
| Kiro | ✅ | `mcp.json`：`url`、`headers` | `${VAR}`，每个变量需用户批准 | 非 localhost 要求 HTTPS；`127.0.0.1` 是否算 localhost 未核实 |
| Grok CLI（xAI 官方） | ✅ | `~/.grok/config.toml`：`[mcp_servers.x] url`、`headers` | `${VAR}`；`{{session_id}}` 占位符 | **会自动读取 `~/.claude.json` 和 `.cursor/mcp.json`**，接管 CC 后 Grok 也随之走网关 |
| Roo Code | — | — | — | 已于 2026-05-15 停止维护，不再适配 |

对设计的影响：
- **入口必须同时支持新旧两版协议**：旧版（2025-11-25 及以前）用 `Mcp-Session-Id`，会话过期时客户端应重新初始化；新版 2026-07-28 去掉了会话、握手和 GET 流。各客户端处理 404 后重建会话的逻辑反复出过问题，所以守护进程重启后应尽量保留旧版会话
- **每个客户端都配静态 Bearer token**，这样 401 时不会触发 OAuth 流程
- **长调用要发进度通知或保活**，否则 Claude Code 5 分钟后中止
- **提供 GET SSE 流**，兼容会探测 SSE 的客户端
- Claude Desktop 用 Go shim 接入，比社区常用的 `mcp-remote`（一个 Node 进程）更轻
- CC 和 Codex 已内置工具检索，M4 的上下文瘦身主要面向其他客户端，优先级下调

## 10. 上下文瘦身（hub，M4）

聚合端点在瘦身模式下只暴露几个元工具：
- `search(query)`：在所有服务的工具名和描述上做本地关键词检索，返回名称和一行说明
- `describe(tools)`：返回完整的输入模式
- `call(tool, arguments)`：路由到对应服务

也可以混合：把最常用的几个工具完整暴露，其余走检索。

代价：客户端只看到一个 `call` 工具，原有的逐工具审批规则会失效。所以瘦身模式必须是可选的，并由守护进程执行自己的调用策略（允许 / 询问 / 拒绝）。客户端支持 elicitation 时用它询问，否则拒绝未授权的工具。

**优先级已下调**：Claude Code 默认开启工具检索（`ENABLE_TOOL_SEARCH`，可按服务设 `alwaysLoad`）；Cursor 从 2026-01 起把工具描述写成文件让 Agent 检索，官方称 token 减少 46.9%；Codex 源码里有 BM25 的 `tool_search`，最新版本已默认开启（来自客户端调研，具体版本未完全核实）。1MCP、ToolHive、lazy-mcp 等也都提供了元工具。对已有工具检索的客户端，更有价值的是**即时返回准确的缓存 `tools/list`**，不需要先拉起进程；瘦身模式只面向没有工具检索的客户端。

## 11. 安全边界

- 只监听 127.0.0.1。校验 `Origin`，防止网页经 DNS 重绑定访问本地端口
- 每个绑定一个独立 token，守护进程据此识别是哪个客户端；token 文件只有当前用户可读
- 管理 API 和数据面分开鉴权，MCP 通道不能修改服务定义或启动参数
- 上游进程的环境变量只来自服务定义，加上基础白名单（沿用 legacy 的 `BASE_ENV`），不继承守护进程的全部环境
- 日志不记录 params、result 或环境变量值；stderr 只保存在内存环形缓冲里
- 守护进程以当前用户权限运行，不防御已经控制了该用户账号的程序

## 12. 可观测性

`mcpcell ps` 显示每个服务的：隔离模式、实例数和状态、附着的会话、在途请求数、私有提交（当前和峰值）、运行时长和空闲时长、最近错误。M5 的状态页展示同样的数据。基准对比统一用 `scripts/measure-mcp.ps1`，四种场景：原始客户端启动、legacy 独占桥、按需启动、按需加共享。

## 13. 技术选型

**Go**，单个静态二进制：
- 用户机器上不需要装 Python 或 pip
- 常驻和 shim 的内存远小于 Python 解释器或 Node（M1 实测确认）
- goroutine 适合多路复用；`golang.org/x/sys/windows` 覆盖 Job Object 和命名管道
- 依赖只有标准库加 `x/sys`

数据面不用 MCP SDK 的类型化 Client/Server：代理必须对协议透明，未知字段原样转发，所以只解析路由需要的字段。

**M1 技术验证结论（2026-10-17）：传输层自己实现，官方 Go SDK（v1.8.0）只用在测试里。** 原因有两个：
- SDK 的 `jsonrpc.ID` 会把数字 id 转成 int64，保不住原始写法；
- 服务端传输判断"一条通知属于哪个请求流"，依赖包内私有的 context key。代理从外部无法设置它，上游的进度通知就送不回对应请求的 SSE 流，Claude Code 可能因此在 5 分钟无进度后中止调用。

测试里，SDK 同时充当两个角色：
- 参照客户端：`StreamableClientTransport` 会先发 `server/discover`，失败后回退，正好模拟 Claude Code v2；
- 参照上游服务端：`internal/testutil/fakemcp`。

Rust 的 rmcp（v3.5.1）基于类型化消息枚举，同样不适合原样透传。

**Go 在 Windows 上的固定开销**：只要链接了 `crypto/rand`，Go 1.26 就会带上 `crypto/internal/fips140/drbg.memory` 这个 32 MB 的全局数组。凡是用到 `net/http` 的程序都躲不开。Go 源码注释认为它放在 BSS 段、首次使用时才分配物理页；但在 Windows 上，Go 把它并进了可写的 `.data` 段，系统加载映像时就把整段计入提交量。实测结果：

| 程序 | 私有提交 |
|---|---|
| 空 Go 程序 | 11.9 MB |
| 只导入 `crypto/rand` 的 Go 程序 | 44.3 MB |
| 同样功能的 Rust 程序 | 0.8 MB |

影响：
- 守护进程只有一个，接受这 32 MB。实测空闲约 45 MB 私有提交，工作集约 17 MB。
- 每个会话一个的 shim（M3）**不能链接 `crypto/rand` 或 `net/http`**：要么用原始 TCP 加简单分帧（约 12 MB），要么用 Rust 写（约 1 MB）。
- 这个问题值得向 Go 上游报告。

## 14. 与 0.5 原型的关系

| 保留并移植 | 冻结 | 替换 |
|---|---|---|
| 11 类客户端的配置发现；局部改写、备份与还原；来源作用域修复；Job Object 的创建方式；环境变量白名单；安全原则 | Agent 终端工作台、ConPTY、xterm 界面 | Python 桥和每连接独占的运行器 |

## 15. 竞品与差异化

2026-10-17 调研（项目 README、文档与部分源码）。

| 项目 | 语言 | 一个上游供所有客户端共享？ | 按需启动 / 闲置回收 | 元工具 | 备注 |
|---|---|---|---|---|---|
| **1MCP**（1mcp-app/agent） | TS | 静态服务：是。模板服务：按渲染后配置的哈希，可共享或每客户端一份 | 静态服务常驻；模板服务按需启动，池化空闲超时默认 5 分钟 | 有（`--enable-lazy-loading`） | **最接近的竞品**。v0.39.3（2026-10-15），约 520 星；有 Windows 二进制 |
| MCPJungle | Go | 默认每次工具调用新建 stdio 进程；stateful 模式全局共享一个连接，不分区 | stateful 模式首次调用时启动，空闲超时默认关闭 | 只有工具分组 | 约 1.3k 星 |
| sparfenyuk/mcp-proxy | Python | 是，每个服务启动时起一份，所有 HTTP 客户端共用 | 启动时全部拉起，不回收 | 无 | 只转发进度通知；约 2.8k 星 |
| mcp-hub（ravitemer） | JS | 是 | 通过 API 启停；无客户端时整个 hub 退出 | 无 | 不支持 sampling；2025-08 后无发布 |
| Docker MCP Gateway | Go | 每个网关实例一份；默认每次调用一个容器 | 容器级按需 | 可查找并添加整个服务，不是单个工具 | 需要 Docker Desktop |
| ToolHive（stacklok） | Go | 每个服务一个容器，HTTP 客户端共用 | 常驻，未见空闲回收 | 有（vMCP 优化器） | 需要 Docker 或 Podman |
| MetaMCP | TS | 每客户端会话一份，带预热池 | 预热，不是按需 | "即将推出" | Docker 部署，更新放缓 |
| supergateway | TS | 否，每个连接或会话一个进程 | 部分 | 无 | stdio 与 HTTP 互转工具 |
| Microsoft MCP Gateway | C# | 无状态路由，进程生命周期交给 Kubernetes | — | 工具路由 | 只支持 2026-07-28 |
| IBM ContextForge、agentgateway、Lunar MCPX | Python / Rust / TS | 未核实 | 未核实 | 未核实 | 面向企业或数据面，不是本机轻量运行时 |

**结论**：只做"共享一个上游"不算新东西，sparfenyuk、mcp-hub、1MCP 的静态服务都做到了。但它们要么启动时就全部拉起，要么把共享当成全局开关，并且基本丢弃或忽略了共享上游发出的服务端请求。元工具也已经有不少实现，而且客户端自己在做。

**MCPcell 的差异化**，按重要程度：
1. **真正的按需进程**：持久化的能力目录直接应答握手和列表，调用时才拉起进程，闲置后回收。1MCP 的"懒加载"只是隐藏工具定义，并不推迟进程启动
2. **安全分区**：按凭据域、工作区、会话决定是否共享，正确处理服务端请求的归属；端点的协议版本跟随隔离模式（§6.3）
3. **两版协议都正确处理**：包括新版客户端连接旧版服务。Microsoft 的网关只支持新版，多数本地工具只支持旧版
4. **Windows 优先的进程管理**：Job Object 的退出即清理、npx 链扁平化、私有提交度量。调研中没有其他项目在做；规范的 stdio 章节本身也建议 Windows 上使用 Job Object
5. **对客户端透明**：逐服务端点保持工具全名和审批规则不变，并且能接管、还原 11 类客户端的配置。1MCP 等聚合类工具会把所有服务合并到一个端点，工具名会变
6. **轻量**：Go 单二进制，不需要 Node、Python 或 Docker

M3 的基准测试会把 1MCP 作为对照组一起测。

## 16. 风险与未决问题

1. **守护进程是单点**：它挂了，所有接管的 MCP 都不可用。对策：登录自启动并自动重启，`restore --all` 一键回退，M1 起做崩溃与重启测试
2. **两版协议并存**：主力客户端已经用新版，本地服务大多还是旧版。M1 先只做旧版到旧版，依赖新版客户端按规范回退；必须在真实的 Claude Code 上验证回退确实发生
3. **shared 实例里服务端请求的归属**：旧版 stdio 上游发出的 sampling、elicitation 请求不带关联的下游请求 id，只能靠"恰好一个在途请求"推断，推断不了就拒绝。新版上游用 MRTR，没有这个问题
4. **与 1MCP 的重叠**：如果 1MCP 补上了真正的按需进程和 Windows 进程管理，差异会缩小。M3 的对照基准用来确认差异是否真实存在
5. **客户端的 HTTP 实现差异**：会话 ID、SSE 重连、超时等行为各家不同，每个客户端都要做真机测试
6. **目录缓存陈旧**：服务升级后工具变了而缓存没失效。对策：指纹包含锁定的包版本，实例每次启动后都重新拉取列表并比对
7. **凭据存储**：服务定义里的环境变量含 token，存储用 DPAPI；导入时不展开客户端配置里的 `${input:…}`、`${file:…}` 等引用
