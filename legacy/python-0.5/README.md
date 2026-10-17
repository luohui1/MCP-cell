# MCPcell

**检测已有 MCP，按来源接入，在一个工作区管理。**

`0.5.0a1 · 本地开发预览`

## 本轮变化

取消固定小窗、浏览器应用窗口参数和双层页签。默认打开 MCP 主界面，按当前浏览器尺寸布局。外层不带圆角，主要控件 4px。左侧为 AI 来源，中间为服务清单。检测范围和 Agent 工具按需进入，多会话使用一个切换器。

修正来源筛选与动作范围不一致：只查看 Cursor 时，接入、暂停和还原仅影响 Cursor 的绑定，不把同配置的其他客户端一起修改。所有来源视图仍可以明确确认后批量接入。

**本轮没有实现 MCP 进程共享。**运行器仍是每连接独占，首协议消息触发启动；初始化也会触发。统一运行的方案与验收条件见 [unification-plan.md](docs/unification-plan.md)。

## 运行

Python 3.11 或以上。ZIP 已包含本地终端静态资源。进入项目目录执行：

```sh
python -m pip install -r requirements.txt
python -m mcpcell --open
```

Windows 可把 `python` 换为 `py -3`。`--open` 使用默认浏览器，尊重现有窗口尺寸，不再生成 960×640 app-window。终端打印的 URL 包含临时访问凭据，请勿分享。

```sh
# 独立演示：不扫描真实配置、不启动 Agent、不修改本机 MCP
python -m mcpcell --demo --open

# 明确指定额外工作区，不递归扫描磁盘
python -m mcpcell --workspace "D:\work\project" --open
```

新 Git 克隆的终端资源准备方式见 `mcpcell/web/vendor/README.md`；本 ZIP 沿用前一轮附带资源及许可证。GitHub 分支尚未同步 0.4/0.5 本地改动，直接克隆当前分支不等于拿到本包。

## 保留的实际能力

11 类客户端配置发现，JSON/JSONC/TOML；显式确认的本地 stdio MCP 接入、备份、暂停、转发与条目还原；检测文件存在不代表服务运行中。远程 MCP 仅检测。

10 类终端 Agent 的入口识别与持久收纳；已有 Agent 终端、工作目录、重命名、结束及关闭功能保留。切换到别的界面或会话不会重启进程。关闭浏览器不停止管理器；退出管理器会终止其 Agent 会话。

不自动安装工具，不改账号，不注入跳过批准的参数。Agent 按启动用户权限运行；工作台不是安全沙箱，部分 Agent 本身没有内置批准机制。输出可能包含原 Agent 打印的敏感信息。

## 接入边界

接管会改写所选来源的启动入口，先确认再备份写入；随后需要重启对应客户端。发生目标配置冲突时不强制覆盖。接管期间不要移动源码、删除 Python 或状态目录；卸载之前先还原。

配置同名或内容相同只是显示/审查候选，不构成跨账号、跨工作区或跨会话复用许可。现有 RSS 是未去重的进程树观测量，不是节省比例，也不是内核硬限额。当前不能承诺解决 OOM。

服务只监听 loopback，管理 API 有随机凭据和 Host/Origin 校验。它不是面向互联网的服务，也不防御已完全控制同一系统账号的程序。

## 验证

本轮 Linux **130 项后端测试、38 项离线 UI 检查通过**，页面 JS 错误 0。真实浏览器访问 loopback 仍被环境策略阻止，未绕过，因此完整浏览器到服务联调不算通过。Windows/macOS 与真实 AI 客户端尚未原生验证。

```sh
python -m unittest discover -s tests -v
python scripts/ui_smoke.py
```

参见 [验证记录](docs/validation.md)、[交互设计](docs/design.md)、[统一运行方案](docs/unification-plan.md)。
