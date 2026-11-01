import type { ReactNode } from 'react';
import { metricsApi, type MetricsNow } from './api';
import { readBytes } from './chart';
import { usePolled, useStatus } from './data';
import { bytes } from './format';
import { agentName } from './timeline';
import { Card, cx } from './ui';

// Big numbers in one strip: the three at the top of the MCP and 内存 pages
// (memory now, what runs, the conversations), and StatGrid for any other
// set, such as one MCP's on its page.

/** The latest sample, every 10 seconds while the page is visible. */
export const useNow = () => usePolled(metricsApi.now, 10_000);

export interface StatCell {
  label: string;
  value: ReactNode;
  tip?: string;
}

function Cell({ label, value, tip }: StatCell) {
  return (
    <div className="min-w-0 px-4 py-2.5" title={tip}>
      <div className="text-xl leading-7 font-semibold tracking-tight tabular-nums">{value}</div>
      <div className="truncate text-xs text-zinc-500">{label}</div>
    </div>
  );
}

const columns: Record<number, string> = { 2: 'grid-cols-2', 3: 'grid-cols-3', 4: 'grid-cols-4' };

/** Big numbers side by side in one card, divided by hairlines. */
export function StatGrid({ cells }: { cells: StatCell[] }) {
  return (
    <Card className={cx('grid divide-x divide-zinc-100 dark:divide-zinc-800', columns[cells.length] ?? 'grid-cols-3')}>
      {cells.map((c) => (
        <Cell key={c.label} {...c} />
      ))}
    </Card>
  );
}

export function StatStrip({ now }: { now: MetricsNow | null }) {
  const { offline } = useStatus();
  if (offline || !now) return <Card className="h-[62px] animate-pulse" children={null} />;
  const parts = [`MCP ${now.instanceBytes ? bytes(now.instanceBytes) : '0 MB'}`, `MCPcell ${bytes(now.daemonBytes)}`];
  if (now.wrappers) parts.push(`转发 ${bytes(now.wrapperBytes)}`);
  const total = now.conversations.reduce((n, c) => n + c.conversations, 0);
  return (
    <StatGrid
      cells={[
        { label: '内存', value: readBytes(now.totalBytes), tip: parts.join(' · ') },
        {
          label: '运行中',
          value: (
            <>
              {now.runningServices}
              <span className="text-sm font-normal text-zinc-400"> / {now.services}</span>
            </>
          ),
        },
        { label: '对话', value: total, tip: now.conversations.map((c) => `${agentName(c.agent)} ${c.conversations}`).join(' · ') || undefined },
      ]}
    />
  );
}
