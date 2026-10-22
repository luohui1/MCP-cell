# MCPcell

**本机 MCP 运行时：每个服务只跑必要的那几份进程，所有 Agent 共用。**

A local MCP runtime that lets every AI agent session on a machine share MCP server processes instead of spawning its own set.

`0.6.0-dev` · Go 新内核。0.5 的 Python 原型保存在 [`legacy/python-0.5/`](legacy/python-0.5/)。

## 为什么

Claude Code、Codex、Cursor 等客户端的每个会话都会把配置里的 stdio MCP 全部启动一遍。开发机上同时开着几个 Agent 时，进程数和内存都按"会话数 × 服务数"增长。一台开发机上实测（2026-10-17，8 个会话）：

- 71 个 MCP 相关进程，约 **2.9 GB 私有提交**（任务管理器的工作集只显示约 0.8 GB）
- 在 Windows 上，每个用 npx 启动的服务要 3–4 个进程，npx 自身的 node 就占约 137 MB
- npx 冷启动慢，同一次会话里 5 个服务撞上客户端的 30 秒连接超时

## 现在能做什么

- 一个守护进程通过 loopback 上的 Streamable HTTP 暴露每个服务：`http://127.0.0.1:47621/s/<服务名>`
- 所有连到同一服务的会话**共用一个上游进程**。实测 6 个 SDK 客户端加 2 个真实 Claude Code 会话共用一个 context7。协议层只改写 id、进度令牌和取消通知的归属，不改工具名，不重放调用
- **连接不启动进程**：握手和工具列表由缓存的能力目录回答，第一次调用工具时才启动，空闲后回收（默认 3 分钟）
- **npx 扁平化**：npm 包装进 MCPcell 的缓存并锁定版本，直接用 node 启动。context7 从 4 个进程、约 235 MB 降到 2 个进程、约 84 MB，冷启动开销从 10 秒以上降到约 2.5 秒
- 上游放在 Windows Job Object 里：守护进程无论怎么退出，整棵进程树都会被清理；可设内存上限，超限立即终止
- **一键托管，配置照样清楚**：读取 Claude Code、Codex、Cursor 的 MCP 配置，在原命令前加上 `mcpcell-run`（`"command": "…\mcpcell-run.exe", "args": ["--", "npx", "-y", "@upstash/context7-mcp"]`），原命令、环境变量和其他设置原样保留。转发程序约 1 MB，带着 Agent 的工作目录和环境变量把会话交给 MCPcell；MCPcell 没运行就拉起它，拉不起来就直接运行原命令。托管前先试启动一次，原生就起不来的条目（程序不存在、缺参数）保持原样并说明原因。改前整文件备份，随时一键还原（逐字节恢复，被你改过的条目不覆盖）
- **会话不因 MCPcell 中断**：升级或重启时转发程序等新守护进程起来后自动重连，守护进程崩溃就把它拉起来再连；关掉窗口或托盘不影响会话，只有停止后台服务才改为直接运行原命令（共享的 MCP 在 MCPcell 恢复后自动回来）；Agent 不用重新握手，只有断开那一刻正在进行的调用报告"结果未知"
- **桌面应用只是用来看和管理的**：托盘图标是单独的小程序（约 14 MB 私有提交），窗口用系统自带的 WebView2 显示管理页；从托盘「退出」或关窗口只关掉界面，后台服务和正在用的 MCP 照常运行。支持开机自启、桌面和开始菜单快捷方式；真要停掉后台服务在设置页或 `mcpcell stop`
- **命令行或图形界面任选**：`mcpcell mode cli` 关掉托盘图标（开机也不再启动它），查看、托管、还原都用命令完成（`mcpcell takeover` 等同于管理页的「一键托管」）；`mcpcell mode gui` 换回来。切换立即生效，只影响托盘图标，后台服务和 MCP 不受影响；命令行模式下仍可用 `mcpcell open` 临时打开窗口。每个命令都可以加 `-h` 只看用法
- 管理页和 `mcpcell ps` 显示每个服务的实例、会话、私有提交和缓存的工具数
- **Skill 一处维护**：同一个 Skill 不用再在 Claude Code、Codex、Gemini 等各自的目录里各放一份。`mcpcell skills` 列出每个 Skill 在哪些 Agent 里、各处内容是否一致、哪份较新、哪些文件不同；`mcpcell skills sync <名称>` 把它放进 `~/.mcpcell/skills`，各 Agent 的目录换成指向它的目录链接（Windows 用 junction，不需要管理员权限），之后改一处处处生效。内容不同的副本不覆盖，由你选以哪一份为准；替换前整目录备份，`mcpcell skills restore` 逐字节还原。见 [Skill 统一管理](docs/skills.md)

