import { useCallback, useEffect, useState } from 'react';
import { ArrowRight, ChevronRight, Info, Plug, Power, RefreshCw, Undo2 } from 'lucide-react';
import { takeover, type AutostartStatus, type ClientEntry, type ClientView, type TakeoverResult } from '../api';
import { useStatus } from '../App';
import { clients as manualClients, masked } from '../clients';
import { since } from '../format';
import { BrandMark, ServiceAvatar, Tile } from '../icons';
import { href } from '../router';
import { Badge, Button, Card, CodeBlock, cx, IconButton, Modal, PageHeader, useToast } from '../ui';

const brandOf: Record<string, string> = { 'claude-code': 'claude', codex: 'openai', cursor: 'cursor' };
const restartHint: Record<string, string> = {
  'claude-code': '已经打开的 Claude Code 会话要重新启动，才会改连 MCPcell',
  codex: '重启 Codex（包括桌面版）后生效',
  cursor: '在 Cursor 的 MCP 设置里刷新，或重启 Cursor',
};

interface Pending {
  client: ClientView;
  entries: ClientEntry[];
  force: boolean;
}

export function ClientsPage() {
  const { refresh: refreshStatus } = useStatus();
  const toast = useToast();
  const [views, setViews] = useState<ClientView[] | null>(null);
  const [auto, setAuto] = useState<AutostartStatus | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [results, setResults] = useState<{ client: ClientView; results: TakeoverResult[] } | null>(null);
  const [enableAuto, setEnableAuto] = useState(true);

  const load = useCallback(async () => {
    try {
      const r = await takeover.overview();
      setViews(r.clients);
      setAuto(r.autostart);
      setError('');
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    load();
  }, [load]);

  async function attach(p: Pending) {
    setBusy(p.client.id);
    setPending(null);
    try {
      if (enableAuto && auto?.supported && !auto.enabled) setAuto(await takeover.autostart(true));
      const r = await takeover.attach(
        p.client.id,
        p.entries.map((e) => e.name),
        p.force,
      );
      setResults({ client: p.client, results: r.results });
    } catch (e) {
      toast((e as Error).message, 'error');
    } finally {
      setBusy(null);
      await Promise.all([load(), refreshStatus()]);
    }
  }

  async function restore(client: ClientView, names: string[], forget = false) {
    setBusy(client.id);
    try {
      const r = await takeover.restore(client.id, names, forget);
      const failed = r.results.filter((x) => !x.ok);
      if (failed.length) toast(`${failed[0].name}：${failed[0].error}`, 'error');
      else toast(forget ? '已忘记接管记录，保留你修改后的配置' : `已还原 ${names.length} 个条目，${restartHint[client.id] ?? '重启客户端后生效'}`);
    } catch (e) {
      toast((e as Error).message, 'error');
    } finally {
      setBusy(null);
      await load();
    }
  }

  async function toggleAuto(on: boolean) {
    try {
      setAuto(await takeover.autostart(on));
      toast(on ? '已开启开机自启' : '已关闭开机自启');
    } catch (e) {
      toast((e as Error).message, 'error');
    }
  }

  const detected = views?.filter((v) => v.found || v.error) ?? [];
  const automated = new Set(views?.map((v) => v.id) ?? []);

  return (
    <>
      <PageHeader
        icon={<Plug className="size-6 text-zinc-700 dark:text-zinc-300" />}
        title="客户端接入"
        subtitle="接管后，客户端里的 MCP 由 MCPcell 统一运行"
        actions={
          <IconButton label="刷新" onClick={load}>
            <RefreshCw className="size-[18px]" />
          </IconButton>
        }
      />
      <div className="mx-auto w-full max-w-[1200px] space-y-5 px-8 py-6">
        {auto?.supported && !auto.enabled && (
          <div className="flex items-center gap-4 rounded-2xl border border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
            <Power className="size-5 shrink-0" />
            <div className="flex-1 leading-6">
              接管后，客户端依赖 MCPcell 守护进程；它没运行时，这些 MCP 不可用。建议开启开机自启（无窗口运行，日志在 ~/.mcpcell/daemon.log）。
            </div>
            <Button className="h-9" variant="brand" onClick={() => toggleAuto(true)}>
              开启自启
            </Button>
          </div>
        )}
        {error && <Card className="border-red-200 p-5 text-sm text-red-700">{error}</Card>}
        {views === null && !error && <p className="py-16 text-center text-sm text-zinc-400">正在读取客户端配置…</p>}

        {detected.map((v) => (
          <ClientCard
            key={v.id}
            view={v}
            busy={busy === v.id}
            onAttach={(entries, force) => setPending({ client: v, entries, force })}
            onRestore={(names, forget) => restore(v, names, forget)}
          />
        ))}
        {views && detected.length === 0 && <Card className="p-8 text-center text-sm text-zinc-500">没有找到 Claude Code、Codex 或 Cursor 的 MCP 配置。</Card>}

        <ManualClients skip={automated} />
      </div>

      <Modal open={pending !== null} onClose={() => setPending(null)}>
        {pending && (
          <>
            <h2 className="text-base font-semibold">
              接管 {pending.client.name} 的 {pending.entries.length} 个条目
            </h2>
            <div className="mt-4 max-h-60 space-y-2 overflow-y-auto">
              {pending.entries.map((e) => (
                <div key={e.name} className="flex items-center gap-2 text-sm">
                  <span className="font-medium">{e.name}</span>
                  <ArrowRight className="size-3.5 text-zinc-400" />
                  <span className="font-mono text-[13px]">/s/{e.service}</span>
                  {!e.serviceExists && <Badge tone="blue">新建服务</Badge>}
                </div>
              ))}
            </div>
            <ul className="mt-4 list-disc space-y-1.5 pl-5 text-sm leading-6 text-zinc-500">
              <li>
                只改 <span className="font-mono text-[13px] text-zinc-700 dark:text-zinc-300">{pending.client.path}</span> 里这些条目，其余内容不动；改之前先备份整个文件
              </li>
              {pending.client.id === 'claude-code' && <li>通过 Claude Code 自带的 claude mcp 命令修改，不和正在运行的会话抢写文件</li>}
              <li>{restartHint[pending.client.id]}</li>
              <li>随时可以在这里还原，或在终端运行 mcpcell restore --all</li>
            </ul>
            {pending.force && (
              <p className="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
                MCPcell 不确定这些服务有没有会话状态。如果它们会为不同客户端保存不同的状态（浏览器页面、连接、登录），共享后会互相干扰；出问题时还原即可。
              </p>
            )}
            {auto?.supported && !auto.enabled && (
              <label className="mt-4 flex items-center gap-2 text-sm">
                <input type="checkbox" checked={enableAuto} onChange={(e) => setEnableAuto(e.target.checked)} className="size-4 accent-blue-500" />
                同时开启开机自启（推荐）
              </label>
            )}
            <div className="mt-6 flex justify-end gap-2">
              <Button onClick={() => setPending(null)}>取消</Button>
              <Button variant="primary" onClick={() => attach(pending)}>
                确认接管
              </Button>
            </div>
          </>
        )}
      </Modal>

      <Modal open={results !== null} onClose={() => setResults(null)}>
        {results && (
          <>
            <h2 className="text-base font-semibold">
              {results.results.every((r) => r.ok) ? '接管完成' : '部分条目没有接管'}
            </h2>
            <div className="mt-4 space-y-2">
              {results.results.map((r) => (
                <div key={r.name} className="flex items-start gap-2 text-sm">
                  <span className={cx('mt-0.5', r.ok ? 'text-emerald-600' : 'text-red-600')}>{r.ok ? '✓' : '✗'}</span>
                  <span className="font-medium">{r.name}</span>
                  <span className="text-zinc-500">{r.ok ? `→ /s/${r.service}${r.created ? '（新建服务）' : ''}` : r.error}</span>
                </div>
              ))}
            </div>
            {results.results.some((r) => r.ok) && (
              <p className="mt-4 text-sm leading-6 text-zinc-500">{restartHint[results.client.id]}。之后这些 MCP 的进程只会在 MCPcell 里出现一份。</p>
            )}
            <div className="mt-6 flex justify-end">
              <Button variant="primary" onClick={() => setResults(null)}>
                好
              </Button>
            </div>
          </>
        )}
      </Modal>
    </>
  );
}

function ClientCard({
  view,
  busy,
  onAttach,
  onRestore,
}: {
  view: ClientView;
  busy: boolean;
  onAttach: (entries: ClientEntry[], force: boolean) => void;
  onRestore: (names: string[], forget?: boolean) => void;
}) {
  const taken = view.entries.filter((e) => e.takenOver);
  const shareable = view.entries.filter((e) => e.class === 'shared' && !e.takenOver);
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-4 border-b border-zinc-100 px-5 py-4 dark:border-zinc-800">
        <Tile>
          <BrandMark brand={brandOf[view.id] ?? ''} className="size-6" />
        </Tile>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-base font-semibold">{view.name}</span>
            <Badge>{view.entries.length} 个 MCP</Badge>
            {taken.length > 0 && <Badge tone="blue">已接管 {taken.length}</Badge>}
          </div>
          <div className="mt-1 truncate font-mono text-[13px] text-zinc-500">{view.path}</div>
        </div>
        {taken.length > 0 && (
          <Button className="h-9" busy={busy} icon={<Undo2 className="size-4" />} onClick={() => onRestore(taken.map((e) => e.name))}>
            全部还原
          </Button>
        )}
        <Button className="h-9" variant="brand" disabled={shareable.length === 0 || busy} onClick={() => onAttach(shareable, false)}>
          接管可共享的 {shareable.length} 个
        </Button>
      </div>
      {view.error ? (
        <p className="px-5 py-4 text-sm text-red-600">{view.error}</p>
      ) : (
        <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
          {view.entries.map((e) => (
            <EntryRow key={e.name} e={e} busy={busy} onAttach={(force) => onAttach([e], force)} onRestore={(forget) => onRestore([e.name], forget)} />
          ))}
        </div>
      )}
    </Card>
  );
}

