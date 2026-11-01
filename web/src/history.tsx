import { useCallback, useEffect, useState } from 'react';
import { metricsApi, type MetricsRange } from './api';
import { MemoryChart } from './chart';
import { usePolled } from './data';
import { Card, Segmented } from './ui';

// The 内存 page's chart: memory over the last hour or day.

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
      <div className="mb-1 flex justify-end">
        <Segmented
          value={range}
          onChange={pick}
          options={[
            { value: '1h', label: '1 小时' },
            { value: '24h', label: '24 小时' },
          ]}
        />
      </div>
      <MemoryChart
        points={shown?.points ?? []}
        step={shown?.step ?? 10}
        from={nowT - span}
        to={nowT}
        height={170}
        label="内存"
        empty={shown ? '暂无数据' : '读取中…'}
        extra={(p) => [p.conversations ? `${p.conversations} 个对话` : '', p.running ? `${p.running} 个进程` : ''].filter(Boolean).join(' · ')}
      />
    </Card>
  );
}
