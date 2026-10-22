import { useState, type ReactNode } from 'react';
import { ChevronRight, Copy, Layers, Package, Play, RotateCw, Square, Trash2, Undo2 } from 'lucide-react';
import { instancesOf, type AgentSummary, type InstanceSnapshot, type SessionInfo } from '../api';
import { useServiceActions } from '../actions';
import { agentBrand, agentsOf, canTake, restoreSteps, takeSteps, useAgentActions } from '../agents';
import { mainClients, masked } from '../clients';
import { useStatus, useUnified, type Unified } from '../data';
import { bytes, clock, firstLine, since, span, until } from '../format';
import { BrandMark, ServiceAvatar } from '../icons';
import { Block, Crumbs, Detail, DetailHead, Placeholder, StatusLine } from '../layout';
import { advice, agentOfClient, mcpView, plainReason, type McpView } from '../model';
import { href, useRoute } from '../router';
import { Button, Card, CodeBlock, CopyButton, cx, Menu, PageHeader, Segmented, useCopy } from '../ui';
import { NewService, ServiceSettings } from './ServiceForm';

// One MCP: what it is for, which apps use it, how it is doing; the technical
// settings sit under 高级.

export function McpPage({ id }: { id: string }) {
  const { status } = useStatus();
  const { data, error } = useUnified();
  const edit = useRoute().query.get('edit') === '1';
  const views = data && id !== 'new' ? data.rows.map((r) => mcpView(r, status, data.auto)) : [];
  const view = views.find((v) => v.id === id) ?? views.find((v) => v.row.name === id); // a name works too
  const crumbs = [
    { label: '我的 MCP', href: href([]) },
    { label: id === 'new' ? '添加' : (view?.row.name ?? id), href: '' },
  ];

  let body: ReactNode;
  if (id === 'new') {
    body = (
      <Detail>
        <DetailHead icon={<ServiceAvatar name="" size="md" />} title="添加 MCP" status="先加到 MCPcell，再在「哪些应用在用」里加到各个应用" />
        <NewService />
      </Detail>
    );
  } else if (view && data) {
    body = <McpDetail key={view.id} view={view} data={data} edit={edit} />;
  } else if (data) {
    body = (
      <Placeholder icon={<Layers className="size-6" />} title={`找不到 ${id}`}>
        它可能已被删除或改回原样。
        <a className="text-blue-600 hover:underline" href={href([])}>
          回到我的 MCP
        </a>
      </Placeholder>
    );
  } else {
    body = <p className="py-16 text-center text-sm text-zinc-400">{error || '正在读取…'}</p>;
  }
  return (
    <>
      <PageHeader title={<Crumbs items={crumbs} />} />
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
    </>
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
              hint: '用到时会自动再打开',
              icon: <Square className="size-4" />,
              onSelect: () => view.services.forEach((s) => instancesOf(s).length && svcActs.stop(s.name)),
            }
          : { label: '先打开一次', hint: '试试能不能正常打开', icon: <Play className="size-4" />, onSelect: () => svcActs.start(svc.name) },
        ...(svc.package
          ? [{ label: '更新到最新版本', hint: `现在是 ${svc.package.version}`, icon: <Package className="size-4" />, onSelect: () => svcActs.update(svc.name) }]
          : []),
        { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${svc.name}`, '已复制接入地址') },
        'sep' as const,
        ...(back.length
          ? [{ label: '全部改回原样', hint: '各个应用恢复成自己开', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back, `${row.name} 改回原样`) }]
          : []),
        { label: '从 MCPcell 删除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => svcActs.askDelete(svc.name) },
      ]
    : back.length
      ? [{ label: '全部改回原样', hint: '各个应用恢复成自己开', icon: <Undo2 className="size-4" />, danger: true, onSelect: () => acts.restore(back) }]
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
                改为共用
              </Button>
            )}
            {view.broken && (
              <Button icon={<RotateCw className="size-4" />} busy={busy} onClick={() => svcActs.start(view.broken!.name)}>
                再试一次
              </Button>
            )}
            {menu.length > 0 && <Menu items={menu} />}
          </>
        }
      />
      {view.purpose && <p className="-mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-300">{view.purpose}</p>}

      {!view.broken && view.autoError && <Broken error={view.autoError} wrapped={false} retry="改为共用" />}
      {view.broken && (
        <Broken error={view.broken.lastError} at={view.broken.lastErrorAt} retryAt={view.broken.retryAt} stderr={view.broken.stderrTail} wrapped={back.length > 0} />
      )}

      <Block title="哪些应用在用">
        <UsedBy view={view} agents={agents} acts={acts} />
      </Block>

      <Block title="现在">
        <Now view={view} />
      </Block>

      {row.service && (
        <section>
          <button
            onClick={() => setAdvanced(!advanced)}
            className="mb-2.5 flex items-center gap-1.5 px-1 text-[13px] font-medium text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
            aria-expanded={advanced}
          >
            <ChevronRight className={cx('size-3.5 transition-transform', advanced && 'rotate-90')} />
            高级
            <span className="font-normal text-zinc-400">运行方式、多久关掉、内存上限、版本、启动命令</span>
          </button>
          {advanced && (
            <div className="space-y-5">
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

/** How it is doing right now, in a sentence or two. */
function Now({ view }: { view: McpView }) {
  const { status } = useStatus();
  const { row } = view;
  const p = (children: ReactNode) => <p className="text-sm leading-7 text-zinc-600 dark:text-zinc-300">{children}</p>;
  if (view.kind === 'other') {
    return p(row.kind === 'http' ? '这是远程服务，应用直接连网使用，本机不用开，不需要处理。' : '这是应用自带的 MCP，由应用自己管，不需要处理。');
  }
  if (view.kind === 'native' || (!row.service && view.autoError)) {
    return p(
      <>
        现在由各个应用自己开：每开一个对话，就多开一份。
        {view.takeable
          ? row.mode === 'shared'
            ? '改为共用后，同一个 MCP 全机只开一份，没人用时自动关掉。'
            : '改为用 MCPcell 这份后，每个对话仍单独一份，但用到时才打开、对话结束就关掉。'
          : ` 现在还不能交给 MCPcell：${row.reason}`}
      </>,
    );
  }
  const svc = status.services.find((s) => s.name === row.service);
  if (!svc) return p('MCPcell 里已经没有它的设置了，但还有应用指向它。可以把这些应用改回原样，或者重新添加同名的 MCP。');
  const perSession = svc.mode === 'per-session';
  const idle = svc.idleTimeoutSec ? `没人用 ${span(svc.idleTimeoutSec * 1000)}后自动关掉` : '开着就不自动关';
  const runs = view.services.flatMap(instancesOf);
  const how = perSession ? '每个对话单独一份' : '所有对话共用一份';
  if (runs.length) {
    const oldest = runs.reduce((a, b) => (a.startedAt < b.startedAt ? a : b));
    return p(
      <>
        运行中，占 <b className="font-semibold tabular-nums">{bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}</b> 内存，已经开了 {since(oldest.startedAt)}。{how}
        {perSession && runs.length > 1 ? `，现在开着 ${runs.length} 份` : ''}；{idle}。
      </>,
    );
  }
  if (view.kind === 'broken') return p(`现在没有开着。${how}；能打开以后，${idle}。`);
  return p(`待命：现在没人在用，不占内存。应用用到它时自动打开（${how}），${idle}。`);
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
        state = c.added ? '在用 MCPcell 这份（是 MCPcell 加的）' : '在用 MCPcell 这份';
        if (c.drift) {
          state += '；它在这里的设置改过，所以单独开了一份';
          why = c.drift;
        }
        action = (
          <Button size="sm" variant="ghost" icon={<Undo2 className="size-3.5" />} onClick={() => acts.restore([{ agent: a, entry: c.entry, added: c.added }])}>
            {c.added ? '去掉' : '改回原样'}
          </Button>
        );
      } else if (c?.state === 'changed') {
        state = '这一项后来被改过，现在不用 MCPcell 这份';
        action = (
          <Button size="sm" onClick={() => acts.forget(a, c.entry)}>
            不再管它
          </Button>
        );
      } else if (c && canTake(c)) {
        const excluded = (data?.auto?.excluded ?? []).some((e) => e.client === a.id && e.name === c.entry);
        state = excluded ? '自己开着一份（你改回过，不会再自动托管）' : '自己开着一份，每个对话多开一份';
        action = (
          <Button size="sm" icon={<Layers className="size-3.5" />} onClick={() => acts.takeover(takeSteps(row, [a]))}>
            改为共用
          </Button>
        );
      } else if (c) {
        state = c.state === 'remote' ? '直接连这个远程服务' : c.state === 'internal' ? '应用自带' : '自己开着';
        why = c.reason;
      } else if (row.canAdd && row.service && view.kind !== 'broken') {
        state = '没有加';
        action = (
          <Button size="sm" variant="ghost" onClick={() => acts.add(a, row)}>
            加到 {a.name}
          </Button>
        );
      } else return null;
      if (c && !c.enabled) state += `（${a.name} 里关掉了）`;
      return { id: a.id, brand: agentBrand[a.id], name: a.name, state, why, action, sessions };
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
                <span className="w-28 shrink-0 text-sm font-medium">{l.name}</span>
                <span className="min-w-0 flex-1 truncate text-[13px] text-zinc-500" title={l.why ? `${l.state}\n${l.why}` : l.state}>
                  {l.state}
                  {l.sessions.length > 0 && <span className="text-zinc-700 dark:text-zinc-300"> · 正连着 {l.sessions.length} 个对话</span>}
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
      <p className="mb-3 text-[13px] text-zinc-500">不经 mcpcell-run，直接用接入地址连。其他应用见「设置 › 手动接入」。</p>
      <div className="mb-4 flex items-center gap-2">
        <code className="rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{url}</code>
        <CopyButton text={url} label="复制接入地址" />
      </div>
      <Segmented value={client} onChange={setClient} options={mainClients.map((c) => ({ value: c.id, label: c.name }))} />
      <p className="mt-4 mb-2 text-sm text-zinc-500">
        替换 <span className="text-zinc-800 dark:text-zinc-200">{spec.file}</span> 里同名的一项，然后重开应用。工具名保持不变。
      </p>
      <CodeBlock shown={spec.snippet!([{ name, url }], masked(status.token))} copy={spec.snippet!([{ name, url }], status.token)} />
      <p className="mt-2 text-xs text-zinc-400">复制的内容带完整访问令牌。</p>
    </Card>
  );
}
