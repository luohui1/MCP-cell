import { useCallback, useEffect, useState } from 'react';
import { ChevronRight, Copy, Gauge, Layers, Pencil, Play, Plus, Power, RefreshCw, Square, Trash2 } from 'lucide-react';
import { takeover, unified, type AgentCell, type AgentSummary, type AutostartStatus, type McpRow, type Service } from '../api';
import { useStatus } from '../App';
import { agentBrand, agentOrder, useAgentActions } from '../agent-actions';
import { useServiceActions } from '../actions';
import { bytes, firstLine, since } from '../format';
import { BrandMark, ServiceAvatar } from '../icons';
import { go } from '../router';
import { Badge, Button, Card, cx, Dot, IconButton, Menu, PageHeader, Segmented, useCopy, useToast } from '../ui';

type Filter = 'all' | 'managed' | 'available' | 'other';

const classBadge: Record<string, { tone: 'gray' | 'blue' | 'green' | 'amber'; text: string }> = {
  shared: { tone: 'green', text: '可共享' },
  stateful: { tone: 'amber', text: '需要会话隔离' },
  unknown: { tone: 'gray', text: '未知' },
  remote: { tone: 'gray', text: '远程 HTTP' },
  internal: { tone: 'gray', text: 'Agent 内置' },
  managed: { tone: 'blue', text: '已托管' },
};

/** Loads /api/mcp and keeps it fresh. */
export function useUnified() {
  const [data, setData] = useState<{ agents: AgentSummary[]; rows: McpRow[]; autostart: AutostartStatus } | null>(null);
  const [error, setError] = useState('');
  const reload = useCallback(async () => {
    try {
      setData(await unified.load());
      setError('');
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    reload();
    const t = setInterval(() => !document.hidden && reload(), 4000);
    return () => clearInterval(t);
  }, [reload]);
  return { data, error, reload };
}

const isManaged = (r: McpRow) => Object.values(r.agents).some((c) => c.state === 'managed');
const isAvailable = (r: McpRow) => Object.values(r.agents).some((c) => c.state === 'native' && c.class === 'shared');

export function McpPage() {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const agentActs = useAgentActions(reload, data?.autostart ?? null);
  const serviceActs = useServiceActions();
  const copy = useCopy();
  const toast = useToast();
  const [filter, setFilter] = useState<Filter>('all');

  if (!data) {
    return (
      <>
        <PageHeader icon={<Layers className="size-6 text-zinc-700 dark:text-zinc-300" />} title="MCP" />
        <p className="py-16 text-center text-sm text-zinc-400">{error || '正在汇总各 Agent 的 MCP…'}</p>
      </>
    );
  }

  const agents = agentOrder.map((id) => data.agents.find((a) => a.id === id)).filter((a): a is AgentSummary => !!a && a.found);
  const rows = data.rows.filter((r) => {
    switch (filter) {
      case 'managed':
        return isManaged(r);
      case 'available':
        return isAvailable(r);
      case 'other':
        return !isManaged(r) && !isAvailable(r) && !r.service;
    }
    return true;
  });
  const running = status.services.filter((s) => s.instance);
  const mcpBytes = running.reduce((n, s) => n + s.instance!.usage.privateBytes, 0);
  const availableCount = agents.reduce((n, a) => n + a.shareable, 0);

  async function takeAllShareable() {
    const targets = agents.filter((a) => a.shareable > 0);
    if (targets.length === 1) {
      const a = targets[0];
      const entries = data!.rows.flatMap((r) => {
        const c = r.agents[a.id];
        return c && c.state === 'native' && c.class === 'shared' ? [{ entry: c.entry, service: r.service ?? r.proposed, isNew: !r.service }] : [];
      });
      agentActs.takeoverEntries(a, entries);
      return;
    }
    toast('请到 Agents 页逐个确认要托管的 Agent', 'error');
    go('agents');
  }

  return (
    <>
      <PageHeader
        icon={<Layers className="size-6 text-zinc-700 dark:text-zinc-300" />}
        title="MCP"
        subtitle={`${data.rows.length} 个，来自 ${agents.length} 个 Agent`}
        actions={
          <>
            {availableCount > 0 && (
              <Button onClick={takeAllShareable} icon={<Layers className="size-4" />}>
                托管可共享的 {availableCount} 个
              </Button>
            )}
            <Button variant="brand" icon={<Plus className="size-[18px]" />} onClick={() => go('services', 'new')}>
              添加 MCP
            </Button>
            <Menu
              items={[
                { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                { label: '复制访问令牌', icon: <Copy className="size-4" />, onSelect: () => copy(status.token, '已复制访问令牌') },
              ]}
            />
          </>
        }
      />
      <div className="mx-auto w-full max-w-[1280px] px-8 py-6">
        {data.autostart.supported && !data.autostart.enabled && isManagedAny(data.rows) && (
          <div className="mb-5 flex items-center gap-4 rounded-2xl border border-amber-200 bg-amber-50 px-5 py-3.5 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
            <Power className="size-5 shrink-0" />
            <div className="flex-1">已有 MCP 由 MCPcell 托管，但开机自启没开：守护进程没运行时它们不可用。</div>
            <Button className="h-9" variant="brand" onClick={async () => (await takeover.autostart(true), toast('已开启开机自启'), reload())}>
              开启自启
            </Button>
          </div>
        )}
        <div className="mb-4 flex items-center gap-4">
          <Segmented<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: 'all', label: `全部 ${data.rows.length}` },
              { value: 'managed', label: <>已托管 {data.rows.filter(isManaged).length}{data.rows.some(isManaged) && <Dot tone="blue" />}</> },
              { value: 'available', label: `可托管 ${data.rows.filter(isAvailable).length}` },
              { value: 'other', label: `其他 ${data.rows.filter((r) => !isManaged(r) && !isAvailable(r) && !r.service).length}` },
            ]}
          />
          <div className="ml-auto flex items-center gap-2 text-sm text-zinc-500">
            <Gauge className="size-4" />
            MCPcell 内存 <b className="font-medium text-zinc-800 dark:text-zinc-200">{bytes(mcpBytes + status.daemon.privateBytes)}</b>
            <span className="text-zinc-300">·</span>
            {running.length} 个实例
            <span className="text-zinc-300">·</span>
            {status.sessions.length} 个会话
          </div>
        </div>

        <div className="mb-2 flex items-center gap-4 px-[21px] text-xs text-zinc-400">
          <span className="flex-1">MCP</span>
          <span className="flex gap-2">
            {agents.map((a) => (
              <span key={a.id} className="w-[88px] text-center">
                {a.name}
              </span>
            ))}
          </span>
          <span className="w-[150px] text-right">运行</span>
          <span className="w-[140px]" />
        </div>
        <div className="space-y-2.5">
          {rows.map((r) => (
            <McpCard
              key={r.key}
              row={r}
              agents={agents}
              svc={status.services.find((s) => s.name === r.service)}
              sessions={status.sessions.filter((x) => x.service === r.service).length}
              onCell={(a, cell) => {
                if (!cell) return r.canAdd && agentActs.add(a, r);
                switch (cell.state) {
                  case 'managed':
                    return agentActs.restoreEntry(a, cell.entry, cell.added);
                  case 'changed':
                    return agentActs.forget(a, cell.entry);
                  case 'native':
                    if (cell.class === 'shared') return agentActs.takeoverEntries(a, [{ entry: cell.entry, service: r.service ?? r.proposed, isNew: !r.service }]);
                    if (cell.class === 'unknown' && r.kind === 'stdio') return agentActs.takeoverEntries(a, [{ entry: cell.entry, service: r.service ?? r.proposed, isNew: !r.service }], true);
                }
              }}
              serviceActs={serviceActs}
            />
          ))}
          {rows.length === 0 && <Card className="py-14 text-center text-sm text-zinc-400">没有符合条件的 MCP</Card>}
        </div>
      </div>
      {agentActs.dialog}
      {serviceActs.deleteDialog}
    </>
  );
}

