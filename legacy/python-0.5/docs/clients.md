# 配置发现矩阵

核对日期：2026-10-16。下表是本原型的读取覆盖，不是各客户端的完整配置解析器或安装探测器。目录中的文件存在与否只用于报告配置来源，不运行客户端，也不调用服务。

| 适配器 | 自动检查的主要位置 | 项目级与限制 |
|---|---|---|
| Codex | `$CODEX_HOME/config.toml`；默认 `~/.codex/config.toml` | 显式工作区 `.codex/config.toml`；读取 `mcp_servers`；不计算企业策略和信任状态 |
| Claude Code | `~/.claude.json` | 显式工作区 `.mcp.json` 及 `.claude.json` 中所选项目的 `projects[path].mcpServers`；不扫描历史项目目录 |
| Claude Desktop | Windows `%APPDATA%/Claude/claude_desktop_config.json`；macOS `~/Library/Application Support/Claude/claude_desktop_config.json` | Linux 无默认官方位置；扩展安装/插件来源未覆盖，可显式指定配置 |
| Cursor | `~/.cursor/mcp.json` | 显式工作区 `.cursor/mcp.json`；读取 `mcpServers` |
| VS Code / Copilot | Code 与 Code Insiders 默认 User/mcp.json；`$COPILOT_HOME/mcp-config.json` 默认 `~/.copilot/mcp-config.json` | 显式工作区 `.vscode/mcp.json` 与 `.mcp.json`；分别读取 `servers`/`mcpServers`；其他 profile、远程及独立启用状态未自动覆盖 |
| Gemini CLI | `~/.gemini/settings.json` | 显式工作区 `.gemini/settings.json`；读取 `mcpServers`，不计算 `mcp.allowed/excluded` 等最终策略 |
| Windsurf / Cascade | 兼容旧 `~/.codeium/windsurf/mcp_config.json`；当前 Cascade 文档中的 `$XDG_CONFIG_HOME/devin/mcp_config.json` 或 Windows `%APPDATA%/devin/mcp_config.json` | 只读取 Cascade 兼容格式，不宣称覆盖 Devin Local agent |
| Kiro | `~/.kiro/settings/mcp.json` | 显式工作区 `.kiro/settings/mcp.json`；agent 配置及动态注入未覆盖 |
| Cline | `$CLINE_MCP_SETTINGS_PATH`，默认 `~/.cline/data/settings/cline_mcp_settings.json` | 另检查 Code/Insiders/Cursor 的既有扩展存储兼容路径；实际安装位置可用 `--config` 指定 |
| Roo Code | Code/Insiders/Cursor 既有 globalStorage 扩展兼容路径中的 settings/mcp_settings.json | 显式工作区 `.roo/mcp.json`；定制扩展存储需手动指定 |
| OpenCode | `$XDG_CONFIG_HOME/opencode/opencode.json` 与 `.jsonc`；显式 `OPENCODE_CONFIG` | 显式工作区 opencode.json/.jsonc；读取 `mcp`，识别 command 数组；不计算远程组织配置等完整优先级 |

除 Windows 的 APPDATA、macOS 的 Application Support 外，默认应用配置根采用 XDG_CONFIG_HOME 或 `~/.config`。旧路径是保守兼容候选，不代表新版本仍然使用它。

用户目录、项目目录与自定义文件均需要由本地启动参数界定。浏览器 API 不接受任意文件路径。不递归查磁盘、不读密钥库、不获取 OAuth token、不展开 `${file:...}`、`${input:...}`、`{env:...}` 等引用。

JSONC 是支持注释和尾逗号的 JSON，不是 JSON5。重复键、非法节点、非常规文件、过大或过深配置会被拒绝，并显示安全化错误。所有来源保留，不跨源猜测覆盖顺序。`disabled`/`enabled` 只展示显式声明，不当作客户端实际运行状态。

## 主要依据

- Codex: https://developers.openai.com/codex/mcp （核对时重定向至 ChatGPT Learn）
- Claude Code: https://code.claude.com/docs/en/mcp
- Claude Desktop: https://modelcontextprotocol.io/docs/develop/connect-local-servers
- Cursor: https://prod.cursor.com/docs/mcp
- VS Code: https://code.visualstudio.com/docs/agent-customization/mcp-servers
- Gemini CLI: https://geminicli.com/docs/tools/mcp-server/
- Cascade: https://docs.windsurf.com/windsurf/cascade/mcp （核对时重定向至 Devin 的 Cascade 文档）
- Kiro: https://kiro.dev/docs/mcp/configuration/
- Cline: https://docs.cline.bot/mcp/mcp-overview
- Roo Code: https://docs.roocode.com/features/mcp/using-mcp-in-roo
- OpenCode: https://opencode.ai/docs/config/ 与 https://docs.opencode.ai/docs/mcp-servers/

文档会变化，客户端也可能迁移配置位置。支持范围必须通过版本化适配器和回归 fixture 扩展，不能只根据品牌名称承诺“全面兼容”。
