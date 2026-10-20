import { useState, type ReactNode } from 'react';
import { Activity, ArrowRight, Copy, Cpu, Gauge, Layers, Pencil, Play, Plus, Power, RefreshCw, Square, Trash2, Undo2 } from 'lucide-react';
import { instancesOf, takeover, type AgentCell, type AgentSummary, type McpRow, type Service } from '../api';
import { useStatus } from '../App';
import { useServiceActions } from '../actions';
import { agentBrand, agentsOf, canTake, cellsOf, groupOf, restoreSteps, takeSteps, useAgentActions, useUnified, type Group } from '../agents';
import { bytes, firstLine } from '../format';
import { BrandMark, Logo, ServiceAvatar } from '../icons';
import { candidates, OneClickDialog } from '../one-click';
import { go } from '../router';
import { Badge, Button, Card, cx, Dot, GroupHeader, Menu, PageHeader, Stat, Switch, TableCard, useCopy, useToast, type MenuItem } from '../ui';

const groups: { id: Group; title: string; hint: string; collapsed?: boolean }[] = [
  { id: 'managed', title: '已托管', hint: '由 MCPcell 运行：共享的全机一份，按会话的用时才启动' },
  { id: 'available', title: '可以托管', hint: '现在每个会话各起一份进程' },
  { id: 'other', title: '远程与内置', hint: '不在本机单独运行，不需要托管', collapsed: true },
];

// Columns shared by group headers and rows so they line up.
const grid = 'grid grid-cols-[minmax(0,1fr)_128px_196px_44px_32px] items-center gap-4';

