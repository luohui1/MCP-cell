import { useState } from 'react';
import { Bot, ChevronRight, Info, Layers, Power, Undo2 } from 'lucide-react';
import { takeover, type AgentCell } from '../api';
import { useStatus } from '../App';
import { agentBrand, agentOrder, restartHint, useAgentActions } from '../agent-actions';
import { clients as manualClients, masked } from '../clients';
import { BrandMark, Tile } from '../icons';
import { Badge, Button, Card, CodeBlock, cx, Dot, PageHeader, useToast } from '../ui';
import { useUnified } from './Mcp';

export function AgentsPage() {
  const { data, error, reload } = useUnified();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const toast = useToast();

  const agents = data ? agentOrder.map((id) => data.agents.find((a) => a.id === id)).filter((a) => !!a) : [];

  return (
    <>
      <PageHeader icon={<Bot className="size-6 text-zinc-700 dark:text-zinc-300" />} title="Agents" subtitle="托管后，Agent 里的 MCP 由 MCPcell 统一运行" />
      <div className="mx-auto w-full max-w-[1200px] space-y-5 px-8 py-6">
        {data?.autostart.supported && !data.autostart.enabled && (
          <div className="flex items-center gap-4 rounded-2xl border border-amber-200 bg-amber-50 px-5 py-3.5 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200">
            <Power className="size-5 shrink-0" />
            <div className="flex-1">托管后 Agent 依赖 MCPcell 守护进程，建议开启开机自启（无窗口运行）。</div>
            <Button className="h-9" variant="brand" onClick={async () => (await takeover.autostart(true), toast('已开启开机自启'), reload())}>
              开启自启
            </Button>
          </div>
        )}
        {error && <Card className="p-5 text-sm text-red-600">{error}</Card>}
        {!data && !error && <p className="py-16 text-center text-sm text-zinc-400">正在读取 Agent 配置…</p>}

        {data && (
          <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
            {agents.map((a) => {
              const managed = data.rows.flatMap((r) => {
                const c = r.agents[a!.id];
                return c?.state === 'managed' ? [c.entry] : [];
              });
              const shareable = data.rows.flatMap((r) => {
                const c = r.agents[a!.id];
                return c?.state === 'native' && c.class === 'shared' ? [{ entry: c.entry, service: r.service ?? r.proposed, isNew: !r.service }] : [];
              });
              return (
                <div key={a!.id} className="flex items-center gap-4 px-5 py-4">
                  <Tile>
                    <BrandMark brand={agentBrand[a!.id]} className="size-6" />
                  </Tile>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-base font-semibold">{a!.name}</span>
                      {a!.found ? <Badge>{a!.total} 个 MCP</Badge> : <Badge>未找到配置</Badge>}
                      {a!.managed > 0 && <Badge tone="blue">托管 {a!.managed}</Badge>}
                      {a!.shareable > 0 && <Badge tone="green">可托管 {a!.shareable}</Badge>}
                    </div>
                    <div className="mt-1 truncate font-mono text-[13px] text-zinc-500">{a!.path}</div>
                    {a!.found && <div className="mt-0.5 text-xs text-zinc-400">{restartHint[a!.id]}</div>}
                    {a!.error && <div className="mt-0.5 text-xs text-red-600">{a!.error}</div>}
                    {a!.found && (
                      <div className="mt-2.5 flex flex-wrap gap-1.5">
                        {data.rows.flatMap((r) => {
                          const c = r.agents[a!.id];
                          if (!c) return [];
                          const [tone, what] = entryTone(c);
                          return [
                            <span
                              key={r.key}
                              title={`${what}${c.enabled ? '' : '（已停用）'}`}
                              className={cx(
                                'inline-flex h-6 items-center gap-1.5 rounded-md border border-zinc-200 px-2 text-xs text-zinc-600 dark:border-zinc-700 dark:text-zinc-300',
                                !c.enabled && 'opacity-50',
                              )}
                            >
                              <Dot tone={tone} />
                              {c.entry}
                            </span>,
                          ];
                        })}
                      </div>
                    )}
                  </div>
                  {managed.length > 0 && (
                    <Button className="h-9" icon={<Undo2 className="size-4" />} onClick={() => acts.restoreAll(a!, managed)}>
                      全部还原
                    </Button>
                  )}
                  {shareable.length > 0 && (
                    <Button className="h-9" variant="brand" icon={<Layers className="size-4" />} onClick={() => acts.takeoverEntries(a!, shareable)}>
                      托管可共享的 {shareable.length} 个
                    </Button>
                  )}
                </div>
              );
            })}
          </Card>
        )}
        <ManualAgents skip={new Set(agentOrder)} />
      </div>
      {acts.dialog}
    </>
  );
}

function entryTone(c: AgentCell): ['blue' | 'green' | 'amber' | 'gray', string] {
  if (c.state === 'managed') return ['blue', '由 MCPcell 托管'];
  if (c.state === 'changed') return ['amber', '托管后被改动过'];
  if (c.state === 'native' && c.class === 'shared') return ['green', '原生，可以托管'];
  if (c.state === 'native' && c.class === 'stateful') return ['amber', c.reason ?? '需要会话隔离'];
  if (c.state === 'remote') return ['gray', '远程 HTTP'];
  if (c.state === 'internal') return ['gray', c.reason ?? 'Agent 内置'];
  return ['gray', c.reason ?? '原生'];
}

function ManualAgents({ skip }: { skip: Set<string> }) {
  const { status } = useStatus();
  const [open, setOpen] = useState<string | null>(null);
  const eps = status.services.map((s) => ({ name: s.name, url: `${status.baseUrl}/s/${s.name}` }));
  const list = manualClients.filter((c) => !skip.has(c.id));
  return (
    <div>
      <h2 className="mt-8 mb-3 flex items-center gap-2 text-[15px] font-semibold">
        其他 Agent
        <span className="text-sm font-normal text-zinc-500">还不能自动托管，复制配置后手动替换同名条目</span>
      </h2>
      <div className="mb-3 flex gap-3 rounded-2xl border border-blue-200 bg-blue-50/60 px-5 py-3.5 text-sm leading-6 text-blue-900 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">
        <Info className="mt-1 size-4 shrink-0" />
        下面的配置包含 MCPcell 里全部 {eps.length} 个服务；复制的内容带完整访问令牌。
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
