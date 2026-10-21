import { useState, type ReactNode } from 'react';
import { TriangleAlert } from 'lucide-react';
import { takeover, unified, type AgentCell, type AgentSummary, type AutostartStatus, type McpRow } from './api';
import { useStatus } from './data';
import { firstLine } from './format';
import { BrandMark } from './icons';
import { agentOrder } from './model';
import { Button, Checkbox, Modal, useToast } from './ui';

export const agentBrand: Record<string, string> = { 'claude-code': 'claude', codex: 'openai', cursor: 'cursor' };
export const restartHint: Record<string, string> = {
  'claude-code': 'Claude Code 新开的对话立即生效，已经开着的重开后生效',
  codex: '重启 Codex（包括桌面版）后生效',
  cursor: '在 Cursor 的 MCP 设置里刷新，或重启 Cursor',
};
const shortHint: Record<string, string> = {
  'claude-code': 'Claude Code 新开的对话立即生效',
  codex: 'Codex 重启后生效',
  cursor: 'Cursor 刷新 MCP 后生效',
};

/** Agents with a configuration file, in display order. */
export const agentsOf = (all: AgentSummary[]) =>
  agentOrder.map((id) => all.find((a) => a.id === id)).filter((a): a is AgentSummary => !!a && a.found);

/** A native entry MCPcell can run: local stdio with an environment it can
 * reproduce (the daemon decides). */
export const canTake = (c?: AgentCell) => !!c && c.state === 'native' && !!c.takeable;

export type Mode = 'shared' | 'per-session';

export interface TakeStep {
  agent: AgentSummary;
  entry: string;
  service: string;
  isNew: boolean;
  mode: Mode;
  force: boolean; // share an unverified server
}
export interface RestoreStep {
  agent: AgentSummary;
  entry: string;
  added?: boolean;
}

/** Steps that take over every eligible cell of a row (optionally one agent).
 * share runs an unverified server shared instead of per session. */
