import { Activity } from 'lucide-react';
import { useStatus } from '../App';
import { clock, since } from '../format';
import { ClientAvatar } from '../icons';
import { go } from '../router';
import { Badge, Card, Dot, PageHeader } from '../ui';

export function SessionsPage() {
  const { status } = useStatus();
  const sessions = [...status.sessions].sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  return (
    <>
      <PageHeader icon={<Activity className="size-6 text-zinc-700 dark:text-zinc-300" />} title="会话" subtitle={`${sessions.length} 个连接中`} />
      <div className="mx-auto w-full max-w-[1200px] px-8 py-6">
        <p className="mb-5 text-sm text-zinc-500">每个客户端会话连到 MCPcell 的一个端点。同一服务的所有会话共用一个进程；会话本身不占进程。</p>
        {sessions.length === 0 ? (
          <Card className="px-8 py-16 text-center text-sm text-zinc-500">还没有客户端连进来。到「客户端接入」复制配置后，重启客户端即可。</Card>
        ) : (
          <div className="space-y-3">
            {sessions.map((x) => (
              <Card key={x.id} onClick={() => go('services', x.service)} className="flex cursor-pointer items-center gap-4 px-5 py-4 transition-colors hover:border-zinc-300 dark:hover:border-zinc-700">
                <ClientAvatar client={x.client} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-base font-semibold">{x.client || '未知客户端'}</span>
                    <Badge tone="blue">{x.service}</Badge>
                    <Badge>
                      <span className="font-mono">{x.protocolVersion}</span>
                    </Badge>
                  </div>
                  <div className="mt-1 text-sm text-zinc-500">
                    连接于 {clock(x.createdAt)} · 会话 <span className="font-mono">{x.id}</span>
                  </div>
                </div>
                <div className="text-right text-sm leading-6 text-zinc-500">
                  <div>最近活动 {since(x.lastSeenAt)}前</div>
                  <div className="inline-flex items-center gap-1.5">
                    {x.inflight ? (
                      <>
                        <Dot tone="amber" />
                        处理中 {x.inflight}
                      </>
                    ) : x.streaming ? (
                      <>
                        <Dot tone="green" />
                        已建立通知流
                      </>
                    ) : (
                      '空闲'
                    )}
                  </div>
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
