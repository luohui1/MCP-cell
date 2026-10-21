import { useEffect, useState } from 'react';
import { Check, TriangleAlert } from 'lucide-react';
import { skillsApi, SOURCE, type SkillAction, type SkillCopy, type SkillDiff, type SkillInfo, type SkillLocation, type SkillResult, type SkillStatus } from './api';
import { Badge, Button, Checkbox, cx, Modal, useToast } from './ui';

// Skill changes always go through a plan: the daemon lists what it would do
// (dry run), the dialog shows it, and only the confirm button applies it.

export const short: Record<string, string> = {
  claude: 'Claude',
  agents: '.agents',
  codex: 'Codex',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  'antigravity-ide': 'AG IDE',
  'antigravity-cli': 'AG CLI',
  cursor: 'Cursor',
  opencode: 'OpenCode',
  copilot: 'Copilot',
  [SOURCE]: '来源目录',
};

export const label = (id: string) => short[id] ?? id;

function message(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}

export function diffText(d?: SkillDiff): string {
  if (!d) return '';
  const part = (name: string, list?: string[]) =>
    list?.length ? `${name} ${list.slice(0, 3).join('、')}${list.length > 3 ? ` 等 ${list.length} 个` : ''}` : '';
  return [part('改动', d.changed), part('多出', d.added), part('缺少', d.removed)].filter(Boolean).join('；');
}

export function when(t: string) {
  if (!t || t.startsWith('0001')) return '—';
  return new Date(t).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
}

// ---- plans -----------------------------------------------------------------

function actionText(a: SkillAction): string {
  const link = a.copy ? '副本' : '链接';
  switch (a.do) {
    case 'import':
      return `放进来源目录（取自 ${label(a.from ?? '')}）`;
    case 'update-source':
      return `用 ${label(a.from ?? '')} 的版本替换来源目录里的版本，旧版本先备份`;
    case 'link':
      return `新建${link}`;
    case 'replace':
      return a.diff ? `备份这一份不同的版本，改为${link}` : `备份后改为${link}`;
    case 'update':
      return '更新 MCPcell 的副本';
    case 'ok':
      return '已是最新';
    case 'restore':
      return '还原';
    case 'conflict':
      return (a.variant ? `版本 ${a.variant}，` : '') + (a.reason || '内容不同，没有改动');
  }
  return `跳过：${a.reason ?? ''}`;
}

function ActionList({ actions, ran }: { actions: SkillAction[]; ran: boolean }) {
  return (
    <div className="max-h-[320px] divide-y divide-zinc-100 overflow-y-auto rounded-xl border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
      {actions.map((a, i) => {
        const quiet = a.do === 'ok' || a.do === 'skip';
        const warn = a.do === 'conflict';
        const diff = diffText(a.diff);
        return (
          <div key={i} className="grid grid-cols-[18px_minmax(0,150px)_80px_minmax(0,1fr)] items-start gap-2 px-3 py-2 text-[13px]">
            <span className="pt-0.5">
              {ran && a.error ? (
                <TriangleAlert className="size-3.5 text-red-600" />
              ) : ran && a.done ? (
                <Check className="size-3.5 text-emerald-600" />
              ) : warn ? (
                <TriangleAlert className="size-3.5 text-amber-500" />
              ) : null}
            </span>
            <span className="truncate font-medium" title={a.skill}>
              {a.skill}
            </span>
            <span className="truncate text-zinc-500">{a.location ? label(a.location) : '—'}</span>
            <div className={cx('min-w-0', quiet && 'text-zinc-400', warn && 'text-amber-700 dark:text-amber-300')}>
              <div>{actionText(a)}</div>
              {diff && a.do !== 'update-source' && (
                <div className="mt-0.5 text-xs text-zinc-500">
                  {a.match === 'newer' ? '这一份较新：' : a.match === 'older' ? '这一份较旧：' : ''}
                  {diff}
                </div>
              )}
              {a.error && <div className="mt-0.5 text-xs text-red-600">{a.error}</div>}
            </div>
          </div>
        );
      })}
    </div>
  );
}

const changes = (r?: SkillResult | null) => r?.actions.filter((a) => !['ok', 'skip', 'conflict'].includes(a.do)).length ?? 0;

