import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { ArrowLeft, ChevronRight, Copy, Layers, Package, Play, RotateCw, Square, Trash2, Undo2 } from 'lucide-react';
import { instancesOf, metricsApi, type AgentSummary, type InstanceSnapshot, type SessionInfo } from '../api';
import { useServiceActions } from '../actions';
import { agentsOf, brandOfAgent, canTake, isProject, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { MemoryChart, readBytes } from '../chart';
import { mainClients, masked } from '../clients';
import { usePolled, useStatus, useUnified, type Unified } from '../data';
import { bytes, clock, firstLine, since, until } from '../format';
import { BrandMark, ServiceAvatar } from '../icons';
import { Block, Detail, DetailHead, Placeholder, StatusLine } from '../layout';
import { advice, agentOfClient, mcpView, plainReason, type McpView } from '../model';
import { href, useRoute } from '../router';
import { StatGrid } from '../stats';
import { Button, Card, CodeBlock, CopyButton, cx, Menu, Segmented, useCopy } from '../ui';
import { NewService, ServiceSettings } from './ServiceForm';

// One MCP: what it is for, which apps use it, how it is doing; the technical
// settings sit under 高级.

export function McpPage({ id }: { id: string }) {
  const { status } = useStatus();
  const { data, error } = useUnified();
  const edit = useRoute().query.get('edit') === '1';
  const views = data && id !== 'new' ? data.rows.map((r) => mcpView(r, status, data.auto)) : [];
  const view = views.find((v) => v.id === id) ?? views.find((v) => v.row.name === id); // a name works too
  let body: ReactNode;
  if (id === 'new') {
    body = (
      <Detail>
        <DetailHead icon={<Back />} title="添加 MCP" />
        <NewService />
      </Detail>
    );
  } else if (view && data) {
    body = <McpDetail key={view.id} view={view} data={data} edit={edit} />;
  } else if (data) {
    body = (
      <Placeholder icon={<Layers className="size-6" />} title={`找不到 ${id}`}>
        <a className="text-blue-600 hover:underline" href={href([])}>
          返回
        </a>
      </Placeholder>
    );
  } else {
    body = <p className="py-12 text-center text-[13px] text-zinc-400">{error || '正在读取…'}</p>;
  }
  return <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>;
}

function Back() {
  return (
    <a href={href([])} title="返回" aria-label="返回" className="-ml-1.5 grid size-8 shrink-0 place-items-center rounded-lg text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800">
      <ArrowLeft className="size-4" />
    </a>
  );
}

function McpDetail({ view, data, edit }: { view: McpView; data: Unified; edit: boolean }) {
  const { status } = useStatus();
  const { reload } = useUnified();
  const acts = useAgentActions(reload, data.autostart);
  const svcActs = useServiceActions();
  const copy = useCopy();
  const [advanced, setAdvanced] = useState(edit);
  const agents = agentsOf(data.agents);
  const { row } = view;
  const svc = status.services.find((s) => s.name === row.service);
  const take = takeSteps(row, agents);
  const back = restoreSteps(row, agents);
  const running = view.services.some((s) => instancesOf(s).length > 0);
  const busy = svc && (svcActs.busy[svc.name] || svc.state === 'starting');

  const menu = svc
    ? [
        running
          ? {
              label: '现在关掉',
              icon: <Square className="size-4" />,
              onSelect: () => view.services.forEach((s) => instancesOf(s).length && svcActs.stop(s.name)),
            }
          : { label: '打开一次试试', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) },
        ...(svc.package
          ? [{ label: `更新（现在 ${svc.package.version}）`, icon: <Package className="size-4" />, onSelect: () => svcActs.update(svc.name) }]
          : []),
        { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${svc.name}`, '已复制接入地址') },
        'sep' as const,
        ...(back.length
          ? [{ label: '改回原样', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back, `${row.name} 改回原样`) }]
          : []),
        { label: '从 MCPcell 删除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => svcActs.askDelete(svc.name) },
      ]
    : back.length
      ? [{ label: '改回原样', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back) }]
      : [];

  return (
    <Detail>
      <DetailHead
        icon={
          <>
            <Back />
            <ServiceAvatar name={row.name} size="sm" />
          </>
        }
        title={row.name}
        status={<StatusLine tone={view.tone}>{view.text}</StatusLine>}
        actions={
          <>
            {!row.service && take.length > 0 && back.length === 0 && (
              <Button size="sm" variant="primary" icon={<Layers className="size-3.5" />} onClick={() => acts.takeover(take)}>
                改为共用
              </Button>
            )}
            {view.broken && (
              <Button size="sm" icon={<RotateCw className="size-3.5" />} busy={busy} onClick={() => svcActs.start(view.broken!.name)}>
                再试一次
              </Button>
            )}
            {menu.length > 0 && <Menu items={menu} />}
          </>
        }
      />
      {view.purpose && <p className="-mt-1 text-[13px] text-zinc-500">{view.purpose}</p>}

      {!view.broken && view.autoError && <Broken error={view.autoError} wrapped={false} retry="改为共用" />}
      {view.broken && (
        <Broken error={view.broken.lastError} at={view.broken.lastErrorAt} retryAt={view.broken.retryAt} stderr={view.broken.stderrTail} wrapped={back.length > 0} />
      )}

      {svc && view.kind !== 'native' && <Numbers view={view} />}

      <Block title="哪些应用在用">
        <UsedBy view={view} agents={agents} acts={acts} />
      </Block>

      {row.service && (
        <section>
          <button
            onClick={() => setAdvanced(!advanced)}
            className="mb-1.5 flex items-center gap-1.5 px-1 text-xs font-medium text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
            aria-expanded={advanced}
          >
            <ChevronRight className={cx('size-3.5 transition-transform', advanced && 'rotate-90')} />
            高级
          </button>
          {advanced && (
            <div className="space-y-4">
              <ServiceSettings name={row.service} svc={svc} openAdvanced={edit} />
              <Technical view={view} />
              <ManualConnect name={row.service} />
            </div>
          )}
        </section>
      )}
      {acts.dialog}
      {svcActs.deleteDialog}
    </Detail>
  );
}

function Broken({ error, at, retryAt, stderr, wrapped, retry = '再试一次' }: { error?: string; at?: string; retryAt?: string; stderr?: string; wrapped: boolean; retry?: string }) {
  const reason = plainReason(error);
  return (
    <Card className="border-red-200 bg-red-50/50 p-5 dark:border-red-900 dark:bg-red-950/20">
      <div className="text-[15px] font-semibold text-red-700 dark:text-red-400">打不开：{reason}</div>
      <p className="mt-1.5 text-sm leading-6 text-red-700/90 dark:text-red-300">
        {advice(reason).replace('「再试一次」', `「${retry}」`)}
        {wrapped && ' 打不开的这段时间，应用会像原来一样自己开它，不影响使用。'}
      </p>
      <details className="mt-2">
        <summary className="cursor-pointer text-[13px] text-red-700/80 dark:text-red-400">
          具体出错信息
          <span className="ml-2 text-red-500/80">
            {at && `${since(at)}前`}
            {retryAt && ` · ${until(retryAt)}后自动再试`}
          </span>
        </summary>
        <pre className="mt-2 overflow-x-auto rounded-xl bg-white/70 p-3 font-mono text-xs leading-5 whitespace-pre-wrap text-zinc-700 dark:bg-zinc-900 dark:text-zinc-300">
          {[error, stderr].filter(Boolean).join('\n\n')}
        </pre>
      </details>
    </Card>
  );
}

/** This MCP in numbers, and its memory over the last hour. */
function Numbers({ view }: { view: McpView }) {
  const { status } = useStatus();
  const svc = status.services.find((s) => s.name === view.row.service)!;
  const metrics = usePolled(useCallback(() => metricsApi.load('1h', svc.name), [svc.name]), 10_000);
  const [nowT, setNowT] = useState(() => Date.now() / 1000);
  useEffect(() => setNowT(Date.now() / 1000), [metrics]);
  const runs = view.services.flatMap(instancesOf);
  const used = runs.reduce((n, i) => n + i.usage.privateBytes, 0);
  const oldest = runs.reduce<string | undefined>((a, i) => (!a || i.startedAt < a ? i.startedAt : a), undefined);
  const conv = new Set(view.sessions.map((x) => (x.agentPid ? `p${x.agentPid}` : `s${x.id}`))).size;
  const calls = view.services.reduce((n, s) => n + (s.calls ?? 0), 0);
  const last = view.services.map((s) => s.lastUsedAt).filter((t): t is string => !!t).sort().at(-1);
  return (
    <section className="space-y-3">
      <StatGrid
        cells={[
          {
            label: '现在占用',
            value: used ? readBytes(used) : '待命',
            sub: runs.length > 1 ? `开着 ${runs.length} 份，每个对话一份` : oldest ? `已经开了 ${since(oldest)}` : '没人用时不占内存',
          },
          {
            label: '平时开一份',
            value: svc.typicalBytes ? readBytes(svc.typicalBytes) : '—',
            sub: svc.typicalRuns ? `最近 ${svc.typicalRuns} 次运行的中位数` : '打开过一次后才知道',
          },
          { label: '在用的对话', value: conv, sub: svc.mode === 'per-session' ? '每个对话单独一份' : '所有对话共用一份' },
          { label: '调用', value: calls, sub: last ? `${since(last)}前用过` : 'MCPcell 这次启动后还没用过' },
        ]}
      />
      <Card className="px-4 pt-3 pb-1.5">
        <h3 className="mb-1 text-xs text-zinc-500">最近 1 小时</h3>
        <MemoryChart
          points={metrics?.points ?? []}
          step={metrics?.step ?? 10}
          from={nowT - 3600}
          to={nowT}
          height={130}
          series={[{ key: 'total', label: '占用' }]}
          empty={metrics ? '最近 1 小时没打开过' : '正在读取…'}
        />
      </Card>
    </section>
  );
}

/** Each app's use of this MCP, its open conversations, and what can be done. */
function UsedBy({ view, agents, acts }: { view: McpView; agents: AgentSummary[]; acts: ReturnType<typeof useAgentActions> }) {
  const { data } = useUnified();
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
      let why: string | undefined;
      let action: ReactNode = null;
      if (c?.state === 'managed') {
        state = '共用';
        if (c.drift) {
          state += '（设置改过，单独一份）';
          why = c.drift;
        }
        action = (
          <Button size="sm" variant="ghost" icon={<Undo2 className="size-3.5" />} onClick={() => acts.restore([{ agent: a, entry: c.entry, added: c.added }])}>
            {c.added ? '去掉' : '改回原样'}
          </Button>
        );
      } else if (c?.state === 'changed') {
        state = '被改过';
        action = (
          <Button size="sm" onClick={() => acts.forget(a, c.entry)}>
            不再管它
          </Button>
        );
      } else if (c && canTake(c)) {
        const excluded = (data?.auto?.excluded ?? []).some((e) => e.client === a.id && e.name === c.entry);
        state = excluded ? '各开各的（你改回过）' : '各开各的';
        action = (
          <Button size="sm" icon={<Layers className="size-3.5" />} onClick={() => acts.takeover(takeSteps(row, [a]))}>
            改为共用
          </Button>
        );
      } else if (c) {
        state = c.state === 'remote' ? '远程' : c.state === 'internal' ? '自带' : '各开各的';
        why = c.reason;
      } else if (row.canAdd && row.service && view.kind !== 'broken' && !isProject(a.id)) {
        state = '没有';
        action = (
          <Button size="sm" variant="ghost" onClick={() => acts.add(a, row)}>
            加到 {a.name}
          </Button>
        );
      } else return null;
      if (c && !c.enabled) state += '（已关）';
      return { id: a.id, brand: brandOfAgent(a.id), name: a.name, state, why, action, sessions };
    })
    .filter((l) => !!l);
  if (others.length) lines.push({ id: 'other', brand: '', name: '其他程序', state: '用接入地址连着', why: undefined, action: null, sessions: others });
  if (!lines.length) return <p className="text-sm text-zinc-500">还没有应用用它。</p>;

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
                <span className="w-40 shrink-0 truncate text-[13px] font-medium" title={l.name}>{l.name}</span>
                <span className="min-w-0 flex-1 truncate text-[13px] text-zinc-500" title={l.why ? `${l.state}\n${l.why}` : l.state}>
                  {l.state}
                  {l.sessions.length > 0 && <span className="text-zinc-700 dark:text-zinc-300"> · {l.sessions.length} 个对话</span>}
                </span>
              </button>
              {l.action}
            </div>
            {expanded && (
              <div className="mb-2.5 ml-[34px] space-y-1 rounded-lg bg-zinc-50 px-3 py-2 dark:bg-zinc-800/50">
                {l.sessions.map((x) => (
                  <Conversation key={x.id} x={x} inst={instances.find((i) => x.id.startsWith(i.session))} />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

function Conversation({ x, inst }: { x: SessionInfo; inst?: InstanceSnapshot }) {
  return (
    <div className="flex items-center gap-3 py-1 text-[13px]">
      <span className="min-w-0 flex-1 truncate">{x.client || '未知程序'}</span>
      {inst && <span className="text-zinc-700 tabular-nums dark:text-zinc-200">{bytes(inst.usage.privateBytes)}</span>}
      <span className="text-zinc-500 tabular-nums">{clock(x.createdAt)} 连上</span>
      <span className="w-32 text-right text-zinc-500">{x.inflight ? `正在处理 ${x.inflight} 个请求` : `${since(x.lastSeenAt)}前用过`}</span>
    </div>
  );
}

/** Numbers for the curious: processes, calls, tools, what the server reports. */
function Technical({ view }: { view: McpView }) {
  const { status } = useStatus();
  const svc = status.services.find((s) => s.name === view.row.service);
  if (!svc) return null;
  const runs = view.services.flatMap(instancesOf);
  const rows: [string, ReactNode][] = [
    [
      '进程',
      runs.length
        ? `${runs.reduce((n, i) => n + i.usage.processes, 0)} 个${svc.direct ? '（用 node 直接启动）' : ''}${runs.length === 1 ? `，PID ${runs[0].pid}` : ''}`
        : '现在没有',
    ],
    ['调用', runs.length ? `${runs.reduce((n, i) => n + i.calls, 0)} 次，失败 ${runs.reduce((n, i) => n + i.failures, 0)} 次` : '—'],
    ['工具', svc.catalog ? `${svc.catalog.tools} 个${svc.catalog.toolNames?.length ? `：${svc.catalog.toolNames.join('、')}` : ''}` : '还不知道（第一次打开后记下）'],
    ['它报告的名称', runs[0]?.serverName ? `${runs[0].serverName} ${runs[0].serverVersion ?? ''}` : '—'],
    ['累计打开', `${svc.starts} 次`],
  ];
  if (svc.note) rows.push(['提示', <span className="text-amber-700 dark:text-amber-300">{svc.note}</span>]);
  if (svc.lastError && !view.broken)
    rows.push([
      '最近出错',
      <>
        {firstLine(svc.lastError)}
        {svc.lastErrorAt && <span className="text-zinc-400">（{since(svc.lastErrorAt)}前）</span>}
        {svc.stderrTail && (
          <details className="mt-1">
            <summary className="cursor-pointer text-xs text-zinc-400">输出</summary>
            <pre className="mt-1.5 overflow-x-auto rounded-lg bg-zinc-50 p-3 font-mono text-xs leading-5 whitespace-pre-wrap dark:bg-zinc-800/60">{svc.stderrTail}</pre>
          </details>
        )}
      </>,
    ]);
  return (
    <Card className="divide-y divide-zinc-100 dark:divide-zinc-800">
      {rows.map(([k, v]) => (
        <div key={k} className="grid grid-cols-[112px_minmax(0,1fr)] gap-4 px-5 py-3 text-sm">
          <span className="text-zinc-500">{k}</span>
          <span className="min-w-0 leading-6 break-words">{v}</span>
        </div>
      ))}
    </Card>
  );
}

/** The endpoint and a config snippet for apps MCPcell cannot edit itself. */
function ManualConnect({ name }: { name: string }) {
  const { status } = useStatus();
  const [client, setClient] = useState(mainClients[0].id);
  const url = `${status.baseUrl}/s/${name}`;
  const spec = mainClients.find((c) => c.id === client)!;
  return (
    <Card className="p-5">
      <div className="mb-1 text-sm font-medium">手动接入</div>
      <div className="mb-4 flex items-center gap-2">
        <code className="rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{url}</code>
        <CopyButton text={url} label="复制接入地址" />
      </div>
      <Segmented value={client} onChange={setClient} options={mainClients.map((c) => ({ value: c.id, label: c.name }))} />
      <p className="mt-3 mb-2 text-xs text-zinc-500">
        替换 <span className="text-zinc-800 dark:text-zinc-200">{spec.file}</span> 里同名的一项
      </p>
      <CodeBlock shown={spec.snippet!([{ name, url }], masked(status.token))} copy={spec.snippet!([{ name, url }], status.token)} />
    </Card>
  );
}
