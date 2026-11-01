import { useState, type ReactNode } from 'react';
import { ChevronDown, ChevronLeft, Compass, Info, Layers, Package, Play, Plus, RefreshCw, RotateCw, Square, Undo2 } from 'lucide-react';
import { autoApi, instancesOf, type AgentSummary, type AutoStatus, type McpRow } from '../api';
import { useServiceActions } from '../actions';
import { agentBrand, agentsOf, appOf, appsOf, candidates, canTake, isProject, pendingSync, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { useStatus, useUnified } from '../data';
import { RunHead } from '../guide';
import { RunView, useTakeoverRun } from '../run';
import { bytes } from '../format';
import { openGuide } from '../guide';
import { BrandMark, ServiceAvatar } from '../icons';
import { Body, Toolbar } from '../layout';
import { kindRank, mcpView, plainReason, type McpView } from '../model';
import { go, href, useRoute } from '../router';
import { StatStrip, useNow } from '../stats';
import { agentName } from '../timeline';
import { Button, Card, cx, Dot, IconButton, Menu, Modal, Segmented, Switch } from '../ui';

// The MCP page: the app filter, three numbers, then every MCP in one card.
// A row shows what the MCP is for, which apps have it, its state, and a
// switch that takes it over or restores it. An app here is Claude Code with
// its projects' servers too (agents "claude-code@<dir>"), shown as one app.

type Acts = ReturnType<typeof useAgentActions>;

export function HomePage() {
  const { status, refresh } = useStatus();
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload);
  const svcActs = useServiceActions();
  const now = useNow();
  const only = useRoute().query.get('agent') ?? '';
  const [showOther, setShowOther] = useState(false);
  const run = useTakeoverRun();
  const [runOpen, setRunOpen] = useState(false);

  const agents = data ? agentsOf(data.agents) : [];
  const apps = appsOf(agents);
  const app = apps.includes(only) ? only : '';
  const withMcps = appsOf(agents.filter((a) => a.total > 0));
  const scope = app ? agents.filter((a) => appOf(a.id) === app) : agents;
  const rows = data ? data.rows.filter((r) => !app || scope.some((a) => r.agents[a.id])) : [];
  const views = data ? rows.map((r) => mcpView(r, status)).sort((a, b) => kindRank[a.kind] - kindRank[b.kind] || a.row.name.localeCompare(b.row.name)) : [];
  // Remote and built-in MCPs need nothing, and ones that cannot start are
  // ignored: both wait under the list, folded.
  const main = views.filter((v) => v.kind !== 'other' && v.kind !== 'ignored');
  const other = views.filter((v) => v.kind === 'other' || v.kind === 'ignored');
  const ignored = other.filter((v) => v.kind === 'ignored').length;
  const broken = new Set(views.filter((v) => v.kind === 'broken').map((v) => v.row.key));
  const offered = data ? candidates(data, app || undefined).filter((c) => !broken.has(c.row.key)) : [];
  const allRestore = rows.flatMap((r) => restoreSteps(r, scope));
  const sync = !!data?.auto?.sync;
  const toSync = sync && data ? pendingSync(data, app || undefined) : 0;

  return (
    <>
      <Body>
        <Toolbar
          actions={
            <>
              {(offered.length > 0 || toSync > 0) && (
                <Button
                  size="sm"
                  variant="primary"
                  icon={<Layers className="size-3.5" />}
                  busy={run.running}
                  title={[offered.length && `托管 ${offered.length} 个`, toSync && `同步到其他应用 ${toSync} 项`].filter(Boolean).join('，')}
                  onClick={async () => {
                    setRunOpen(true);
                    await run.run(offered, sync, app ? [app] : undefined);
                    await Promise.all([reload(), refresh()]);
                  }}
                >
                  一键托管
                </Button>
              )}
              <IconButton label="添加 MCP" onClick={() => go(['mcp', 'new'])}>
                <Plus className="size-4" />
              </IconButton>
              <Menu
                items={[
                  { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                  { label: '使用引导', icon: <Compass className="size-4" />, onSelect: openGuide },
                  'sep',
                  {
                    label: app ? `还原 ${agentName(app)} 全部` : '全部还原',
                    icon: <Undo2 className="size-4" />,
                    danger: true,
                    disabled: allRestore.length === 0,
                    onSelect: () => acts.restoreAll(allRestore, `还原${app ? ` ${agentName(app)} 的` : '全部'} ${allRestore.length} 项`),
                  },
                ]}
              />
            </>
          }
        >
          {withMcps.length > 1 && <AppFilter apps={withMcps} value={app} />}
        </Toolbar>
        <StatStrip now={now} />
        {data?.auto && <AutoNews auto={data.auto} onAck={async () => (await autoApi.ack().catch(() => undefined), reload())} />}
        {data && <Changed views={main} />}
        {error && <p className="text-[13px] text-red-600">{error}</p>}
        {!data && !error && <p className="py-12 text-center text-[13px] text-zinc-400">读取中…</p>}
        {data && (
          <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
            {main.map((v) => (
              <McpRowView key={v.id} view={v} agents={agents} scope={scope} acts={acts} svcActs={svcActs} />
            ))}
            {main.length === 0 && <p className="px-4 py-8 text-center text-[13px] text-zinc-400">暂无 MCP</p>}
            {showOther && other.map((v) => <McpRowView key={v.id} view={v} agents={agents} scope={scope} acts={acts} svcActs={svcActs} />)}
            {other.length > 0 && (
              <button
                onClick={() => setShowOther(!showOther)}
                className="flex w-full items-center justify-center gap-1 px-4 py-2 text-[13px] text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
              >
                {showOther
                  ? '收起'
                  : [ignored && `已忽略 ${ignored} 个`, other.length > ignored && `远程和内置 ${other.length - ignored} 个`].filter(Boolean).join(' · ')}
                <ChevronDown className={cx('size-3.5 transition-transform', showOther && 'rotate-180')} />
              </button>
            )}
          </Card>
        )}
      </Body>
      <Modal open={runOpen} onClose={() => !run.running && setRunOpen(false)} width={480}>
        <RunHead run={run} />
        <div className="mt-4 max-h-[400px] overflow-y-auto">
          <RunView run={run} />
        </div>
        <div className="mt-5 flex justify-end">
          <Button variant="primary" disabled={run.running} onClick={() => setRunOpen(false)}>
            完成
          </Button>
        </div>
      </Modal>
      {acts.dialog}
      {svcActs.deleteDialog}
    </>
  );
}

/** 全部 and the apps that have MCPs; past the first three, the rest are
 * under 更多. */
function AppFilter({ apps, value }: { apps: string[]; value: string }) {
  const shown = apps.slice(0, 3);
  if (value && !shown.includes(value)) shown.push(value);
  const rest = apps.filter((id) => !shown.includes(id));
  const pick = (id: string) => go([], { agent: id || undefined });
  return (
    <div className="flex items-center gap-1">
      <Segmented
        value={value}
        onChange={pick}
        options={[
          { value: '', label: '全部' },
          ...shown.map((id) => ({
            value: id,
            label: (
              <>
                <BrandMark brand={agentBrand[id] ?? ''} className="size-3.5" />
                {agentName(id)}
              </>
            ),
          })),
        ]}
      />
      {rest.length > 0 && (
        <Menu
          align="left"
          items={rest.map((id) => ({ label: agentName(id), icon: <BrandMark brand={agentBrand[id] ?? ''} className="size-4" />, onSelect: () => pick(id) }))}
          trigger={(toggle) => (
            <button
              onClick={toggle}
              className="flex h-7 items-center gap-1 rounded-md px-2 text-[13px] text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 dark:hover:bg-zinc-800 dark:hover:text-zinc-100"
            >
              更多 {rest.length}
              <ChevronDown className="size-3.5" />
            </button>
          )}
        />
      )}
    </div>
  );
}

/** A row's state in a word or two, and its tooltip. */
function statusOf(v: McpView): { tone: McpView['tone']; text: string; tip: string } {
  const conv = new Set(v.sessions.map((s) => (s.agentPid ? `p${s.agentPid}` : `s${s.id}`))).size;
  const calls = v.services.reduce((n, s) => n + (s.calls ?? 0), 0);
  const facts = [conv ? `${conv} 个对话` : '', calls ? `调用 ${calls} 次` : ''].filter(Boolean).join(' · ');
  switch (v.kind) {
    case 'running': {
      const used = v.services.flatMap(instancesOf).reduce((n, i) => n + i.usage.privateBytes, 0);
      return v.tone === 'amber' ? { tone: 'amber', text: '启动中', tip: '' } : { tone: 'green', text: bytes(used), tip: facts };
    }
    case 'standby':
      return { tone: 'gray', text: '待命', tip: facts };
    case 'broken':
      return { tone: v.tone, text: v.tone === 'red' ? '打不开' : '配置缺失', tip: v.text };
    case 'native':
      return { tone: 'gray', text: '未托管', tip: '' };
    case 'ignored':
      return { tone: 'gray', text: '无法启动', tip: v.text.replace(/^无法启动：/, '') };
  }
  return { tone: 'gray', text: v.row.kind === 'http' ? '远程' : '内置', tip: '' };
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
    ...(svc && running ? [{ label: '停止', icon: <Square className="size-4" />, onSelect: stop }] : []),
    ...(svc?.package ? [{ label: `更新（${svc.package.version}）`, icon: <Package className="size-4" />, onSelect: () => svcActs.update(svc.name) }] : []),
    ...missing.map((a) => ({ label: `添加到 ${a.name}`, icon: <Plus className="size-4" />, onSelect: () => acts.add(a, row) })),
    ...(back.length ? ['sep' as const, { label: '还原', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back) }] : []),
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

      <div
        className={cx('flex items-center justify-end gap-1.5 text-xs tabular-nums', st.tone === 'red' ? 'text-red-600 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-300')}
        title={st.tip || undefined}
      >
        <Dot tone={st.tone} pulse={v.kind === 'running' && v.sessions.some((x) => x.inflight > 0)} />
        <span className="truncate">{st.text}</span>
      </div>

      <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
        <div className={cx('flex items-center transition-opacity', v.kind !== 'broken' && 'opacity-0 group-hover:opacity-100 focus-within:opacity-100')}>
          {v.kind === 'broken' && v.broken ? (
            <IconButton label="重试" busy={busy} onClick={() => svcActs.start(v.broken!.name)}>
              <RotateCw className="size-3.5" />
            </IconButton>
          ) : svc && running ? (
            <IconButton label="停止" busy={svcActs.busy[svc.name]} onClick={stop}>
              <Square className="size-3" />
            </IconButton>
          ) : svc ? (
            <IconButton label="启动" busy={busy} onClick={() => svcActs.start(svc.name)}>
              <Play className="size-3.5" />
            </IconButton>
          ) : (
            <span className="size-8" />
          )}
          <Menu items={menu} />
        </div>
        <div className="flex w-[38px] justify-end">
          {v.kind !== 'other' && (on || take.length > 0) && (
            <Switch checked={on} label={`托管 ${row.name}`} title={on ? '已托管' : '未托管'} disabled={acts.busy} onChange={(next) => (next ? acts.takeover(take) : acts.restore(back))} />
          )}
        </div>
      </div>
    </div>
  );
}

