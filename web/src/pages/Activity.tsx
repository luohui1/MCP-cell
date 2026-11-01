import { useCallback, useState } from 'react';
import { metricsApi } from '../api';
import { usePolled, useStatus } from '../data';
import { Body, Toolbar } from '../layout';
import { href } from '../router';
import { timeline, when, type TimelineItem } from '../timeline';
import { Card, cx, Dot, Segmented } from '../ui';

// 动态: what MCPcell did lately, newest first, by day; the filter narrows it
// to problems, MCPs opening and closing, or apps being changed.

type Filter = 'all' | 'problems' | 'runs' | 'apps';

const groups: Record<Exclude<Filter, 'all'>, (it: TimelineItem) => boolean> = {
  problems: (it) => it.tone === 'red' || it.tone === 'amber',
  runs: (it) => it.kind === 'started' || it.kind.startsWith('stopped_') || it.kind === 'crashed',
  apps: (it) => ['taken_over', 'restored', 'added', 'auto_taken', 'auto_failed', 'auto_stopped'].includes(it.kind),
};

function day(at: string): string {
  const d = new Date(at);
  const today = new Date();
  const diff = Math.round((new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime() - new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()) / 86_400_000);
  if (diff === 0) return '今天';
  if (diff === 1) return '昨天';
  return `${d.getMonth() + 1}月${d.getDate()}日`;
}

const toneText: Record<string, string> = { red: 'text-red-600 dark:text-red-400', amber: 'text-amber-700 dark:text-amber-400' };

export function ActivityPage() {
  const { status } = useStatus();
  const events = usePolled(useCallback(() => metricsApi.events().then((r) => r.events), []), 10_000);
  const [filter, setFilter] = useState<Filter>('all');
  const all = events ? timeline(events, status.services) : [];
  const items = filter === 'all' ? all : all.filter(groups[filter]);
  const problems = all.filter(groups.problems).length;
  const known = (name?: string) => !!name && status.services.some((s) => s.name === name);
  const days: { day: string; items: TimelineItem[] }[] = [];
  for (const it of items) {
    const d = day(it.at);
    if (days.at(-1)?.day === d) days.at(-1)!.items.push(it);
    else days.push({ day: d, items: [it] });
  }
  return (
    <Body>
      <Toolbar>
        <Segmented<Filter>
          value={filter}
          onChange={setFilter}
          options={[
            { value: 'all', label: '全部' },
            { value: 'problems', label: problems ? `问题 ${problems}` : '问题' },
            { value: 'runs', label: '打开和关掉' },
            { value: 'apps', label: '应用设置' },
          ]}
        />
      </Toolbar>
      {!events && <p className="py-12 text-center text-[13px] text-zinc-400">正在读取…</p>}
      {events && !items.length && <p className="py-12 text-center text-[13px] text-zinc-400">没有动态</p>}
      {days.map((g) => (
        <section key={g.day}>
          <h2 className="mb-1.5 px-1 text-xs text-zinc-400">{g.day}</h2>
          <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
            {g.items.map((it) => (
              <div key={it.key} className="flex items-start gap-2.5 px-4 py-2">
                <span className="flex h-5 items-center">
                  <Dot tone={it.tone} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className={cx('truncate text-[13px] leading-5', toneText[it.tone] ?? 'text-zinc-800 dark:text-zinc-100')} title={it.detail}>
                    {known(it.service) ? (
                      <a href={href(['mcp', it.service!])} className="hover:underline">
                        {it.text}
                      </a>
                    ) : (
                      it.text
                    )}
                  </div>
                  {it.detail && it.tone !== 'gray' && <div className="truncate text-xs leading-5 text-zinc-500">{it.detail}</div>}
                </div>
                <span className="shrink-0 text-xs leading-5 text-zinc-400 tabular-nums">{when(it.at).replace(/^(昨天|\d+月\d+日) /, '')}</span>
              </div>
            ))}
          </Card>
        </section>
      ))}
    </Body>
  );
}
