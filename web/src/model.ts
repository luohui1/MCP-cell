import { instancesOf, SOURCE, type McpRow, type Service, type SessionInfo, type SkillCopy, type SkillInfo, type SkillStatus, type Status } from './api';
import { bytes, firstLine } from './format';
import { purposes } from './purposes';

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

// Three states for an MCP MCPcell runs: 待命 (not running, opens when an app
// needs it), 运行中, 打不开 (with a plain reason). An MCP the apps still run
// themselves is native; remote and built-in ones need nothing (other).
export type McpKind = 'broken' | 'running' | 'standby' | 'native' | 'other';

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

export const kindRank: Record<McpKind, number> = { broken: 0, running: 1, standby: 2, native: 3, other: 4 };

export function mcpView(row: McpRow, status: Status): McpView {
  const services = row.service ? status.services.filter((s) => s.name === row.service || s.of === row.service) : [];
  const names = new Set(services.map((s) => s.name));
  const sessions = status.sessions.filter((x) => names.has(x.service));
  const cells = Object.values(row.agents);
  const managed = cells.some((c) => c.state === 'managed');
  const takeable = cells.some((c) => c.state === 'native' && !!c.takeable);
  const base = { id: mcpId(row), row, services, sessions, managed, takeable, purpose: purposeOf(row, services) };

  const broken = services.find(failing);
  if (broken) return { ...base, broken, kind: 'broken', tone: 'red', text: `打不开：${plainReason(broken.lastError)}` };
  if (managed && !row.service) return { ...base, kind: 'broken', tone: 'amber', text: 'MCPcell 里的设置被删了' };
  if (!row.service) {
    if (row.kind === 'http') return { ...base, kind: 'other', tone: 'gray', text: '远程服务，不用管' };
    if (row.class === 'internal') return { ...base, kind: 'other', tone: 'gray', text: '应用自带' };
    if (cells.every((c) => c.state === 'remote' || c.state === 'internal')) return { ...base, kind: 'other', tone: 'gray', text: '不用管' };
    return { ...base, kind: 'native', tone: 'gray', text: '各个应用自己开着' };
  }
  const runs = services.flatMap(instancesOf);
  if (runs.length) return { ...base, kind: 'running', tone: 'green', text: `运行中 · ${bytes(runs.reduce((n, i) => n + i.usage.privateBytes, 0))}` };
  if (services.some((s) => s.state === 'starting')) return { ...base, kind: 'running', tone: 'amber', text: '正在打开…' };
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
  const [head, stderr = ''] = e.split('\nstderr：');
  let m: RegExpMatchArray | null;
  if ((m = head.match(/在 PATH 中找不到启动程序 "?([^"]+)"?/))) return `电脑上没有装 ${m[1]}`;
  if (/找不到启动程序|指向的脚本不存在/.test(head)) return '程序不在了';
  if (/找不到 node/.test(head)) return '电脑上没有装 Node.js';
  if (/npm install .*(失败|超时)/.test(head)) return '下载安装失败，检查一下网络';
  if (/不支持直接运行批处理文件/.test(head)) return '启动命令是批处理文件，没法直接运行';
  if (/超出内存上限/.test(head)) return '占的内存超过上限，被停掉了';
  if (/崩溃/.test(head)) return '反复崩溃，先暂停打开';
  if (/没有完成初始化/.test(head)) return '打开太慢，超时了';
  if (/拒绝初始化/.test(head)) return '它拒绝了连接';
  const out = stderr.toLowerCase();
  const missing = /missing|required|need to provide|must (be )?provide|not set|undefined|缺少|未提供|必须/.test(out);
  if (missing && /host|server|url|address|endpoint|地址/.test(out)) return '缺少服务器地址';
  if (/api[ _-]?key|token|secret|credential|unauthori[sz]ed|forbidden|\b40[13]\b|密钥/.test(out)) return '缺少密钥，或者密钥不对';
  if (missing) return '缺少必要的参数';
  if (/econnrefused|enotfound|etimedout|connection refused|network/.test(out)) return '连不上它要用的服务';
  if (/eaddrinuse|address already in use|端口/.test(out)) return '要用的端口被占用了';
  if (/cannot find module|modulenotfounderror|no module named/.test(out)) return '缺少它依赖的组件';
  if (/进程意外退出/.test(head)) return '运行中意外退出了';
  if (/进程在完成初始化前退出了/.test(head)) return '一打开就退出了';
  return firstLine(head) || '启动失败';
}

/** What to do about a plain reason. */
export function advice(reason: string): string {
  if (/缺少|密钥/.test(reason)) return '到用它的应用里把 MCP 的设置补全，改好后点「再试一次」。';
  if (/没有装|程序不在了/.test(reason)) return '程序可能被卸载或移动了。重新安装它，或者在应用里把路径改对，然后点「再试一次」。';
  if (/网络|连不上/.test(reason)) return '检查网络，或者它要连的服务是否开着，然后点「再试一次」。';
  if (/内存/.test(reason)) return '可以在下面「高级」里调高内存上限。';
  return '看看下面的具体出错信息，处理好后点「再试一次」。';
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
