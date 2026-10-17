export function bytes(n?: number): string {
  if (!n) return '—';
  const mb = n / 1048576;
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`;
}

export function span(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} 秒`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} 分钟`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h} 小时`;
  return `${Math.round(h / 24)} 天`;
}

export const since = (t: string) => span(Date.now() - new Date(t).getTime());
export const until = (t: string) => span(new Date(t).getTime() - Date.now());

export function clock(t: string): string {
  return new Date(t).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

export function firstLine(s?: string): string {
  return (s ?? '').split('\n')[0];
}
