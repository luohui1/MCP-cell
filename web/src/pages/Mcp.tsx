import { useState, type ReactNode } from 'react';
import { ChevronRight, Copy, Layers, Package, Play, Plus, RefreshCw, RotateCw, Square, Trash2, Undo2 } from 'lucide-react';
import { instancesOf, type AgentSummary, type InstanceSnapshot, type SessionInfo } from '../api';
import { useServiceActions } from '../actions';
import { agentBrand, agentsOf, canTake, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { mainClients, masked } from '../clients';
import { useStatus, useUnified, type Unified } from '../data';
import { bytes, clock, firstLine, since, span, until } from '../format';
import { BrandMark, ServiceAvatar } from '../icons';
import { AgentFilter, Block, Crumbs, Detail, DetailHead, ListGroup, ListItem, ListScroll, ListTools, Placeholder, SearchBox, Split, StatusLine } from '../layout';
import { agentOfClient, failing, mcpView, type McpGroup, type McpView } from '../model';
import { candidates, OneClickDialog } from '../one-click';
import { go, href, redirect, useRoute } from '../router';
import { Button, Card, CodeBlock, CopyButton, cx, Menu, PageHeader, Segmented, useCopy } from '../ui';
import { NewService, ServiceSettings } from './ServiceForm';

// Level 2 and 3 for MCP: every MCP grouped by state, the selected one beside it.

const groups: { id: McpGroup; title: string; collapsed?: boolean }[] = [
  { id: 'attention', title: '需要处理' },
  { id: 'ok', title: '正常' },
  { id: 'unmanaged', title: '未托管', collapsed: true },
];

const toneRank = { red: 0, amber: 1, blue: 2, green: 3, gray: 4 };
const kindRank = (v: McpView) => (v.takeable ? 0 : v.row.kind === 'stdio' ? 1 : v.row.kind === 'http' ? 2 : 3);

export function McpPage({ id, agent }: { id?: string; agent: string }) {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const copy = useCopy();
  const edit = useRoute().query.get('edit') === '1';
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [oneClick, setOneClick] = useState<string[] | null>(null); // [] for every candidate
  const acts = useAgentActions(reload, data?.autostart ?? null);

  const agents = data ? agentsOf(data.agents) : [];
  const views = data ? data.rows.map((r) => mcpView(r, status, agents)) : [];
  const selected = id && id !== 'new' ? views.find((v) => v.id === id) : undefined;
  const q = search.trim().toLowerCase();
  const shown = views.filter((v) => (!agent || v.row.agents[agent]) && (!q || v.row.name.toLowerCase().includes(q)));
  const offers = data ? candidates(data, agent || undefined).length : 0;
  const unmanaged = shown.filter((v) => v.group === 'unmanaged' && v.takeable).map((v) => v.row.key);
  const allRestore = data ? data.rows.flatMap((r) => restoreSteps(r, agents)) : [];
  const link = (v: McpView) => href(['mcp', v.id], { agent });

  const crumbs = [{ label: 'MCP', href: href(['mcp'], { agent }) }];
  if (id === 'new') crumbs.push({ label: '添加', href: '' });
  else if (id) crumbs.push({ label: selected?.row.name ?? id, href: '' });

  const list = (
    <>
      <ListTools>
        <AgentFilter
          value={agent}
          total={views.length}
          options={agents.map((a) => ({ id: a.id, name: a.name, count: views.filter((v) => v.row.agents[a.id]).length }))}
          onChange={(a) => redirect(id ? ['mcp', id] : ['mcp'], { agent: a })}
        />
        <SearchBox value={search} onChange={setSearch} placeholder="搜索 MCP" />
      </ListTools>
      <ListScroll>
        {!data && <p className="py-10 text-center text-sm text-zinc-400">{error || '正在汇总各 Agent 的 MCP…'}</p>}
        {data && shown.length === 0 && <p className="py-10 text-center text-sm text-zinc-400">没有符合条件的 MCP</p>}
        {groups.map((g) => {
          const rows = shown
            .filter((v) => v.group === g.id)
            .sort((a, b) => toneRank[a.tone] - toneRank[b.tone] || kindRank(a) - kindRank(b) || a.row.name.localeCompare(b.row.name));
          if (!rows.length) return null;
          const collapsed = g.collapsed && !(selected?.group === g.id) && !q;
          const expanded = open[g.id] ?? !collapsed;
          return (
            <ListGroup
              key={g.id}
              title={g.title}
              count={rows.length}
              open={expanded}
              onToggle={() => setOpen((o) => ({ ...o, [g.id]: !expanded }))}
              action={
                g.id === 'unmanaged' &&
                unmanaged.length > 0 && (
                  <Button size="sm" variant="ghost" className="!h-7 !px-2 text-blue-600 dark:text-blue-400" onClick={() => setOneClick(unmanaged)}>
                    一键托管 {unmanaged.length} 个
                  </Button>
                )
              }
            >
              {rows.map((v) => (
                <ListItem key={v.id} href={link(v)} selected={v === selected} icon={<ServiceAvatar name={v.row.name} size="sm" />} name={v.row.name} tone={v.tone} text={v.text} />
              ))}
            </ListGroup>
          );
        })}
      </ListScroll>
    </>
  );

  let detail: ReactNode;
  if (id === 'new') {
    detail = (
      <Detail>
        <DetailHead icon={<ServiceAvatar name="" size="md" />} title="添加 MCP 服务" status="直接在 MCPcell 里运行；Agent 可以用接入地址连接，或从「谁在用」加入" />
        <NewService />
      </Detail>
    );
  } else if (selected && data) {
    detail = <McpDetail key={selected.id} view={selected} data={data} agents={agents} edit={edit} />;
  } else if (id && data) {
    detail = (
      <Placeholder icon={<Layers className="size-6" />} title={`找不到 ${id}`}>
        它可能已被删除或还原。<a className="text-blue-600 hover:underline" href={href(['mcp'], { agent })}>回到列表</a>
      </Placeholder>
    );
  } else {
    const count = (g: McpGroup) => views.filter((v) => v.group === g).length;
    detail = (
      <Placeholder icon={<Layers className="size-6" />} title="选择左侧的一个 MCP">
        {data && `共 ${views.length} 个：${count('attention')} 个需要处理，${count('ok')} 个由 MCPcell 运行，${count('unmanaged')} 个未托管。`}
      </Placeholder>
    );
  }

  return (
    <>
      <PageHeader
        title={<Crumbs items={crumbs} />}
        actions={
          <>
            <Button icon={<Plus className="size-4" />} onClick={() => go(['mcp', 'new'], { agent })}>
              添加
            </Button>
            <Menu
              items={[
                { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                { label: '一键托管', hint: offers ? `还有 ${offers} 个可以托管` : '没有可以托管的了', icon: <Layers className="size-4" />, disabled: !offers, onSelect: () => setOneClick([]) },
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
          </>
        }
      />
      <Split list={list} detail={detail} />
      {data && (
        <OneClickDialog
          open={oneClick !== null}
          only={agent || undefined}
          keys={oneClick?.length ? oneClick : undefined}
          onClose={() => setOneClick(null)}
          data={data}
          reload={reload}
        />
      )}
      {acts.dialog}
    </>
  );
}

// ---- detail ----------------------------------------------------------------

function McpDetail({ view, data, agents, edit }: { view: McpView; data: Unified; agents: AgentSummary[]; edit: boolean }) {
  const { status } = useStatus();
  const { reload } = useUnified();
  const acts = useAgentActions(reload, data.autostart);
  const svcActs = useServiceActions();
  const copy = useCopy();
  const { row } = view;
  const svc = status.services.find((s) => s.name === row.service);
  const take = takeSteps(row, agents);
  const back = restoreSteps(row, agents);
  const broken = view.services.find(failing);
  const running = view.services.some((s) => instancesOf(s).length > 0);
  const perSession = row.mode === 'per-session';
  const busy = svc && (svcActs.busy[svc.name] || svc.state === 'starting');

  const menu = svc
    ? [
        running
          ? { label: perSession ? '回收全部实例' : '回收进程', hint: '会话保留，下次调用自动启动', icon: <Square className="size-4" />, onSelect: () => view.services.forEach((s) => instancesOf(s).length && svcActs.stop(s.name)) }
          : perSession
            ? { label: '检查启动', hint: '启动一次并缓存工具列表', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) }
            : { label: '立即启动', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) },
        ...(svc.package ? [{ label: '更新 npm 包', hint: `现在是 ${svc.package.version}`, icon: <Package className="size-4" />, onSelect: () => svcActs.update(svc.name) }] : []),
        { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${svc.name}`, '已复制接入地址') },
        'sep' as const,
        ...(back.length
          ? [{ label: '取消托管', hint: '各 Agent 恢复托管前的配置', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back, `取消托管 ${row.name}`) }]
          : []),
        { label: '删除服务', hint: '从 config.json 移除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => svcActs.askDelete(svc.name) },
      ]
    : back.length
      ? [{ label: '还原 Agent 配置', hint: '恢复托管前的条目', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back) }]
      : [];

  return (
    <Detail>
      <DetailHead
        icon={<ServiceAvatar name={row.name} size="md" />}
        title={row.name}
        status={<StatusLine tone={view.tone}>{view.text}</StatusLine>}
        actions={
          <>
            {!row.service && take.length > 0 && back.length === 0 && (
              <Button variant="primary" icon={<Layers className="size-4" />} onClick={() => acts.takeover(take)}>
                托管
              </Button>
            )}
            {broken && (
              <Button icon={<RotateCw className="size-4" />} busy={busy} onClick={() => svcActs.start(broken.name)}>
                重新检查
              </Button>
            )}
            {menu.length > 0 && <Menu items={menu} />}
          </>
        }
      />

      {broken && (
        <ErrorCard
          name={broken.name === row.service ? undefined : '改过的条目'}
          wrapped={back.length > 0}
          error={broken.lastError}
          at={broken.lastErrorAt}
          retryAt={broken.retryAt}
          stderr={broken.stderrTail}
        />
      )}

      <Block title="运行">
        <RunInfo view={view} />
      </Block>

      <Block title="谁在用">
        <UsedBy view={view} agents={agents} acts={acts} />
      </Block>

      {row.service && (
        <Block title="设置" bare>
          <ServiceSettings name={row.service} svc={svc} openAdvanced={edit} />
        </Block>
      )}

      {row.service && <ManualConnect name={row.service} />}
      {acts.dialog}
      {svcActs.deleteDialog}
    </Detail>
  );
}

function ErrorCard({ name, wrapped, error, at, retryAt, stderr }: { name?: string; wrapped: boolean; error?: string; at?: string; retryAt?: string; stderr?: string }) {
  return (
    <Card className="border-red-200 bg-red-50/50 p-5 dark:border-red-900 dark:bg-red-950/20">
      <div className="text-sm font-semibold text-red-700 dark:text-red-400">
        最近一次启动失败{name && `（${name}）`}
        <span className="ml-2 font-normal text-red-500">
          {at && `${since(at)}前`}
          {retryAt && ` · ${until(retryAt)}后自动重试`}
        </span>
      </div>
      <p className="mt-1.5 text-sm leading-6 text-red-700 dark:text-red-300">{firstLine(error)}</p>
      <p className="mt-1 text-[13px] leading-6 text-red-600/80 dark:text-red-300/70">
        {wrapped && '起不来期间，托管的会话会直接运行原命令，Agent 里照常可用。'}修好原因后点「重新检查」。
      </p>
      {stderr && (
        <details className="mt-2">
          <summary className="cursor-pointer text-[13px] text-red-700 dark:text-red-400">进程输出</summary>
          <pre className="mt-2 overflow-x-auto rounded-xl bg-white/70 p-3 font-mono text-xs leading-5 whitespace-pre-wrap text-zinc-700 dark:bg-zinc-900 dark:text-zinc-300">{stderr}</pre>
        </details>
      )}
    </Card>
  );
}

/** How it runs, when it is reaped, and what runs now. */
function RunInfo({ view }: { view: McpView }) {
  const { status } = useStatus();
  const { row } = view;
  if (!row.service && view.group === 'unmanaged') {
    if (row.kind === 'http') return <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">远程 HTTP 服务（<span className="font-mono text-[13px]">{row.launch}</span>），Agent 直接连接，本机没有进程，不需要托管。</p>;
    if (row.class === 'internal') return <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">Agent 自带的 MCP，由 Agent 自己运行，不需要托管。</p>;
    return (
      <div className="space-y-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">
        <p>
          现在由 Agent 直接运行 <span className="font-mono text-[13px]">{row.launch}</span>{row.detail && <span className="font-mono text-[13px] text-zinc-400"> {row.detail}</span>}，每个会话各起一份进程。
        </p>
        {view.takeable ? (
          <p className="text-zinc-500">
            托管后
            {row.mode === 'shared' ? '所有会话共用一份进程，空闲后回收。' : '按会话运行：每个会话一个实例，第一次调用时才启动，会话结束就回收。'}
            工具名不变，随时可以还原。
          </p>
        ) : (
          <p className="text-zinc-500">{row.reason}</p>
        )}
      </div>
    );
  }
  const svc = status.services.find((s) => s.name === row.service);
  if (!svc) return <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">MCPcell 里已经没有这个服务，Agent 里的条目还指向它。还原条目，或重新添加同名服务。</p>;

  const perSession = svc.mode === 'per-session';
  const idle = svc.idleTimeoutSec ? `空闲 ${span(svc.idleTimeoutSec * 1000)}后回收` : '不自动回收';
  const runs = view.services.flatMap(instancesOf);
  const mem = runs.reduce((n, i) => n + i.usage.privateBytes, 0);
  const procs = runs.reduce((n, i) => n + i.usage.processes, 0);
  const calls = runs.reduce((n, i) => n + i.calls, 0);
  const fails = runs.reduce((n, i) => n + i.failures, 0);
  const tools = svc.catalog ? [`${svc.catalog.tools} 个工具`, svc.catalog.prompts && `${svc.catalog.prompts} 个提示`, svc.catalog.resources && `${svc.catalog.resources} 个资源`].filter(Boolean).join('、') : '';
  return (
    <div className="space-y-4">
      <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">
        {perSession ? `按会话：每个会话一个实例，第一次调用时启动，会话结束或${idle}。` : `共享：所有会话共用一份进程，${idle}，下次调用自动启动。`}
        {tools ? ` 连接和列出工具由能力目录（${tools}）回答，不占进程。` : ' 能力目录还没缓存，第一次启动后建立。'}
      </p>
      {svc.note && <p className="rounded-lg bg-amber-50 px-3 py-2 text-[13px] leading-6 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">{svc.note}</p>}
      {runs.length > 0 ? (
        <div className="grid grid-cols-4 gap-3">
          <Fig label="内存（私有提交）" value={bytes(mem)} sub={perSession ? `${runs.length} 个实例` : `工作集 ${bytes(runs[0].usage.workingSetBytes)}`} />
          <Fig label="进程" value={`${procs} 个`} sub={svc.direct ? 'node 直接启动' : runs.length === 1 ? `PID ${runs[0].pid}` : ''} />
          <Fig label="调用" value={`${calls} 次`} sub={fails ? `失败 ${fails} 次` : '没有失败'} />
          <Fig label="已运行" value={since(oldest(runs).startedAt)} sub={`最近活动 ${since(newest(runs).lastActiveAt)}前`} />
        </div>
      ) : (
        <p className="text-sm text-zinc-500">现在没有进程{svc.state === 'starting' ? '，正在启动…' : '。'}</p>
      )}
      <p className="text-xs text-zinc-400">
        {[
          `累计启动 ${svc.starts} 次`,
          runs[0]?.serverName && `上游报告 ${runs[0].serverName} ${runs[0].serverVersion ?? ''}`.trim(),
          svc.memoryLimit ? `内存上限 ${bytes(svc.memoryLimit)}` : '',
        ]
          .filter(Boolean)
          .join(' · ')}
      </p>
      {svc.lastError && !failing(svc) && (
        <div className="text-[13px] text-zinc-500">
          最近一次错误：{firstLine(svc.lastError)}
          {svc.lastErrorAt && `（${since(svc.lastErrorAt)}前）`}
          {svc.stderrTail && (
            <details className="mt-1">
              <summary className="cursor-pointer text-xs text-zinc-400">进程输出</summary>
              <pre className="mt-1.5 overflow-x-auto rounded-lg bg-zinc-50 p-3 font-mono text-xs leading-5 whitespace-pre-wrap dark:bg-zinc-800/60">{svc.stderrTail}</pre>
            </details>
          )}
        </div>
      )}
    </div>
  );
}

const oldest = (r: InstanceSnapshot[]) => r.reduce((a, b) => (a.startedAt < b.startedAt ? a : b));
const newest = (r: InstanceSnapshot[]) => r.reduce((a, b) => (a.lastActiveAt > b.lastActiveAt ? a : b));

function Fig({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="rounded-xl bg-zinc-50 px-4 py-3 dark:bg-zinc-800/60">
      <div className="text-xs text-zinc-500">{label}</div>
      <div className="mt-1 truncate text-lg font-semibold tabular-nums">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-zinc-400">{sub}</div>}
    </div>
  );
}

/** Each agent's entry for this MCP, its sessions, and what can be done. */
function UsedBy({ view, agents, acts }: { view: McpView; agents: AgentSummary[]; acts: ReturnType<typeof useAgentActions> }) {
  const { row } = view;
  const [open, setOpen] = useState<string | null>(null);
  const instances = view.services.flatMap((s) => s.instances ?? []);
  const byAgent = (id: string) => view.sessions.filter((x) => agentOfClient(x.client) === id);
  const others = view.sessions.filter((x) => !agents.some((a) => a.id === agentOfClient(x.client)));
  const lines = agents
    .map((a) => {
      const c = row.agents[a.id];
      const sessions = byAgent(a.id);
      let state = '';
      let action: ReactNode = null;
      if (c?.state === 'managed') {
        state = c.added ? '由 MCPcell 加入' : '已托管';
        if (c.drift) state += ' · 条目托管后改过，按改后的设置单独运行';
        action = (
          <Button size="sm" variant="ghost" icon={<Undo2 className="size-3.5" />} onClick={() => acts.restore([{ agent: a, entry: c.entry, added: c.added }])}>
            {c.added ? '移除' : '还原'}
          </Button>
        );
      } else if (c?.state === 'changed') {
        state = '托管后条目被改过，MCPcell 不再管它';
        action = (
          <Button size="sm" onClick={() => acts.forget(a, c.entry)}>
            忘记记录
          </Button>
        );
      } else if (c && canTake(c)) {
        state = '原生运行，每个会话各起一份';
        action = (
          <Button size="sm" icon={<Layers className="size-3.5" />} onClick={() => acts.takeover(takeSteps(row, [a]))}>
            托管
          </Button>
        );
      } else if (c) {
        state = c.state === 'remote' ? '远程服务' : (c.reason ?? '原生运行');
      } else if (row.canAdd) {
        state = '没有配置';
        action = (
          <Button size="sm" variant="ghost" icon={<Plus className="size-3.5" />} onClick={() => acts.add(a, row)}>
            加入
          </Button>
        );
      } else return null;
      if (c && !c.enabled) state += `（在 ${a.name} 里已停用）`;
      return { id: a.id, brand: agentBrand[a.id], name: a.name, state, why: c?.drift ?? c?.reason, action, sessions };
    })
    .filter((l) => !!l);
  if (others.length) lines.push({ id: 'other', brand: '', name: '其他客户端', state: '经接入地址连接', why: undefined, action: null, sessions: others });
  if (!lines.length) return <p className="text-sm text-zinc-500">没有 Agent 配置它。</p>;

  return (
    <div className="-my-2 divide-y divide-zinc-100 dark:divide-zinc-800">
      {lines.map((l) => {
        const expanded = open === l.id && l.sessions.length > 0;
        return (
          <div key={l.id}>
            <div className="flex items-center gap-3 py-2.5">
              <button
                disabled={!l.sessions.length}
                onClick={() => setOpen(expanded ? null : l.id)}
                className="flex min-w-0 flex-1 items-center gap-3 text-left disabled:cursor-default"
                aria-expanded={expanded}
              >
                <ChevronRight className={cx('size-3.5 shrink-0 text-zinc-400 transition-transform', expanded && 'rotate-90', !l.sessions.length && 'invisible')} />
                <BrandMark brand={l.brand} className="size-4 shrink-0" />
                <span className="w-28 shrink-0 text-sm font-medium">{l.name}</span>
                <span className="min-w-0 flex-1 truncate text-[13px] text-zinc-500" title={l.why ? `${l.state}
${l.why}` : l.state}>
                  {l.state}
                  {l.sessions.length > 0 && <span className="text-zinc-700 dark:text-zinc-300"> · {l.sessions.length} 个会话</span>}
                </span>
              </button>
              {l.action}
            </div>
            {expanded && (
              <div className="mb-2.5 ml-[34px] space-y-1 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-zinc-800/50">
                {l.sessions.map((x) => (
                  <SessionLine key={x.id} x={x} inst={instances.find((i) => x.id.startsWith(i.session))} />
                ))}
              </div>
            )}
          </div>
        );
      })}
      {view.sessions.length === 0 && row.service && <p className="pt-2.5 text-[13px] text-zinc-400">现在没有会话连着它。</p>}
    </div>
  );
}

function SessionLine({ x, inst }: { x: SessionInfo; inst?: InstanceSnapshot }) {
  return (
    <div className="flex items-center gap-3 py-1 text-[13px]">
      <span className="min-w-0 flex-1 truncate">{x.client || '未知客户端'}</span>
      {inst && <span className="text-zinc-700 tabular-nums dark:text-zinc-200">{bytes(inst.usage.privateBytes)}</span>}
      <span className="text-zinc-500 tabular-nums">连接于 {clock(x.createdAt)}</span>
      <span className="w-28 text-right text-zinc-500">{x.inflight ? `处理中 ${x.inflight}` : `${since(x.lastSeenAt)}前活动`}</span>
    </div>
  );
}

/** The endpoint and a config snippet for agents MCPcell cannot edit itself. */
function ManualConnect({ name }: { name: string }) {
  const { status } = useStatus();
  const [shown, setShown] = useState(false);
  const [client, setClient] = useState(mainClients[0].id);
  const url = `${status.baseUrl}/s/${name}`;
  const spec = mainClients.find((c) => c.id === client)!;
  return (
    <section>
      <button onClick={() => setShown(!shown)} className="mb-2.5 flex items-center gap-1.5 px-1 text-[13px] font-medium text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200">
        <ChevronRight className={cx('size-3.5 transition-transform', shown && 'rotate-90')} />
        手动接入
        <span className="font-normal text-zinc-400">用接入地址连接，不经 mcpcell-run</span>
      </button>
      {shown && (
        <Card className="p-5">
          <div className="mb-4 flex items-center gap-2">
            <code className="rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{url}</code>
            <CopyButton text={url} label="复制接入地址" />
          </div>
          <Segmented value={client} onChange={setClient} options={mainClients.map((c) => ({ value: c.id, label: c.name }))} />
          <p className="mt-4 mb-2 text-sm text-zinc-500">
            替换 <span className="text-zinc-800 dark:text-zinc-200">{spec.file}</span> 里同名的条目，然后重启客户端。工具全名保持不变。
          </p>
          <CodeBlock shown={spec.snippet!([{ name, url }], masked(status.token))} copy={spec.snippet!([{ name, url }], status.token)} />
          <p className="mt-2 text-xs text-zinc-400">复制的内容包含完整访问令牌。其他 Agent 见「设置 › 手动接入」。</p>
        </Card>
      )}
    </section>
  );
}
