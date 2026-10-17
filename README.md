# MCPcell

**一个小窗口，收纳 Agents，接管 MCP，独立运行多个会话。**

`0.3.0a1 · 本地工作台预览`

三个主页：**检测 / Agents / MCP**。Claude Code、Pi 等 Agent 的交互会话在独立页签里打开，切页不会重复启动。保留 0.2 的本地 stdio MCP 接入、暂停及冲突安全还原能力，不把它们和 Agent 程序混为一张表。

## 启动

需要 Python 3.11+。源码 ZIP 已附本地终端静态资源；解压后进入项目目录：

```sh
python -m pip install -r requirements.txt
python -m mcpcell --open
```

Windows 可将 `python` 换成 `py -3`，或运行 `start-windows.cmd`。优先用已有浏览器应用窗口打开约 1000×710 的界面，不打包 Electron。Windows Agent 终端使用 pywinpty/ConPTY，原生验证尚未完成。

**从 Git 克隆**时，另执行一次 `python scripts/setup_terminal.py` 准备本地 xterm 静态资源。此显式步骤从 npm registry 下载固定版本，验证 registry 提供的完整性值，只提取 JS/CSS/许可证，不运行 npm 生命周期脚本；应用运行时不访问 CDN。该联网准备流程未在本开发环境完成在线验证，见验证记录。ZIP 中已测试的资源来源见 `mcpcell/web/vendor/THIRD_PARTY_NOTICES.md`。

```sh
# 只看隔离演示，不扫描真实配置、不运行 Agent
python -m mcpcell --demo --open

# 追加明确选择的项目配置；也可在检测页添加目录
python -m mcpcell --workspace "D:\work\my-project" --open

# 自定义 MCP 配置位置
python -m mcpcell --config "cursor=D:\configs\mcp.json" --open
```

终端输出的启动链接含临时访问凭据，不要分享。退出管理器用 Ctrl+C；它会结束自己启动的 Agent 会话。关闭浏览器窗口不等于关闭后台管理器。

## 检测页

Agent 程序和 MCP 配置分开显示。Agent 检测只查找 PATH 和已知安装目录，不运行 `--version`、不自动安装、不读取登录密钥。找到程序不证明其版本、登录状态或所有插件兼容。

Agent 目录包含 Claude Code（检测 `claude`，不会把 C 编译器 `cc` 当成 Agent）、Pi、Codex、Gemini CLI、OpenCode、Factory Droid、Copilot CLI、Aider、Goose、CCS。支持手动登记其他本地 Agent 的绝对可执行路径和结构化参数。

MCP 检测继续覆盖 11 类配置适配器，支持 JSON/JSONC/TOML，保留来源和作用域。用户级位置自动检查，项目级仅检查明确选择的目录，不递归遍历磁盘。企业策略、插件注入、远程/WSL 配置和所有覆盖优先级并未完整解析。详见 [客户端矩阵](docs/clients.md)。

## Agents 页与会话页签

检测到的 Agent 经确认后收纳，收纳不会启动。可以置顶、启用/禁用、移除、指定工作目录、打开多个独立会话。禁用仅阻止新会话，不暗中结束正在运行的工作；仍有运行会话时不能直接移除。

会话使用原生交互终端，不是将输出伪装成聊天消息。Linux 使用真实 PTY；Windows 的直接 ConPTY 后端为待原生验证预览。保留 Agent 自己的交互、登录、模型设置和批准流程，不增加绕过批准参数，不替用户登录或安装。

会话页签支持切换、重命名、输入、Ctrl+C、调整尺寸、结束和关闭。结束保留本次进程内输出，关闭销毁所选会话；不影响其他会话。界面刷新可重新附着于仍在同一管理器中的会话，不等于管理器重启后的持久恢复。

收纳信息与置顶设置持久化；终端输出只存在内存中，不保存聊天记录到磁盘。每会话输出缓冲 1 MiB，最多 8 个并发会话、24 个保留页签。超过输出缓冲会明确报告丢失，不装作历史完整。输入失败不自动重放。目录和可执行程序入口变更需重新核对。

## MCP 页

选中本地 stdio MCP 并确认后，备份原文件并替换其启动入口。保持原服务名、参数、环境声明及批准字段。重启相应 AI 客户端后，由轻量 bridge 接入 MCPcell，再启动原 MCP。暂停阻止新连接并停止所属实例；还原只替换本次托管条目，遇到冲突拒绝覆盖。

这是**统一接管**，不是全部会话共用一个进程：本版仍按连接独占原 MCP 实例。远程 HTTP/SSE 仅检测，未代理。不要移动已接入的源码目录或删除 Python/状态目录；卸载前先还原。

## 重要边界

Agent 程序收纳、MCP 配置发现和 MCP 运行托管是三种不同状态。启动 Pi 或 CCS 不代表已向它们注入 MCP 配置；这需要独立的工具接入适配。目录里没有映射客户端的 Agent 会明确显示没有关联配置来源。

Agent 按当前用户权限运行，可能读取其本地配置、执行自身插件和操作工作目录。MCPcell 不是权限沙箱；Pi 的默认运行模式尤其不能被包装成有独立审批保护。这里的进程回收、并发与缓冲上限不等于硬内存隔离，不能承诺防止 OOM。

尚未交付：自动安装/更新 Agent、账号或模型切换、读取全部历史会话、远程 MCP 代理、跨会话 MCP 安全共享、硬内存预算、完整文件/网络沙箱、任意 CLI 任务平台、原生 EXE。支持的终端 CLI Agent 已可启动，但不开放任意 shell 输入拼接接口。

服务仅监听 127.0.0.1，API 要求随机凭据并校验 Host/Origin。静态资源本地提供；脚本 CSP 保持 self，xterm 的动态布局样式需要 inline style。输出会进入认证后的浏览器终端，可能包含 Agent 自己打印的敏感信息；不要分享会话或临时 URL。已控制同一系统账号的恶意程序不在防御保证范围内。

## 验证

```sh
python -m unittest discover -s tests -v
python scripts/build_preview.py
```

Linux 125 项单元/集成/HTTP 测试、22 项 Chromium 离线界面检查通过。包括真正的 HTTP → PTY → 无害测试 Agent 调用，不只是演示按钮。真实 Claude/Pi 等生产 Agent、Windows/macOS 原生运行、完整浏览器到本地服务的联调尚未完成。具体环境和限制见 [验证记录](docs/validation.md)。