- **按会话运行**：chrome-devtools、ssh 这类为每个会话保存状态的 MCP，每个会话一个实例，第一次调用时才启动、会话结束就回收；没用到它的会话不占内存。MCPcell 不认识的 MCP 默认也这样运行，确认无状态后可以改为共享

尚未实现：按项目共享的实例（依赖当前目录的 MCP 目前以 MCPcell 的工作目录运行）、只说新版协议的上游。见 [里程碑](docs/roadmap.md)。

## 试用

```sh
go build -ldflags "-s -w" -o bin/mcpcell.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcelld.exe ./cmd/mcpcell
go build -ldflags "-s -w -H windowsgui" -o bin/mcpcell-tray.exe ./cmd/mcpcell-tray
cd shim && cargo build --release && cd .. && cp shim/target/release/mcpcell-run.exe bin/
bin/mcpcell.exe install       # 装到 ~/.mcpcell/bin：Agent 配置、开机自启、快捷方式都指向那里
~/.mcpcell/bin/mcpcell.exe open
~/.mcpcell/bin/mcpcell.exe shortcut on   # 可选：桌面和开始菜单快捷方式
```

打开后点右上角的「一键托管」，勾选要交给 MCPcell 的 MCP 即可；已经打开的 Agent 会话重启后生效。

更习惯命令行的话，安装时加 `--cli`（不显示托盘图标，之后也可以随时 `mcpcell mode gui|cli` 切换），然后：

```sh
~/.mcpcell/bin/mcpcell.exe clients     # 各 Agent 的 MCP 条目、能否托管、托管后怎么运行
~/.mcpcell/bin/mcpcell.exe takeover    # 列出托管计划，确认后执行；-y 直接执行
~/.mcpcell/bin/mcpcell.exe takeover context7 --client codex   # 只托管 Codex 的 context7
```

`takeover` 和管理页走同一个守护进程接口：已知无状态的 MCP 共享，需要会话隔离的和未验证的每个会话一个实例；`--shared` 让未验证的也共享（需要会话隔离的仍按会话），`--per-session` 让新建的服务一律按会话。计划里列出要改的文件、每个条目的运行方式，以及跳过的条目和原因。

也可以直接在 `~/.mcpcell/config.json` 的 `services` 里添加服务，写法与客户端的 `mcpServers` 条目相同，然后重启守护进程：

```json
{
  "services": {
    "context7": { "command": "cmd", "args": ["/c", "npx", "@upstash/context7-mcp"] }
  }
}
```

Skill 也一样：

```sh
~/.mcpcell/bin/mcpcell.exe skills                 # 每个 Skill 在哪些目录、是否一致
~/.mcpcell/bin/mcpcell.exe skills pdf-tools       # 逐处查看：哪份较新、哪些文件不同
~/.mcpcell/bin/mcpcell.exe skills sync pdf-tools  # 统一成一份，各 Agent 链接到它；先列计划，确认后执行
~/.mcpcell/bin/mcpcell.exe skills sync deploy-notes --from codex   # 各处不同时，选以 Codex 那份为准
~/.mcpcell/bin/mcpcell.exe skills restore --all   # 全部还原
```

