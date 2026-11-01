import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { autoApi, daemonApi, desktop, instancesOf, takeover, uiMode, type AutostartStatus, type DesktopStatus, type InterfaceMode, type InterfaceStatus } from '../api';
import { clients as manualClients, masked } from '../clients';
import { useStatus } from '../data';
import { bytes, clock, since } from '../format';
import { BrandMark, brandOf } from '../icons';
import { Body } from '../layout';
import { agentOrder } from '../model';
import { useRoute } from '../router';
import { Badge, Button, CodeBlock, CopyButton, cx, Dot, GroupHeader, Modal, Segmented, Switch, TableCard, useToast } from '../ui';

// Settings: one line per setting, its explanation in the tooltip.

const commands = [
  'mcpcell open            # 打开窗口',
  'mcpcell status          # 各个 MCP 的情况',
  'mcpcell takeover        # 一键托管',
  'mcpcell restore --all   # 全部改回原样',
  'mcpcell stop            # 停止后台服务',
  'mcpcell help --all      # 全部命令',
].join('\n');

export function SettingsPage({ diag }: { diag: boolean }) {
  const { status } = useStatus();
  const manual = useRoute().query.get('manual') === '1';
  const toast = useToast();
  const attempt = useAttempt();
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  const [autoOn, setAutoOn] = useState<boolean | null>(null);
  const [desk, setDesk] = useState<DesktopStatus | null>(null);
  const [mode, setMode] = useState<InterfaceStatus | null>(null);
  const [stopping, setStopping] = useState(false);
  const [stopBusy, setStopBusy] = useState(false);
  useEffect(() => {
    takeover.autostartStatus().then(setAuto).catch(() => setAuto(null));
    autoApi.status().then((s) => setAutoOn(s.enabled)).catch(() => setAutoOn(null));
    desktop.status().then(setDesk).catch(() => setDesk(null));
    uiMode.status().then(setMode).catch(() => setMode(null));
  }, []);

  return (
    <Body>
      <section>
        <GroupHeader title="常规" />
        <TableCard>
          <Row title="开机自启" tip={auto?.supported === false ? auto.error : '登录电脑时就在后台打开 MCPcell。关着也行：应用用到 MCP 时会自动打开它。'}>
            {auto?.supported && (
              <Switch checked={auto.enabled} label="开机自启" onChange={(on) => attempt(() => takeover.autostart(on), on ? '已开启开机自启' : '已关闭开机自启', setAuto)} />
            )}
          </Row>
          {autoOn !== null && (
            <Row title="自动托管新 MCP" tip="在 Claude Code、Codex、Cursor 里新加的 MCP 自动改用 MCPcell 共用的这份；打不开的、你改回原样的不动。">
              <Switch checked={autoOn} label="自动托管新 MCP" onChange={(on) => attempt(() => autoApi.set(on), on ? '已开启' : '已关闭', (s) => setAutoOn(s.enabled))} />
            </Row>
          )}
          {mode && <TrayRow status={mode} onChange={(m) => attempt(() => uiMode.set(m), m === 'cli' ? '已关掉托盘图标' : '已显示托盘图标', setMode)} />}
          {desk?.supported && (
            <>
              <Row title="桌面快捷方式">
                <Switch
                  checked={desk.desktopShortcut}
                  label="桌面快捷方式"
                  onChange={(on) => attempt(() => desktop.set({ desktopShortcut: on }), on ? '已创建' : '已删除', setDesk)}
                />
              </Row>
              <Row title="开始菜单">
                <Switch
                  checked={desk.startMenuShortcut}
                  label="开始菜单"
                  onChange={(on) => attempt(() => desktop.set({ startMenuShortcut: on }), on ? '已添加' : '已移除', setDesk)}
                />
              </Row>
            </>
          )}
        </TableCard>
      </section>

      <section>
        <GroupHeader title="后台服务" />
        <TableCard>
          <Info label="状态">
            <span className="inline-flex items-center gap-2">
              <Dot tone="green" />
              运行中 · {since(status.startedAt)} · v{status.version}
            </span>
          </Info>
          <Info label="地址">
            <span className="font-mono" title="只接受本机连接">
              127.0.0.1:{status.port}
            </span>
          </Info>
          <Info label="内存">
            <span title={`PID ${status.pid}，工作集 ${bytes(status.daemon.workingSetBytes)}`}>{bytes(status.daemon.privateBytes)}</span>
          </Info>
          <Info label="令牌">
            <span className="flex items-center gap-1" title="手动接入其他应用时用，不要分享">
              <code className="font-mono text-xs">{masked(status.token)}</code>
              <CopyButton text={status.token} label="复制令牌" />
            </span>
          </Info>
          <Info label="配置">
            <span className="flex min-w-0 items-center gap-1">
              <code className="truncate font-mono text-xs" title={status.configPath}>
                {status.configPath}
              </code>
              <CopyButton text={status.configPath} label="复制路径" />
            </span>
          </Info>
          <Row title="停止后台服务" tip="一般不需要：关掉窗口、从托盘退出都不会停止它。">
            <button onClick={() => setStopping(true)} className="rounded-md px-2 py-1 text-[13px] font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950">
              停止
            </button>
          </Row>
        </TableCard>
      </section>
      <Modal open={stopping} onClose={() => !stopBusy && setStopping(false)} width={400}>
        <h2 className="text-[15px] font-semibold">停止后台服务？</h2>
        <p className="mt-2 text-[13px] leading-6 text-zinc-600 dark:text-zinc-300">正在用的 MCP 会改回各开各的，直到 MCPcell 再次打开。</p>
        <div className="mt-5 flex justify-end gap-2">
          <Button size="sm" disabled={stopBusy} onClick={() => setStopping(false)}>
            取消
          </Button>
          <Button
            size="sm"
            variant="danger"
            busy={stopBusy}
            onClick={async () => {
              setStopBusy(true);
              try {
                await daemonApi.shutdown();
                toast('后台服务正在停止');
              } catch (e) {
                toast((e as Error).message, 'error');
              } finally {
                setStopBusy(false);
                setStopping(false);
              }
            }}
          >
            停止
          </Button>
        </div>
      </Modal>

      <section>
        <GroupHeader title="命令行" />
        <CodeBlock shown={commands} copy={commands} />
      </section>

      <ManualAgents open={manual} />
      <Diagnostics open={diag} />
    </Body>
  );
}

