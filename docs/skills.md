# Skill 统一管理

日期：2026-10-20。对应代码：`internal/skills`、`cmd/mcpcell/skills.go`、`internal/httpd/skills.go`、管理页 `web/src/pages/Skills.tsx`。

> 2026-10-21 起管理页上的 Skill 页**暂时隐藏**（导航和首页都不显示；「设置 › 诊断」里有开关，默认关）。`mcpcell skills` 命令和 `/api/skills` 照常可用。

一句话：**每个 Skill 只在 MCPcell 的来源目录存一份，各 Agent 的 Skill 目录用目录链接指向它；能看出各处是否一致，改动前先备份，随时逐字节还原。**

## 1. 问题

Skill 是放在 Agent 的 Skill 目录里的一个文件夹（`SKILL.md` 加脚本和参考资料），没有常驻进程，所以没有内存问题。麻烦在于分散：同一个 Skill 要在 Claude Code、Codex、Gemini 等各自的目录里各放一份，更新时要改好几遍，时间一长各处内容就不一样了，也看不出哪一份是新的。

一台开发机上的现状（2026-10-20，只读扫描）：10 个 Agent 目录里有 60 多个不同的 Skill，十几个在多处出现。其中一组是某个安装工具放的：它在自己的目录里存一份，再往 8 个 Agent 目录各建一个目录链接，已经是"一处维护"；另有几个是各处独立的相同拷贝，要逐个更新。

## 2. 各 Agent 从哪里读 Skill

只列用户级目录（项目级的 `.claude/skills`、`.agents/skills` 等不在管理范围内）。依据是 2026-10-20 的官方文档，Codex 另外核对了源码。

| 位置 ID | 目录 | 读取它的 Agent | 说明 |
|---|---|---|---|
| `claude` | `~/.claude/skills`（设了 `CLAUDE_CONFIG_DIR` 时是它下面的 `skills`） | Claude Code；Cursor、OpenCode 兼容读取 | 跟随符号链接；几个位置指向同一目标时只加载一次。`synced/`（claude.ai 同步）和 `.trash/` 由 Claude Code 自己管 |
| `agents` | `~/.agents/skills` | Codex、Gemini CLI、Cursor、OpenCode、GitHub Copilot | 事实上的跨 Agent 公共位置，见下 |
| `codex` | `$CODEX_HOME/skills`（默认 `~/.codex/skills`） | Codex；Cursor 兼容读取 | 文档已不再提，源码里仍作为用户级位置读取；`.system/` 是 Codex 自带的 |
| `gemini` | `~/.gemini/skills` | Gemini CLI | 同名时 `~/.agents/skills` 优先 |
| `antigravity` | `~/.gemini/config/skills` | Antigravity（2.0，各产品通用） | 来自官方 codelab |
| `antigravity-ide` | `~/.gemini/antigravity/skills` | Antigravity IDE | 早期文档的全局位置 |
| `antigravity-cli` | `~/.gemini/antigravity-cli/skills` | Antigravity CLI | 只见于第三方资料，未在官方文档核实 |
| `cursor` | `~/.cursor/skills` | Cursor | |
| `opencode` | `~/.config/opencode/skills` | OpenCode | |
| `copilot` | `~/.copilot/skills` | GitHub Copilot | |

**`~/.agents/skills` 是谁在用**：Codex 的官方用户级位置就是它；Gemini CLI 把它当作 `~/.gemini/skills` 的别名且优先；Cursor、OpenCode、GitHub Copilot 也都读它。社区的 `npx skills add` 也把它当默认安装位置（旁边的 `~/.agents/.skill-lock.json` 是它的记录）。Claude Code 不读它。

**一个 Agent 读多个目录**：Codex 同时读 `~/.codex/skills` 和 `~/.agents/skills`，同名的两份都会出现在选择列表里，不会合并；Cursor 读 `claude`、`codex`、`agents`、`cursor` 四处。所以不要为了"覆盖所有 Agent"把同一个 Skill 链接进每个目录。只想让所有 Agent 都能用的话，`claude` 加 `agents` 两处就覆盖了上表里除 Antigravity 以外的全部 Agent（Cursor、OpenCode 会从这两处各读到一次，指向的是同一份）；`mcpcell skills dirs` 会列出每个目录对应的 Agent。