管理页的首页就是「我的 MCP」，用大白话写：
- 顶部一句话说明 MCPcell 在做什么、结果如何，例如"Claude Code 和 Codex 共用这些 MCP：同一个 MCP 全机只开一份，现在一共占 99 MB 内存"；有事时下面一条提示（哪些打不开、哪些还是各应用自己开着、没开开机自启），旁边是「一键托管」。
- 列表三列：MCP（名字和一句用途，取自 MCP 自己的说明，没有就列出工具名）｜哪些应用在用（绿色标签是在用 MCPcell 共用的这份，虚线「+ 应用名」点一下就让这个应用也改用这份）｜状态。状态只有三种：「待命」（没人在用，不占内存，用到时自动打开）、「运行中 · 占多少内存」、「打不开：原因」（如缺少服务器地址、程序不在了）；打不开的排在最前。打不开的状态会保存下来，守护进程重启后仍然显示，直到配置改了或下一次打开成功。
- 点一行进详情（地址可以直达，如 `#/mcp/context7`）：哪些应用在用（可展开看连着的对话）、现在的情况、打不开时的原因和怎么处理；运行方式（所有对话共用一份 / 每个对话单独一份）、多久关掉、内存上限、npm 包版本、启动命令放在「高级」里。

首页可以添加 MCP（带常用预设，也能粘贴客户端里的 JSON 条目）。托管、改回原样都先列出要改的内容，确认后才执行；工具全名保持不变。还不能自动托管的客户端在「设置 › 手动接入」给出可复制的配置；原始的连接和服务列表在「设置 › 诊断」。Skill 管理页暂时隐藏（「设置 › 诊断」里可以打开），`mcpcell skills` 命令照常可用。

| 命令 | 作用 |
|---|---|
| `mcpcell install [--cli\|--gui]` | 把程序装到 `~/.mcpcell/bin`（运行中的转发程序会被移到一边再替换），开机自启和快捷方式一并改过去（图形界面方式下开机自启也包括托盘图标），升级时会话不断；`--cli`/`--gui` 同时选择界面方式，不写则不变 |
| `mcpcell mode [cli\|gui]` | 界面方式：`gui` 图形界面（托盘图标，默认），`cli` 命令行（无托盘图标）；不带参数显示当前方式，切换立即启停托盘图标，后台服务不受影响 |
| `mcpcell open` | 打开 MCPcell 窗口，需要时先启动守护进程（图形界面方式下也显示托盘图标）；命令行模式下也可用 |
| `mcpcell start` / `stop` | 在后台启动守护进程（图形界面方式下同时显示托盘图标）/ 停止守护进程：正在用的 MCP 会各自改为直接运行，共享的在 MCPcell 恢复后自动回来 |
| `mcpcell daemon [--port N] [--log] [--open]` | 前台运行守护进程，Ctrl+C 退出；守护进程本身没有界面 |
| `mcpcell clients` | 各客户端的 MCP 条目、分类、托管状态与运行方式 |
| `mcpcell takeover [条目...] [--client 客户端] [--shared\|--per-session] [-y]` | 托管 Agent 的 MCP，同管理页「一键托管」：先列出计划，确认后执行 |
| `mcpcell restore --all` | 还原所有被托管的客户端配置，不需要守护进程；`restore <客户端> [条目...]` 只还原部分 |
| `mcpcell skills [名称...]` | 各 Agent 的 Skill 在哪些目录、各处是否一致；给出名称时逐处列出差异；`skills dirs` 列出各目录对应的 Agent |
| `mcpcell skills sync <名称...>\|--all [--to 位置,...] [--from 位置] [--copy] [-y]` | 把 Skill 放进 `~/.mcpcell/skills`，各位置换成指向它的链接；内容不同的副本不覆盖，`--from` 选版本 |
| `mcpcell skills restore <名称...>\|--all` | 逐字节还原同步时替换的内容（不需要守护进程） |
| `mcpcell autostart on` | 开机自启 |
| `mcpcell ps` | 服务、实例、会话、私有提交 |
| `mcpcell ui --browser` | 在浏览器里用一次性链接打开管理页 |
| `mcpcell shortcut on` | 创建桌面和开始菜单快捷方式 |
| `mcpcell update <服务>` | 把 npx 服务的 npm 包更新到最新版并重新锁定 |

可选配置：

```json
{
  "interface": "cli",
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

- `interface`：`"gui"`（默认，托盘图标，开机随之启动）或 `"cli"`（命令行，无托盘图标），用 `mcpcell mode` 改可以立即生效
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
- 改写客户端配置和 Skill 目录前先备份，只改所选的客户端、条目和位置；被你改过的不覆盖，随时逐字节还原
