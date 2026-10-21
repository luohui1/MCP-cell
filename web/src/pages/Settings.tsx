import { useEffect, useRef, useState, type ReactNode } from 'react';
import { AppWindow, ChevronRight, Settings as SettingsIcon } from 'lucide-react';
import { desktop, instancesOf, takeover, uiMode, type AutostartStatus, type DesktopStatus, type InterfaceMode, type InterfaceStatus } from '../api';
import { clients as manualClients, masked } from '../clients';
import { useStatus } from '../data';
import { bytes, clock, since } from '../format';
import { BrandMark, brandOf } from '../icons';
import { showSkills, useSkillsShown } from '../features';
import { Body } from '../layout';
import { agentOrder } from '../model';
import { useRoute } from '../router';
import { Badge, Button, CodeBlock, CopyButton, cx, Dot, GroupHeader, PageHeader, Segmented, Switch, TableCard, useToast } from '../ui';

const commands = [
  'mcpcell open            # 打开 MCPcell 窗口（需要时先启动守护进程）',
  'mcpcell start           # 在后台启动守护进程',
  'mcpcell stop            # 停止守护进程',
  'mcpcell mode cli|gui    # 界面方式：命令行 / 图形界面，立即生效',
  'mcpcell ps              # 各个 MCP 开着几份、连着的对话、占多少内存',
  'mcpcell clients         # 各应用的 MCP，以及是否在用 MCPcell 这份',
  'mcpcell takeover        # 一键托管：先列出要改的内容，确认后执行',
  'mcpcell restore --all   # 全部改回原样（不需要守护进程）',
  'mcpcell autostart on    # 开机自启',
  'mcpcell shortcut on     # 创建桌面和开始菜单快捷方式',
  'mcpcell update <服务>   # 更新 npx 服务的 npm 包',
].join('\n');

export function SettingsPage({ diag }: { diag: boolean }) {
  const { status } = useStatus();
  const manual = useRoute().query.get('manual') === '1';
  const toast = useToast();
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  const [desk, setDesk] = useState<DesktopStatus | null>(null);
  const [mode, setMode] = useState<InterfaceStatus | null>(null);
  useEffect(() => {
    takeover.autostartStatus().then(setAuto).catch(() => setAuto(null));
    desktop.status().then(setDesk).catch(() => setDesk(null));
    uiMode.status().then(setMode).catch(() => setMode(null));
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
      <Body width={880}>
        <section>
          <GroupHeader title="常规" />
          <TableCard>
            {mode && <InterfaceRow status={mode} onChange={(m) => attempt(() => uiMode.set(m), m === 'cli' ? '已改为命令行' : '已改为图形界面', setMode)} />}
            <SettingRow
              title="开机自启"
              desc={auto?.supported === false ? auto.error : '登录 Windows 时在后台打开 MCPcell；图形界面方式下托盘里显示图标。应用在用 MCPcell 的 MCP 时建议开启，否则 MCPcell 没开着时这些 MCP 用不了。'}
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
                <SettingRow title="桌面窗口" desc={desk.webview ? '用独立窗口打开管理页；关闭窗口后 MCPcell 继续在后台运行。' : (desk.webviewError ?? '本机没有 WebView2 运行时')}>
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
            <InfoRow label="同时最多开">{status.maxInstances} 份 MCP；满了先关掉最久没用的</InfoRow>
            <InfoRow label="访问令牌">
              <span className="flex items-center gap-1">
                <code className="font-mono text-[13px]">{masked(status.token)}</code>
                <CopyButton text={status.token} label="复制访问令牌" />
                <span className="text-zinc-400">应用配置里用的 Bearer 令牌，不要分享</span>
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

        <ManualAgents open={manual} />

        <section>
          <GroupHeader title="命令行" />
          <CodeBlock shown={commands} copy={commands} />
        </section>

        <Diagnostics open={diag} />
      </Body>
    </>
  );
}

/** Scrolls a section into view when a link asked for it. */
function useReveal(open: boolean) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (open) ref.current?.scrollIntoView({ block: 'start' });
  }, [open]);
  return ref;
}

