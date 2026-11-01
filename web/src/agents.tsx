import { useState, type ReactNode } from 'react';
import { TriangleAlert } from 'lucide-react';
import { autoApi, takeover, unified, type AgentCell, type AgentSummary, type McpRow } from './api';
import { useStatus, type Unified } from './data';
import { firstLine } from './format';
import { BrandMark } from './icons';
import { agentOrder } from './model';
import { Button, Modal, useToast } from './ui';

export const agentBrand: Record<string, string> = {
  'claude-code': 'claude',
  codex: 'openai',
  cursor: 'cursor',
  'claude-desktop': 'claude',
  vscode: 'vscode',
  copilot: 'copilot',
  gemini: 'gemini',
  antigravity: 'gemini',
  windsurf: 'windsurf',
  kiro: 'kiro',
  cline: 'cline',
  opencode: 'opencode',
};
/** When a change to an app's configuration takes effect. */
const applies: Record<string, string> = {
  'claude-code': 'Claude Code 新对话生效',
  codex: 'Codex 重启后生效',
  cursor: 'Cursor 刷新 MCP 后生效',
};

/** The app an agent id belongs to: a Claude Code project
 * ("claude-code@<dir>") belongs to Claude Code. */
export const appOf = (id: string) => id.split('@')[0];
export const isProject = (id: string) => id.includes('@');

/** Agents with a configuration file, in display order: each app, then the
 * projects that have servers of their own. */
export const agentsOf = (all: AgentSummary[]) =>
  agentOrder.flatMap((app) =>
    all.filter((a) => a.found && appOf(a.id) === app).sort((x, y) => Number(isProject(x.id)) - Number(isProject(y.id)) || x.id.localeCompare(y.id)),
  );

/** The apps among agents, for filtering by app. */
export const appsOf = (agents: AgentSummary[]) => [...new Set(agents.map((a) => appOf(a.id)))];

export const brandOfAgent = (id: string) => agentBrand[appOf(id)] ?? '';

/** A native entry MCPcell can run: local stdio with an environment it can
 * reproduce (the daemon decides), and not one that cannot start. */
export const canTake = (c?: AgentCell) => !!c && c.state === 'native' && !!c.takeable && !c.dead;

export interface TakeStep {
  agent: AgentSummary;
  entry: string;
}
export interface RestoreStep {
  agent: AgentSummary;
  entry: string;
  added?: boolean;
}

/** Steps that take over every eligible cell of a row (optionally of one
 * app). The daemon picks how each runs: known-stateless servers shared, the
 * rest one instance per conversation. */
export function takeSteps(r: McpRow, agents: AgentSummary[], only?: string): TakeStep[] {
  return agents.flatMap((a) => {
    const c = r.agents[a.id];
    return (only && appOf(a.id) !== only) || !canTake(c) ? [] : [{ agent: a, entry: c.entry }];
  });
}

export function restoreSteps(r: McpRow, agents: AgentSummary[]): RestoreStep[] {
  return agents.flatMap((a) => {
    const c = r.agents[a.id];
    return c?.state === 'managed' ? [{ agent: a, entry: c.entry, added: c.added }] : [];
  });
}

/** How many additions a sync would make now (optionally to one app's agents). */
export const pendingSync = (data: Unified, only?: string) =>
  data.rows.reduce((n, r) => n + (r.syncTo ?? []).filter((id) => !only || appOf(id) === only).length, 0);

/** Takes over, then, when every app keeps the same set, adds the MCPs to
 * the apps that lack them (only to apps, when given). */
export async function takeAndSync(steps: TakeStep[], sync: boolean, apps?: string[]) {
  const taken = steps.length ? await attachAll(steps) : { ok: 0, failed: [] as string[], failedSteps: [] as TakeStep[] };
  const added = sync ? (await autoApi.sync(apps)).results.filter((r) => r.ok).length : 0;
  return { ...taken, added };
}

