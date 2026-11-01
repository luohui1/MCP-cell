# MCPcell

**同一台电脑上的 AI 应用共用 MCP：每个 MCP 全机只开一份，不用的时候不占内存。**

A local MCP runtime that lets every AI agent session on a machine share MCP server processes instead of spawning its own set.

`0.6.0-dev` · Windows

## 为什么

Claude Code、Codex、Cursor 每开一个对话，就把配置里的 MCP 全部启动一遍。一台开发机实测（8 个对话）：71 个 MCP 进程，约 **2.9 GB** 内存；用 npx 启动的 MCP 冷启动十几秒，常常撞上应用的 30 秒连接超时，结果对话里用不上。

用 MCPcell 之后（同一台机器，2026-10-31）：2 个对话各连 6 个 MCP，对话这边只有 12 个转发小程序共约 **11 MB**，加上 MCPcell 自己约 56 MB；MCP 没人调用时不开，用到时才打开。对话连上 MCP 只要零点几秒。

## 上手

1. **安装**：`bin/mcpcell.exe install`（现在还要自己编译，见下面的「开发」；之后会提供直接下载的安装包）
2. **打开**：`~/.mcpcell/bin/mcpcell.exe open`，第一次打开会出现使用引导：一键检测各应用的 MCP，再一键托管（之后也可以点右上角的「一键托管」）
3. **重开 Agent 的对话**：新对话就会用 MCPcell 共用的 MCP

Claude Code 里按项目加的 MCP（`claude mcp add` 默认的项目私有配置、项目里的 `.mcp.json`）也会找到；`.mcp.json` 是团队共享的文件，MCPcell 不改它，而是在你本机加一条同名的私有配置覆盖它。支持的应用：Claude Code、Codex、Cursor、Claude Desktop、VS Code、Copilot CLI、Gemini CLI、Antigravity、Windsurf、Kiro、Cline、Roo Code、OpenCode。所有应用用同一套 MCP：一键托管时，一个应用里有的 MCP 也会加到其他应用里（设置里可以关掉；从某个应用里还原或删掉的不会再加回去）。之后在这些应用里新加的 MCP 会自动交给 MCPcell，并同步到其他应用。不想用了：首页右上角菜单里的「全部还原」，或者 `mcpcell restore --all`，各应用的配置逐字节恢复。

## 常用命令

| 命令 | 作用 |
|---|---|
| `mcpcell open` | 打开窗口（需要时先启动后台服务） |
| `mcpcell status` | 各个 MCP 开着几份、连着的对话、占多少内存 |
| `mcpcell takeover` | 一键托管：先列出要改的内容，确认后执行（`-y` 直接执行），再把 MCP 同步到其他应用（`--sync off` 关掉） |
| `mcpcell restore --all` | 全部改回原样（后台服务在运行时由它来做，不在也能用） |
| `mcpcell stop` | 停止后台服务 |

`mcpcell help --all` 列出全部命令，每个命令都可以加 `-h`。

## 它怎么做

- **包一层，不换掉**：托管时在应用配置的原命令前加上约 1 MB 的 `mcpcell-run`，原命令、环境变量和其他设置都不动。MCPcell 没开着就由它拉起来，拉不起来就直接运行原命令
- **用到才打开**：对话连接和列工具由缓存回答，第一次调用工具时才启动 MCP，一段时间没人用就关掉
- **能共用的共用**：已知无状态的 MCP 全机一份；浏览器调试这类为每个对话保存状态的，每个对话一份，对话结束就关
- **npm 包装一次**：npx 启动的 MCP 装进 MCPcell 的缓存直接用 node 运行，context7 从 4 个进程约 235 MB 降到 2 个进程约 84 MB
- **不留孤儿进程**：每个 MCP 放在 Windows Job Object 里，MCPcell 怎么退出都会一起清理；可以设内存上限
- **改前先备份**：只改所选的应用和条目，被你改过的不覆盖，随时逐字节还原；打不开的 MCP 不托管，原样留着并说明原因

设计见 [架构](docs/architecture.md)，测量记录见 [里程碑](docs/roadmap.md)。

## 配置（可选）

窗口里能改的都不用碰配置文件。需要时可以在 `~/.mcpcell/config.json` 里给服务加设置：

```json
{
  "services": {
    "context7": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"], "idleTimeout": "5m", "memoryLimit": "1GB" }
  }
}
```

`idleTimeout` 空闲多久关掉（`"0"` 不关）；`memoryLimit` 整棵进程树的内存上限；`flatten: false` 保留 npx 启动；`catalog: "off"` 不缓存工具列表；顶层 `interface: "cli"` 不显示托盘图标。

## 开发

```sh
go vet ./... && go test ./...
cd web && npm install && npm run build && cd ..        # 改了管理页才需要（产物已提交）
go build -ldflags "-s -w" -o bin/mcpcell.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcelld.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcell-tray.exe ./cmd/mcpcell-tray
cd shim && cargo build --release && cd .. && cp shim/target/release/mcpcell-run.exe bin/
pwsh scripts/measure-mcp.ps1                           # 测量本机所有 Agent 的 MCP 进程与内存
```

## 原则

- 不同凭据、不同工作区的服务绝不共用实例
- 路由只改写 id 这类归属字段，不改工具名，不重放调用；MCP 中途退出时报告"结果未知"
- 只监听本机，校验 Host 和 Origin，每个请求都要令牌
- 改写应用配置前先备份，被你改过的不覆盖，随时逐字节还原
