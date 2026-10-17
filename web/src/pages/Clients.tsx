import { useState } from 'react';
import { ChevronRight, Info, Plug } from 'lucide-react';
import { useStatus } from '../App';
import { clients, masked } from '../clients';
import { BrandMark, Tile } from '../icons';
import { Badge, Card, CodeBlock, cx, PageHeader } from '../ui';

export function ClientsPage() {
  const { status } = useStatus();
  const [open, setOpen] = useState<string | null>('claude-code');
  const eps = status.services.map((s) => ({ name: s.name, url: `${status.baseUrl}/s/${s.name}` }));

  return (
    <>
      <PageHeader icon={<Plug className="size-6 text-zinc-700 dark:text-zinc-300" />} title="客户端接入" subtitle={`${clients.length} 个客户端`} />
      <div className="mx-auto w-full max-w-[1200px] px-8 py-6">
        <div className="mb-5 flex gap-3 rounded-2xl border border-blue-200 bg-blue-50/60 px-5 py-4 text-sm leading-6 text-blue-900 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">
          <Info className="mt-1 size-4 shrink-0" />
          <div>
            把下面的配置替换到客户端里<b className="font-medium">同名</b>的 MCP 条目，然后重启客户端。工具全名不变，原有的允许规则继续有效。
            自动接管与一键还原在 M3 实现。
          </div>
        </div>
        <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
          {clients.map((c) => {
            const expanded = open === c.id && c.http && eps.length > 0;
            return (
              <div key={c.id}>
                <button
                  disabled={!c.http || eps.length === 0}
                  onClick={() => setOpen(open === c.id ? null : c.id)}
                  className="flex w-full items-center gap-4 px-5 py-4 text-left transition-colors enabled:hover:bg-zinc-50 disabled:cursor-default dark:enabled:hover:bg-zinc-800/50"
                >
                  <Tile>
                    <BrandMark brand={c.brand} className="size-6" />
                  </Tile>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-base font-semibold">{c.name}</span>
                      {c.http ? <Badge tone="green">HTTP</Badge> : <Badge tone="amber">仅 stdio</Badge>}
                    </div>
                    <div className="mt-1 truncate text-[13px] text-zinc-500">{c.file}</div>
                    {c.note && <div className="mt-0.5 text-xs text-zinc-400">{c.note}</div>}
                  </div>
                  {c.http && eps.length > 0 && (
                    <span className="flex items-center gap-1 text-sm text-zinc-500">
                      {expanded ? '收起' : '查看配置'}
                      <ChevronRight className={cx('size-4 transition-transform', expanded && 'rotate-90')} />
                    </span>
                  )}
                </button>
                {expanded && (
                  <div className="px-5 pb-5 pl-[84px]">
                    <CodeBlock shown={c.snippet!(eps, masked(status.token))} copy={c.snippet!(eps, status.token)} />
                    <p className="mt-2 text-xs text-zinc-400">包含全部 {eps.length} 个服务；复制的内容带完整访问令牌。</p>
                  </div>
                )}
              </div>
            );
          })}
        </Card>
      </div>
    </>
  );
}