/** Rows 一键托管 takes over, with their steps (optionally for one app). */
export function candidates(data: Unified, only?: string) {
  const agents = data.agents.filter((a) => a.found);
  return data.rows.map((row) => ({ row, steps: takeSteps(row, agents, only) })).filter((c) => c.steps.length > 0);
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

/** When the changes take effect, in one phrase: Claude Code's new
 * conversations pick them up, the other apps once restarted. */
export function appliesTo(steps: { agent: AgentSummary }[]): string {
  const apps = [...new Map(steps.map((s) => [appOf(s.agent.id), s.agent])).entries()];
  const claude = apps.some(([app]) => app === 'claude-code');
  const others = apps.filter(([app]) => app !== 'claude-code');
  if (!others.length) return claude ? applies['claude-code'] : '';
  if (others.length === 1 && !claude) return applies[others[0][0]] ?? `${others[0][1].name} 重启后生效`;
  return claude ? `${applies['claude-code']}，其他应用重启后生效` : '重启应用后生效';
}

/** Takes over steps agent by agent: how many were, each failure, and the
 * steps that failed. */
export async function attachAll(steps: TakeStep[]): Promise<{ ok: number; failed: string[]; failedSteps: TakeStep[] }> {
  const failed: string[] = [];
  const failedSteps: TakeStep[] = [];
  let ok = 0;
  for (const g of byAgent(steps)) {
    const { results } = await takeover.attach(g.agent.id, g.steps.map((s) => s.entry));
    for (const r of results) {
      if (r.ok) {
        ok++;
        continue;
      }
      failed.push(`${g.agent.name} ${r.name}：${r.error}`);
      failedSteps.push(...g.steps.filter((s) => s.entry === r.name));
    }
  }
  return { ok, failed, failedSteps };
}

/** Applies takeover steps; throws only when none worked. The daemon
 * remembers the ones that did not start, which are left out from then on. */
async function applyTakeover(steps: TakeStep[]): Promise<string> {
  const { ok, failed } = await attachAll(steps);
  if (!ok && failed.length) throw new Error(`托管失败：${failed[0]}`);
  return `已托管 ${ok} 个，${appliesTo(steps)}${failed.length ? `；${failed.length} 项无法托管，已忽略` : ''}`;
}

async function applyRestore(steps: RestoreStep[]): Promise<string> {
  const failed: string[] = [];
  let ok = 0;
  for (const g of byAgent(steps)) {
    const { results } = await takeover.restore(g.agent.id, g.steps.map((s) => s.entry));
    for (const r of results) r.ok ? ok++ : failed.push(`${g.agent.name} ${r.name}：${r.error}`);
  }
  if (failed.length) throw new Error(`${ok ? `已还原 ${ok} 个，` : ''}${failed.length} 个失败：${failed[0]}`);
  return `已还原 ${ok} 个，${appliesTo(steps)}`;
}

interface Pending {
  title: string;
  intro?: ReactNode;
  groups: { agent: AgentSummary; lines: ReactNode[] }[];
  /** What happens to each agent's file. */
  note?: string;
  confirm: string;
  danger?: boolean;
  run: () => Promise<string>;
}

/** Changes to agent configurations. Taking over, restoring one MCP and
 * adding one run at once, since each is as easily undone; restoring many
 * and forgetting a record ask first. */
export function useAgentActions(reload: () => Promise<void>) {
  const { refresh } = useStatus();
  const toast = useToast();
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);

  async function apply(work: () => Promise<string>) {
    if (busy) return;
    setBusy(true);
    try {
      toast(await work());
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy(false);
      setPending(null);
      await Promise.all([reload(), refresh()]);
    }
  }

  const actions = {
    busy,
    takeover(steps: TakeStep[]) {
      if (steps.length) apply(() => applyTakeover(steps));
    },
    /** 一键托管: take everything over, and give every app the same set. */
    oneClick(steps: TakeStep[], sync: boolean, apps?: string[]) {
      apply(async () => {
        const { ok, failed, added } = await takeAndSync(steps, sync, apps);
        if (!ok && !added && failed.length) throw new Error(`托管失败：${failed[0]}`);
        const done = [ok && `已托管 ${ok} 个`, added && `同步到其他应用 ${added} 项`].filter(Boolean).join('，') || '没有需要托管的';
        const when = added ? `${applies['claude-code']}，其他应用重启后生效` : appliesTo(steps);
        return `${done}${when ? `，${when}` : ''}${failed.length ? `；${failed.length} 项无法托管，已忽略` : ''}`;
      });
    },
    restore(steps: RestoreStep[]) {
      if (steps.length) apply(() => applyRestore(steps));
    },
    /** Restores many after a confirmation. */
    restoreAll(steps: RestoreStep[], title: string) {
      if (!steps.length) return;
      setPending({
        title,
        groups: byAgent(steps).map((g) => ({ agent: g.agent, lines: g.steps.map((s) => (s.added ? `${s.entry}（移除）` : s.entry)) })),
        note: '先备份配置文件',
        confirm: '还原',
        danger: true,
        run: () => applyRestore(steps),
      });
    },
    forget(agent: AgentSummary, entry: string) {
      setPending({
        title: `忽略 ${entry}`,
        intro: '保留当前配置，只删除托管记录',
        groups: [{ agent, lines: [entry] }],
        confirm: '忽略',
        run: async () => {
          const { results } = await takeover.restore(agent.id, [entry], true);
          if (!results[0]?.ok) throw new Error(results[0]?.error ?? '失败');
          return '已忽略';
        },
      });
    },
    add(agent: AgentSummary, row: McpRow) {
      apply(async () => (await unified.add(agent.id, row.key), `已添加到 ${agent.name}，${applies[appOf(agent.id)] ?? '重启后生效'}`));
    },
  };

  const dialog = (
    <Modal open={pending !== null} onClose={() => !busy && setPending(null)} width={460}>
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
              {pending.intro && <p className="mt-1 text-sm text-zinc-500">{pending.intro}</p>}
            </div>
          </div>
          <div className="mt-4 space-y-2.5">
            {pending.groups.map((g) => (
              <AgentChangeCard key={g.agent.id} agent={g.agent} lines={g.lines} note={pending.note} />
            ))}
          </div>
          <div className="mt-6 flex justify-end gap-2">
            <Button disabled={busy} onClick={() => setPending(null)}>
              取消
            </Button>
            <Button variant={pending.danger ? 'danger' : 'primary'} busy={busy} onClick={() => apply(pending.run)}>
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
function AgentChangeCard({ agent, lines, note }: { agent: AgentSummary; lines: ReactNode[]; note?: string }) {
  return (
    <div className="rounded-xl border border-zinc-200 px-4 py-3 dark:border-zinc-700">
      <div className="flex items-center gap-2">
        <BrandMark brand={brandOfAgent(agent.id)} className="size-4" />
        <span className="shrink-0 text-sm font-medium">{agent.name}</span>
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
      {note && (
        <div className="mt-2 text-xs text-zinc-400">
          {note}，{applies[appOf(agent.id)] ?? '重启后生效'}
        </div>
      )}
    </div>
  );
}
