import { useEffect, useRef, useState } from 'react';
import type { MetricPoint } from './api';
import { cx } from './ui';

// The dashboard's memory chart: what MCPs take now (solid, the one series
// in color) and, when given, what they would take without sharing (gray,
// dashed: an estimate). One y-axis in bytes; hovering snaps a crosshair to
// the nearest sample and reads both out.

export interface ChartSeries {
  key: 'total' | 'estimate';
  label: string;
}

const pad = { left: 56, right: 14, top: 10, bottom: 24 };

/** 0, then "200 MB" / "1.5 GB" with as few digits as the step needs. */
function axisBytes(n: number, step: number): string {
  if (n === 0) return '0';
  const mb = n / 1048576;
  if (mb >= 1024) {
    const gb = mb / 1024;
    return `${step / 1048576 >= 1024 ? Math.round(gb) : gb.toFixed(1)} GB`;
  }
  return `${step / 1048576 < 1 ? mb.toFixed(1) : Math.round(mb)} MB`;
}

/** A round step giving at most four gridlines above zero. */
function niceStep(max: number): number {
  const raw = max / 4;
  const p = 10 ** Math.floor(Math.log10(raw));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * p >= raw) return m * p;
  return 10 * p;
}

export function readBytes(n: number): string {
  const mb = n / 1048576;
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`;
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`;
}

