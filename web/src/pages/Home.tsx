import { useState, type ReactNode } from 'react';
import { ChevronRight, Copy, Layers, Plus, RefreshCw, Undo2 } from 'lucide-react';
import { autoApi, instancesOf, takeover, type AgentSummary, type AutoStatus, type McpRow } from '../api';
import { agentsOf, canTake, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { useStatus, useUnified } from '../data';
import { bytes } from '../format';
import { ServiceAvatar } from '../icons';
import { kindRank, mcpView, type McpView } from '../model';
import { candidates, OneClickDialog } from '../one-click';
import { go, href } from '../router';
import { Button, Card, cx, Dot, Menu, PageHeader, useCopy, useToast } from '../ui';

// The home page: every MCP, what it is for, which apps use MCPcell's copy,
// and whether it is 待命, 运行中 or 打不开. Problems come first.

const grid = 'grid grid-cols-[minmax(0,1fr)_minmax(0,280px)_minmax(0,230px)] items-center gap-6';

export function HomePage() {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const copy = useCopy();
  const toast = useToast();
  const [oneClick, setOneClick] = useState(false);
  const [showOther, setShowOther] = useState(false);

  const agents = data ? agentsOf(data.agents) : [];
  const views = data ? data.rows.map((r) => mcpView(r, status, data.auto)).sort((a, b) => kindRank[a.kind] - kindRank[b.kind] || a.row.name.localeCompare(b.row.name)) : [];
  const main = views.filter((v) => v.kind !== 'other');
  const other = views.filter((v) => v.kind === 'other');
  const broken = new Set(views.filter((v) => v.kind === 'broken').map((v) => v.row.key));
  const offers = data ? candidates(data).filter((c) => !broken.has(c.row.key)).length : 0;
  const allRestore = data ? data.rows.flatMap((r) => restoreSteps(r, agents)) : [];

  return (
    <>
      <PageHeader
        icon={<Layers className="size-5 text-zinc-500" />}
        title="我的 MCP"
        actions={
          <>
            {offers > 0 && (
              <Button variant="primary" icon={<Layers className="size-4" />} onClick={() => setOneClick(true)}>
                一键托管
              </Button>
            )}
            <Button icon={<Plus className="size-4" />} onClick={() => go(['mcp', 'new'])}>
              添加
            </Button>
            <Menu
              items={[
                { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                { label: '复制访问令牌', hint: '手动接入其他应用时要用', icon: <Copy className="size-4" />, onSelect: () => copy(status.token, '已复制访问令牌') },
                'sep',
                {
                  label: '全部改回原样',
                  hint: '各个应用恢复成自己开 MCP',
                  icon: <Undo2 className="size-4" />,
                  danger: true,
                  disabled: allRestore.length === 0,
                  onSelect: () => acts.restore(allRestore, `全部 ${allRestore.length} 项改回原样`),
                },
              ]}
            />
          </>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[1180px] space-y-5 px-8 pt-6 pb-12">
          <Summary views={views} agents={agents} />
          {data?.auto && <AutoNews auto={data.auto} agents={agents} onAck={async () => (await autoApi.ack().catch(() => undefined), reload())} />}
          {data && <Hints views={main} offers={offers} data={data} onTake={() => setOneClick(true)} onAuto={async () => {
            try {
              await takeover.autostart(true);
              toast('已开启开机自启');
              reload();
            } catch (e) {
              toast((e as Error).message, 'error');
            }
          }} />}
          {error && <p className="text-sm text-red-600">{error}</p>}
          {!data && !error && <p className="py-16 text-center text-sm text-zinc-400">正在读取各个应用的 MCP…</p>}
          {data && (
            <section>
              <div className={cx(grid, 'px-5 pb-2 text-xs text-zinc-400')}>
                <span>MCP</span>
                <span>哪些应用在用</span>
                <span>状态</span>
              </div>
              <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
                {main.map((v) => (
                  <Row key={v.id} view={v} agents={agents} acts={acts} />
                ))}
                {main.length === 0 && <p className="px-5 py-8 text-center text-sm text-zinc-400">各个应用里还没有配置 MCP。点「添加」在 MCPcell 里加一个。</p>}
                {other.length > 0 && (
                  <button
                    onClick={() => setShowOther(!showOther)}
                    className="flex w-full items-center gap-2 bg-zinc-50/60 px-5 py-2.5 text-left text-[13px] text-zinc-500 hover:text-zinc-800 dark:bg-zinc-900/60 dark:hover:text-zinc-200"
                  >
                    <ChevronRight className={cx('size-3.5 transition-transform', showOther && 'rotate-90')} />
                    另外 {other.length} 个不用管：{other.map((v) => v.row.name).join('、')}
                    <span className="text-zinc-400">（远程服务或应用自带，本机不用开）</span>
                  </button>
                )}
                {showOther && other.map((v) => <Row key={v.id} view={v} agents={agents} acts={acts} />)}
              </Card>
              <p className="mt-3 px-1 text-xs leading-5 text-zinc-400">
                <Dot tone="gray" /> <span className="ml-1">待命：现在没人在用，不占内存；应用用到它时会自动打开，一段时间没人用就自动关掉。</span>
              </p>
            </section>
          )}
        </div>
      </div>
      {data && <OneClickDialog open={oneClick} onClose={() => setOneClick(false)} data={data} reload={reload} />}
      {acts.dialog}
    </>
  );
}

/** One sentence on what MCPcell is doing for the apps, and what it costs. */
function Summary({ views, agents }: { views: McpView[]; agents: AgentSummary[] }) {
  const { status, offline } = useStatus();
  const users = agents.filter((a) => a.managed > 0).map((a) => a.name);
  const mcpBytes = status.services.flatMap(instancesOf).reduce((n, i) => n + i.usage.privateBytes, 0);
  const total = bytes(status.daemon.privateBytes + mcpBytes);
  const running = views.filter((v) => v.kind === 'running').length;
  const apps = users.length <= 2 ? users.join(' 和 ') : `${users.slice(0, -1).join('、')} 和 ${users[users.length - 1]}`;
  return (
    <Card className="px-6 py-5">
      {offline ? (
        <p className="text-[15px] font-medium text-red-600">MCPcell 没有响应，下面是最后一次看到的情况。</p>
      ) : users.length ? (
        <p className="text-[15px] leading-7">
          <span className="font-semibold">{apps}</span> 共用这些 MCP：同一个 MCP 全机只开一份，现在一共占{' '}
          <span className="font-semibold tabular-nums" title={`MCPcell 自己 ${bytes(status.daemon.privateBytes)}，正在运行的 MCP ${mcpBytes ? bytes(mcpBytes) : '0 MB'}`}>
            {total}
          </span>{' '}
          内存{running ? `，${running} 个正在运行，其余待命` : '，都在待命'}。
        </p>
      ) : (
        <p className="text-[15px] leading-7">
          还没有应用在用 MCPcell。点「一键托管」，各个应用就共用同一份 MCP：同一个 MCP 全机只开一份，不用的时候不占内存。
        </p>
      )}
    </Card>
  );
}

/** What automatic takeover did since the user last looked. */
function AutoNews({ auto, agents, onAck }: { auto: AutoStatus; agents: AgentSummary[]; onAck: () => void }) {
  const fresh = (auto.events ?? []).filter((e) => !e.seen);
  if (!fresh.length) return null;
  const name = (id: string) => agents.find((a) => a.id === id)?.name ?? id;
  const taken = [...new Map(fresh.filter((e) => e.ok).map((e) => [e.service ?? e.name, e])).values()];
  const stopped = fresh.filter((e) => !e.ok && e.error?.includes('停止自动托管'));
  const failed = fresh.filter((e) => !e.ok && !e.error?.includes('停止自动托管'));
  const link = (e: { service?: string; name: string }) => (
    <a key={e.service ?? e.name} href={href(['mcp', e.service ?? e.name])} className="font-medium text-blue-700 hover:underline dark:text-blue-300">
      {e.service ?? e.name}
    </a>
  );
  const join = (items: ReactNode[]) => items.flatMap((x, i) => (i ? ['、', x] : [x]));
  return (
    <div className="flex items-start gap-3 rounded-xl border border-blue-200/70 bg-blue-50/60 px-5 py-3 text-[13px] leading-6 text-zinc-700 dark:border-blue-900/60 dark:bg-blue-950/20 dark:text-zinc-200">
      <div className="min-w-0 flex-1 space-y-0.5">
        {taken.length > 0 && (
          <div>
            新发现 {taken.length} 个 MCP，已自动托管：{join(taken.map(link))}。想改回原样，点开它在「哪些应用在用」里改。
          </div>
        )}
        {failed.length > 0 && <div>新发现的 {join(failed.map((e) => e.name))} 打不开，没有托管，原因写在列表里。</div>}
        {stopped.map((e) => (
          <div key={e.client + e.name}>
            {name(e.client)} 一直把 {e.name} 改回原来的设置，已停止自动托管它。
          </div>
        ))}
      </div>
      <button onClick={onAck} className="shrink-0 font-medium text-blue-600 hover:underline dark:text-blue-400">
        知道了
      </button>
    </div>
  );
}

/** The few things worth doing now, in one strip. */
function Hints({ views, offers, data, onTake, onAuto }: { views: McpView[]; offers: number; data: { autostart: { supported: boolean; enabled: boolean } }; onTake: () => void; onAuto: () => void }) {
  const broken = views.filter((v) => v.kind === 'broken');
  const native = views.filter((v) => v.kind === 'native' && v.takeable);
  const changed = views.filter((v) => Object.values(v.row.agents).some((c) => c.state === 'changed'));
  const managed = views.some((v) => v.managed);
  const lines: { tone: 'red' | 'amber' | 'blue'; text: ReactNode; action?: ReactNode }[] = [];
  if (broken.length) lines.push({ tone: 'red', text: `${broken.map((v) => v.row.name).join('、')} 打不开，原因写在状态里，点开看怎么处理。` });
  if (offers)
    lines.push({
      tone: 'blue',
      text: native.length
        ? `${native.map((v) => v.row.name).join('、')} 还是各个应用自己开着，每开一个对话就多开一份。`
        : '有的应用还自己开着已有的 MCP，每开一个对话就多开一份。',
      action: (
        <button onClick={onTake} className="shrink-0 font-medium text-blue-600 hover:underline dark:text-blue-400">
          一键托管
        </button>
      ),
    });
  if (changed.length) lines.push({ tone: 'amber', text: `${changed.map((v) => v.row.name).join('、')} 在应用里被改过，MCPcell 不再管这几项，点开看看。` });
  if (managed && data.autostart.supported && !data.autostart.enabled)
    lines.push({
      tone: 'amber',
      text: '开机自启没开：电脑重启后要先打开 MCPcell，这些 MCP 才能用。',
      action: (
        <button onClick={onAuto} className="shrink-0 font-medium text-blue-600 hover:underline dark:text-blue-400">
          开启
        </button>
      ),
    });
  if (!lines.length) return null;
  return (
    <div className="space-y-1.5 rounded-xl border border-amber-200/70 bg-amber-50/60 px-5 py-3 dark:border-amber-900/60 dark:bg-amber-950/20">
      {lines.map((l, i) => (
        <div key={i} className="flex items-start gap-3 text-[13px] leading-6 text-zinc-700 dark:text-zinc-200">
          <span className="pt-2">
            <Dot tone={l.tone} />
          </span>
          <span className="min-w-0 flex-1">{l.text}</span>
          {l.action}
        </div>
      ))}
    </div>
  );
}

function Row({ view: v, agents, acts }: { view: McpView; agents: AgentSummary[]; acts: ReturnType<typeof useAgentActions> }) {
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => go(['mcp', v.id])}
      onKeyDown={(e) => e.key === 'Enter' && go(['mcp', v.id])}
      className={cx(grid, 'group cursor-pointer px-5 py-3.5 transition-colors hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40')}
    >
      <div className="flex min-w-0 items-center gap-3.5">
        <ServiceAvatar name={v.row.name} size="sm" />
        <div className="min-w-0">
          <a href={href(['mcp', v.id])} onClick={(e) => e.stopPropagation()} className="block truncate text-[15px] font-medium hover:text-blue-600">
            {v.row.name}
          </a>
          <div className="mt-0.5 truncate text-[13px] text-zinc-500" title={v.purpose}>
            {v.purpose || <span className="font-mono text-xs text-zinc-400">{v.row.launch}</span>}
          </div>
        </div>
      </div>
      <Apps row={v.row} agents={agents} acts={acts} offerAdd={v.kind !== 'broken'} />
      <div className={cx('flex min-w-0 items-center gap-2 text-[13px]', v.tone === 'red' ? 'text-red-600 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-300')} title={v.broken?.lastError ?? v.text}>
        <Dot tone={v.tone} pulse={v.kind === 'running' && v.tone === 'green' && v.sessions.some((x) => x.inflight > 0)} />
        <span className="truncate">{v.text}</span>
      </div>
    </div>
  );
}

const shape = 'h-6 shrink-0 items-center rounded-md px-2 text-xs whitespace-nowrap';
const tag = `inline-flex ${shape}`;

/** Which apps use MCPcell's copy (green), and which could (dashed +). */
export function Apps({ row, agents, acts, offerAdd }: { row: McpRow; agents: AgentSummary[]; acts: ReturnType<typeof useAgentActions>; offerAdd: boolean }) {
  // Apps that lack it only offer "+ app" on hover, after the others.
  const missing = offerAdd && row.service && row.canAdd ? agents.filter((a) => !row.agents[a.id]) : [];
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
      {agents.map((a) => {
        const c = row.agents[a.id];
        if (!c) return null;
        if (c.state === 'managed') {
          const off = !c.enabled;
          return (
            <span
              key={a.id}
              title={off ? `${a.name} 里关掉了它` : c.drift ? `${a.name} 在用 MCPcell 这份；它在 ${a.name} 里的设置改过，所以单独开了一份` : `${a.name} 在用 MCPcell 共用的这份`}
              className={cx(tag, off ? 'bg-zinc-100 text-zinc-400 line-through dark:bg-zinc-800' : 'bg-emerald-50 text-emerald-700 ring-1 ring-emerald-200 ring-inset dark:bg-emerald-950/50 dark:text-emerald-300 dark:ring-emerald-900')}
            >
              {a.name}
              {c.drift && !off && <span className="ml-1 size-1.5 rounded-full bg-amber-500" />}
            </span>
          );
        }
        if (c.state === 'changed') {
          return (
            <span key={a.id} title={`${a.name} 里的这一项被改过，现在不用 MCPcell 这份`} className={cx(tag, 'bg-amber-50 text-amber-700 ring-1 ring-amber-200 ring-inset dark:bg-amber-950/40 dark:text-amber-300 dark:ring-amber-900')}>
              {a.name} · 改过
            </span>
          );
        }
        if (canTake(c)) {
          return (
            <button
              key={a.id}
              title={`${a.name} 现在自己开一份；点一下改为用 MCPcell 共用的这份`}
              onClick={() => acts.takeover(takeSteps(row, [a]))}
              className={cx(tag, 'border border-dashed border-zinc-300 text-zinc-600 hover:border-blue-400 hover:bg-blue-50 hover:text-blue-700 dark:border-zinc-600 dark:text-zinc-300 dark:hover:bg-blue-950/40')}
            >
              + {a.name}
            </button>
          );
        }
        return (
          <span key={a.id} title={c.state === 'remote' ? `${a.name} 直接连这个远程服务` : `${a.name} 自己开着：${c.reason ?? ''}`} className={cx(tag, 'bg-zinc-100 text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400', !c.enabled && 'line-through')}>
            {a.name}
          </span>
        );
      })}
      {missing.map((a) => (
        <button
          key={a.id}
          title={`把 ${row.name} 加到 ${a.name}`}
          onClick={() => acts.add(a, row)}
          className={cx(shape, 'hidden border border-dashed border-zinc-300 text-zinc-500 group-hover:inline-flex hover:border-blue-400 hover:text-blue-600 dark:border-zinc-600')}
        >
          + {a.name}
        </button>
      ))}
    </div>
  );
}
