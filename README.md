# MCPcell

**检测已有 MCP，接入一个小窗口统一管理。**

0.2.0a1 · 从只读原型升级为实际的 stdio 接管。

## 使用

需要 Python 3.11+。在源码根目录：

```sh
python -m pip install -r requirements.txt
python -m mcpcell --open
```

Windows 可用 `py -3` 替代 `python`，或双击 `start-windows.cmd`。支持的浏览器通过应用模式打开约 **960×660** 的窗口；没有合适浏览器时回退到普通浏览器。不是已经打包的原生 EXE。

点击 **检测 MCP → 选择 → 接入**。确认后备份原配置、替换启动入口。**重启对应 AI 客户端** 后，由 MCPcell 启动和转发原服务；列表中可暂停、启用、停止实例和还原配置。

```sh
python -m mcpcell --demo --open                    # 隔离的演示配置
python -m mcpcell --workspace "D:\work\project" --open
python -m mcpcell --config "cursor=D:\configs\mcp.json" --open
python -m mcpcell scan                            # 只读扫描，无接管
```

演示 HTML 只模拟页面操作；它不会修改本机。实际接入需要运行 Python 管理器。不要把终端的临时访问链接发给其他人。

## 本版确实做了什么

- 11 类客户端的已有配置发现；JSON/JSONC/TOML，支持显式项目与自定义路径。
- 在一个列表中展示 MCP 与来源；相同配置可折叠显示，但**不会共享进程**。
- 选中并确认后进行真实配置接入：保持原服务名称、原权限/批准字段及环境声明，仅替换启动入口。
- 私有本机清单、原始备份、持久化准备记录、启动恢复核对、逐项原子替换。
- 客户端通过轻量 stdio bridge 连接统一管理器；收到第一条协议消息才启动原服务，原样转发，不改工具名称或请求 ID。
- 每连接独占，最多 8 个连接；断开后回收受控进程组。Windows 实现 Job Object 归属建立后再恢复初始线程。
- 暂停会阻止新连接并终止所属实例；还原会恢复原启动配置。暂停/停止可能中断正在执行的调用，不自动重试。
- 进程树 PID/RSS 观测（psutil）；不存在的实例不伪装成运行中，不展示未测得的节省比例。

Codex 的 TOML 接入需要 tomlkit；缺少时仅检测，不强行改写。JSONC 替换保留未修改字段与外围注释；配置没有其他变更时支持原始字节还原，有其他变更时只还原目标条目。

## 必须知道的边界

本版主要接管**本地 stdio MCP**。远程 HTTP/SSE/OAuth MCP 保持直连，仅检测；动态启动参数、环境文件、显式 shell 入口等不自动接管。客户端自定义策略可能限制桥接启动命令，需要在原客户端重新批准，MCPcell 不绕过它。

没有跨会话进程共享、内核内存硬限、完整文件/网络沙箱或 CLI 执行。因此不能称为已经解决所有内存溢出或安全问题。8 连接上限限制的是受控连接数量，不是整机硬内存保证。状态中的 RSS 是进程树求和，可能重复计算共享页。

Windows/macOS 尚未完成原生真机验证；Linux 已执行测试。Linux/macOS 正常断开或退出使用受控进程组清理；恶意进程主动脱离进程组、宿主被强制杀死或内核级隔离不在本版保证范围。Windows Job Object 代码需要在真实 Windows/AI 客户端上继续验证。

## 备份、冲突与位置

管理状态默认在 `~/.mcpcell`，可用 `--state-dir` 指定。Windows 使用当前用户 DPAPI；Linux/macOS 使用 0700 目录及 0600 文件权限。备份包含原始配置，可能包含凭据，**不能上传或提交到 Git**。已经还原的历史备份也留在此目录。

运行中的桥接入口使用当前源码目录和 Python 的绝对路径。**接入期间不要移动源码、删除 Python 环境或删除状态目录；卸载前先还原配置。** 管理器退出后，新连接可通过桥自动启动管理器；不会绕过接管直接启动原工具。

还原前检查目标条目是否仍是本次写入的桥接配置。外部修改了目标条目时报告冲突，不强行覆盖；无关字段修改会保留。跨文件批量操作逐项报告，不声称具有全局原子性。与不配合锁的外部写入程序之间仍存在检查与替换间的竞态窗口。

前端不接收原始参数、环境变量值、认证头或备份内容；服务名、文件路径和主机名仍可能是隐私。没有同账号恶意软件防护承诺。管理 API 仅本机，使用随机认证、Host/Origin 校验；普通 bridge 凭据不拥有管理 API 权限。

## 开发与测试

```sh
python -m unittest discover -s tests -v
python scripts/build_preview.py
# 可选 UI 测试：需要 Playwright 与 Chromium；不是应用运行依赖
python scripts/ui_smoke.py
```

验证细节见 [docs/validation.md](docs/validation.md)。设计只借鉴 CCS / CC Switch 的短操作路径，不复制其源码、品牌或资产。