function usePlan(plan: () => Promise<SkillResult>, deps: unknown[]) {
  const [result, setResult] = useState<SkillResult | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let live = true;
    setResult(null);
    plan()
      .then((r) => live && (setResult(r), setError('')))
      .catch((e) => live && setError(message(e)));
    return () => {
      live = false;
    };
  }, deps);
  return { result, error };
}

function useRun(run: () => Promise<SkillResult>, verb: string, onDone: () => void) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [ran, setRan] = useState<SkillResult | null>(null);
  const go = async () => {
    setBusy(true);
    try {
      const r = await run();
      const failed = r.actions.filter((a) => a.error).length;
      if (failed) {
        setRan(r);
        toast(`${failed} 项没有完成`, 'error');
      } else {
        toast(`已${verb} ${changes(r)} 项`);
        onDone();
      }
    } catch (e) {
      toast(message(e), 'error');
    } finally {
      setBusy(false);
    }
  };
  return { busy, ran, go };
}

export function PlanDialog({
  title,
  intro,
  plan,
  run,
  verb,
  danger,
  onClose,
  onDone,
}: {
  title: string;
  intro: string;
  plan: () => Promise<SkillResult>;
  run: () => Promise<SkillResult>;
  verb: string;
  danger?: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const { result, error } = usePlan(plan, []);
  const { busy, ran, go } = useRun(run, verb, onDone);
  const n = changes(result);
  const shown = ran ?? result;
  return (
    <Modal open onClose={onClose} width={640}>
      <h2 className="text-base font-semibold">{title}</h2>
      <p className="mt-1.5 text-sm leading-6 text-zinc-500">{intro}</p>
      <div className="mt-4">
        {error && <p className="text-sm text-red-600">{error}</p>}
        {!shown && !error && <p className="py-6 text-center text-sm text-zinc-400">正在生成计划…</p>}
        {shown && (shown.actions.length ? <ActionList actions={shown.actions} ran={!!ran} /> : <p className="text-sm text-zinc-400">没有需要处理的。</p>)}
      </div>
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={ran ? onDone : onClose}>
          {ran ? '完成' : '取消'}
        </Button>
        {!ran && (
          <Button variant={danger ? 'danger' : 'primary'} busy={busy} disabled={n === 0} onClick={go}>
            {verb} {n} 项
          </Button>
        )}
      </div>
    </Modal>
  );
}

/** Sync one skill: pick the version that wins (when copies differ) and the
 * locations that should link to it. */
export function SyncDialog({ skill, data, onClose, onDone }: { skill: SkillInfo; data: SkillStatus; onClose: () => void; onDone: () => void }) {
  const has = (id: string) => skill.copies.find((c) => c.location === id);
  const versions = variantsOf(skill);
  const needChoice = skill.state === 'differs';
  const [from, setFrom] = useState<string>(needChoice ? (versions[0]?.from ?? '') : '');
  const installed = data.locations.filter((l) => l.installed);
  const [to, setTo] = useState<string[]>(() => installed.filter((l) => has(l.id) && has(l.id)!.kind !== 'link' && has(l.id)!.kind !== 'broken').map((l) => l.id));
  const toggle = (id: string, on: boolean) => setTo((t) => (on ? [...t, id] : t.filter((x) => x !== id)));
  const req = { names: [skill.name], to, from: from || undefined };
  const { result, error } = usePlan(() => skillsApi.sync({ ...req, dryRun: true }), [from, to.join(',')]);
  const { busy, ran, go } = useRun(() => skillsApi.sync(req), '完成', onDone);
  const n = changes(result);
  const shown = ran ?? result;

  return (
    <Modal open onClose={onClose} width={720}>
      <h2 className="text-base font-semibold">
        {needChoice ? `选择「${skill.name}」的版本` : skill.state === 'same' ? `合并「${skill.name}」` : `把「${skill.name}」添加到其他 Agent`}
      </h2>
      <p className="mt-1.5 text-sm leading-6 text-zinc-500">
        来源目录只存一份，选中的位置换成指向它的目录链接；被替换的副本先移到备份，随时可以逐字节还原。
      </p>

      {needChoice && (
        <section className="mt-5">
          <h3 className="mb-2 text-[13px] font-medium text-zinc-500">以哪一份为准</h3>
          <div className="space-y-1.5">
            {versions.map((v) => (
              <label
                key={v.from}
                className={cx(
                  'flex cursor-pointer items-start gap-3 rounded-xl border px-3.5 py-2.5 text-sm transition-colors',
                  from === v.from ? 'border-blue-500 bg-blue-50/60 dark:bg-blue-950/30' : 'border-zinc-200 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800/40',
                )}
              >
                <input type="radio" className="mt-1" checked={from === v.from} onChange={() => setFrom(v.from)} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">版本 {v.variant}</span>
                    <span className="text-zinc-500">{v.where.map(label).join('、')}</span>
                    {v.reference && <Badge tone="blue">{skill.reference === SOURCE ? '来源目录现在的版本' : '最近修改'}</Badge>}
                  </div>
                  <div className="mt-0.5 text-xs text-zinc-500">
                    {v.files} 个文件 · 修改于 {when(v.time)}
                    {v.diff && ` · 与${skill.reference === SOURCE ? '来源目录' : '最近修改的一份'}相比：${v.diff}`}
                  </div>
                </div>
              </label>
            ))}
          </div>
          <p className="mt-2 text-xs leading-5 text-zinc-400">其他版本的副本会先移到备份，再换成链接；还原时会原样放回。</p>
        </section>
      )}

      <section className="mt-5">
        <h3 className="mb-2 text-[13px] font-medium text-zinc-500">同步到</h3>
        <div className="grid grid-cols-2 gap-1.5">
          {installed.map((l) => (
            <LocationOption key={l.id} loc={l} copy={has(l.id)} checked={to.includes(l.id)} onChange={(on) => toggle(l.id, on)} />
          ))}
        </div>
      </section>

      <section className="mt-5">
        <h3 className="mb-2 text-[13px] font-medium text-zinc-500">{ran ? '结果' : '将要执行'}</h3>
        {error && <p className="text-sm text-red-600">{error}</p>}
        {!shown && !error && <p className="py-4 text-center text-sm text-zinc-400">正在生成计划…</p>}
        {shown && <ActionList actions={shown.actions} ran={!!ran} />}
      </section>

      <div className="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onClick={ran ? onDone : onClose}>
          {ran ? '完成' : '取消'}
        </Button>
        {!ran && (
          <Button variant="primary" busy={busy} disabled={n === 0} onClick={go}>
            执行 {n} 项
          </Button>
        )}
      </div>
    </Modal>
  );
}

function LocationOption({ loc, copy, checked, onChange }: { loc: SkillLocation; copy?: SkillCopy; checked: boolean; onChange: (v: boolean) => void }) {
  const note = !copy
    ? '还没有'
    : copy.kind === 'link'
      ? '链接到其他工具的目录；勾选会改为 MCPcell 的版本'
      : copy.kind === 'linked'
        ? '已链接'
        : '有一份副本';
  return (
    <div
      onClick={() => onChange(!checked)}
      className="flex cursor-pointer items-start gap-2.5 rounded-lg border border-zinc-200 px-3 py-2 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800/40"
      title={`${loc.path}\n读取它的 Agent：${loc.agents.join('、')}`}
    >
      <span className="pt-0.5">
        <Checkbox checked={checked} onChange={onChange} />
      </span>
      <div className="min-w-0">
        <div className="text-[13px] font-medium">{loc.name}</div>
        <div className="truncate text-xs text-zinc-400">
          {note} · {loc.agents.join('、')}
        </div>
      </div>
    </div>
  );
}

/** The distinct versions of a skill, most recently modified first. */
function variantsOf(skill: SkillInfo) {
  const out: { variant: string; from: string; where: string[]; files: number; time: string; diff: string; reference: boolean }[] = [];
  for (const c of skill.copies) {
    if (!c.variant || c.kind === 'broken' || c.error) continue;
    const v = out.find((x) => x.variant === c.variant);
    if (v) {
      v.where.push(c.location);
      v.reference ||= c.location === skill.reference;
      continue;
    }
    out.push({
      variant: c.variant,
      from: c.location,
      where: [c.location],
      files: c.files,
      time: c.newest,
      diff: diffText(c.diff),
      reference: c.location === skill.reference,
    });
  }
  return out.sort((a, b) => b.time.localeCompare(a.time));
}
