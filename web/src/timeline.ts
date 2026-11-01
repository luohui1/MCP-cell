import type { AppEvent, Service } from './api';
import { plainReason, type Tone } from './model';

// The home page's 最近动态: the daemon's events (internal/events) in plain
// words. Runs of the same thing happening to the same MCP fold into one line.

export interface TimelineItem {
  key: string;
  kind: string; // the event's kind (internal/events)
  at: string; // the newest of the run
  tone: Tone;
  text: string;
  detail?: string;
  service?: string; // the MCP to open, when there is one
  count: number;
}

const agentNames: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
  cursor: 'Cursor',
  'claude-desktop': 'Claude Desktop',
  vscode: 'VS Code',
  copilot: 'Copilot CLI',
  gemini: 'Gemini CLI',
  antigravity: 'Antigravity',
  windsurf: 'Windsurf',
  kiro: 'Kiro',
  cline: 'Cline',
  roo: 'Roo Code',
  opencode: 'OpenCode',
};
/** An agent's name; a Claude Code project's is "Claude Code · <folder>". */
export function agentName(id?: string): string {
  if (!id) return '';
  const [app, dir] = id.split('@');
  const name = agentNames[app] ?? app;
  return dir ? `${name} · ${dir.split('/').filter(Boolean).pop() ?? dir}` : name;
}

function describe(e: AppEvent, name: string, count: number, services: Service[]): Omit<TimelineItem, 'key' | 'kind' | 'at' | 'count' | 'service'> | null {
  const app = agentName(e.client);
  const entry = e.entry ?? name;
  const times = count > 1 ? ` ${count} 次` : '';
  switch (e.kind) {
    case 'started':
      return { tone: 'green', text: `${name} 启动${times}` };
    case 'stopped_idle':
      return { tone: 'gray', text: `${name} 空闲停止${times}` };
    case 'stopped_manual':
      return { tone: 'gray', text: `${name} 手动停止` };
    case 'stopped_session':
      return { tone: 'gray', text: `${name} 对话结束，停止${count > 1 ? ` ${count} 份` : ''}` };
    case 'stopped_budget':
      return { tone: 'gray', text: `${name} 超出数量上限，停止` };
    case 'stopped_config':
      return { tone: 'gray', text: `${name} 配置变更，重启` };
    case 'stopped_update':
      return { tone: 'gray', text: `${name} 版本更新，重启` };
    case 'stopped_deleted':
    case 'deleted':
      return { tone: 'gray', text: `${name} 已删除` };
    case 'crashed':
      return { tone: 'amber', text: `${name} 意外退出${times}` };
    case 'start_failed': {
      // The error kept with the service has the program's own output; the
      // saved event only its first line (stderr stays in memory).
      const now = services.find((s) => s.name === e.service && s.lastError)?.lastError;
      return { tone: 'red', text: `${name} 启动失败${times}`, detail: plainReason(now ?? e.detail) };
    }
    case 'quarantined':
      return { tone: 'red', text: `${name} 反复崩溃，已暂停` };
    case 'taken_over':
      return { tone: 'blue', text: `${app} · ${entry} 已托管` };
    case 'restored':
      return { tone: 'gray', text: `${app} · ${entry} 已还原` };
    case 'added':
      return { tone: 'blue', text: `${name} 已添加到 ${app}` };
    case 'auto_taken':
      return { tone: 'blue', text: `${app} · ${entry} 已自动托管` };
    case 'auto_failed':
      return { tone: 'red', text: `${app} · ${entry} 自动托管失败`, detail: plainReason(e.detail) };
    case 'auto_stopped':
      return { tone: 'amber', text: `${app} · ${entry} 反复被改回，停止自动托管` };
    case 'saved':
      return { tone: 'gray', text: `${name} 配置已保存` };
    case 'daemon_started':
      return { tone: 'blue', text: 'MCPcell 启动' };
    case 'daemon_stopped':
      return { tone: 'gray', text: 'MCPcell 退出' };
  }
  return null;
}

/** Folds what is the same thing happening again within ten minutes. */
const folds = new Set(['started', 'stopped_idle', 'stopped_session', 'crashed', 'start_failed']);

export function timeline(events: AppEvent[], services: Service[]): TimelineItem[] {
  // A run- variant shows under the configured MCP it belongs to.
  const base = (name?: string) => (name ? (services.find((s) => s.name === name)?.of ?? name) : '');
  const out: (TimelineItem & { last: string })[] = [];
  for (const e of events) {
    const name = base(e.service) || e.entry || '';
    const prev = out.at(-1);
    if (prev && folds.has(e.kind) && prev.kind === e.kind && prev.service === name && Date.parse(prev.last) - Date.parse(e.at) < 10 * 60_000) {
      prev.count++;
      prev.last = e.at;
      const d = describe(e, name, prev.count, services);
      if (d) Object.assign(prev, d);
      continue;
    }
    const d = describe(e, name, 1, services);
    if (!d) continue;
    out.push({ ...d, key: `${e.at}-${e.kind}-${name}`, at: e.at, last: e.at, count: 1, kind: e.kind, service: e.service ? name : undefined });
  }
  return out;
}

/** "14:05", "昨天 14:05" or "10月20日 14:05". */
export function when(at: string): string {
  const d = new Date(at);
  const time = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(new Date()) - day(d)) / 86_400_000);
  if (diff === 0) return time;
  if (diff === 1) return `昨天 ${time}`;
  return `${d.getMonth() + 1}月${d.getDate()}日 ${time}`;
}