export function McpPage() {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const svcActs = useServiceActions();
  const copy = useCopy();
  const toast = useToast();
  const [oneClick, setOneClick] = useState(false);
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const header = (actions?: ReactNode) => (
    <PageHeader
      icon={<Layers className="size-5 text-zinc-500" />}
      title="MCP"
      subtitle={data && `${data.rows.length} 个，来自 ${agentsOf(data.agents).length} 个 Agent`}
      actions={actions}
    />
  );
  if (!data) {
    return (
      <>
        {header()}
        <p className="py-20 text-center text-sm text-zinc-400">{error || '正在汇总各 Agent 的 MCP…'}</p>
      </>
    );
  }

  const agents = agentsOf(data.agents);
  const byGroup = (g: Group) => data.rows.filter((r) => groupOf(r) === g);
  const managedRows = byGroup('managed');
  const offers = candidates(data);
  const offer = offers.length;
  const running = status.services.flatMap(instancesOf);
  const mcpBytes = running.reduce((n, i) => n + i.usage.privateBytes, 0);
  const allRestore = data.rows.flatMap((r) => restoreSteps(r, agents));

  return (
    <>
      {header(
        <>
          {offer > 0 && (
            <Button variant="primary" icon={<Layers className="size-4" />} onClick={() => setOneClick(true)}>
              一键托管
            </Button>
          )}
          <Button icon={<Plus className="size-4" />} onClick={() => go('services', 'new')}>
            添加
          </Button>
          <Menu
            items={[
              { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
              { label: '复制访问令牌', icon: <Copy className="size-4" />, onSelect: () => copy(status.token, '已复制访问令牌') },
              'sep',
              {
                label: '全部还原',
                hint: '所有 Agent 恢复托管前的配置',
                icon: <Undo2 className="size-4" />,
                danger: true,
                disabled: allRestore.length === 0,
                onSelect: () => acts.restore(allRestore, `还原全部 ${allRestore.length} 个托管`),
              },
            ]}
          />
        </>,
      )}
      <div className="mx-auto w-full max-w-[1240px] space-y-7 px-8 pt-6 pb-12">
        <div className="grid grid-cols-4 gap-4">
          <Stat
            icon={<Layers className="size-4" />}
            label="已托管"
            value={
              <>
                {managedRows.length}
                <span className="text-base font-normal text-zinc-400"> / {data.rows.length}</span>
              </>
            }
            sub={offer ? `还有 ${offer} 个可以托管` : '本机的 MCP 都已托管'}
          />
          <Stat icon={<Cpu className="size-4" />} label="运行中的实例" value={running.length} sub="空闲后自动回收，调用时再启动" />
          <Stat
            icon={<Gauge className="size-4" />}
            label="内存（私有提交）"
            value={bytes(mcpBytes + status.daemon.privateBytes)}
            sub={`守护进程 ${bytes(status.daemon.privateBytes)} · MCP ${mcpBytes ? bytes(mcpBytes) : '0 MB'}`}
          />
          <Stat icon={<Activity className="size-4" />} label="会话" value={status.sessions.length} sub="同一个 MCP 的会话共用进程" />
        </div>

        {offer > 0 && <OfferBanner first={managedRows.length === 0} count={offer} onClick={() => setOneClick(true)} />}
        {managedRows.length > 0 && data.autostart.supported && !data.autostart.enabled && (
          <div className="flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
            <Power className="size-4 shrink-0" />
            <span className="flex-1">开机自启没开：MCPcell 没在运行时，托管的 MCP 在 Agent 里不可用。</span>
            <Button size="sm" variant="primary" onClick={async () => (await takeover.autostart(true), toast('已开启开机自启'), reload())}>
              开启自启
            </Button>
          </div>
        )}

        {groups.map((g) => {
          const rows = byGroup(g.id);
          if (!rows.length) return null;
          const expanded = open[g.id] ?? !g.collapsed;
          return (
            <section key={g.id}>
              <div className={cx(grid, 'px-5')}>
                <div className="-ml-5 min-w-0">
                  <GroupHeader
                    title={g.title}
                    count={rows.length}
                    hint={g.hint}
                    open={expanded}
                    onToggle={g.collapsed ? () => setOpen((o) => ({ ...o, [g.id]: !expanded })) : undefined}
                  />
                </div>
                {expanded && (
                  <>
                    <ColumnLabel>Agent</ColumnLabel>
                    <ColumnLabel>状态</ColumnLabel>
                    <ColumnLabel className="text-center">托管</ColumnLabel>
                    <span />
                  </>
                )}
              </div>
              {expanded && (
                <TableCard>
                  {rows.map((r) => (
                    <McpRowView
                      key={r.key}
                      row={r}
                      agents={agents}
                      svc={status.services.find((s) => s.name === r.service)}
                      sessions={status.sessions.filter((x) => x.service === r.service).length}
                      acts={acts}
                      svcActs={svcActs}
                    />
                  ))}
                </TableCard>
              )}
            </section>
          );
        })}
      </div>
      <OneClickDialog open={oneClick} onClose={() => setOneClick(false)} data={data} reload={reload} />
      {acts.dialog}
      {svcActs.deleteDialog}
    </>
  );
}

function ColumnLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cx('mb-2.5 text-xs text-zinc-400', className)}>{children}</span>;
}

function OfferBanner({ first, count, onClick }: { first: boolean; count: number; onClick: () => void }) {
  return (
    <Card className="flex items-center gap-5 overflow-hidden border-blue-100 bg-gradient-to-r from-blue-50 via-white to-white px-6 py-5 dark:border-blue-900/60 dark:from-blue-950/40 dark:via-zinc-900 dark:to-zinc-900">
      <Logo className="size-11 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="text-[15px] font-semibold">{first ? '把本机的 MCP 交给 MCPcell' : `还有 ${count} 个 MCP 可以托管`}</div>
        <p className="mt-1 text-sm leading-6 text-zinc-500">
          {first
            ? `各个 Agent 里有 ${count} 个 MCP 可以交给 MCPcell。现在每开一个会话就多一套进程；托管后能共享的全机只跑一份，其余按会话运行、用时才启动。`
            : '能共享的全机只跑一份，其余按会话运行、用时才启动；工具名不变，随时可以还原。'}
        </p>
      </div>
      <Button variant="primary" icon={<ArrowRight className="size-4" />} onClick={onClick}>
        一键托管 {count} 个
      </Button>
    </Card>
  );
}

