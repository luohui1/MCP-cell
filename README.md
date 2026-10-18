# MCPcell

**本机 MCP 运行时：每个服务只跑必要的那几份进程，所有 Agent 共用。**

A local MCP runtime that lets every AI agent session on a machine share MCP server processes instead of spawning its own set.

`0.6.0-dev` · Go 新内核。0.5 的 Python 原型保存在 [`legacy/python-0.5/`](legacy/python-0.5/)。

## 为什么

Claude Code、Codex、Cursor 等客户端的每个会话都会把配置里的 stdio MCP 全部启动一遍。开发机上同时开着几个 Agent 时，进程数和内存都按"会话数 × 服务数"增长。一台开发机上实测（2026-10-17，8 个会话）：

- 71 个 MCP 相关进程，约 **2.9 GB 私有提交**（任务管理器的工作集只显示约 0.8 GB）
- 在 Windows 上，每个用 npx 启动的服务要 3–4 个进程，npx 自身的 node 就占约 137 MB
- npx 冷启动慢，同一次会话里 5 个服务撞上客户端的 30 秒连接超时

## 现在能做什么（M2）

- 一个守护进程通过 loopback 上的 Streamable HTTP 暴露每个服务：`http://127.0.0.1:47621/s/<服务名>`
- 所有连到同一服务的会话**共用一个上游进程**。实测 6 个 SDK 客户端加 2 个真实 Claude Code 会话共用一个 context7。协议层只改写 id、进度令牌和取消通知的归属，不改工具名，不重放调用
- **连接不启动进程**：握手和工具列表由缓存的能力目录回答，第一次调用工具时才启动，空闲后回收（默认 3 分钟）
- **npx 扁平化**：npm 包装进 MCPcell 的缓存并锁定版本，直接用 node 启动。context7 从 4 个进程、约 235 MB 降到 2 个进程、约 84 MB，冷启动开销从 10 秒以上降到约 2.5 秒
- 上游放在 Windows Job Object 里：守护进程无论怎么退出，整棵进程树都会被清理；可设内存上限，超限立即终止
- 状态页和 `mcpcell ps` 显示每个服务的实例、会话、私有提交和缓存的工具数

尚未实现：自动接管客户端配置、按会话或工作区隔离的服务、只说新版协议的上游。见 [里程碑](docs/roadmap.md)。

## 试用

```sh
go build -o bin/mcpcell.exe ./cmd/mcpcell
bin/mcpcell.exe daemon        # 首次运行生成 ~/.mcpcell/config.json 和 token
```

在 `~/.mcpcell/config.json` 的 `services` 里添加服务，写法与客户端的 `mcpServers` 条目相同，然后重启守护进程：

```json
{
  "services": {
    "context7": { "command": "cmd", "args": ["/c", "npx", "@upstash/context7-mcp"] }
  }
}
```

运行 `bin/mcpcell.exe ui` 打开管理页。「MCP」页把 MCPcell 的服务和 Claude Code、Codex、Cursor 里配置的 MCP 合成一张表，每个 Agent 一格，点一下就能托管、还原或加入其他 Agent；也可以在页面上添加、编辑、删除服务（带常用预设，也能粘贴客户端里的 JSON 条目）。「Agents」页按 Agent 批量托管和还原，其他客户端给出可复制的配置，替换原来的同名条目后重启即可。工具全名保持不变。

| 命令 | 作用 |
|---|---|
| `mcpcell start` / `stop` | 在后台启动（无窗口）/ 停止守护进程 |
| `mcpcell daemon [--port N]` | 前台运行守护进程，Ctrl+C 退出 |
| `mcpcell clients` | 各客户端的 MCP 条目、分类与接管状态 |
| `mcpcell restore --all` | 还原所有被接管的客户端配置，不需要守护进程 |
| `mcpcell autostart on` | 开机自启 |
| `mcpcell ps` | 服务、实例、会话、私有提交 |
| `mcpcell ui` | 用一次性链接打开管理页 |
| `mcpcell update <服务>` | 把 npx 服务的 npm 包更新到最新版并重新锁定 |

可选配置：

```json
{
  "limits": { "maxInstances": 32 },
  "services": {
    "context7": {
      "command": "npx", "args": ["-y", "@upstash/context7-mcp"],
      "idleTimeout": "5m",
      "memoryLimit": "1GB"
    }
  }
}
```

- `idleTimeout`：空闲多久回收，`"0"` 表示不回收
- `memoryLimit`：整棵进程树的内存上限（目前仅 Windows）
- `flatten`：默认开启；设为 `false` 时保留 npx 启动
- `catalog`：设为 `"off"` 时不缓存工具列表，适合工具列表会在运行时变化、却不发通知的服务

## 开发

```sh
go vet ./... && go test ./...
cd web && npm install && npm run build   # 修改管理页后重新构建（产物已提交）
go run ./tools/mcpprobe -service context7 -n 6 -tool resolve-library-id -args '{"libraryName":"react"}'
pwsh scripts/measure-mcp.ps1          # 测量本机所有 Agent 的 MCP 进程与内存
```

设计见 [架构](docs/architecture.md)。

## 原则

- 不同凭据、不同工作区的服务绝不共用实例；同名或配置相同只是合并候选，不代表可以共享
- 路由只改写 id 这类归属字段，不重放调用；上游中途退出时报告"结果未知"
- 只监听 loopback，校验 Host 和 Origin，每个请求都要 token
- 改写客户端配置前先备份，只改所选的客户端和条目
