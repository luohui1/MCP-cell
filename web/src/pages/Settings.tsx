import { useEffect, useState, type ReactNode } from 'react';
import { AppWindow, Settings as SettingsIcon } from 'lucide-react';
import { desktop, takeover, type AutostartStatus, type DesktopStatus } from '../api';
import { useStatus } from '../App';
import { masked } from '../clients';
import { bytes, since } from '../format';
import { Button, CodeBlock, CopyButton, Dot, GroupHeader, PageHeader, Switch, TableCard, useToast } from '../ui';

const commands = [
  'mcpcell open            # 打开 MCPcell 窗口（需要时先启动守护进程）',
  'mcpcell start           # 在后台启动守护进程',
  'mcpcell stop            # 停止守护进程',
  'mcpcell ps              # 服务、实例、会话与内存',
  'mcpcell clients         # 各 Agent 的 MCP 条目与托管状态',
  'mcpcell restore --all   # 还原所有托管（不需要守护进程）',
  'mcpcell autostart on    # 开机自启',
  'mcpcell shortcut on     # 创建桌面和开始菜单快捷方式',
  'mcpcell update <服务>   # 更新 npx 服务的 npm 包',
].join('\n');

export function SettingsPage() {
  const { status } = useStatus();
  const toast = useToast();
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  const [desk, setDesk] = useState<DesktopStatus | null>(null);
  useEffect(() => {
    takeover.autostartStatus().then(setAuto).catch(() => setAuto(null));
    desktop.status().then(setDesk).catch(() => setDesk(null));
  }, []);

  async function attempt<T>(task: () => Promise<T>, done: string, set: (v: T) => void) {
    try {
      set(await task());
      toast(done);
    } catch (e) {
      toast((e as Error).message, 'error');
    }
  }

  return (
    <>
      <PageHeader icon={<SettingsIcon className="size-5 text-zinc-500" />} title="设置" />
      <div className="mx-auto w-full max-w-[880px] space-y-7 px-8 pt-6 pb-12">
        <section>
          <GroupHeader title="常规" />
          <TableCard>
            <SettingRow
              title="开机自启"
              desc={auto?.supported === false ? auto.error : '登录 Windows 时在后台启动 MCPcell，托盘里显示图标。托管了 MCP 时建议开启，否则 MCPcell 没运行时这些 MCP 不可用。'}
            >
              {auto?.supported && (
                <Switch
                  checked={auto.enabled}
                  label="开机自启"
                  onChange={(on) => attempt(() => takeover.autostart(on), on ? '已开启开机自启' : '已关闭开机自启', setAuto)}
                />
              )}
            </SettingRow>
            {desk?.supported && (
              <>
                <SettingRow title="桌面快捷方式" desc="在桌面放一个 MCPcell 图标，双击打开窗口。">
                  <Switch
                    checked={desk.desktopShortcut}
                    label="桌面快捷方式"
                    onChange={(on) => attempt(() => desktop.set({ desktopShortcut: on }), on ? '已创建桌面快捷方式' : '已删除桌面快捷方式', setDesk)}
                  />
                </SettingRow>
                <SettingRow title="开始菜单" desc="添加到开始菜单，可以搜索「MCPcell」打开。">
                  <Switch
                    checked={desk.startMenuShortcut}
                    label="开始菜单"
                    onChange={(on) => attempt(() => desktop.set({ startMenuShortcut: on }), on ? '已添加到开始菜单' : '已从开始菜单移除', setDesk)}
                  />
                </SettingRow>
                <SettingRow title="桌面窗口" desc={desk.webview ? '用独立窗口打开管理页；关闭窗口后 MCPcell 继续在托盘里运行。' : (desk.webviewError ?? '本机没有 WebView2 运行时')}>
                  {desk.webview && (
                    <Button size="sm" icon={<AppWindow className="size-3.5" />} onClick={() => desktop.open().catch((e) => toast((e as Error).message, 'error'))}>
                      打开窗口
                    </Button>
                  )}
                </SettingRow>
              </>
            )}
          </TableCard>
        </section>

        <section>
          <GroupHeader title="守护进程" />
          <TableCard>
            <InfoRow label="状态">
              <span className="inline-flex items-center gap-2">
                <Dot tone="green" />
                运行中 · 已运行 {since(status.startedAt)} · v{status.version}
              </span>
            </InfoRow>
            <InfoRow label="监听地址">
              <span className="font-mono">127.0.0.1:{status.port}</span>
              <span className="text-zinc-400"> · 只接受本机连接</span>
            </InfoRow>
            <InfoRow label="进程">
              PID {status.pid} · 私有提交 {bytes(status.daemon.privateBytes)}
              <span className="text-zinc-400"> · 工作集 {bytes(status.daemon.workingSetBytes)}</span>
            </InfoRow>
            <InfoRow label="实例上限">同时最多 {status.maxInstances} 个；满了先回收最久没用的空闲实例</InfoRow>
            <InfoRow label="访问令牌">
              <span className="flex items-center gap-1">
                <code className="font-mono text-[13px]">{masked(status.token)}</code>
                <CopyButton text={status.token} label="复制访问令牌" />
                <span className="text-zinc-400">Agent 配置里的 Bearer 令牌，不要分享</span>
              </span>
            </InfoRow>
            <InfoRow label="配置文件">
              <span className="flex min-w-0 items-center gap-1">
                <code className="truncate font-mono text-[13px]">{status.configPath}</code>
                <CopyButton text={status.configPath} label="复制路径" />
              </span>
            </InfoRow>
          </TableCard>
          <p className="mt-2.5 px-1 text-xs leading-5 text-zinc-400">在管理页增删改服务会直接写入配置文件并立即生效；手动改文件后需要重启守护进程。</p>
        </section>

        <section>
          <GroupHeader title="命令行" />
          <CodeBlock shown={commands} copy={commands} />
        </section>
      </div>
    </>
  );
}

function SettingRow({ title, desc, children }: { title: string; desc?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex items-center gap-8 px-5 py-4">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{title}</div>
        {desc && <div className="mt-1 text-[13px] leading-5 text-zinc-500">{desc}</div>}
      </div>
      {children}
    </div>
  );
}

function InfoRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-12 items-center gap-6 px-5 py-2.5 text-sm">
      <div className="w-20 shrink-0 text-zinc-500">{label}</div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