function McpRowView({
  row,
  agents,
  svc,
  sessions,
  acts,
  svcActs,
}: {
  row: McpRow;
  agents: AgentSummary[];
  svc?: Service;
  sessions: number;
  acts: ReturnType<typeof useAgentActions>;
  svcActs: ReturnType<typeof useServiceActions>;
}) {
  const copy = useCopy();
  const { status } = useStatus();
  const group = groupOf(row);
  const take = takeSteps(row, agents);
  const back = restoreSteps(row, agents);
  const managed = back.length > 0;
  const cells = cellsOf(row);
  const allOff = cells.length > 0 && cells.every((c) => !c.enabled);
  const meta = [row.launch + (row.detail ? ` ${row.detail}` : ''), svc?.package && `v${svc.package.version}`, svc?.catalog && `${svc.catalog.tools} 个工具`]
    .filter(Boolean)
    .join(' · ');

  return (
    <div
      onClick={() => svc && go('services', svc.name)}
      className={cx(grid, 'group px-5 py-3', svc && 'cursor-pointer transition-colors hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40')}
    >
      <div className="flex min-w-0 items-center gap-3.5">
        <ServiceAvatar name={row.name} size="sm" />
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate text-[15px] font-medium">{row.name}</span>
            {row.mode === 'per-session' && group !== 'other' && <Badge tone="blue">按会话</Badge>}
            {allOff && <Badge>已停用</Badge>}
          </div>
          <div className="mt-0.5 truncate font-mono text-xs text-zinc-400" title={meta}>
            {meta}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
        {agents.map((a) => (
          <AgentIcon key={a.id} agent={a} row={row} cell={row.agents[a.id]} acts={acts} />
        ))}
      </div>

      <RunState row={row} group={group} svc={svc} sessions={sessions} />

      <div className="flex justify-center">
        {(take.length > 0 || managed) && (
          <Switch
            checked={managed}
            label="托管"
            title={managed ? '关闭：还原为各 Agent 原来的配置' : '打开：交给 MCPcell 运行'}
            onChange={(on) => (on ? acts.takeover(take) : acts.restore(back))}
          />
        )}
      </div>

      <div onClick={(e) => e.stopPropagation()}>
        {svc && (
          <Menu
            items={[
              instancesOf(svc).length
                ? {
                    label: svc.mode === 'per-session' ? '回收全部实例' : '回收进程',
                    hint: '会话保留，下次调用自动启动',
                    icon: <Square className="size-4" />,
                    onSelect: () => svcActs.stop(svc.name),
                  }
                : svc.mode === 'per-session'
                  ? { label: '检查启动', hint: '启动一次并缓存工具列表', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) }
                  : { label: '立即启动', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) },
              { label: '编辑', icon: <Pencil className="size-4" />, onSelect: () => go('services', svc.name, 'edit') },
              { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${svc.name}`, '已复制接入地址') },
              'sep',
              { label: '删除服务', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => svcActs.askDelete(svc.name) },
            ]}
          />
        )}
      </div>
    </div>
  );
}

function RunState({ row, group, svc, sessions }: { row: McpRow; group: Group; svc?: Service; sessions: number }) {
  const line = (dot: ReactNode, text: ReactNode, title?: string) => (
    <div className="flex min-w-0 items-center gap-2 text-[13px] text-zinc-500" title={title}>
      {dot}
      <span className="truncate">{text}</span>
    </div>
  );
  if (svc?.instance) {
    return line(
      <Dot tone="green" pulse={svc.instance.inflight > 0} />,
      <>
        <b className="font-medium text-zinc-800 tabular-nums dark:text-zinc-200">{bytes(svc.instance.usage.privateBytes)}</b> · {sessions} 个会话
        {svc.instance.inflight > 0 && ` · 处理中 ${svc.instance.inflight}`}
      </>,
    );
  }
  if (svc?.state === 'starting') return line(<Dot tone="amber" pulse />, '启动中…');
  if (svc?.state === 'failed') return line(<Dot tone="red" />, <span className="text-red-600">启动失败</span>, firstLine(svc.lastError));
  if (svc?.mode === 'per-session') {
    const runs = svc.instances ?? [];
    if (!runs.length) return line(<Dot tone="gray" />, sessions ? `${sessions} 个会话 · 都还没调用` : '调用时启动');
    return line(
      <Dot tone="green" pulse={runs.some((i) => i.inflight > 0)} />,
      <>
        <b className="font-medium text-zinc-800 tabular-nums dark:text-zinc-200">{bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}</b> · {runs.length} 个会话实例
      </>,
    );
  }
  if (svc) return line(<Dot tone="gray" />, sessions ? `空闲 · ${sessions} 个会话` : '空闲 · 调用时启动');
  const text = { managed: '', available: '每个会话各起一份', other: row.kind === 'http' ? '远程服务' : 'Agent 内置' }[group];
  return line(null, <span className="text-zinc-400">{text}</span>);
}

/** An agent's state for one MCP; clicking opens the actions that apply. */
function AgentIcon({ agent, row, cell, acts }: { agent: AgentSummary; row: McpRow; cell?: AgentCell; acts: ReturnType<typeof useAgentActions> }) {
  const off = cell && !cell.enabled ? `（在 ${agent.name} 里已停用）` : '';
  let look = 'opacity-0';
  let title = '';
  let items: MenuItem[] = [];
  if (!cell) {
    if (row.canAdd) {
      look = 'border border-dashed border-zinc-300 opacity-0 group-hover:opacity-100 dark:border-zinc-600 [&_svg]:opacity-40';
      title = `${agent.name} 里没有它`;
      items = [{ label: `加入 ${agent.name}`, hint: '新增一个连到 MCPcell 的条目', icon: <Plus className="size-4" />, onSelect: () => acts.add(agent, row) }];
    }
  } else if (cell.state === 'managed') {
    look = 'bg-blue-50 ring-[1.5px] ring-blue-500 dark:bg-blue-950';
    title = `${agent.name} · 由 MCPcell 托管${off}`;
    if (cell.drift) title += `。${cell.drift}，MCPcell 按条目现在的设置单独运行它`;
    items = [
      cell.added
        ? {
            label: `从 ${agent.name} 移除`,
            hint: '这个条目是 MCPcell 加入的',
            icon: <Trash2 className="size-4" />,
            danger: true,
            onSelect: () => acts.restore([{ agent, entry: cell.entry, added: true }]),
          }
        : { label: '还原为原生配置', hint: '恢复托管前的条目', icon: <Undo2 className="size-4" />, onSelect: () => acts.restore([{ agent, entry: cell.entry }]) },
    ];
  } else if (cell.state === 'changed') {
    look = 'bg-amber-50 ring-[1.5px] ring-amber-400 dark:bg-amber-950';
    title = `${agent.name} · 托管后被改动过${off}`;
    items = [{ label: '忘记托管记录', hint: '保留你现在的配置', icon: <Undo2 className="size-4" />, onSelect: () => acts.forget(agent, cell.entry) }];
  } else if (canTake(cell)) {
    look = 'bg-zinc-100 dark:bg-zinc-800 [&_svg]:opacity-60';
    title = `${agent.name} · 原生运行，每个会话各起一份${off}`;
    items = [
      {
        label: '交给 MCPcell 托管',
        hint: row.mode === 'shared' ? '所有会话共用一份进程' : '按会话运行：每个会话一个实例，调用时才启动',
        icon: <Layers className="size-4" />,
        onSelect: () => acts.takeover(takeSteps(row, [agent])),
      },
    ];
  } else {
    look = 'bg-zinc-100 dark:bg-zinc-800 [&_svg]:opacity-30';
    title = `${agent.name} · ${cell.state === 'remote' ? '远程 HTTP 服务' : (cell.reason ?? '原生运行')}${off}`;
  }
  const icon = (
    <span className={cx('relative grid size-7 place-items-center rounded-full transition', look, cell && !cell.enabled && 'opacity-50')}>
      <BrandMark brand={agentBrand[agent.id]} className="size-[15px]" />
      {cell?.state === 'managed' && (
        <span
          className={cx(
            'absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-white dark:border-zinc-900',
            cell.drift ? 'bg-amber-500' : 'bg-blue-500',
          )}
        />
      )}
    </span>
  );
  if (!items.length) {
    return (
      <span title={title} className="cursor-default">
        {icon}
      </span>
    );
  }
  return (
    <Menu
      align="left"
      items={items}
      header={<div className="text-xs leading-5 text-zinc-500">{title}</div>}
      trigger={(toggle) => (
        <button title={title} onClick={toggle} className="rounded-full focus-visible:outline-2 focus-visible:outline-blue-500">
          {icon}
        </button>
      )}
    />
  );
}
