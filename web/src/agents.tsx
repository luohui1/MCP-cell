import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { TriangleAlert } from 'lucide-react';
import { takeover, unified, type AgentCell, type AgentSummary, type AutostartStatus, type McpRow } from './api';
import { useStatus } from './App';
import { firstLine } from './format';
import { BrandMark } from './icons';
import { Button, Checkbox, Modal, useToast } from './ui';

export const agentOrder = ['claude-code', 'codex', 'cursor'];
export const agentBrand: Record<string, string> = { 'claude-code': 'claude', codex: 'openai', cursor: 'cursor' };
export const restartHint: Record<string, string> = {
  'claude-code': '新开的 Claude Code 会话立即生效，已打开的会话重启后生效',
  codex: '重启 Codex（包括桌面版）后生效',
  cursor: '在 Cursor 的 MCP 设置里刷新，或重启 Cursor',
};
const shortHint: Record<string, string> = {
  'claude-code': 'Claude Code 新会话立即生效',
  codex: 'Codex 重启后生效',
  cursor: 'Cursor 刷新 MCP 后生效',
};

export type Unified = { agents: AgentSummary[]; rows: McpRow[]; autostart: AutostartStatus };

/** Loads /api/mcp and keeps it fresh. */
export function useUnified() {
  const [data, setData] = useState<Unified | null>(null);
  const [error, setError] = useState('');
  const reload = useCallback(async () => {
    try {
      setData(await unified.load());
      setError('');
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    reload();
    const t = setInterval(() => !document.hidden && reload(), 4000);
    return () => clearInterval(t);
  }, [reload]);
  return { data, error, reload };
}

/** Agents with a configuration file, in display order. */
export const agentsOf = (all: AgentSummary[]) =>
  agentOrder.map((id) => all.find((a) => a.id === id)).filter((a): a is AgentSummary => !!a && a.found);

export const cellsOf = (r: McpRow) => Object.values(r.agents);

/** A native entry MCPcell can take over now; unknown ones need force. */
export const canTake = (r: McpRow, c?: AgentCell) =>
  !!c && c.state === 'native' && (c.class === 'shared' || (c.class === 'unknown' && r.kind === 'stdio'));

export type Group = 'managed' | 'available' | 'blocked' | 'other';

export function groupOf(r: McpRow): Group {
  const cells = cellsOf(r);
  if (r.service || cells.some((c) => c.state === 'managed' || c.state === 'changed')) return 'managed';
  if (cells.some((c) => canTake(r, c))) return 'available';
  if (r.class === 'stateful' || cells.some((c) => c.class === 'stateful')) return 'blocked';
  return 'other';
}

export interface TakeStep {
  agent: AgentSummary;
  entry: string;
  service: string;
  isNew: boolean;
  force: boolean;
}
export interface RestoreStep {
  agent: AgentSummary;
  entry: string;
  added?: boolean;
}

/** Steps that take over every eligible cell of a row (optionally one agent). */
export function takeSteps(r: McpRow, agents: AgentSummary[], only?: string): TakeStep[] {
  return agents.flatMap((a) => {
    const c = r.agents[a.id];
    if ((only && a.id !== only) || !canTake(r, c)) return [];
    return [{ agent: a, entry: c.entry, service: r.service ?? r.proposed ?? c.entry, isNew: !r.service, force: c.class !== 'shared' }];
  });
}

export function restoreSteps(r: McpRow, agents: AgentSummary[]): RestoreStep[] {
  return agents.flatMap((a) => {
    const c = r.agents[a.id];
    return c?.state === 'managed' ? [{ agent: a, entry: c.entry, added: c.added }] : [];
  });
}

const byAgent = <T extends { agent: AgentSummary }>(steps: T[]) => {
  const m = new Map<string, { agent: AgentSummary; steps: T[] }>();
  for (const s of steps) {
    const g = m.get(s.agent.id) ?? { agent: s.agent, steps: [] };
    g.steps.push(s);
    m.set(s.agent.id, g);
  }
  return [...m.values()];
};

const hints = (agents: AgentSummary[]) => agents.map((a) => shortHint[a.id] ?? `${a.name} 重启后生效`).join('；');

/** Applies takeover steps agent by agent; throws with the first failure. */
export async function applyTakeover(steps: TakeStep[]): Promise<string> {
  const failed: string[] = [];
  let ok = 0;
  for (const g of byAgent(steps)) {
    for (const force of [false, true]) {
      const names = g.steps.filter((s) => s.force === force).map((s) => s.entry);
      if (!names.length) continue;
      const { results } = await takeover.attach(g.agent.id, names, force);
      for (const r of results) r.ok ? ok++ : failed.push(`${g.agent.name} ${r.name}：${r.error}`);
    }
  }
  if (failed.length) throw new Error(`${ok ? `已托管 ${ok} 个，` : ''}${failed.length} 个失败。${failed[0]}`);
  return `已托管 ${ok} 个。${hints(byAgent(steps).map((g) => g.agent))}`;
}

async function applyRestore(steps: RestoreStep[]): Promise<string> {
  const failed: string[] = [];
  let ok = 0;
  for (const g of byAgent(steps)) {
    const { results } = await takeover.restore(g.agent.id, g.steps.map((s) => s.entry));
    for (const r of results) r.ok ? ok++ : failed.push(`${g.agent.name} ${r.name}：${r.error}`);
  }
  if (failed.length) throw new Error(`${ok ? `已还原 ${ok} 个，` : ''}${failed.length} 个失败。${failed[0]}`);
  return `已还原 ${ok} 个。${hints(byAgent(steps).map((g) => g.agent))}`;
}

interface Pending {
  title: string;
  intro?: ReactNode;
  groups: { agent: AgentSummary; lines: ReactNode[] }[];
  warn?: string;
  confirm: string;
  danger?: boolean;
  run: () => Promise<string>;
}

/** Confirmed changes to agent configurations. Every change is listed, per
 * agent, before anything is written. */
export function useAgentActions(reload: () => Promise<void>, auto: AutostartStatus | null) {
  const { refresh } = useStatus();
  const toast = useToast();
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const [enableAuto, setEnableAuto] = useState(true);
  const offerAuto = !!auto?.supported && !auto.enabled;

  async function run() {
    if (!pending) return;
    setBusy(true);
    try {
      if (offerAuto && enableAuto && !pending.danger) await takeover.autostart(true);
      toast(await pending.run());
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy(false);
      setPending(null);
      await Promise.all([reload(), refresh()]);
    }
  }

  const actions = {
    takeover(steps: TakeStep[], title?: string) {
      if (!steps.length) return;
      const n = new Set(steps.map((s) => s.service)).size;
      setPending({
        title: title ?? (n === 1 ? `托管 ${steps[0].service}` : `托管 ${n} 个 MCP`),
        intro: '改由 MCPcell 运行：所有会话共用一份进程，工具名不变，随时可以还原。',
        groups: byAgent(steps).map((g) => ({
          agent: g.agent,
          lines: g.steps.map((s) => (
            <>
              <span className="font-medium text-zinc-800 dark:text-zinc-200">{s.entry}</span>
              <span className="text-zinc-400"> → /s/{s.service}</span>
              {s.isNew && <span className="text-zinc-400">（新建服务）</span>}
            </>
          )),
        })),
        warn: steps.some((s) => s.force)
          ? 'MCPcell 不确定其中有的 MCP 是否保存会话状态（浏览器页面、连接、登录）。如果有，多个会话共享后会互相干扰，出问题时还原即可。'
          : undefined,
        confirm: '托管',
        run: () => applyTakeover(steps),
      });
    },
    restore(steps: RestoreStep[], title?: string) {
      if (!steps.length) return;
      setPending({
        title: title ?? (steps.length === 1 ? (steps[0].added ? `从 ${steps[0].agent.name} 移除 ${steps[0].entry}` : `还原 ${steps[0].entry}`) : `还原 ${steps.length} 个托管`),
        intro: '恢复成托管前的原始配置（逐字节），之后每个会话会各自启动进程。托管后被你改过的条目不会被覆盖。',
        groups: byAgent(steps).map((g) => ({
          agent: g.agent,
          lines: g.steps.map((s) => (
            <>
              <span className="font-medium text-zinc-800 dark:text-zinc-200">{s.entry}</span>
              <span className="text-zinc-400">{s.added ? ' · 由 MCPcell 加入，将被移除' : ' · 还原为原生'}</span>
            </>
          )),
        })),
        confirm: '还原',
        danger: true,
        run: () => applyRestore(steps),
      });
    },
    forget(agent: AgentSummary, entry: string) {
      setPending({
        title: `忘记 ${entry} 的托管记录`,
        intro: '这个条目在托管后被改过。保留你现在的配置，只删除 MCPcell 的记录。',
        groups: [{ agent, lines: [entry] }],
        confirm: '忘记记录',
        run: async () => {
          const { results } = await takeover.restore(agent.id, [entry], true);
          if (!results[0]?.ok) throw new Error(results[0]?.error ?? '失败');
          return '已忘记记录';
        },
      });
    },
    add(agent: AgentSummary, row: McpRow) {
      const service = row.service ?? row.proposed ?? row.name;
      setPending({
        title: `把 ${row.name} 加入 ${agent.name}`,
        intro: row.service ? undefined : `会先在 MCPcell 里创建服务 ${service}，配置取自其他 Agent 里的同一个 MCP。`,
        groups: [
          {
            agent,
            lines: [
              <>
                新增 <span className="font-medium text-zinc-800 dark:text-zinc-200">{service}</span>
                <span className="text-zinc-400"> → /s/{service}</span>
              </>,
            ],
          },
        ],
        confirm: '加入',
        run: async () => (await unified.add(agent.id, row.key), `已把 ${row.name} 加入 ${agent.name}。${shortHint[agent.id] ?? ''}`),
      });
    },
  };

  const dialog = (
    <Modal open={pending !== null} onClose={() => !busy && setPending(null)} width={500}>
      {pending && (
        <>
          <div className="flex gap-3.5">
            {pending.danger && (
              <div className="grid size-9 shrink-0 place-items-center rounded-full bg-amber-50 text-amber-600 dark:bg-amber-950">
                <TriangleAlert className="size-[18px]" />
              </div>
            )}
            <div className="min-w-0">
              <h2 className="text-base font-semibold">{pending.title}</h2>
              {pending.intro && <p className="mt-1.5 text-sm leading-6 text-zinc-500">{pending.intro}</p>}
            </div>
          </div>
          <div className="mt-4 space-y-2.5">
            {pending.groups.map((g) => (
              <AgentChangeCard key={g.agent.id} agent={g.agent} lines={g.lines} />
            ))}
          </div>
          {pending.warn && (
            <p className="mt-3 rounded-xl bg-amber-50 px-4 py-3 text-[13px] leading-6 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">{pending.warn}</p>
          )}
          {offerAuto && !pending.danger && <AutostartOption checked={enableAuto} onChange={setEnableAuto} />}
          <div className="mt-6 flex justify-end gap-2">
            <Button disabled={busy} onClick={() => setPending(null)}>
              取消
            </Button>
            <Button variant={pending.danger ? 'danger' : 'primary'} busy={busy} onClick={run}>
              {pending.confirm}
            </Button>
          </div>
        </>
      )}
    </Modal>
  );

  return { ...actions, dialog };
}

/** One agent's planned changes, with the file that will be edited. */
export function AgentChangeCard({ agent, lines }: { agent: AgentSummary; lines: ReactNode[] }) {
  return (
    <div className="rounded-xl border border-zinc-200 px-4 py-3 dark:border-zinc-700">
      <div className="flex items-center gap-2">
        <BrandMark brand={agentBrand[agent.id]} className="size-4" />
        <span className="text-sm font-medium">{agent.name}</span>
        <span className="min-w-0 truncate font-mono text-xs text-zinc-400" title={agent.path}>
          {agent.path}
        </span>
      </div>
      <ul className="mt-2 space-y-1 text-[13px] leading-5">
        {lines.map((l, i) => (
          <li key={i} className="truncate">
            {l}
          </li>
        ))}
      </ul>
      <div className="mt-2 text-xs text-zinc-400">只改这些条目，其余内容不动；改之前先备份整个文件。{restartHint[agent.id]}</div>
    </div>
  );
}

export function AutostartOption({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="mt-4 flex cursor-pointer items-center gap-2.5 text-sm">
      <Checkbox checked={checked} onChange={onChange} />
      同时开启开机自启
      <span className="text-zinc-400">托管后 Agent 依赖 MCPcell 在后台运行</span>
    </label>
  );
}