/** Agents MCPcell cannot edit: copy a config that points at its endpoints. */
function ManualAgents({ open }: { open: boolean }) {
  const { status } = useStatus();
  const [shown, setShown] = useState(open);
  const [picked, setPicked] = useState<string | null>(null);
  const ref = useReveal(open);
  const eps = status.services.filter((s) => !s.name.startsWith('run-')).map((s) => ({ name: s.name, url: `${status.baseUrl}/s/${s.name}` }));
  const list = manualClients.filter((c) => !agentOrder.includes(c.id));
  return (
    <section ref={ref}>
      <GroupHeader title="手动接入" count={list.length} hint="还不能自动接管的应用：复制配置，替换原来同名的一项后重开" open={shown} onToggle={() => setShown(!shown)} />
      {shown && (
        <TableCard>
          {list.map((c) => {
            const usable = c.http && eps.length > 0;
            const expanded = picked === c.id && usable;
            return (
              <div key={c.id}>
                <button
                  disabled={!usable}
                  onClick={() => setPicked(expanded ? null : c.id)}
                  className="flex w-full items-center gap-3.5 px-5 py-3 text-left transition-colors enabled:hover:bg-zinc-50 disabled:cursor-default dark:enabled:hover:bg-zinc-800/40"
                >
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-900">
                    <BrandMark brand={c.brand} className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{c.name}</span>
                      {!c.http && <Badge tone="amber">仅 stdio</Badge>}
                    </div>
                    <div className="mt-0.5 truncate text-xs text-zinc-400">
                      {c.file}
                      {c.note && ` · ${c.note}`}
                    </div>
                  </div>
                  {usable && <ChevronRight className={cx('size-4 text-zinc-400 transition-transform', expanded && 'rotate-90')} />}
                </button>
                {expanded && (
                  <div className="px-5 pb-4 pl-[66px]">
                    <CodeBlock shown={c.snippet!(eps, masked(status.token))} copy={c.snippet!(eps, status.token)} />
                    <p className="mt-2 text-xs text-zinc-400">包含 MCPcell 里全部 {eps.length} 个服务；复制的内容带完整访问令牌。</p>
                  </div>
                )}
              </div>
            );
          })}
        </TableCard>
      )}
    </section>
  );
}

const diagGrid = 'grid grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_96px_110px_110px] items-center gap-4';

const stateText = { cold: '未运行', starting: '启动中', ready: '运行中', failed: '失败' };

/** Raw state for troubleshooting: every connection and every service the
 * daemon runs, including the run- variants the lists fold into their MCP. */