## 3. 来源目录：为什么是 `~/.mcpcell/skills`

| | `~/.mcpcell/skills`（采用） | 沿用 `~/.agents/skills` |
|---|---|---|
| 谁能看到 | 没有 Agent 读它，哪个 Agent 能用由链接决定 | 放进去的每个 Skill 立刻被 5 个 Agent 读到 |
| 只改所选 Agent | 能：只给选中的位置建链接 | 不能：只想给 Claude Code 用的 Skill 移进来后，Codex 等也会读到 |
| 其他工具 | 不冲突 | `npx skills`、各家安装器都往这里写，会和同步互相覆盖 |
| 还原 | 删掉链接、移回原目录即可，来源目录是 MCPcell 自己的数据 | 要区分哪些是 MCPcell 移进来的 |
| 代价 | 每个位置都要一个链接（`agents` 也一样） | 少建几个链接 |

结论：来源目录放在 MCPcell 自己的目录里，和 MCP 接管的"定义与绑定分离"一致：`config.json` 存服务定义，Agent 配置只是指向它；这里来源目录存 Skill，Agent 目录只是指向它。

## 4. 看出各处是否一致

`mcpcell skills` 和管理页的「Skill」页按名称汇总来源目录和每个 Agent 目录：

- **内容比较**：按目录内容的 SHA-256 比较（相对路径加每个文件的哈希），不看时间戳。不计入运行脚本或浏览留下的缓存：`.git/`、`__pycache__/`、`*.pyc`、`.DS_Store`、`Thumbs.db`、`desktop.ini`（复制和移动时照样带上）。
- **只在需要时读文件**：一个 Skill 只在一个目录有实体（其余是指向同一目标的链接）时，只看目录信息不读文件；在两个以上不同目录有实体时才逐个哈希。这样冷启动扫描 60 多个 Skill 也只要零点几秒，不会把所有文件读一遍。
- **比较基准**：来源目录有这个 Skill 时以它为准，否则以最近修改的一份为准。不同的内容标成版本 A、B……，每一份标"一致 / 较新 / 较旧"，并列出与基准相比改动、多出、缺少了哪些文件。
- **状态**：已统一（每个位置都链接到来源目录）、相同（各处内容一样但各是一份）、不一致、只在一处、外部管理（各处都链接到同一个外部目录，比如安装工具自己的目录）、只在来源目录。
- **链接**：指向来源目录的是「链接」；指向别处的是「外链」，读取它指向的内容来比较。

## 5. 同步规则

`mcpcell skills sync <名称...>|--all [--to 位置,...] [--from 位置|mcpcell] [--copy] [--dry-run] [-y]`

`--all` 指所有在多处出现、还没统一的 Skill；其中内容不同的只列出差异，不改动。

1. **先定以哪一份为准**：来源目录已有就用它；没有时，各处的实体副本必须完全相同才自动导入（复制进来源目录），否则不做任何改动，列出各版本和差异，让用户用 `--from` 选。`--from <位置>` 用那一份（来源目录已有不同版本时，旧版本先移到备份）；`--from mcpcell` 用来源目录里的。
2. **再定改哪些位置**：不写 `--to` 时只处理已经有这个 Skill 的位置；写了就只处理列出的位置，没有的新建链接。Agent 没安装（它的目录不存在）的位置跳过。
3. **每个位置**：
   - 没有：新建链接（Skill 目录不存在时一并创建，还原时空了就删掉）。如果是 MCPcell 之前建的链接被删了，重建时仍保留第一次同步前的原始记录，还原照样放回最初的内容
   - 已链接到来源目录：不动
   - 与要同步的版本相同的副本：先移到备份，再换成链接
   - 内容不同的副本：**不覆盖**，列出差异；只有用户用 `--from` 明确选了版本，才把它移到备份后换成链接
   - 链接到别处（其他工具管理的）：默认不动；在 `--to` 里明确列出才替换，还原时重建原来的链接
   - 同步后被改动过（链接被换掉、MCPcell 的副本被编辑）：不动，提示先还原或手动处理