const classBadge: Record<string, { tone: 'gray' | 'blue' | 'green' | 'amber' | 'red'; text: string }> = {
  shared: { tone: 'green', text: '可共享' },
  stateful: { tone: 'amber', text: '需要会话隔离' },
  unknown: { tone: 'gray', text: '未知' },
  remote: { tone: 'gray', text: '远程 HTTP' },
  internal: { tone: 'gray', text: '客户端内置' },
  managed: { tone: 'blue', text: '已连接 MCPcell' },
};

function EntryRow({ e, busy, onAttach, onRestore }: { e: ClientEntry; busy: boolean; onAttach: (force: boolean) => void; onRestore: (forget: boolean) => void }) {
  const launch = e.kind === 'http' ? e.url : e.package || e.command;
  let badge = classBadge[e.class] ?? classBadge.unknown;
  if (e.takenOver) badge = { tone: 'blue', text: '已接管' };
  if (e.changed) badge = { tone: 'amber', text: '接管后被修改' };
  const canForce = e.class === 'unknown' && e.kind === 'stdio' && !e.envVars?.length;
  return (
    <div className={cx('flex items-center gap-4 px-5 py-3.5', (e.class === 'internal' || e.class === 'remote') && 'opacity-70')}>
      <ServiceAvatar name={e.name} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium">{e.name}</span>
          <Badge tone={badge.tone}>{badge.text}</Badge>
          {!e.enabled && <Badge>已停用</Badge>}
          {e.takenOver && e.service && (
            <a href={href('services', e.service)} className="inline-flex items-center gap-1 text-sm text-blue-600 hover:underline dark:text-blue-400">
              <ArrowRight className="size-3.5" />
              {e.service}
            </a>
          )}
        </div>
        <div className="mt-0.5 truncate text-[13px] text-zinc-500">
          <span className="font-mono">{launch}</span>
          <span className="text-zinc-400"> · {e.takenOver && e.takenAt ? `${since(e.takenAt)}前接管` : e.reason}</span>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {e.takenOver && (
          <Button className="h-8 px-3" busy={busy} onClick={() => onRestore(false)}>
            还原
          </Button>
        )}
        {e.changed && (
          <Button className="h-8 px-3" variant="ghost" busy={busy} onClick={() => onRestore(true)}>
            忘记记录
          </Button>
        )}
        {!e.takenOver && !e.changed && e.class === 'shared' && (
          <Button className="h-8 px-3" busy={busy} onClick={() => onAttach(false)}>
            接管
          </Button>
        )}
        {!e.takenOver && !e.changed && canForce && (
          <Button className="h-8 px-3" variant="ghost" busy={busy} onClick={() => onAttach(true)}>
            强制共享…
          </Button>
        )}
      </div>
    </div>
  );
}

