import { Activity } from 'lucide-react';
import { useStatus } from '../App';
import { clock, since } from '../format';
import { BrandMark, brandOf, ServiceAvatar } from '../icons';
import { go } from '../router';
import { cx, Dot, EmptyState, GroupHeader, PageHeader, TableCard } from '../ui';

const grid = 'grid grid-cols-[minmax(0,1fr)_200px_120px_150px_150px] items-center gap-4';

export function SessionsPage() {
  const { status } = useStatus();
  const sessions = [...status.sessions].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return (
    <>
      <PageHeader icon={<Activity className="size-5 text-zinc-500" />} title="会话" subtitle={`${sessions.length} 个连接中`} />
      <div className="mx-auto w-full max-w-[1240px] px-8 pt-6 pb-12">
        {sessions.length === 0 ? (
          <EmptyState
            icon={<Activity className="size-6" />}
            title="还没有会话"
            text="Agent 连接托管的 MCP 时会出现在这里。同一个 MCP 的所有会话共用一个进程，会话本身不占进程。"
          />
        ) : (
          <section>
            <div className={cx(grid, 'px-5')}>
              <div className="-ml-5 min-w-0">
                <GroupHeader title="连接中" count={sessions.length} hint="同一个 MCP 的会话共用一个进程" />
              </div>
              {['MCP', '协议', '连接于', '活动'].map((l) => (
                <span key={l} className="mb-2.5 text-xs text-zinc-400">
                  {l}
                </span>
              ))}
            </div>
            <TableCard>
              {sessions.map((x) => (
                <div
                  key={x.id}
                  onClick={() => go('services', x.service)}
                  className={cx(grid, 'cursor-pointer px-5 py-3 transition-colors hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40')}
                >
                  <div className="flex min-w-0 items-center gap-3.5">
                    <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-900">
                      <BrandMark brand={brandOf(x.client)} className="size-4" />
                    </span>
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{x.client || '未知客户端'}</div>
                      <div className="truncate font-mono text-xs text-zinc-400">{x.id}</div>
                    </div>
                  </div>
                  <div className="flex min-w-0 items-center gap-2.5">
                    <ServiceAvatar name={x.service} size="xs" />
                    <span className="truncate text-sm">{x.service}</span>
                  </div>
                  <span className="font-mono text-xs text-zinc-500">{x.protocolVersion}</span>
                  <span className="text-[13px] text-zinc-500 tabular-nums">{clock(x.createdAt)}</span>
                  <div className="flex items-center gap-2 text-[13px] text-zinc-500">
                    {x.inflight ? (
                      <>
                        <Dot tone="amber" pulse />
                        处理中 {x.inflight}
                      </>
                    ) : (
                      <>
                        <Dot tone={x.streaming ? 'green' : 'gray'} />
                        {since(x.lastSeenAt)}前活动
                      </>
                    )}
                  </div>
                </div>
              ))}
            </TableCard>
          </section>
        )}
      </div>
    </>
  );
}
