import { instancesOf, type McpRow, type Service, type SessionInfo, type Status } from './api';
import { bytes, firstLine } from './format';
import { purposes } from './purposes';

// What the lists show: each MCP reduced to a group and one line of
// status, and what the overview lists as needing attention.

export type Tone = 'green' | 'blue' | 'amber' | 'red' | 'gray';

/** Agents that can be taken over automatically, in display order. */
/** The agents MCPcell edits (internal/clients), in display order. */
export const agentOrder = ['claude-code', 'codex', 'cursor', 'claude-desktop', 'vscode', 'copilot', 'gemini', 'antigravity', 'windsurf', 'kiro', 'cline', 'roo', 'opencode'];

// Tell the agent from a session's clientInfo name, first match wins; the
// daemon's agentOf uses the same list.
const clientMarks: [string, string][] = [
  ['claude-ai', 'claude-desktop'],
  ['claude', 'claude-code'],
  ['codex', 'codex'],
  ['openai', 'codex'],
  ['cursor', 'cursor'],
  ['visual studio code', 'vscode'],
  ['vscode', 'vscode'],
  ['copilot', 'copilot'],
  ['antigravity', 'antigravity'],
  ['gemini', 'gemini'],
  ['windsurf', 'windsurf'],
  ['kiro', 'kiro'],
  ['cline', 'cline'],
  ['roo code', 'roo'],
  ['roo-code', 'roo'],
  ['opencode', 'opencode'],
];

/** Which agent an MCP session came from, by its clientInfo name. */
export function agentOfClient(client: string): string {
  const c = client.toLowerCase();
  return clientMarks.find(([mark]) => c.includes(mark))?.[1] ?? '';
}

export const failing = (s: Service) => s.state === 'failed' || (s.failures ?? 0) > 0;

// ---- MCP -------------------------------------------------------------------

// Three states for an MCP MCPcell runs: 待命 (not running, opens when an app
// needs it), 运行中, 打不开 (with a plain reason). An MCP the apps still run
// themselves is native; remote and built-in ones need nothing (other); one
// whose program is gone or that never started is ignored.
export type McpKind = 'broken' | 'running' | 'standby' | 'native' | 'other' | 'ignored';

export interface McpView {
  id: string; // route segment: the service name, or the row key
  row: McpRow;
  kind: McpKind;
  tone: Tone;
  text: string;
  purpose: string;
  /** The configured service and its run- variants (agent entries edited since). */
  services: Service[];
  sessions: SessionInfo[];
  managed: boolean; // some app uses MCPcell's copy
  takeable: boolean; // some app runs it itself and could use MCPcell's copy
  broken?: Service; // the service that cannot start
}

export const mcpId = (r: McpRow) => r.service ?? r.key;

export const kindRank: Record<McpKind, number> = { broken: 0, running: 1, standby: 2, native: 3, other: 4, ignored: 5 };

export function mcpView(row: McpRow, status: Status): McpView {
  const services = row.service ? status.services.filter((s) => s.name === row.service || s.of === row.service) : [];
  const names = new Set(services.map((s) => s.name));
  const sessions = status.sessions.filter((x) => names.has(x.service));
  const cells = Object.values(row.agents);
  const managed = cells.some((c) => c.state === 'managed');
  const takeable = cells.some((c) => c.state === 'native' && !!c.takeable && !c.dead);
  const base = { id: mcpId(row), row, services, sessions, managed, takeable, purpose: purposeOf(row, services) };

  const broken = services.find(failing);
  if (broken) {
    const reason = plainReason(broken.lastError);
    // One that never started, or whose program is gone, will not come back by
    // itself: ignored like a native one that cannot start. Red is for one
    // that worked and broke.
    if (!services.some((s) => s.starts > 0 || s.catalog) || /程序不存在|未安装/.test(reason)) {
      return { ...base, kind: 'ignored', tone: 'gray', text: `无法启动：${reason}` };
    }
    return { ...base, broken, kind: 'broken', tone: 'red', text: `打不开：${reason}` };
  }
  if (managed && !row.service) return { ...base, kind: 'broken', tone: 'amber', text: 'MCPcell 中的配置已删除' };
  if (!row.service) {
    if (row.kind === 'http') return { ...base, kind: 'other', tone: 'gray', text: '远程' };
    if (row.class === 'internal') return { ...base, kind: 'other', tone: 'gray', text: '内置' };
    if (cells.every((c) => c.state === 'remote' || c.state === 'internal')) return { ...base, kind: 'other', tone: 'gray', text: '远程或内置' };
    const dead = cells.find((c) => c.state === 'native' && c.dead);
    if (dead && !takeable) return { ...base, kind: 'ignored', tone: 'gray', text: `无法启动：${plainReason(dead.dead)}` };
    return { ...base, kind: 'native', tone: 'gray', text: '未托管' };
  }
  const runs = services.flatMap(instancesOf);
  if (runs.length) return { ...base, kind: 'running', tone: 'green', text: `运行中 · ${bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}` };
  if (services.some((s) => s.state === 'starting')) return { ...base, kind: 'running', tone: 'amber', text: '启动中' };
  return { ...base, kind: 'standby', tone: 'gray', text: '待命' };
}