function ManualClients({ skip }: { skip: Set<string> }) {
  const { status } = useStatus();
  const [open, setOpen] = useState<string | null>(null);
  const eps = status.services.map((s) => ({ name: s.name, url: `${status.baseUrl}/s/${s.name}` }));
  const list = manualClients.filter((c) => !skip.has(c.id));
  return (
    <div>
      <h2 className="mt-8 mb-3 flex items-center gap-2 text-[15px] font-semibold">
        其他客户端
        <span className="text-sm font-normal text-zinc-500">复制配置后手动替换同名条目</span>
      </h2>
      <div className="mb-3 flex gap-3 rounded-2xl border border-blue-200 bg-blue-50/60 px-5 py-3.5 text-sm leading-6 text-blue-900 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">
        <Info className="mt-1 size-4 shrink-0" />
        这些客户端还不能自动接管。下面的配置包含全部 {eps.length} 个服务，复制的内容带完整访问令牌。
      </div>
      <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
        {list.map((c) => {
          const expanded = open === c.id && c.http && eps.length > 0;
          return (
            <div key={c.id}>
              <button
                disabled={!c.http || eps.length === 0}
                onClick={() => setOpen(open === c.id ? null : c.id)}
                className="flex w-full items-center gap-4 px-5 py-3.5 text-left transition-colors enabled:hover:bg-zinc-50 disabled:cursor-default dark:enabled:hover:bg-zinc-800/50"
              >
                <Tile size="sm">
                  <BrandMark brand={c.brand} className="size-5" />
                </Tile>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{c.name}</span>
                    {c.http ? <Badge tone="green">HTTP</Badge> : <Badge tone="amber">仅 stdio</Badge>}
                  </div>
                  <div className="mt-0.5 truncate text-[13px] text-zinc-500">
                    {c.file}
                    {c.note && <span className="text-zinc-400"> · {c.note}</span>}
                  </div>
                </div>
                {c.http && eps.length > 0 && <ChevronRight className={cx('size-4 text-zinc-400 transition-transform', expanded && 'rotate-90')} />}
              </button>
              {expanded && (
                <div className="px-5 pb-5 pl-[76px]">
                  <CodeBlock shown={c.snippet!(eps, masked(status.token))} copy={c.snippet!(eps, status.token)} />
                </div>
              )}
            </div>
          );
        })}
      </Card>
    </div>
  );
}
