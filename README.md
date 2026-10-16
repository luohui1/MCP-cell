# MCP-cell

**发现分散的 MCP 配置，逐步进入统一、轻量、可审计的工具治理。**

`0.1.0a1 · discovery preview` — 本地只读发现与液态简洁控制台。

> 当前不是 MCP 代理或执行沙箱。不会启动 MCP/CLI、接管进程、修改客户端配置或声称已节省内存。CLI、事务迁移与运行治理属于下一阶段。

## 立即运行

需要 **Python 3.11 或以上**。运行时只用 Python 标准库及静态 HTML/CSS/JavaScript，不需要 npm install、Node 服务、Electron 或第三方 Python 包。

在项目根目录打开终端：

```sh
# 演示：仅使用隔离的合成配置，不读取真实用户目录
python -m mcpcell --demo --open

# 实际扫描本机已知的用户级配置位置
python -m mcpcell --open

# 加入明确选择的项目；不会递归搜索整块磁盘
python -m mcpcell --workspace "D:\work\my-project" --open

# 显式读取自定义位置
python -m mcpcell --config "cursor=D:\configs\mcp.json" --open

# 只输出隐藏敏感值的清单或不可执行的审查提案
python -m mcpcell scan
python -m mcpcell plan
```

Windows 可把 `python` 换成 `py -3`。默认仅监听 `127.0.0.1:43123`，`--port 0` 可选择空闲端口。终端打印的 URL 含临时访问凭据，请勿分享；关闭进程即使访问凭据失效。`--open` 只打开系统浏览器，不执行发现到的工具。

`--home PATH` 指定隔离扫描根目录，并禁用真实环境中的配置路径覆盖。`--workspace` 最多 16 项，`--config` 最多 32 项。`--demo` 不能与真实扫描路径混用。

## 本版实现

11 类客户端配置适配器：Codex、Claude Code、Claude Desktop、Cursor、VS Code/Copilot、Gemini CLI、Windsurf/Cascade、Kiro、Cline、Roo Code、OpenCode。支持已知路径、显式工作区、自定义文件、JSON/JSONC/TOML、来源及作用域保留。具体覆盖与未覆盖项见 [客户端矩阵](docs/clients.md)。

配置等值比较用于提出审查候选，**不等于进程共享许可**。同一物理配置被两个客户端读取，不计成两份重复配置。不同环境变量、认证头或扩展字段会影响候选指纹。指纹使用进程内随机 HMAC 密钥，不跨管理器重启复用。

静态检查包括命令解释器启动、环境/凭据上下文、自动批准设置、非加密远程地址、未知字段与未展开变量。没有发现线索不代表安全。扫描不会读取变量引用的外部文件，也不通过 `--help`、`--version` 或网络连接试运行服务。

控制台提供概览、服务筛选、来源详情、批量选择、静态检查、审查提案导出、CLI 规划及能力边界页。示例数据明确标记为演示；实际进程数与内存节省使用空值，而不是编造遥测。

## 重要边界

配置发现不证明客户端已安装、已启用或正在运行。客户端可能还有企业策略、插件注入、其他用户配置档、远程/WSL 配置、运行时注册和覆盖优先级；本版不完整计算最终生效配置。

本版不会执行配置回写、备份、回滚、OAuth 迁移、生命周期托管、MCP 消息转发、进程复用或内核资源限制。导出的提案明确带 `applicable: false`，不能覆盖客户端配置。

浏览器和导出省略原始参数值、环境变量值、认证头值和 OAuth 原文；自定义可执行路径也不显示。远程地址仅显示协议、主机和端口，隐藏 userinfo、路径、查询和 fragment。服务名、主机名及配置文件路径仍可能属于隐私，分享导出前请检查。

服务只提供固定静态文件和受认证的 `/api/snapshot`、`/api/plan`、`/api/session`、`/api/rescan`。限制 Host/Origin，使用 HttpOnly/SameSite Cookie、CSP 和禁止缓存。不会防御已经控制同一系统账号的恶意程序。它不是公共互联网 Web 服务。

读取限额：单文件 1 MiB、最多 256 个来源、1024 条绑定、64 层嵌套、20000 个解析节点；跳过符号链接/重解析点及非常规文件。源码读取和进程内解析不是强隔离沙箱，同账号攻击者的文件竞态不在本版保证范围内。

## 测试

```sh
python -m unittest discover -s tests -v
```

验证记录与环境限制见 [validation.md](docs/validation.md)。不要把路径映射测试等同于 Windows/macOS 真机验证。

## 后续路线

| 阶段 | 交付边界 |
|---|---|
| 发现与控制台 | 本版：只读扫描、归一化、来源追踪、审查提案 |
| 事务迁移 | 原文件备份、逐客户端语义核对、无损补丁、前置版本检查、原子替换与冲突安全回滚 |
| 受控运行 | 每用户管理器、轻量桥、独占实例、进程树所有权、预算准入、可靠清理 |
| 经过验证的共享 | 按制品、工作区、身份、权限和状态证据分区，不合并权限 |
| CLI 适配 | 共用执行契约；结构化 argv、无隐式 shell、批准/预算/取消/审计 |

详见 [运行契约](docs/runtime-contract.md)、[视觉规范](docs/design.md)。

## 开发约定

新能力在 feature branch 上开发，通过测试和审查后再合并。不得把示意数据标成真实监控，不得添加没有执行后端的“已启用安全”开关，不得为减少进程而默认合并安全域。