function isManagedAny(rows: McpRow[]) {
  return rows.some(isManaged);
}

function McpCard({
  row,
  agents,
  svc,
  sessions,
  onCell,
  serviceActs,
}: {
  row: McpRow;
  agents: AgentSummary[];
  svc?: Service;
  sessions: number;
  onCell: (a: AgentSummary, cell?: AgentCell) => void;
  serviceActs: ReturnType<typeof useServiceActions>;
}) {
  const copy = useCopy();
  const { status } = useStatus();
  const inst = svc?.instance;
  const badge = classBadge[row.class] ?? classBadge.unknown;
  const busy = !!svc && (serviceActs.busy[svc.name] || svc.state === 'starting');

  let line1: React.ReactNode = '每个会话各自启动';
  let line2: React.ReactNode = '未托管';
  if (row.kind === 'http') [line1, line2] = ['远程服务', '本机没有它的进程'];
  else if (row.class === 'internal') [line1, line2] = ['Agent 自带组件', '和 Agent 进程绑定'];
  else if (svc && inst) {
    line1 = (
      <>
        <b className="font-medium text-zinc-800 dark:text-zinc-200">{bytes(inst.usage.privateBytes)}</b> · {inst.usage.processes} 个进程
      </>
    );
    line2 = `${sessions} 个会话 · ${inst.inflight ? `处理中 ${inst.inflight}` : `空闲 ${since(inst.lastActiveAt)}`}`;
  } else if (svc?.state === 'failed') {
    line1 = <span className="text-red-600">{firstLine(svc.lastError)}</span>;
    line2 = '启动失败';
  } else if (svc) {
    line1 = '调用时启动';
    line2 = `${sessions} 个会话`;
  }

  return (
    <Card
      onClick={() => svc && go('services', svc.name)}
      className={cx(
        'flex items-center gap-4 px-5 py-3.5 transition-colors',
        svc && 'cursor-pointer',
        inst ? 'border-blue-500 bg-[#eef5ff] ring-1 ring-blue-500/25 dark:bg-blue-950/30' : 'hover:border-zinc-300 dark:hover:border-zinc-700',
      )}
    >
      <ServiceAvatar name={row.name} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-base font-semibold">{row.name}</span>
          {svc ? <Badge tone="blue">MCPcell</Badge> : <Badge tone={badge.tone}>{badge.text}</Badge>}
          {svc?.package && <Badge>npm {svc.package.version}</Badge>}
          {svc?.catalog && <Badge>{svc.catalog.tools} 个工具</Badge>}
        </div>
        <div className="mt-0.5 truncate font-mono text-[13px] text-zinc-500" title={row.reason}>
          {row.launch}
          {row.detail && <span className="text-zinc-400"> {row.detail}</span>}
        </div>
      </div>

      <div className="flex shrink-0 gap-2" onClick={(e) => e.stopPropagation()}>
        {agents.map((a) => (
          <AgentPill key={a.id} agent={a} cell={row.agents[a.id]} row={row} onClick={() => onCell(a, row.agents[a.id])} />
        ))}
      </div>

      <div className="w-[150px] shrink-0 text-right text-sm leading-6 text-zinc-500">
        <div className="truncate">{line1}</div>
        <div className="truncate">{line2}</div>
      </div>
      <div className="flex w-[140px] shrink-0 items-center justify-end gap-0.5" onClick={(e) => e.stopPropagation()}>
        {svc &&
          (inst ? (
            <IconButton label="回收进程（会话保留，下次调用自动启动）" busy={busy} onClick={() => serviceActs.stop(svc.name)}>
              <Square className="size-4" />
            </IconButton>
          ) : (
            <IconButton label="立即启动" busy={busy} onClick={() => serviceActs.start(svc.name)}>
              <Play className="size-[18px]" />
            </IconButton>
          ))}
        {svc && (
          <IconButton label="编辑" onClick={() => go('services', svc.name, 'edit')}>
            <Pencil className="size-[17px]" />
          </IconButton>
        )}
        {svc && (
          <Menu
            items={[
              { label: '详情与接入', icon: <ChevronRight className="size-4" />, onSelect: () => go('services', svc.name) },
              { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${svc.name}`, '已复制接入地址') },
              'sep' as const,
              { label: '删除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => serviceActs.askDelete(svc.name) },
            ]}
          />
        )}
      </div>
    </Card>
  );
}

/** One agent's state for an MCP, and the action a click takes. */
function AgentPill({ agent, cell, row, onClick }: { agent: AgentSummary; cell?: AgentCell; row: McpRow; onClick: () => void }) {
  let label = '—';
  let tone = 'border-transparent text-zinc-300 dark:text-zinc-600';
  let title = `${agent.name} 没有配置它`;
  let actionable = false;
  if (!cell) {
    if (row.canAdd) {
      [label, tone, title, actionable] = ['+ 添加', 'border-dashed border-zinc-300 text-zinc-500 hover:border-blue-400 hover:text-blue-600 dark:border-zinc-600', `加入 ${agent.name}，经 MCPcell 运行`, true];
    }
  } else {
    switch (cell.state) {
      case 'managed':
        [label, tone, title, actionable] = ['托管', 'border-blue-500 bg-blue-500 text-white hover:bg-blue-600', cell.added ? `MCPcell 加进 ${agent.name} 的；点击移除` : `${agent.name} 经 MCPcell 运行它；点击还原为原生`, true];
        break;
      case 'changed':
        [label, tone, title, actionable] = ['已改动', 'border-amber-300 bg-amber-50 text-amber-700', '托管后被修改过；点击忘记记录', true];
        break;
      case 'native': {
        const can = cell.class === 'shared' || (cell.class === 'unknown' && row.kind === 'stdio');
        label = '原生';
        tone = can
          ? 'border-zinc-300 bg-white text-zinc-700 hover:border-blue-400 hover:text-blue-600 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-200'
          : 'border-zinc-200 bg-zinc-50 text-zinc-400 dark:border-zinc-700 dark:bg-zinc-800';
        title = can ? `${agent.name} 每个会话自己启动它；点击${cell.class === 'unknown' ? '强制共享' : '交给 MCPcell 托管'}` : cell.reason ?? '';
        actionable = can;
        break;
      }
      case 'remote':
        [label, tone, title] = ['远程', 'border-zinc-200 bg-zinc-50 text-zinc-400 dark:border-zinc-700 dark:bg-zinc-800', '远程 HTTP 服务，不需要托管'];
        break;
      case 'internal':
        [label, tone, title] = ['内置', 'border-zinc-200 bg-zinc-50 text-zinc-400 dark:border-zinc-700 dark:bg-zinc-800', cell.reason ?? ''];
        break;
    }
    if (!cell.enabled) title += '（这个 Agent 里已停用）';
  }
  return (
    <button
      title={title}
      disabled={!actionable}
      onClick={onClick}
      className={cx(
        'inline-flex h-8 w-[88px] items-center justify-center gap-1.5 rounded-lg border text-xs font-medium transition-colors disabled:cursor-default',
        tone,
        cell && !cell.enabled && 'opacity-60',
      )}
    >
      <BrandMark brand={agentBrand[agent.id]} className={cx('size-3.5', cell?.state === 'managed' && 'text-white [&_path]:fill-current')} />
      {label}
    </button>
  );
}
