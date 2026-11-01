import type { ReactNode } from 'react';
import { metricsApi, type MetricsNow } from './api';
import { readBytes } from './chart';
import { usePolled, useStatus } from './data';
import { bytes } from './format';
import { agentName } from './timeline';
import { Card, cx } from './ui';

// Big numbers in one strip: the four at the top of the MCP and 内存 pages
// (memory now, what sharing saves, what runs, who uses it), and StatGrid
// for any other set, such as one MCP's on its page.

/** The latest sample, every 10 seconds while the page is visible. */
export const useNow = () => usePolled(metricsApi.now, 10_000);

export interface StatCell {
  label: string;
  value: ReactNode;
  sub: ReactNode;
  title?: string;
  accent?: boolean;
}

/** A big number and its label; the details are in the tooltip. */
function Cell({ label, value, sub, title, accent }: StatCell) {
  return (
    <div className="min-w-0 px-4 py-2.5" title={[title, typeof sub === 'string' ? sub : ''].filter(Boolean).join('\n')}>
      <div className={cx('text-xl leading-7 font-semibold tracking-tight tabular-nums', accent && 'text-emerald-600 dark:text-emerald-400')}>{value}</div>
      <div className="truncate text-xs text-zinc-500">{label}</div>
    </div>
  );
}

const columns: Record<number, string> = { 3: 'grid-cols-3', 4: 'grid-cols-4', 5: 'grid-cols-5' };

/** Big numbers side by side in one card, divided by hairlines. */
export function StatGrid({ cells }: { cells: StatCell[] }) {
  return (
    <Card className={cx('grid divide-x divide-zinc-100 dark:divide-zinc-800', columns[cells.length] ?? 'grid-cols-4')}>
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
  const saved = now.estimateBytes > now.totalBytes ? now.estimateBytes - now.totalBytes : 0;
  const total = now.conversations.reduce((n, c) => n + c.conversations, 0);
  return (
    <StatGrid
      cells={[
        {
          label: '现在占用',
          value: readBytes(now.totalBytes),
          sub: parts.join(' · '),
          title: `和 MCP 有关的全部内存：正在运行的 MCP、MCPcell 自己${now.wrappers ? `、${now.wrappers} 个替对话转发的小程序` : ''}`,
        },
        {
          label: '比各开各的省',
          accent: saved > 0,
          value: saved > 0 ? readBytes(saved) : '—',
          sub: saved > 0 ? `各开各的约 ${readBytes(now.estimateBytes)}，少用 ${Math.round((saved / now.estimateBytes) * 100)}%` : now.estimateBytes ? '对话不多，还没省下' : '现在没有对话在用 MCP',
          title: `如果每个对话各开一份：每个 MCP 在用的对话数 × 它实测的单份内存。${now.unmeasured.length ? `还没测过的 ${now.unmeasured.join('、')} 不算。` : ''}`,
        },
        {
          label: '运行中的 MCP',
          value: (
            <>
              {now.runningServices}
              <span className="text-sm font-normal text-zinc-400"> / {now.services}</span>
            </>
          ),
          sub: now.runningServices >= now.services && now.services > 0 ? '都在运行' : '其余待命，不占内存',
          title: `正在运行 ${now.instances} 个 MCP 进程（每个对话单独一份的 MCP，每个对话一个）`,
        },
        { label: '在用的对话', value: total, sub: total ? now.conversations.map((c) => `${agentName(c.agent)} ${c.conversations}`).join(' · ') : '现在没有对话连着' },
      ]}
    />
  );
}