/** What the MCP is for, in one line. */
function purposeOf(row: McpRow, services: Service[]): string {
  const known = purposes[row.launch];
  if (known) return known;
  const cat = services.find((s) => s.catalog)?.catalog;
  if (cat?.description) return cat.description;
  const tools = cat?.toolNames ?? [];
  if (tools.length) return `${tools.length} 个工具：${tools.slice(0, 3).join('、')}${tools.length > 3 ? ' 等' : ''}`;
  return '';
}

/** Why a service cannot start, in words a user can act on. */
export function plainReason(err?: string): string {
  const e = err ?? '';
  // A takeover's start check reports "…条目保持原样：<error>；它的输出：<last line>".
  const [head, stderr = ''] = e.replace(/^MCPcell 试着启动它失败，没有接管，条目保持原样：/, '').split(/\nstderr：|；它的输出：/);
  let m: RegExpMatchArray | null;
  if ((m = head.match(/在 PATH 中找不到启动程序 "?([^"]+)"?/))) return `未安装 ${m[1]}`;
  if (/找不到启动程序|指向的脚本不存在/.test(head)) return '程序不存在';
  if (/找不到 node/.test(head)) return '未安装 Node.js';
  if (/npm install .*(失败|超时)/.test(head)) return '下载安装失败';
  if (/不支持直接运行批处理文件/.test(head)) return '无法直接运行批处理文件';
  if (/超出内存上限/.test(head)) return '超出内存上限';
  if (/崩溃/.test(head)) return '反复崩溃，已暂停';
  if (/没有完成初始化/.test(head)) return '启动超时';
  if (/拒绝初始化/.test(head)) return '拒绝连接';
  const out = stderr.toLowerCase();
  const missing = /missing|required|need to provide|must (be )?provide|not set|undefined|缺少|未提供|必须/.test(out);
  if (missing && /host|server|url|address|endpoint|地址/.test(out)) return '缺少服务器地址';
  if (/api[ _-]?key|token|secret|credential|unauthori[sz]ed|forbidden|\b40[13]\b|密钥/.test(out)) return '密钥缺失或无效';
  if (missing) return '缺少必要的参数';
  if (/econnrefused|enotfound|etimedout|connection refused|network/.test(out)) return '无法连接所需服务';
  if (/eaddrinuse|address already in use|端口/.test(out)) return '端口被占用';
  if (/cannot find module|modulenotfounderror|no module named/.test(out)) return '缺少依赖';
  if (/进程意外退出/.test(head)) return '意外退出';
  if (/进程在完成初始化前退出了/.test(head)) return '启动后立即退出';
  return firstLine(head) || '启动失败';
}

/** What to do about a plain reason. */
export function advice(reason: string): string {
  if (/缺少|密钥|地址|参数/.test(reason)) return '在应用中补全配置后重试。';
  if (/未安装|不存在/.test(reason)) return '安装程序或修正路径后重试。';
  if (/下载|连接/.test(reason)) return '检查网络后重试。';
  if (/内存/.test(reason)) return '可在「高级」中调高内存上限。';
  return '查看出错信息，处理后重试。';
}