export function takeSteps(r: McpRow, agents: AgentSummary[], only?: string, share = false): TakeStep[] {
  return agents.flatMap((a) => {
    const c = r.agents[a.id];
    if ((only && a.id !== only) || !canTake(c)) return [];
    const force = c.class === 'unknown' && share;
    const mode: Mode = r.service ? (r.mode as Mode) : c.class === 'shared' || force ? 'shared' : 'per-session';
    return [{ agent: a, entry: c.entry, service: r.service ?? r.proposed ?? c.entry, isNew: !r.service, mode, force }];
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
  if (failed.length) throw new Error(`${ok ? `已改好 ${ok} 个，` : ''}${failed.length} 个没改成。${failed[0]}`);
  return `已改好 ${ok} 个。${hints(byAgent(steps).map((g) => g.agent))}`;
}

async function applyRestore(steps: RestoreStep[]): Promise<string> {
  const failed: string[] = [];
  let ok = 0;
  for (const g of byAgent(steps)) {
    const { results } = await takeover.restore(g.agent.id, g.steps.map((s) => s.entry));
    for (const r of results) r.ok ? ok++ : failed.push(`${g.agent.name} ${r.name}：${r.error}`);
  }
  if (failed.length) throw new Error(`${ok ? `已改回 ${ok} 个，` : ''}${failed.length} 个没改成。${failed[0]}`);
  return `已改回原样 ${ok} 个。${hints(byAgent(steps).map((g) => g.agent))}`;
}

interface Pending {
  title: string;
  intro?: ReactNode;
  groups: { agent: AgentSummary; lines: ReactNode[] }[];
  warn?: string;
  /** What happens to each agent's file, when it is not a takeover. */
  note?: string;
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
        title: title ?? (n === 1 ? `${steps[0].service} 改为用 MCPcell 这份` : `${n} 个 MCP 改为用 MCPcell 这份`),
        intro: steps.every((s) => s.mode === 'shared')
          ? '改由 MCPcell 打开：所有对话共用一份，同一个 MCP 全机只开一份。工具名不变，随时可以改回原样。'
          : '改由 MCPcell 打开，工具名不变，随时可以改回原样。标着「每个对话一份」的，仍然每个对话单独一份，但用到时才打开、对话结束就关掉。',
        groups: byAgent(steps).map((g) => ({
          agent: g.agent,
          lines: g.steps.map((s) => (
            <>
              <span className="font-medium text-zinc-800 dark:text-zinc-200">{s.entry}</span>
              <span className="text-zinc-400">
                （{s.isNew ? '新加 · ' : ''}
                {s.mode === 'shared' ? '共用一份' : '每个对话一份'}）
              </span>
            </>
          )),
        })),
        warn: steps.some((s) => s.force)
          ? '你让没确认过的 MCP 共用一份。如果它为不同对话记着各自的东西（浏览器页面、连接、登录），共用后会互相干扰；出问题时改成每个对话一份，或改回原样。'
          : undefined,
        confirm: '确定',
        run: () => applyTakeover(steps),
      });
    },
    restore(steps: RestoreStep[], title?: string) {
      if (!steps.length) return;
      setPending({
        title: title ?? (steps.length === 1 ? (steps[0].added ? `从 ${steps[0].agent.name} 去掉 ${steps[0].entry}` : `${steps[0].entry} 改回原样`) : `${steps.length} 项改回原样`),
        intro: '恢复成交给 MCPcell 之前的配置（一个字节都不差），之后应用每开一个对话就自己再开一份。之后被你改过的项不会被覆盖。',
        groups: byAgent(steps).map((g) => ({
          agent: g.agent,
          lines: g.steps.map((s) => (
            <>
              <span className="font-medium text-zinc-800 dark:text-zinc-200">{s.entry}</span>
              <span className="text-zinc-400">{s.added ? ' · 是 MCPcell 加的，会去掉' : ' · 改回应用自己开'}</span>
            </>
          )),
        })),
        note: '这一项恢复成原来的样子，其他内容不动，改之前先备份整个文件。',
        confirm: '改回原样',
        danger: true,
        run: () => applyRestore(steps),
      });
    },
    forget(agent: AgentSummary, entry: string) {
      setPending({
        title: `不再管 ${entry}`,
        intro: '这一项后来在应用里被改过。保留你现在的配置，只删掉 MCPcell 的记录。',
        groups: [{ agent, lines: [entry] }],
        note: '配置文件不改动。',
        confirm: '不再管它',
        run: async () => {
          const { results } = await takeover.restore(agent.id, [entry], true);
          if (!results[0]?.ok) throw new Error(results[0]?.error ?? '失败');
          return '已删掉记录';
        },
      });
    },
    add(agent: AgentSummary, row: McpRow) {
      const service = row.service ?? row.proposed ?? row.name;
      setPending({
        title: `把 ${row.name} 加到 ${agent.name}`,
        intro: row.service ? undefined : `会先在 MCPcell 里建好 ${service}，设置照搬其他应用里的同一个 MCP。`,
        groups: [
          {
            agent,
            lines: [
              <>
                新加 <span className="font-medium text-zinc-800 dark:text-zinc-200">{service}</span>
                <span className="text-zinc-400">，用 MCPcell 这份</span>
              </>,
            ],
          },
        ],
        note: '新加一项，其他内容不动，改之前先备份整个文件。',
        confirm: '加上',
        run: async () => (await unified.add(agent.id, row.key), `已把 ${row.name} 加到 ${agent.name}。${shortHint[agent.id] ?? ''}`),
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
              <AgentChangeCard key={g.agent.id} agent={g.agent} lines={g.lines} note={pending.note} />
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
export function AgentChangeCard({ agent, lines, note }: { agent: AgentSummary; lines: ReactNode[]; note?: string }) {
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
      <div className="mt-2 text-xs text-zinc-400">
        {note ?? '原来的命令保留，前面加上 mcpcell-run；其他内容不动，改之前先备份整个文件。'}
        {restartHint[agent.id]}
      </div>
    </div>
  );
}

export function AutostartOption({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="mt-4 flex cursor-pointer items-center gap-2.5 text-sm">
      <Checkbox checked={checked} onChange={onChange} />
      同时开启开机自启
      <span className="text-zinc-400">改完后应用要靠 MCPcell 在后台开着</span>
    </label>
  );
}
