import { instancesOf, type Service } from '../api';
import { readBytes } from '../chart';
import { useStatus } from '../data';
import { since } from '../format';
import { MemoryPanel } from '../history';
import { ServiceAvatar } from '../icons';
import { Body } from '../layout';
import { failing } from '../model';
import { href } from '../router';
import { StatStrip, useNow } from '../stats';
import { Card, cx, Dot } from '../ui';

// 内存: the three numbers, memory over the last hour or day, and each MCP's
// share.

export function MemoryPage() {
  const now = useNow();
  return (
    <Body>
      <StatStrip now={now} />
      <MemoryPanel />
      <PerMcp />
    </Body>
  );
}

interface Line {
  name: string;
  now: number;
  copies: number;
  sessions: number;
  calls: number;
  last?: string;
  broken: boolean;
}

/** One line per configured MCP; run- variants count toward theirs. */
function lines(services: Service[], sessions: { service: string }[]): Line[] {
  const byName = new Map<string, Line>();
  for (const s of services) {
    const name = s.of ?? s.name;
    const l = byName.get(name) ?? { name, now: 0, copies: 0, sessions: 0, calls: 0, broken: false };
    const runs = instancesOf(s);
    l.now += runs.reduce((n, i) => n + i.usage.privateBytes, 0);
    l.copies += runs.length;
    l.sessions += sessions.filter((x) => x.service === s.name).length;
    l.calls += s.calls ?? 0;
    if (s.lastUsedAt && (!l.last || s.lastUsedAt > l.last)) l.last = s.lastUsedAt;
    l.broken ||= failing(s);
    byName.set(name, l);
  }
  return [...byName.values()].sort((a, b) => b.now - a.now || a.name.localeCompare(b.name));
}

const grid = 'grid grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_72px_40px_40px_64px] items-center gap-3';

function PerMcp() {
  const { status } = useStatus();
  const list = lines(status.services, status.sessions);
  const max = Math.max(1, ...list.map((l) => l.now));
  return (
    <Card className="overflow-hidden">
      <div className={cx(grid, 'border-b border-zinc-100 px-4 py-2 text-xs text-zinc-400 dark:border-zinc-800')}>
        <span>MCP</span>
        <span />
        <span className="text-right">内存</span>
        <span className="text-right">对话</span>
        <span className="text-right">调用</span>
        <span className="text-right">上次使用</span>
      </div>
      {list.length === 0 && <p className="px-4 py-6 text-center text-[13px] text-zinc-400">暂无 MCP</p>}
      <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
        {list.map((l) => (
          <a key={l.name} href={href(['mcp', l.name])} className={cx(grid, 'px-4 py-2 text-[13px] transition-colors hover:bg-zinc-50 dark:hover:bg-zinc-800/40')}>
            <span className="flex min-w-0 items-center gap-2">
              <ServiceAvatar name={l.name} size="xs" />
              <span className="truncate font-medium">{l.name}</span>
              {l.broken && <Dot tone="red" />}
            </span>
            <span className="relative h-1.5 rounded-full bg-zinc-100 dark:bg-zinc-800">
              {l.now > 0 && <span className="absolute inset-y-0 left-0 rounded-full bg-blue-600 dark:bg-blue-400" style={{ width: `${(l.now / max) * 100}%` }} />}
            </span>
            <span className={cx('text-right tabular-nums', l.now ? 'font-medium' : 'text-zinc-400')}>
              {l.now ? readBytes(l.now) : '待命'}
              {l.copies > 1 && <span className="font-normal text-zinc-400"> ×{l.copies}</span>}
            </span>
            <span className="text-right text-zinc-500 tabular-nums">{l.sessions || '—'}</span>
            <span className="text-right text-zinc-500 tabular-nums">{l.calls || '—'}</span>
            <span className="text-right text-xs text-zinc-500">{l.last ? since(l.last) : '—'}</span>
          </a>
        ))}
      </div>
    </Card>
  );
}
