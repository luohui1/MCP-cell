import { useCallback, useEffect, useState } from 'react';
import { metricsApi, type MetricsRange } from './api';
import { MemoryChart, Swatch } from './chart';
import { usePolled } from './data';
import { Card, Segmented } from './ui';

// The 内存 page's chart: memory over the last hour or day, against every
// conversation running its own copy.

const rangeKey = 'mcpcell.chartRange';

function savedRange(): MetricsRange {
  try {
    return localStorage.getItem(rangeKey) === '1h' ? '1h' : '24h';
  } catch {
    return '24h';
  }
}

export function MemoryPanel() {
  const [range, setRange] = useState<MetricsRange>(savedRange);
  const metrics = usePolled(useCallback(() => metricsApi.load(range), [range]), 10_000);
  const span = range === '1h' ? 3600 : 86400;
  const [nowT, setNowT] = useState(() => Date.now() / 1000);
  useEffect(() => setNowT(Date.now() / 1000), [metrics]);
  const shown = metrics?.range === range ? metrics : null;
  const pick = (r: MetricsRange) => {
    setRange(r);
    try {
      localStorage.setItem(rangeKey, r);
    } catch {
      // the choice just is not remembered
    }
  };
  return (
    <Card className="px-4 pt-3 pb-1.5">
      <div className="mb-1 flex items-center gap-4 text-xs text-zinc-500">
        <span className="flex items-center gap-1.5">
          <Swatch kind="total" />
          实际
        </span>
        <span className="flex items-center gap-1.5" title="每个对话各开一份时的估计">
          <Swatch kind="estimate" />
          各开各的
        </span>
        <div className="ml-auto">
          <Segmented
            value={range}
            onChange={pick}
            options={[
              { value: '1h', label: '1 小时' },
              { value: '24h', label: '24 小时' },
            ]}
          />
        </div>
      </div>
      <MemoryChart
        points={shown?.points ?? []}
        step={shown?.step ?? 10}
        from={nowT - span}
        to={nowT}
        height={170}
        series={[
          { key: 'total', label: '实际' },
          { key: 'estimate', label: '各开各的' },
        ]}
        empty={shown ? '还没有记录' : '正在读取…'}
        extra={(p) => [p.conversations ? `${p.conversations} 个对话` : '', p.running ? `${p.running} 个 MCP 进程` : ''].filter(Boolean).join(' · ')}
      />
    </Card>
  );
}