/** A cell's state, for a tooltip line. */
function cellText(c: McpRow['agents'][string]): string {
  switch (c.state) {
    case 'managed':
      return !c.enabled ? '已停用' : c.drift ? '已托管（设置不同）' : '已托管';
    case 'changed':
      return '配置被改动';
    case 'remote':
      return '远程';
    case 'internal':
      return '内置';
  }
  if (c.dead) return `无法启动：${plainReason(c.dead)}`;
  return canTake(c) || !c.reason ? '未托管' : `未托管：${c.reason}`;
}

/** Which apps have this MCP, one mark per app (Claude Code with its
 * projects): in color when taken over, faded and dashed when not (a click
 * takes those over). */
function AppMarks({ row, agents, acts }: { row: McpRow; agents: AgentSummary[]; acts: Acts }) {
  const [open, setOpen] = useState(false);
  const marks: ReactNode[] = [];
  const tips: string[] = [];
  for (const app of appsOf(agents)) {
    const cells = agents.filter((a) => appOf(a.id) === app && row.agents[a.id]).map((a) => ({ a, c: row.agents[a.id] }));
    if (!cells.length) continue;
    const takeable = cells.filter(({ c }) => canTake(c)).map(({ a }) => a);
    const managed = cells.filter(({ c }) => c.state === 'managed');
    const tip = cells.map(({ a, c }) => `${a.name}：${cellText(c)}`).join('\n');
    tips.push(tip);
    const brand = <BrandMark brand={agentBrand[app] ?? ''} className="size-3.5" />;
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
          title={`${tip}\n点击托管`}
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
  // Past three apps the rest fold into +N; a click unfolds them.
  const folded = marks.length > maxMarks && !open;
  const chip = 'grid h-6 min-w-6 place-items-center rounded-full border border-zinc-200 px-1.5 text-[11px] text-zinc-500 tabular-nums hover:border-zinc-400 hover:text-zinc-800 dark:border-zinc-700 dark:hover:text-zinc-100';
  return (
    <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
      {folded ? marks.slice(0, maxMarks) : marks}
      {folded && (
        <button className={chip} title={tips.slice(maxMarks).join('\n')} onClick={() => setOpen(true)}>
          +{marks.length - maxMarks}
        </button>
      )}
      {open && marks.length > maxMarks && (
        <button className={chip} title="收起" aria-label="收起" onClick={() => setOpen(false)}>
          <ChevronLeft className="size-3" />
        </button>
      )}
    </div>
  );
}

const maxMarks = 3;

const join = (items: ReactNode[]) => items.flatMap((x, i) => (i ? ['、', x] : [x]));

/** What automatic takeover did since the user last looked, in a line. The
 * entries it could not start are ignored quietly (they are in 动态). */
function AutoNews({ auto, onAck }: { auto: AutoStatus; onAck: () => void }) {
  const fresh = (auto.events ?? []).filter((e) => !e.seen && e.ok);
  if (!fresh.length) return null;
  const taken = [...new Map(fresh.map((e) => [e.service ?? e.name, e])).values()];
  const link = (e: { service?: string; name: string }) => (
    <a key={e.service ?? e.name} href={href(['mcp', e.service ?? e.name])} className="font-medium text-blue-700 hover:underline dark:text-blue-300">
      {e.service ?? e.name}
    </a>
  );
  return (
    <Strip tone="blue" action={<StripButton onClick={onAck}>知道了</StripButton>}>
      已自动托管 {join(taken.map(link))}
    </Strip>
  );
}

/** Takeovers whose entry the app has since changed. */
function Changed({ views }: { views: McpView[] }) {
  const changed = views.filter((v) => Object.values(v.row.agents).some((c) => c.state === 'changed'));
  if (!changed.length) return null;
  return <Strip tone="amber">配置被改动：{changed.map((v) => v.row.name).join('、')}</Strip>;
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
