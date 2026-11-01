import { useEffect, useRef, useState } from 'react';
import { Check, CircleMinus, LoaderCircle, TriangleAlert } from 'lucide-react';
import { autoApi, takeover, unified } from './api';
import { agentBrand, appliesTo, appOf, byAgent, candidates, type TakeStep } from './agents';
import { firstLine } from './format';
import { BrandMark } from './icons';
import { agentOrder } from './model';
import { agentName } from './timeline';
import { cx } from './ui';

// 一键托管 where the user can watch it: each app's entries are taken over
// (one request per app; the daemon tries every MCP before rewriting the
// entry), then, when every app keeps the same set, each app gets the MCPs
// it lacks (one sync per app). The guide and the home page show it.

type TaskState = 'wait' | 'run' | 'ok' | 'partial' | 'fail';

interface Task {
  id: string;
  phase: 'take' | 'sync';
  agent: string;
  name: string;
  what: string; // the MCPs, for a takeover
  total: number;
  ok: number;
  failed: string[];
  state: TaskState;
}

export interface RunSummary {
  mcps: number; // MCPs taken over in at least one app
  entries: number;
  added: number; // entries synced into other apps
  ignored: string[]; // what could not be taken over or added
  applies: string;
}

const rank = (id: string) => {
  const i = agentOrder.indexOf(appOf(id));
  return i < 0 ? agentOrder.length : i;
};

export function useTakeoverRun() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [summary, setSummary] = useState<RunSummary | null>(null);
  const running = tasks.length > 0 && !summary;
  const update = (id: string, patch: Partial<Task>) => setTasks((ts) => ts.map((t) => (t.id === id ? { ...t, ...patch } : t)));

  /** Takes list over, then syncs (only into apps, when given). */
  async function run(list: ReturnType<typeof candidates>, sync: boolean, apps?: string[]): Promise<RunSummary> {
    setSummary(null);
    const steps = list.flatMap((c) => c.steps);
    const groups = byAgent(steps).sort((a, b) => rank(a.agent.id) - rank(b.agent.id));
    setTasks(
      groups.map((g) => ({
        id: `take:${g.agent.id}`,
        phase: 'take',
        agent: g.agent.id,
        name: g.agent.name,
        what: g.steps.map((s) => s.entry).join('、'),
        total: g.steps.length,
        ok: 0,
        failed: [],
        state: 'wait',
      })),
    );
    const failedSteps: TakeStep[] = [];
    const ignored: string[] = [];
    let entries = 0;
    for (const g of groups) {
      const id = `take:${g.agent.id}`;
      update(id, { state: 'run' });
      try {
        const { results } = await takeover.attach(g.agent.id, g.steps.map((s) => s.entry));
        const bad = results.filter((r) => !r.ok);
        entries += results.length - bad.length;
        for (const r of bad) failedSteps.push(...g.steps.filter((s) => s.entry === r.name));
        ignored.push(...bad.map((r) => `${g.agent.name} ${r.name}：${r.error}`));
        update(id, { ok: results.length - bad.length, failed: bad.map((r) => `${r.name}：${firstLine(r.error ?? '')}`), state: bad.length ? (bad.length < results.length ? 'partial' : 'fail') : 'ok' });
      } catch (e) {
        failedSteps.push(...g.steps);
        ignored.push(`${g.agent.name}：${firstLine((e as Error).message)}`);
        update(id, { failed: [firstLine((e as Error).message)], state: 'fail' });
      }
    }
    const mcps = list.filter((c) => c.steps.some((s) => !failedSteps.includes(s))).length;

    let added = 0;
    const touched = groups.filter((g) => g.steps.some((s) => !failedSteps.includes(s))).map((g) => ({ agent: g.agent }));
    if (sync) {
      // Where each MCP goes is known once the takeovers are in.
      const fresh = await unified.load();
      const per = new Map<string, number>();
      for (const r of fresh.rows) for (const id of r.syncTo ?? []) if (!apps || apps.includes(id)) per.set(id, (per.get(id) ?? 0) + 1);
      const names = new Map(fresh.agents.map((a) => [a.id, a]));
      const todo = [...per].sort((a, b) => rank(a[0]) - rank(b[0]));
      setTasks((ts) => [
        ...ts,
        ...todo.map(([id, n]) => ({ id: `sync:${id}`, phase: 'sync' as const, agent: id, name: names.get(id)?.name ?? agentName(id), what: '', total: n, ok: 0, failed: [], state: 'wait' as const })),
      ]);
      for (const [id] of todo) {
        update(`sync:${id}`, { state: 'run' });
        try {
          const { results } = await autoApi.sync([id]);
          const bad = results.filter((r) => !r.ok);
          added += results.length - bad.length;
          ignored.push(...bad.map((r) => `${names.get(id)?.name ?? id} ${r.service}：${r.error}`));
          if (results.length > bad.length && names.get(id)) touched.push({ agent: names.get(id)! });
          update(`sync:${id}`, {
            ok: results.length - bad.length,
            what: results.filter((r) => r.ok).map((r) => r.service).join('、'),
            failed: bad.map((r) => `${r.service}：${firstLine(r.error ?? '')}`),
            state: bad.length ? (bad.length < results.length ? 'partial' : 'fail') : 'ok',
          });
        } catch (e) {
          update(`sync:${id}`, { failed: [firstLine((e as Error).message)], state: 'fail' });
        }
      }
    }
    const s = { mcps, entries, added, ignored, applies: appliesTo(touched) };
    setSummary(s);
    return s;
  }

  return { tasks, summary, running, run };
}

