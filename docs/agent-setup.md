# MCPcell 安装说明（给 AI 助手）

你是一个 AI 助手，用户请你在这台电脑上安装 MCPcell，并把本机各个 AI 应用（Claude Code、Codex、Cursor、Gemini CLI 等）的 MCP 交给它托管。按下面的步骤做，每一步都把命令输出的要点告诉用户。

MCPcell 是本机的 MCP 运行时：后台常驻，每个 MCP 全机只开一份，所有应用、所有对话共用。这里装的是命令行版本，不装托盘图标和窗口。

## 1. 安装

按系统选一条命令。`MCPCELL_NO_TAKEOVER=1` 表示这一步只安装、开启开机自启，还不改各应用的配置。

Windows（PowerShell）：

```powershell
$env:MCPCELL_NO_TAKEOVER = '1'; irm https://raw.githubusercontent.com/luohui1/MCP-cell/main/scripts/install.ps1 | iex
```

macOS、Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/luohui1/MCP-cell/main/scripts/install.sh | MCPCELL_NO_TAKEOVER=1 sh
```

程序装在 `~/.mcpcell/bin`。下面用 `mcpcell` 指代它：

- Windows：`& "$env:USERPROFILE\.mcpcell\bin\mcpcell.exe"`
- macOS、Linux：`~/.mcpcell/bin/mcpcell`

支持的系统：64 位 Windows 10 1803 及以后、macOS（Apple 芯片和 Intel）、x86_64 Linux。其他系统和架构脚本会报错退出，把报错原样告诉用户。

## 2. 预览要改的内容

```sh
echo n | mcpcell takeover
```

PowerShell 写作 `'n' | & "$env:USERPROFILE\.mcpcell\bin\mcpcell.exe" takeover`。

它按应用列出要托管的条目，然后因为回答了 `n` 而退出，输出「已取消，没有改动」，什么也不改。把计划简要告诉用户：涉及哪些应用、多少个 MCP。

如果输出「没有需要托管的条目」（都已托管，或者没有能托管的），它会像一键托管一样直接把已托管的 MCP 同步到其他应用，然后结束；这时跳过第 3 步。

## 3. 托管

```sh
mcpcell takeover -y
```

用户请你安装 MCPcell 就包括这一步；用户说只安装、不托管时跳过。这条命令会：

- 先试着在 MCPcell 下启动每个 MCP，起不来的保持原样，不托管，输出里写明原因
- 改每个配置文件前整文件备份到 `~/.mcpcell/backups`，只改被托管的条目
- 把一个应用里有的 MCP 同步到还没有它的应用里

第一次托管 npx 启动的 MCP 时会下载 npm 包，可能要一两分钟。有条目没托管时退出码不为 0，把输出里的原因转告用户，不要自己改配置绕过。

## 4. 确认

```sh
mcpcell status
```

输出里能看到后台在运行（端口、PID）和每个 MCP 的状态。MCP 显示「未启动」是正常的：第一次调用工具时才会启动。

告诉用户：**要重新打开 Agent 的对话**。Claude Code 新开的对话就会用 MCPcell，其他应用要重启。你自己当前这个对话里的 MCP 还是原来那份，重开后才换过来。

## 不要做的事

- 不要编辑 `~/.mcpcell` 里的文件，也不要手动改应用配置里带 `mcpcell-run` 的条目。改回原样用下面的还原命令
- 不要运行 `mcpcell stop`，除非用户要求：停掉后各应用的 MCP 会退回各自启动
- 不要删除 `~/.mcpcell/backups`

## 撤销与卸载

还原所有应用的配置（逐字节恢复成托管前的样子）：

```sh
mcpcell restore --all
```

完全卸载：依次运行 `mcpcell restore --all`、`mcpcell autostart off`、`mcpcell stop`，再删除 `~/.mcpcell`。

## 可选

- 指定版本：设 `MCPCELL_VERSION=v0.6.1`
- 不开机自启：设 `MCPCELL_NO_AUTOSTART=1`
- 想要托盘图标和窗口（只有 Windows）：`mcpcell mode gui`；管理页随时可以用 `mcpcell open` 打开