function Diagnostics({ open }: { open: boolean }) {
  const { status } = useStatus();
  const skills = useSkillsShown();
  const [shown, setShown] = useState(open);
  const ref = useReveal(open);
  const sessions = [...status.sessions].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return (
    <section ref={ref}>
      <GroupHeader title="诊断" hint="原始的连接和服务列表，排查问题时用" open={shown} onToggle={() => setShown(!shown)} />
      {shown && (
        <div className="space-y-5">
          <div>
            <div className={cx(diagGrid, 'px-5 pb-2 text-xs text-zinc-400')}>
              <span>连接 · {sessions.length}</span>
              <span>服务</span>
              <span>协议</span>
              <span>连接于</span>
              <span>活动</span>
            </div>
            <TableCard>
              {sessions.length === 0 && <p className="px-5 py-4 text-sm text-zinc-400">没有连接</p>}
              {sessions.map((x) => (
                <div key={x.id} className={cx(diagGrid, 'px-5 py-2.5 text-[13px]')}>
                  <div className="flex min-w-0 items-center gap-2.5">
                    <BrandMark brand={brandOf(x.client)} className="size-4 shrink-0" />
                    <span className="truncate">{x.client || '未知客户端'}</span>
                    <span className="font-mono text-xs text-zinc-400">{x.id}</span>
                  </div>
                  <span className="truncate font-mono text-xs">{x.service}</span>
                  <span className="font-mono text-xs text-zinc-500">{x.protocolVersion}</span>
                  <span className="text-zinc-500 tabular-nums">{clock(x.createdAt)}</span>
                  <span className="flex items-center gap-2 text-zinc-500">
                    <Dot tone={x.inflight ? 'amber' : x.streaming ? 'green' : 'gray'} pulse={x.inflight > 0} />
                    {x.inflight ? `处理中 ${x.inflight}` : `${since(x.lastSeenAt)}前`}
                  </span>
                </div>
              ))}
            </TableCard>
          </div>
          <div>
            <div className={cx(diagGrid, 'px-5 pb-2 text-xs text-zinc-400')}>
              <span>服务 · {status.services.length}</span>
              <span>属于</span>
              <span>运行方式</span>
              <span>状态</span>
              <span>开着几份 · 内存</span>
            </div>
            <TableCard>
              {status.services.map((s) => {
                const runs = instancesOf(s);
                return (
                  <div key={s.name} className={cx(diagGrid, 'px-5 py-2.5 text-[13px]')}>
                    <span className="truncate font-mono text-xs">{s.name}</span>
                    <span className="truncate text-zinc-500">{s.of ? `${s.of} 的变体（条目改过）` : s.name.startsWith('run-') ? '包装的条目' : '—'}</span>
                    <span className="text-zinc-500">{s.mode === 'per-session' ? '每个对话一份' : '共用一份'}</span>
                    <span className="text-zinc-500">
                      {stateText[s.state]}
                      {s.failures ? ` · 失败 ${s.failures} 次` : ''}
                    </span>
                    <span className="text-zinc-500 tabular-nums">
                      {runs.length} · {bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}
                    </span>
                  </div>
                );
              })}
            </TableCard>
          </div>
          <TableCard>
            <SettingRow title="显示 Skill 页" desc="统一管理各应用的 Skill（暂时隐藏的功能）。打开后左侧出现「Skill」；mcpcell skills 命令不受影响。">
              <Switch checked={skills} label="显示 Skill 页" onChange={showSkills} />
            </SettingRow>
          </TableCard>
        </div>
      )}
    </section>
  );
}

/** GUI or command line: only the tray icon differs, and switching is
 * immediate. */
function InterfaceRow({ status, onChange }: { status: InterfaceStatus; onChange: (m: InterfaceMode) => void }) {
  const cli = status.interface === 'cli';
  return (
    <SettingRow
      title="界面方式"
      desc={
        <>
          两种方式的内核完全一样：接管、用到时才打开、记住工具列表、断线重连都相同，只是操作入口不同，切换立即生效。
          {cli ? (
            <>
              现在是命令行：不显示托盘图标，用 <code className="font-mono text-[12px]">mcpcell ps</code>、<code className="font-mono text-[12px]">clients</code>、
              <code className="font-mono text-[12px]">takeover</code>、<code className="font-mono text-[12px]">restore</code> 管理，需要时{' '}
              <code className="font-mono text-[12px]">mcpcell open</code> 打开这个窗口。
            </>
          ) : (
            ' 现在是图形界面：托盘图标常驻，点它打开这个窗口。'
          )}
          {!status.traySupported && ' 这个守护进程不显示托盘图标（以 --no-tray 运行）。'}
        </>
      }
    >
      <Segmented<InterfaceMode>
        value={status.interface}
        onChange={(m) => m !== status.interface && onChange(m)}
        options={[
          { value: 'gui', label: '图形界面' },
          { value: 'cli', label: '命令行' },
        ]}
      />
    </SettingRow>
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
