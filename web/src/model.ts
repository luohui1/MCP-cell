import { instancesOf, SOURCE, type AgentSummary, type McpRow, type Service, type SessionInfo, type SkillCopy, type SkillInfo, type SkillStatus, type Status } from './api';
import { bytes, firstLine } from './format';

// What the lists show: each MCP and skill reduced to a group and one line of
// status, and what the overview lists as needing attention.

export type Tone = 'green' | 'blue' | 'amber' | 'red' | 'gray';

/** Agents that can be taken over automatically, in display order. */
export const agentOrder = ['claude-code', 'codex', 'cursor'];

/** Which agent an MCP session came from, by its clientInfo name. */
export function agentOfClient(client: string): string {
  const c = client.toLowerCase();
  if (c.includes('claude')) return 'claude-code';
  if (c.includes('codex') || c.includes('openai')) return 'codex';
  if (c.includes('cursor')) return 'cursor';
  return '';
}

export const failing = (s: Service) => s.state === 'failed' || (s.failures ?? 0) > 0;

// ---- MCP -------------------------------------------------------------------

export type McpGroup = 'attention' | 'ok' | 'unmanaged';

export interface McpView {
  id: string; // route segment: the service name, or the row key
  row: McpRow;
  group: McpGroup;
  tone: Tone;
  text: string;
  /** The configured service and its run- variants (agent entries edited since). */
  services: Service[];
  sessions: SessionInfo[];
  takeable: boolean;
}

export const mcpId = (r: McpRow) => r.service ?? r.key;

export function mcpView(row: McpRow, status: Status, agents: AgentSummary[]): McpView {
  const v = view(row, status, agents);
  // Rows sharing a name (detail is set then) also say what they run.
  return row.detail && row.launch ? { ...v, text: `${v.text} · ${row.launch}` } : v;
}

function view(row: McpRow, status: Status, agents: AgentSummary[]): McpView {
  const services = row.service ? status.services.filter((s) => s.name === row.service || s.of === row.service) : [];
  const names = new Set(services.map((s) => s.name));
  const sessions = status.sessions.filter((x) => names.has(x.service));
  const cells = Object.values(row.agents);
  const managed = !!row.service || cells.some((c) => c.state === 'managed' || c.state === 'changed');
  const takeable = cells.some((c) => c.state === 'native' && !!c.takeable);
  const base = { id: mcpId(row), row, services, sessions, takeable };

  const broken = services.find(failing);
  if (broken) return { ...base, group: 'attention', tone: 'red', text: `起不来：${firstLine(broken.lastError) || '启动失败'}` };
  if (managed && !row.service) return { ...base, group: 'attention', tone: 'amber', text: '托管的服务已从 MCPcell 删除' };
  const changed = agents.filter((a) => row.agents[a.id]?.state === 'changed');
  if (changed.length) return { ...base, group: 'attention', tone: 'amber', text: `${changed.map((a) => a.name).join('、')} 里的条目托管后被改过` };

  if (!managed) {
    if (takeable) return { ...base, group: 'unmanaged', tone: 'gray', text: '可以托管 · 现在每个会话各起一份进程' };
    if (row.kind === 'http') return { ...base, group: 'unmanaged', tone: 'gray', text: '远程服务，不需要托管' };
    if (row.class === 'internal') return { ...base, group: 'unmanaged', tone: 'gray', text: 'Agent 内置' };
    return { ...base, group: 'unmanaged', tone: 'gray', text: row.reason || '不能托管' };
  }

  const off = cells.length > 0 && cells.every((c) => !c.enabled);
  const runs = services.flatMap(instancesOf);
  const mem = bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0));
  const n = sessions.length;
  if (runs.length) {
    const what = row.mode === 'per-session' ? `${runs.length} 个实例运行中` : '运行中';
    return { ...base, group: 'ok', tone: 'green', text: `${what} · ${mem}${n ? ` · ${n} 个会话` : ''}` };
  }
  if (services.some((s) => s.state === 'starting')) return { ...base, group: 'ok', tone: 'amber', text: '启动中…' };
  if (off) return { ...base, group: 'ok', tone: 'gray', text: '已在 Agent 里停用' };
  return { ...base, group: 'ok', tone: 'gray', text: n ? `空闲 · ${n} 个会话` : '调用时启动' };
}

// ---- Skill -----------------------------------------------------------------

export type SkillGroup = 'attention' | 'synced' | 'single' | 'external';

export interface SkillView {
  skill: SkillInfo;
  group: SkillGroup;
  tone: Tone;
  text: string;
}

const brokenCopy = (c: SkillCopy) => c.changed || !!c.error || c.kind === 'broken';

export function skillView(k: SkillInfo, data: SkillStatus): SkillView {
  const copies = k.copies.filter((c) => c.location !== SOURCE);
  const bad = copies.find(brokenCopy);
  if (bad) {
    const where = locationName(data, bad.location);
    const what = bad.changed ? '同步后被改过' : bad.kind === 'broken' ? '链接失效' : '读取失败';
    return { skill: k, group: 'attention', tone: 'red', text: `${where} ${what}` };
  }
  switch (k.state) {
    case 'differs':
      return { skill: k, group: 'attention', tone: 'amber', text: `内容不一致 · ${k.variants} 个版本` };
    case 'same':
      return { skill: k, group: 'attention', tone: 'blue', text: `可以合并 · ${copies.length} 处相同` };
    case 'synced':
      return { skill: k, group: 'synced', tone: 'green', text: `已统一 · ${copies.length} 处用同一份` };
    case 'source':
      return { skill: k, group: 'synced', tone: 'gray', text: '只在 MCPcell 的来源目录' };
    case 'external':
      return { skill: k, group: 'external', tone: 'gray', text: `链接到其他工具的目录 · ${copies.length} 处` };
  }
  return { skill: k, group: 'single', tone: 'gray', text: `只在 ${locationName(data, copies[0]?.location ?? '')}` };
}

export function locationName(data: SkillStatus, id: string): string {
  if (id === SOURCE) return 'MCPcell 来源目录';
  return data.locations.find((l) => l.id === id)?.name ?? id;
}

/** The agents that read a skill: those of every location holding a copy. */
export function skillAgents(k: SkillInfo, data: SkillStatus): Set<string> {
  const out = new Set<string>();
  for (const c of k.copies) for (const a of data.locations.find((l) => l.id === c.location)?.agents ?? []) out.add(a);
  return out;
}