export type TakeoverRun = ReturnType<typeof useTakeoverRun>;

/** A finished run in a title and a line. */
export function summaryText(s: RunSummary): { title: string; line: string } {
  const title = s.mcps ? `已托管 ${s.mcps} 个 MCP` : s.added ? '已同步' : s.ignored.length ? '没有托管' : '没有需要托管的';
  const line = [s.entries && `${s.entries} 项配置`, s.added && `同步到其他应用 ${s.added} 项`, s.ignored.length && `跳过 ${s.ignored.length} 项`, (s.entries || s.added) && s.applies]
    .filter(Boolean)
    .join(' · ');
  return { title, line };
}

/** The run as it goes: a bar, then a line per app and step. */
export function RunView({ run }: { run: TakeoverRun }) {
  const { tasks, summary } = run;
  const finished = tasks.filter((t) => t.state !== 'wait' && t.state !== 'run').length;
  const take = tasks.filter((t) => t.phase === 'take');
  const sync = tasks.filter((t) => t.phase === 'sync');
  const pct = summary ? 100 : tasks.length ? Math.round((finished / tasks.length) * 100) : 0;
  return (
    <div>
      <div className="h-1.5 overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
        <div className="h-full rounded-full bg-blue-600 transition-[width] duration-500 dark:bg-blue-500" style={{ width: `${pct}%` }} />
      </div>
      {take.length > 0 && <Group title="托管" tasks={take} />}
      {sync.length > 0 && <Group title="同步到其他应用" tasks={sync} />}
    </div>
  );
}

function Group({ title, tasks }: { title: string; tasks: Task[] }) {
  return (
    <section className="mt-4">
      <h3 className="mb-1.5 text-xs text-zinc-500">{title}</h3>
      <div className="divide-y divide-zinc-100 rounded-xl border border-zinc-200 bg-white dark:divide-zinc-800 dark:border-zinc-800 dark:bg-zinc-900">
        {tasks.map((t) => (
          <TaskRow key={t.id} task={t} />
        ))}
      </div>
    </section>
  );
}

/** One app's step; the one under way scrolls into view. */
function TaskRow({ task: t }: { task: Task }) {
  const row = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (t.state === 'run') row.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [t.state]);
  return (
    <div ref={row} className="flex items-center gap-3 px-3.5 py-2">
      <BrandMark brand={agentBrand[appOf(t.agent)] ?? ''} className={cx('size-4 shrink-0', t.state === 'wait' && 'opacity-40 grayscale')} />
      <div className="min-w-0 flex-1">
        <div className={cx('truncate text-[13px] font-medium', t.state === 'wait' && 'text-zinc-400')}>{t.name}</div>
        {t.what && <div className="truncate text-xs text-zinc-400">{t.what}</div>}
      </div>
      <State task={t} />
    </div>
  );
}

function State({ task: t }: { task: Task }) {
  const tip = t.failed.length ? t.failed.join('\n') : undefined;
  switch (t.state) {
    case 'wait':
      return <span className="shrink-0 text-xs text-zinc-400 tabular-nums">{t.total} 项</span>;
    case 'run':
      return (
        <span className="flex shrink-0 items-center gap-1.5 text-xs text-blue-600 dark:text-blue-400">
          <LoaderCircle className="size-3.5 animate-spin" />
          {t.phase === 'take' ? '试启动并改写' : '添加中'}
        </span>
      );
    case 'ok':
      return (
        <span className="flex shrink-0 items-center gap-1 text-xs text-emerald-600 tabular-nums dark:text-emerald-400">
          <Check className="size-3.5" />
          {t.ok} 项
        </span>
      );
    case 'partial':
      return (
        <span title={tip} className="flex shrink-0 items-center gap-1 text-xs text-amber-600 tabular-nums dark:text-amber-400">
          <TriangleAlert className="size-3.5" />
          {t.ok} 项 · 跳过 {t.failed.length}
        </span>
      );
  }
  return (
    <span title={tip} className="flex shrink-0 items-center gap-1 text-xs text-zinc-400">
      <CircleMinus className="size-3.5" />
      已跳过
    </span>
  );
}