function hhmm(t: number): string {
  return new Date(t * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
}

export function MemoryChart({
  points,
  step,
  from,
  to,
  series,
  height = 200,
  empty,
  extra,
}: {
  points: MetricPoint[];
  step: number; // seconds between points
  from: number; // Unix seconds, the left edge
  to: number;
  series: ChartSeries[];
  height?: number;
  empty: string;
  /** More lines for the hover readout. */
  extra?: (p: MetricPoint) => string | undefined;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | null>(null); // index into points
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const w = Math.max(0, width - pad.left - pad.right);
  const h = height - pad.top - pad.bottom;
  const maxV = Math.max(1, ...points.flatMap((p) => series.map((s) => p[s.key])));
  const yStep = niceStep(maxV * 1.08);
  const yMax = yStep * Math.max(1, Math.ceil((maxV * 1.08) / yStep));
  const x = (t: number) => pad.left + ((t - from) / Math.max(1, to - from)) * w;
  const y = (v: number) => pad.top + h - (v / yMax) * h;

  // A gap of more than a few samples (the daemon was not running) breaks the line.
  const path = (key: ChartSeries['key']) => {
    let d = '';
    let prev: MetricPoint | null = null;
    for (const p of points) {
      const join = prev && p.t - prev.t <= step * 2.5;
      d += `${join ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p[key]).toFixed(1)}`;
      prev = p;
    }
    return d;
  };
  const area = () => {
    // The area under the solid line, per unbroken run.
    const runs: MetricPoint[][] = [];
    for (const p of points) {
      const last = runs.at(-1);
      if (last && p.t - last[last.length - 1].t <= step * 2.5) last.push(p);
      else runs.push([p]);
    }
    return runs
      .filter((r) => r.length > 1)
      .map((r) => `M${x(r[0].t)},${y(0)}` + r.map((p) => `L${x(p.t).toFixed(1)},${y(p.total).toFixed(1)}`).join('') + `L${x(r[r.length - 1].t)},${y(0)}Z`)
      .join('');
  };

  // Time ticks on the local clock: every 10 minutes for an hour, 3 hours for a day.
  const tickEvery = to - from <= 3600 ? 600 : 3 * 3600;
  const offset = new Date().getTimezoneOffset() * 60;
  const ticks: number[] = [];
  for (let t = Math.ceil((from - offset) / tickEvery) * tickEvery + offset; t <= to; t += tickEvery) ticks.push(t);
  const yTicks: number[] = [];
  for (let v = 0; v <= yMax + 1; v += yStep) yTicks.push(v);

  const onMove = (e: React.MouseEvent<SVGRectElement>) => {
    if (!points.length) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const t = from + ((e.clientX - rect.left) / Math.max(1, rect.width)) * (to - from);
    let lo = 0;
    let hi = points.length - 1;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (points[mid].t < t) lo = mid + 1;
      else hi = mid;
    }
    if (lo > 0 && Math.abs(points[lo - 1].t - t) < Math.abs(points[lo].t - t)) lo--;
    setHover(Math.abs(points[lo].t - t) <= Math.max(step * 3, (to - from) / 60) ? lo : null);
  };

  const hp = hover !== null ? points[hover] : null;
  const tipLeft = hp ? x(hp.t) : 0;
  const flip = tipLeft > pad.left + w * 0.66;
  const shown = points.length >= 2;

  return (
    <div ref={box} className="relative select-none" style={{ height }}>
      {width > 0 && (
        <svg width={width} height={height} role="img" aria-label={series.map((s) => s.label).join('与') + '随时间的变化'} className="block overflow-visible">
          {yTicks.map((v) => (
            <g key={v}>
              <line x1={pad.left} x2={pad.left + w} y1={y(v)} y2={y(v)} className={v === 0 ? 'stroke-zinc-200 dark:stroke-zinc-700' : 'stroke-zinc-100 dark:stroke-zinc-800'} />
              <text x={pad.left - 8} y={y(v)} dy="0.32em" textAnchor="end" className="fill-zinc-400 text-[11px] tabular-nums">
                {axisBytes(v, yStep)}
              </text>
            </g>
          ))}
          {ticks.map((t) => (
            <text key={t} x={x(t)} y={height - 6} textAnchor="middle" className="fill-zinc-400 text-[11px] tabular-nums">
              {hhmm(t)}
            </text>
          ))}
          {shown && (
            <>
              {series.some((s) => s.key === 'total') && <path d={area()} className="fill-blue-500/[0.07] dark:fill-blue-400/[0.08]" />}
              {series
                .filter((s) => s.key === 'estimate')
                .map((s) => (
                  <path key={s.key} d={path(s.key)} fill="none" strokeWidth={2} strokeDasharray="5 4" strokeLinejoin="round" className="stroke-zinc-500" />
                ))}
              {series
                .filter((s) => s.key === 'total')
                .map((s) => (
                  <path key={s.key} d={path(s.key)} fill="none" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" className="stroke-blue-600 dark:stroke-blue-500" />
                ))}
            </>
          )}
          {hp && (
            <g pointerEvents="none">
              <line x1={x(hp.t)} x2={x(hp.t)} y1={pad.top} y2={pad.top + h} className="stroke-zinc-300 dark:stroke-zinc-600" strokeDasharray="2 3" />
              {series.map((s) => (
                <circle
                  key={s.key}
                  cx={x(hp.t)}
                  cy={y(hp[s.key])}
                  r={4}
                  strokeWidth={2}
                  className={cx('stroke-white dark:stroke-zinc-900', s.key === 'total' ? 'fill-blue-600 dark:fill-blue-500' : 'fill-zinc-500')}
                />
              ))}
            </g>
          )}
          <rect x={pad.left} y={pad.top} width={w} height={h} fill="transparent" onMouseMove={onMove} onMouseLeave={() => setHover(null)} />
        </svg>
      )}
      {!shown && width > 0 && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-[13px] text-zinc-400" style={{ paddingLeft: pad.left, paddingBottom: pad.bottom }}>
          {empty}
        </div>
      )}
      {hp && (
        <div
          className="pointer-events-none absolute z-10 min-w-44 rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs shadow-lg dark:border-zinc-700 dark:bg-zinc-900"
          style={{ top: pad.top, left: flip ? undefined : tipLeft + 12, right: flip ? width - tipLeft + 12 : undefined }}
        >
          <div className="mb-1 font-medium text-zinc-500 tabular-nums">{hhmm(hp.t)}</div>
          {series.map((s) => (
            <div key={s.key} className="flex items-center gap-2 leading-5">
              <Swatch kind={s.key} />
              <span className="text-zinc-500">{s.label}</span>
              <span className="ml-auto pl-3 font-medium text-zinc-800 tabular-nums dark:text-zinc-100">{s.key === 'estimate' && !hp.estimate ? '—' : readBytes(hp[s.key])}</span>
            </div>
          ))}
          {extra?.(hp) && <div className="mt-1 border-t border-zinc-100 pt-1 text-zinc-500 dark:border-zinc-800">{extra(hp)}</div>}
        </div>
      )}
    </div>
  );
}

/** The legend mark of a series: a solid or dashed stroke. */
export function Swatch({ kind }: { kind: ChartSeries['key'] }) {
  return (
    <svg width="16" height="8" aria-hidden className="shrink-0">
      <line
        x1="1"
        x2="15"
        y1="4"
        y2="4"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray={kind === 'estimate' ? '4 3' : undefined}
        className={kind === 'total' ? 'stroke-blue-600 dark:stroke-blue-500' : 'stroke-zinc-500'}
      />
    </svg>
  );
}
