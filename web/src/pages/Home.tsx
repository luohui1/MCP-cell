import { useState, type ReactNode } from 'react';
import { ChevronDown, Info, Layers, Package, Play, Plus, RefreshCw, RotateCw, Square, Undo2 } from 'lucide-react';
import { autoApi, instancesOf, type AgentSummary, type AutoStatus, type McpRow } from '../api';
import { useServiceActions } from '../actions';
import { agentBrand, agentsOf, appOf, appsOf, canTake, isProject, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { useStatus, useUnified } from '../data';
import { bytes, since } from '../format';
import { BrandMark, ServiceAvatar } from '../icons';
import { Body, Toolbar } from '../layout';
import { kindRank, mcpView, type McpView } from '../model';
import { candidates, OneClickDialog } from '../one-click';
import { go, href, useRoute } from '../router';
import { StatStrip, useNow } from '../stats';
import { agentName } from '../timeline';
import { Button, Card, cx, Dot, IconButton, Menu, Segmented, Switch } from '../ui';

// The MCP page: the app filter, four numbers, then every MCP in one card.
// A row says what the MCP is for, which apps have it, how it is doing, and
// has a switch that hands it to MCPcell or gives it back. Explanations live
// in tooltips, not on the page. An app here is Claude Code with its
// projects' servers too (agents "claude-code@<dir>"), shown as one app.

type Acts = ReturnType<typeof useAgentActions>;

export function HomePage() {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const svcActs = useServiceActions();
  const now = useNow();
  const only = useRoute().query.get('agent') ?? '';
  const [oneClick, setOneClick] = useState(false);
  const [showOther, setShowOther] = useState(false);

  const agents = data ? agentsOf(data.agents) : [];
  const apps = appsOf(agents);
  const app = apps.includes(only) ? only : '';
  const scope = app ? agents.filter((a) => appOf(a.id) === app) : agents;
  const rows = data ? data.rows.filter((r) => !app || scope.some((a) => r.agents[a.id])) : [];
  const views = data ? rows.map((r) => mcpView(r, status, data.auto)).sort((a, b) => kindRank[a.kind] - kindRank[b.kind] || a.row.name.localeCompare(b.row.name)) : [];
  const main = views.filter((v) => v.kind !== 'other');
  const other = views.filter((v) => v.kind === 'other');
  const broken = new Set(views.filter((v) => v.kind === 'broken').map((v) => v.row.key));
  const offered = data ? candidates(data, app || undefined).filter((c) => !broken.has(c.row.key)) : [];
  const allRestore = rows.flatMap((r) => restoreSteps(r, scope));

  return (
    <>
      <Body>
        <Toolbar
          actions={
            <>
              {offered.length > 0 && (
                <Button size="sm" variant="primary" icon={<Layers className="size-3.5" />} onClick={() => setOneClick(true)}>
                  一键托管
                </Button>
              )}
              <IconButton label="添加 MCP" onClick={() => go(['mcp', 'new'])}>
                <Plus className="size-4" />
              </IconButton>
              <Menu
                items={[
                  { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                  'sep',
                  {
                    label: app ? `${agentName(app)} 全部改回原样` : '全部改回原样',
                    icon: <Undo2 className="size-4" />,
                    danger: true,
                    disabled: allRestore.length === 0,
                    onSelect: () => acts.restore(allRestore, `${app ? agentName(app) + ' 的' : '全部'} ${allRestore.length} 项改回原样`),
                  },
                ]}
              />
            </>
          }
        >
          {apps.length > 1 && (
            <Segmented
              value={app}
              onChange={(id) => go([], { agent: id || undefined })}
              options={[
                { value: '', label: '全部' },
                ...apps.map((id) => ({
                  value: id,
                  label: (
                    <>
                      <BrandMark brand={agentBrand[id]} className="size-3.5" />
                      {agentName(id)}
                    </>
                  ),
                })),
              ]}
            />
          )}
        </Toolbar>
        <StatStrip now={now} />
        {data?.auto && <AutoNews auto={data.auto} onAck={async () => (await autoApi.ack().catch(() => undefined), reload())} />}
        {data && <Hints views={main} offered={offered.map((c) => c.row.name)} onTake={() => setOneClick(true)} />}
        {error && <p className="text-[13px] text-red-600">{error}</p>}
        {!data && !error && <p className="py-12 text-center text-[13px] text-zinc-400">正在读取…</p>}
        {data && (
          <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
            {main.map((v) => (
              <McpRowView key={v.id} view={v} agents={agents} scope={scope} acts={acts} svcActs={svcActs} />
            ))}
            {main.length === 0 && <p className="px-4 py-8 text-center text-[13px] text-zinc-400">还没有 MCP</p>}
            {showOther && other.map((v) => <McpRowView key={v.id} view={v} agents={agents} scope={scope} acts={acts} svcActs={svcActs} />)}
            <div className="flex items-center gap-3 px-4 py-2 text-[13px]">
              <a href={href(['mcp', 'new'])} className="flex items-center gap-1 text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
                <Plus className="size-3.5" />
                添加 MCP
              </a>
              {other.length > 0 && (
                <button
                  onClick={() => setShowOther(!showOther)}
                  title="远程服务和应用自带的 MCP 不在本机开，不用管"
                  className="ml-auto flex items-center gap-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
                >
                  {showOther ? '收起' : `另外 ${other.length} 个远程或自带`}
                  <ChevronDown className={cx('size-3.5 transition-transform', showOther && 'rotate-180')} />
                </button>
              )}
            </div>
          </Card>
        )}
      </Body>
      {data && <OneClickDialog open={oneClick} onClose={() => setOneClick(false)} data={data} only={app || undefined} reload={reload} />}
      {acts.dialog}
      {svcActs.deleteDialog}
    </>
  );
}

/** A row's status in a word or two, and the details for its tooltip. */
function statusOf(v: McpView): { tone: McpView['tone']; text: string; tip: string } {
  const conv = new Set(v.sessions.map((s) => (s.agentPid ? `p${s.agentPid}` : `s${s.id}`))).size;
  const calls = v.services.reduce((n, s) => n + (s.calls ?? 0), 0);
  const last = v.services.map((s) => s.lastUsedAt).filter((t): t is string => !!t).sort().at(-1);
  const facts = [conv ? `${conv} 个对话在用` : '', calls ? `调用 ${calls} 次` : '', last && !conv ? `${since(last)}前用过` : ''].filter(Boolean).join('，');
  switch (v.kind) {
    case 'running': {
      const used = v.services.flatMap(instancesOf).reduce((n, i) => n + i.usage.privateBytes, 0);
      return v.tone === 'amber' ? { tone: 'amber', text: '打开中', tip: '' } : { tone: 'green', text: bytes(used), tip: `运行中${facts ? `，${facts}` : ''}` };
    }
    case 'standby':
      return { tone: 'gray', text: '待命', tip: `没人用，不占内存；用到时自动打开${facts ? `。${facts}` : ''}` };
    case 'broken':
      return { tone: v.tone, text: v.tone === 'red' ? '打不开' : '设置被删', tip: v.text };
    case 'native':
      return { tone: 'gray', text: '各开各的', tip: '各个应用自己开，每开一个对话就多开一份' };
  }
  return { tone: 'gray', text: v.row.kind === 'http' ? '远程' : '自带', tip: v.text };
}

function McpRowView({
  view: v,
  agents,
  scope,
  acts,
  svcActs,
}: {
  view: McpView;
  agents: AgentSummary[];
  scope: AgentSummary[]; // the agents of the app shown, or all of them
  acts: Acts;
  svcActs: ReturnType<typeof useServiceActions>;
}) {
  const { status } = useStatus();
  const { row } = v;
  const take = takeSteps(row, scope);
  const back = restoreSteps(row, scope);
  const on = scope.some((a) => row.agents[a.id]?.state === 'managed');
  const svc = status.services.find((s) => s.name === row.service);
  const running = v.services.some((s) => instancesOf(s).length > 0);
  const busy = !!svc && (svcActs.busy[svc.name] || svc.state === 'starting');
  const missing = row.service && row.canAdd && v.kind !== 'broken' ? agents.filter((a) => !isProject(a.id) && !row.agents[a.id]) : [];
  const st = statusOf(v);
  const reason = v.kind === 'broken' ? v.text.replace(/^打不开：/, '') : '';
  const stop = () => v.services.forEach((s) => instancesOf(s).length && svcActs.stop(s.name));

  const menu = [
    { label: '详情', icon: <Info className="size-4" />, onSelect: () => go(['mcp', v.id]) },
    ...(svc && running ? [{ label: '现在关掉', icon: <Square className="size-4" />, onSelect: stop }] : []),
    ...(svc?.package ? [{ label: `更新（现在 ${svc.package.version}）`, icon: <Package className="size-4" />, onSelect: () => svcActs.update(svc.name) }] : []),
    ...missing.map((a) => ({ label: `加到 ${a.name}`, icon: <Plus className="size-4" />, onSelect: () => acts.add(a, row) })),
    ...(back.length ? ['sep' as const, { label: '改回原样', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back, `${row.name} 改回原样`) }] : []),
  ];

  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => go(['mcp', v.id])}
      onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && go(['mcp', v.id])}
      className="group grid cursor-pointer grid-cols-[minmax(0,1fr)_auto_76px_auto] items-center gap-4 px-4 py-2.5 transition-colors hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40"
    >
      <div className="flex min-w-0 items-center gap-3">
        <ServiceAvatar name={row.name} size="row" />
        <div className="min-w-0">
          <div className="truncate text-[13.5px] font-medium">{row.name}</div>
          <div className={cx('truncate text-xs', reason ? 'text-red-600 dark:text-red-400' : 'text-zinc-500')} title={v.broken?.lastError ?? v.purpose}>
            {reason || v.purpose || <span className="font-mono text-[11px] text-zinc-400">{row.launch}</span>}
          </div>
        </div>
      </div>

      <AppMarks row={row} agents={scope} acts={acts} />

      <div className={cx('flex items-center justify-end gap-1.5 text-xs tabular-nums', st.tone === 'red' ? 'text-red-600 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-300')} title={st.tip}>
        <Dot tone={st.tone} pulse={v.kind === 'running' && v.sessions.some((x) => x.inflight > 0)} />
        <span className="truncate">{st.text}</span>
      </div>

      <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
        <div className={cx('flex items-center transition-opacity', v.kind !== 'broken' && 'opacity-0 group-hover:opacity-100 focus-within:opacity-100')}>
          {v.kind === 'broken' && v.broken ? (
            <IconButton label="再试一次" busy={busy} onClick={() => svcActs.start(v.broken!.name)}>
              <RotateCw className="size-3.5" />
            </IconButton>
          ) : svc && running ? (
            <IconButton label="现在关掉" busy={svcActs.busy[svc.name]} onClick={stop}>
              <Square className="size-3" />
            </IconButton>
          ) : svc ? (
            <IconButton label="打开一次试试" busy={busy} onClick={() => svcActs.start(svc.name)}>
              <Play className="size-3.5" />
            </IconButton>
          ) : (
            <span className="size-8" />
          )}
          <Menu items={menu} />
        </div>
        <div className="flex w-[38px] justify-end">
          {v.kind !== 'other' && (on || take.length > 0) && (
            <Switch
              checked={on}
              label={`${row.name} 用 MCPcell 共用的这份`}
              title={on ? '开：用 MCPcell 共用的这份；关掉改回各开各的' : '关：各开各的；打开改用 MCPcell 共用的这份'}
              onChange={(next) => (next ? acts.takeover(take) : acts.restore(back, `${row.name} 改回原样`))}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/** A cell's state for a tooltip line. */
function cellText(c: McpRow['agents'][string]): string {
  switch (c.state) {
    case 'managed':
      return !c.enabled ? '关着' : c.drift ? '共用（设置改过，单独一份）' : '共用';
    case 'changed':
      return '被改过，不用 MCPcell 这份';
    case 'remote':
      return '远程';
    case 'internal':
      return '自带';
  }
  return canTake(c) ? '各开各的' : `各开各的${c.reason ? `：${c.reason}` : ''}`;
}

/** Which apps have this MCP, one mark per app (Claude Code with its
 * projects): in color when they use MCPcell's copy, faded and dashed when
 * some run their own (a click hands those over). */
function AppMarks({ row, agents, acts }: { row: McpRow; agents: AgentSummary[]; acts: Acts }) {
  const marks: ReactNode[] = [];
  for (const app of appsOf(agents)) {
    const cells = agents.filter((a) => appOf(a.id) === app && row.agents[a.id]).map((a) => ({ a, c: row.agents[a.id] }));
    if (!cells.length) continue;
    const takeable = cells.filter(({ c }) => canTake(c)).map(({ a }) => a);
    const managed = cells.filter(({ c }) => c.state === 'managed');
    const tip = cells.map(({ a, c }) => `${a.name}：${cellText(c)}`).join('\n');
    const brand = <BrandMark brand={agentBrand[app]} className="size-3.5" />;
    const base = 'relative grid size-6 place-items-center rounded-full border';
    const count = cells.length > 1 && (
      <span className="absolute -top-1 -right-1.5 grid h-3.5 min-w-3.5 place-items-center rounded-full bg-zinc-700 px-0.5 text-[9px] leading-none text-white dark:bg-zinc-300 dark:text-zinc-900">
        {cells.length}
      </span>
    );
    if (takeable.length) {
      marks.push(
        <button
          key={app}
          title={`${tip}\n点一下改用 MCPcell 共用的这份`}
          onClick={(e) => {
            e.stopPropagation();
            acts.takeover(takeSteps(row, takeable));
          }}
          className={cx(base, 'border-dashed border-zinc-300 opacity-60 grayscale transition hover:border-blue-400 hover:opacity-100 hover:grayscale-0 dark:border-zinc-600')}
        >
          {brand}
          {count}
        </button>,
      );
    } else if (managed.length) {
      const off = managed.every(({ c }) => !c.enabled);
      const drift = managed.some(({ c }) => c.drift);
      marks.push(
        <span key={app} title={tip} className={cx(base, 'border-emerald-300 bg-white dark:border-emerald-700 dark:bg-zinc-900', off && 'opacity-40')}>
          {brand}
          {count}
          <span className={cx('absolute -right-px -bottom-px size-2 rounded-full ring-2 ring-white dark:ring-zinc-900', drift ? 'bg-amber-500' : 'bg-emerald-500')} />
        </span>,
      );
    } else {
      const changed = cells.some(({ c }) => c.state === 'changed');
      marks.push(
        <span key={app} title={tip} className={cx(base, 'border-zinc-200 opacity-50 grayscale dark:border-zinc-700', changed && 'border-amber-300 opacity-90 grayscale-0')}>
          {brand}
          {count}
        </span>,
      );
    }
  }
  return (
    <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
      {marks}
    </div>
  );
}

const join = (items: ReactNode[]) => items.flatMap((x, i) => (i ? ['、', x] : [x]));

/** What automatic takeover did since the user last looked, in a line. */
function AutoNews({ auto, onAck }: { auto: AutoStatus; onAck: () => void }) {
  const fresh = (auto.events ?? []).filter((e) => !e.seen);
  if (!fresh.length) return null;
  const taken = [...new Map(fresh.filter((e) => e.ok).map((e) => [e.service ?? e.name, e])).values()];
  const failed = fresh.filter((e) => !e.ok);
  const link = (e: { service?: string; name: string }) => (
    <a key={e.service ?? e.name} href={href(['mcp', e.service ?? e.name])} className="font-medium text-blue-700 hover:underline dark:text-blue-300">
      {e.service ?? e.name}
    </a>
  );
  return (
    <Strip tone="blue" action={<StripButton onClick={onAck}>知道了</StripButton>}>
      {taken.length > 0 && <>新加的 {join(taken.map(link))} 已改用 MCPcell 共用的这份</>}
      {taken.length > 0 && failed.length > 0 && '；'}
      {failed.length > 0 && <>{join(failed.map((e) => e.name))} 没能接上</>}
    </Strip>
  );
}

/** The few things worth doing now, a line each. */
function Hints({ views, offered, onTake }: { views: McpView[]; offered: string[]; onTake: () => void }) {
  const broken = views.filter((v) => v.kind === 'broken');
  const changed = views.filter((v) => Object.values(v.row.agents).some((c) => c.state === 'changed'));
  return (
    <>
      {broken.length > 0 && <Strip tone="red">{broken.map((v) => v.row.name).join('、')} 打不开</Strip>}
      {offered.length > 0 && (
        <Strip tone="blue" action={<StripButton onClick={onTake}>一键托管</StripButton>}>
          {offered.join('、')} 还是各开各的
        </Strip>
      )}
      {changed.length > 0 && <Strip tone="amber">{changed.map((v) => v.row.name).join('、')} 在应用里被改过</Strip>}
    </>
  );
}

function Strip({ tone, action, children }: { tone: 'red' | 'amber' | 'blue'; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center gap-2.5 rounded-lg border border-zinc-200/80 bg-white px-3.5 py-2 text-[13px] text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-200">
      <Dot tone={tone} />
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {action}
    </div>
  );
}

function StripButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button onClick={onClick} className="shrink-0 font-medium text-blue-600 hover:underline dark:text-blue-400">
      {children}
    </button>
  );
}