4. **链接方式**：Windows 用目录联接（junction），不需要管理员权限或开发者模式；其他系统用符号链接。建不了链接时（比如文件系统不支持）改为复制，之后再同步会刷新这份副本；副本被编辑过就不再动它。`--copy` 一律复制。
5. 命令行先列出计划，确认后执行（`-y` 跳过确认，`--dry-run` 只看计划）。改完后，已经打开的 Agent 会话一般要新开会话才会读到。

在链接方式下，从任何一个 Agent 的目录里编辑 Skill，改的都是来源目录里的那一份，所有 Agent 立即看到同样的内容。

## 6. 备份与还原

- 同步替换实体目录时用**移动**（同一卷上是重命名），原目录完整地放进 `~/.mcpcell/skill-backups/<时间>/<位置>/<名称>`，字节和修改时间都不变。跨卷时先复制、校验哈希一致后才删除原目录；文件被占用导致移动失败时什么都不改。
- `mcpcell skills restore <名称...>|--all [--to 位置,...]`：只还原 MCPcell 做过的改动。先确认这个位置仍是同步时写下的样子（同一个链接目标，或内容哈希不变的副本），再删除链接（只删链接，不碰目标）或副本，把原目录移回、重建原来的链接（先在旁边建好再换上，建不了就什么都不动），或者恢复为空。移回后再核对一次内容哈希。
- 被改动过的不还原，提示原目录的备份位置；用户自己处理掉之后（比如删掉换上去的目录）再还原即可。
- 来源目录保留，不随还原删除。

实测（2026-10-20，临时目录）：两份不同的副本加一组外链，经过"冲突拒绝 → `--from` 同步 → 一个链接被换成用户自己的目录 → 还原被拒 → 用户删掉后再还原"，最终 37 个条目的文件哈希、修改时间、链接目标与同步前完全一致。

## 7. 记录

都在 `~/.mcpcell/` 下（`MCPCELL_HOME` 可改），不需要守护进程：

| 文件 | 内容 |
|---|---|
| `skills/` | 来源目录 |
| `skills.json` | 每个被同步的位置：原来是什么（不存在 / 目录及其内容哈希 / 指向哪里的链接）、备份路径、写下了什么（链接目标或副本哈希）、是否创建了 Skill 目录；以及来源目录里每个 Skill 取自哪个位置 |
| `skill-backups/` | 被替换的原目录，以及被 `--from` 替换掉的来源目录旧版本 |
| `skills.lock` | 同步或还原期间存在，避免命令行和管理页同时改；崩溃留下的超过 2 分钟自动失效 |

## 8. 接口

- 命令行：`mcpcell skills [名称...]`（总览或逐处详情）、`skills dirs`、`skills sync`、`skills restore`，直接读写文件，守护进程不运行时也能用
- 管理 API：`GET /api/skills`；`POST /api/skills/sync`（`names`、`all`、`to`、`from`、`copy`、`dryRun`）；`POST /api/skills/restore`（`names`、`all`、`locations`、`dryRun`）。管理页的每个操作都先用 `dryRun` 显示计划，确认后执行
- 位置的路径跟随守护进程或命令行自己的 `USERPROFILE`/`HOME`、`CLAUDE_CONFIG_DIR`、`CODEX_HOME`。测试和试验时把它们和 `MCPCELL_HOME` 一起指向临时目录，就不会碰到真实的 Skill 目录

## 9. 不做的事

- 不管项目级 Skill（仓库里的 `.claude/skills` 等），它们随仓库走
- 不从网络安装或更新 Skill；外部工具安装的保持由那个工具更新（显示为外部管理）
- 不合并两个版本的内容，只让用户选一份