/** Runs a settings change and says how it went. */
function useAttempt() {
  const toast = useToast();
  return async function attempt<T>(task: () => Promise<T>, done: string, set: (v: T) => void) {
    try {
      set(await task());
      toast(done);
    } catch (e) {
      toast((e as Error).message, 'error');
    }
  };
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
      <GroupHeader title="手动接入" count={list.length} open={shown} onToggle={() => setShown(!shown)} />
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
                  className="flex w-full items-center gap-3 px-4 py-2 text-left transition-colors enabled:hover:bg-zinc-50 disabled:cursor-default dark:enabled:hover:bg-zinc-800/40"
                >
                  <BrandMark brand={c.brand} className="size-4 shrink-0" />
                  <span className="text-[13px] font-medium">{c.name}</span>
                  {!c.http && <Badge tone="amber">仅 stdio</Badge>}
                  <span className="min-w-0 flex-1 truncate text-xs text-zinc-400" title={c.note}>
                    {c.file}
                  </span>
                  {usable && <ChevronRight className={cx('size-4 text-zinc-400 transition-transform', expanded && 'rotate-90')} />}
                </button>
                {expanded && (
                  <div className="px-4 pb-3 pl-11">
                    <CodeBlock shown={c.snippet!(eps, masked(status.token))} copy={c.snippet!(eps, status.token)} />
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

const diagGrid = 'grid grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)_88px_64px_72px] items-center gap-3';

const stateText = { cold: '未运行', starting: '启动中', ready: '运行中', failed: '失败' };

/** Raw state for troubleshooting: every connection and every service the
 * daemon runs, including the run- variants the lists fold into their MCP. */
function Diagnostics({ open }: { open: boolean }) {
  const { status } = useStatus();
  const [shown, setShown] = useState(open);
  const ref = useReveal(open);
  const sessions = [...status.sessions].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return (
    <section ref={ref}>
      <GroupHeader title="诊断" open={shown} onToggle={() => setShown(!shown)} />
      {shown && (
        <div className="space-y-3">
          <TableCard>
            <div className={cx(diagGrid, 'px-4 py-2 text-xs text-zinc-400')}>
              <span>连接 · {sessions.length}</span>
              <span>服务</span>
              <span>协议</span>
              <span>连上</span>
              <span>活动</span>
            </div>
            {sessions.map((x) => (
              <div key={x.id} className={cx(diagGrid, 'px-4 py-2 text-xs')}>
                <div className="flex min-w-0 items-center gap-2">
                  <BrandMark brand={brandOf(x.client)} className="size-3.5 shrink-0" />
                  <span className="truncate">{x.client || '未知'}</span>
                  <span className="font-mono text-zinc-400">{x.id}</span>
                </div>
                <span className="truncate font-mono">{x.service}</span>
                <span className="font-mono text-zinc-500">{x.protocolVersion}</span>
                <span className="text-zinc-500 tabular-nums">{clock(x.createdAt)}</span>
                <span className="flex items-center gap-1.5 text-zinc-500">
                  <Dot tone={x.inflight ? 'amber' : x.streaming ? 'green' : 'gray'} pulse={x.inflight > 0} />
                  {x.inflight ? `处理中 ${x.inflight}` : since(x.lastSeenAt)}
                </span>
              </div>
            ))}
          </TableCard>
          <TableCard>
            <div className={cx(diagGrid, 'px-4 py-2 text-xs text-zinc-400')}>
              <span>服务 · {status.services.length}</span>
              <span>属于</span>
              <span>方式</span>
              <span>状态</span>
              <span>份数 · 内存</span>
            </div>
            {status.services.map((s) => {
              const runs = instancesOf(s);
              return (
                <div key={s.name} className={cx(diagGrid, 'px-4 py-2 text-xs')}>
                  <span className="truncate font-mono">{s.name}</span>
                  <span className="truncate text-zinc-500">{s.of ?? (s.name.startsWith('run-') ? '包装的条目' : '—')}</span>
                  <span className="text-zinc-500">{s.mode === 'per-session' ? '每对话一份' : '共用'}</span>
                  <span className="text-zinc-500" title={s.failures ? `失败 ${s.failures} 次` : undefined}>
                    {stateText[s.state]}
                  </span>
                  <span className="text-zinc-500 tabular-nums">
                    {runs.length} · {bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}
                  </span>
                </div>
              );
            })}
          </TableCard>
        </div>
      )}
    </section>
  );
}

/** The interface setting (gui or cli), shown as what it decides: whether
 * the tray icon runs. Switching is immediate. */
function TrayRow({ status, onChange }: { status: InterfaceStatus; onChange: (m: InterfaceMode) => void }) {
  return (
    <Row title="托盘图标" tip={status.error ?? '只决定要不要托盘图标；后台服务和 MCP 不受影响。从托盘退出只关托盘和窗口。'}>
      <Segmented<InterfaceMode>
        value={status.interface}
        onChange={(m) => m !== status.interface && onChange(m)}
        options={[
          { value: 'gui', label: '显示' },
          { value: 'cli', label: '不显示' },
        ]}
      />
    </Row>
  );
}

function Row({ title, tip, children }: { title: string; tip?: string; children?: ReactNode }) {
  return (
    <div className="flex min-h-11 items-center gap-6 px-4 py-1.5" title={tip}>
      <div className="min-w-0 flex-1 text-[13px] font-medium">{title}</div>
      {children}
    </div>
  );
}

function Info({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-10 items-center gap-4 px-4 py-1.5 text-[13px]">
      <div className="w-12 shrink-0 text-zinc-500">{label}</div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
