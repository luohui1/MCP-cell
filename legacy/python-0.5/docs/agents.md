> 0.5 交互更新：会话仍独立运行，但常驻页签已改为会话选择器；未新增执行权限或共享运行。

# Agent hub · 0.3

## 分层

ClientAdapter 发现 MCP 配置；AgentCatalog 发现已安装的终端 Agent；AgentHub 保存收纳设置并创建独立终端会话；MCP Manager 继续管理原有 stdio bridge。它们共享工作台，不混淆权限或接入状态。

检测只查找非空绝对 PATH 项和有限的已知安装目录，不执行探测命令。Claude Code 只用 `claude`，不会误认 Unix `cc`。Windows 已知 npm shim 按 package.json bin 解析成 node.exe + 脚本，不执行 cmd /c；未知脚本入口不自动执行。手动登记仅接受绝对可执行路径和字符串数组参数，拒绝直接登记已知 shell。

程序存在不等于登录有效或版本兼容。启动入口的文件元数据指纹用于发现变更，并非供应链签名或强完整性证明；同账号恶意修改的 TOCTOU 不在此保证内。

## 执行语义

Linux：openpty + start_new_session + 独立 terminal_child 设置控制终端后 exec；不在多线程服务中使用 preexec_fn。每会话独立进程组、目录、输入和输出游标。Linux 在清理前通过 WNOWAIT 保留退出的 leader PID。

Windows：直接 pywinpty PTY/ConPTY，不使用额外未认证的终端代理 socket，不拼接 cmd。此后端尚未真机验证。退出回收按 PID 与创建时间做 best-effort 进程树处理，不宣称是 Job Object 沙箱；原 MCP 运行器的 Job 代码与 Agent ConPTY 后端要分开说明。

默认使用各 Agent 正常入口，不注入跳过审批参数。环境变量按 Agent 类型和基础运行白名单传递，不继承管理器所有环境变量；Agent 仍能按自身逻辑读取用户配置及凭据。Pi 的上游默认没有内置权限审批系统，MCPcell 不为其伪造一个已生效的权限系统。

## 会话与限额

最多收纳 48 个 Agent，8 个同时运行会话，24 个保留页签；管理器内启动 requestId 去重，结果未知时不自动重放。每会话输出 ring buffer 1 MiB，单次读 64 KiB，单次输入 8 KiB；终端视图 scrollback 1000 行。输出按字节与 base64 传输，避免拆分 UTF-8 导致跨请求乱码。

工作台页签只是连接已有进程；切页不执行 launch。活动页签轮询输出，其他会话在服务端有界缓冲。已结束会话保留输出，关闭会话才清除；管理器退出会停止所属会话，不将聊天记录持久保存。服务端状态不等于跨重启恢复。

会话层不解析 Agent 的工具调用，无法强制其批准或实施逐工具沙箱；只能明确管理进程与终端交互。MCP 安全共享、远程 MCP 和 Pi 等 Agent 的工具注入仍需独立适配，不因图标出现在库中就宣称完成。

## 依据（2026-10-17 核对）

- Claude CLI: https://code.claude.com/docs/en/cli-reference
- Pi: https://github.com/earendil-works/pi 及其 README；默认无权限系统，交互命令 pi。
- CCS: https://github.com/kaitranntt/ccs
- xterm API: https://xtermjs.org/docs/api/terminal/classes/terminal/
- pywinpty: https://github.com/andfoy/pywinpty

其余内置项目前属于有限的程序入口识别与普通交互启动，不是经过真实账号验证的深度集成。
