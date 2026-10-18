import { useState } from 'react';
import { Bot, ChevronRight, Layers, Power, Undo2 } from 'lucide-react';
import { takeover } from '../api';
import { useStatus } from '../App';
import { agentBrand, agentOrder, restoreSteps, useAgentActions, useUnified } from '../agents';
import { clients as manualClients, masked } from '../clients';
import { BrandMark } from '../icons';
import { candidates, OneClickDialog } from '../one-click';
import { Badge, Button, CodeBlock, cx, GroupHeader, PageHeader, TableCard, useToast } from '../ui';

const grid = 'grid grid-cols-[minmax(0,1fr)_88px_180px_240px] items-center gap-4';

export function AgentsPage() {
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const toast = useToast();
  const [only, setOnly] = useState<string | null>(null);

  const agents = data ? agentOrder.map((id) => data.agents.find((a) => a.id === id)).filter((a) => !!a) : [];

  return (
    <>
      <PageHeader icon={<Bot className="size-5 text-zinc-500" />} title="Agents" subtitle="托管后，Agent 里的 MCP 由 MCPcell 统一运行" />
      <div className="mx-auto w-full max-w-[1240px] space-y-7 px-8 pt-6 pb-12">
        {error && <p className="text-sm text-red-600">{error}</p>}
        {!data && !error && <p className="py-20 text-center text-sm text-zinc-400">正在读取 Agent 配置…</p>}

        {data && (
          <section>
            {data.autostart.supported && !data.autostart.enabled && agents.some((a) => a.managed > 0) && (
              <div className="mb-6 flex items-center gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
                <Power className="size-4 shrink-0" />
                <span className="flex-1">托管后 Agent 依赖 MCPcell 在后台运行，建议开启开机自启。</span>
                <Button size="sm" variant="primary" onClick={async () => (await takeover.autostart(true), toast('已开启开机自启'), reload())}>
                  开启自启
                </Button>
              </div>
            )}
            <div className={cx(grid, 'px-5')}>
              <div className="-ml-5 min-w-0">
                <GroupHeader title="可以自动托管" count={agents.length} hint="直接改写这些 Agent 的配置，随时可以还原" />
              </div>
              <span className="mb-2.5 text-xs text-zinc-400">MCP</span>
              <span className="mb-2.5 text-xs text-zinc-400">已托管</span>
              <span />
            </div>
            <TableCard>
              {agents.map((a) => {
                const back = data.rows.flatMap((r) => restoreSteps(r, [a!]));
                const offers = candidates(data, a!.id);
                const offer = offers.length;
                const recommended = offers.filter((c) => c.steps.every((s) => !s.force)).length;
                const pct = a!.total ? Math.round((a!.managed / a!.total) * 100) : 0;
                return (
                  <div key={a!.id} className={cx(grid, 'px-5 py-4', !a!.found && 'opacity-60')}>
                    <div className="flex min-w-0 items-center gap-3.5">
                      <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-900">
                        <BrandMark brand={agentBrand[a!.id]} className="size-5" />
                      </span>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-[15px] font-medium">{a!.name}</span>
                          {!a!.found && <Badge>未找到配置</Badge>}
                        </div>
                        <div className="mt-0.5 truncate font-mono text-xs text-zinc-400" title={a!.path}>
                          {a!.path}
                        </div>
                        {a!.error && <div className="mt-0.5 truncate text-xs text-red-600">{a!.error}</div>}
                      </div>
                    </div>
                    <div className="text-sm tabular-nums">{a!.found ? `${a!.total} 个` : '—'}</div>
                    <div className="flex items-center gap-3">
                      <div className="h-1.5 w-20 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
                        <div className="h-full rounded-full bg-blue-500" style={{ width: `${pct}%` }} />
                      </div>
                      <span className="text-sm text-zinc-600 tabular-nums dark:text-zinc-300">{a!.managed} 个</span>
                    </div>
                    <div className="flex justify-end gap-2">
                      {back.length > 0 && (
                        <Button size="sm" variant="ghost" icon={<Undo2 className="size-3.5" />} onClick={() => acts.restore(back, `还原 ${a!.name} 的 ${back.length} 个托管`)}>
                          全部还原
                        </Button>
                      )}
                      {offer > 0 && (
                        <Button size="sm" icon={<Layers className="size-3.5" />} onClick={() => setOnly(a!.id)}>
                          {recommended ? `托管 ${recommended} 个` : `可选托管 ${offer} 个`}
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}
            </TableCard>
          </section>
        )}
        <ManualAgents skip={new Set(agentOrder)} />
      </div>
      {data && <OneClickDialog open={only !== null} only={only ?? undefined} onClose={() => setOnly(null)} data={data} reload={reload} />}
      {acts.dialog}
    </>
  );
}

function ManualAgents({ skip }: { skip: Set<string> }) {
  const { status } = useStatus();
  const [shown, setShown] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const eps = status.services.map((s) => ({ name: s.name, url: `${status.baseUrl}/s/${s.name}` }));
  const list = manualClients.filter((c) => !skip.has(c.id));
  return (
    <section>
      <GroupHeader title="其他 Agent" count={list.length} hint="还不能自动托管：复制配置，替换原来的同名条目后重启" open={shown} onToggle={() => setShown(!shown)} />
      {shown && (
        <TableCard>
          {list.map((c) => {
            const usable = c.http && eps.length > 0;
            const expanded = open === c.id && usable;
            return (
              <div key={c.id}>
                <button
                  disabled={!usable}
                  onClick={() => setOpen(open === c.id ? null : c.id)}
                  className="flex w-full items-center gap-3.5 px-5 py-3 text-left transition-colors enabled:hover:bg-zinc-50 disabled:cursor-default dark:enabled:hover:bg-zinc-800/40"
                >
                  <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-900">
                    <BrandMark brand={c.brand} className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{c.name}</span>
                      {!c.http && <Badge tone="amber">仅 stdio</Badge>}
                    </div>
                    <div className="mt-0.5 truncate text-xs text-zinc-400">
                      {c.file}
                      {c.note && ` · ${c.note}`}
                    </div>
                  </div>
                  {usable && <ChevronRight className={cx('size-4 text-zinc-400 transition-transform', expanded && 'rotate-90')} />}
                </button>
                {expanded && (
                  <div className="px-5 pb-4 pl-[66px]">
                    <CodeBlock shown={c.snippet!(eps, masked(status.token))} copy={c.snippet!(eps, status.token)} />
                    <p className="mt-2 text-xs text-zinc-400">包含 MCPcell 里全部 {eps.length} 个服务；复制的内容带完整访问令牌。</p>
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
