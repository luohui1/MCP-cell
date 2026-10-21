import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Check, ExternalLink, FolderSync, Link2, Plus, Puzzle, RefreshCw, TriangleAlert, Undo2 } from 'lucide-react';
import {
  skillsApi,
  SOURCE,
  type SkillAction,
  type SkillCopy,
  type SkillDiff,
  type SkillInfo,
  type SkillLocation,
  type SkillResult,
  type SkillState,
  type SkillStatus,
} from '../api';
import { BrandMark } from '../icons';
import { Badge, Button, Card, Checkbox, cx, Dot, GroupHeader, Menu, Modal, PageHeader, Stat, TableCard, useToast } from '../ui';

// The Skill page: where each agent skill is, whether the copies agree, and
// syncing them to one copy in MCPcell's source directory (`mcpcell skills`).

const groups: { id: string; title: string; hint: string; states: SkillState[]; collapsed?: boolean }[] = [
  { id: 'differs', title: '内容不一致', hint: '同名 Skill 在各处的内容不同：选一份为准后统一', states: ['differs'] },
  { id: 'same', title: '可以统一', hint: '各处是相同的独立副本：统一后只保留一份，各 Agent 链接到它', states: ['same'] },
  { id: 'synced', title: '已统一', hint: '各 Agent 的目录链接到 MCPcell 的来源目录，改一处处处生效', states: ['synced', 'source'] },
  { id: 'single', title: '只在一处', hint: '统一后可以再添加到其他 Agent', states: ['single'], collapsed: true },
  { id: 'external', title: '其他工具管理', hint: '各处都链接到同一个外部目录，已经是一份', states: ['external'], collapsed: true },
];

const grid = 'grid grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_112px_112px] items-center gap-4';

const short: Record<string, string> = {
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

const brand: Record<string, string> = {
  claude: 'claude',
  codex: 'openai',
  gemini: 'gemini',
  antigravity: 'gemini',
  'antigravity-ide': 'gemini',
  'antigravity-cli': 'gemini',
  cursor: 'cursor',
  opencode: 'opencode',
  copilot: 'copilot',
};

const label = (id: string) => short[id] ?? id;

function message(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}

export function SkillsPage() {
  const [data, setData] = useState<SkillStatus | null>(null);
  const [error, setError] = useState('');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [rowOpen, setRowOpen] = useState<string | null>(null);
  const [dialog, setDialog] = useState<ReactNode>(null);
  const reload = useCallback(async () => {
    try {
      setData(await skillsApi.load());
      setError('');
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => {
    reload();
  }, [reload]);
  const close = useCallback(() => setDialog(null), []);
  const done = useCallback(() => {
    setDialog(null);
    reload();
  }, [reload]);

  const count = (s: SkillState) => data?.skills.filter((k) => k.state === s).length ?? 0;
  const same = data?.skills.filter((k) => k.state === 'same') ?? [];
  const managed = data?.skills.some((k) => k.copies.some((c) => c.managed)) ?? false;

  const header = (
    <PageHeader
      icon={<Puzzle className="size-5 text-zinc-500" />}
      title="Skill"
      subtitle={data && `${data.skills.length} 个，来自 ${data.locations.filter((l) => l.exists).length} 个目录`}
      actions={
        data && (
          <>
            {same.length > 0 && (
              <Button
                variant="primary"
                icon={<FolderSync className="size-4" />}
                onClick={() =>
                  setDialog(
                    <PlanDialog
                      title={`统一 ${same.length} 个各处相同的 Skill`}
                      intro="每个 Skill 放一份到 MCPcell 的来源目录，原来各处的副本先移到备份，再换成指向来源的目录链接。内容不变，随时可以还原。"
                      plan={() => skillsApi.sync({ names: same.map((k) => k.name), dryRun: true })}
                      run={() => skillsApi.sync({ names: same.map((k) => k.name) })}
                      verb="统一"
                      onClose={close}
                      onDone={done}
                    />,
                  )
                }
              >
                统一 {same.length} 个
              </Button>
            )}
            <Menu
              items={[
                { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: reload },
                'sep',
                {
                  label: '全部还原',
                  hint: '各 Agent 目录恢复同步前的内容，来源目录保留',
                  icon: <Undo2 className="size-4" />,
                  danger: true,
                  disabled: !managed,
                  onSelect: () =>
                    setDialog(
                      <PlanDialog
                        title="还原全部 Skill 同步"
                        intro="把同步时移走的目录逐字节移回原处，删除 MCPcell 建的链接；同步后被改动过的不动。"
                        plan={() => skillsApi.restore({ all: true, dryRun: true })}
                        run={() => skillsApi.restore({ all: true })}
                        verb="还原"
                        danger
                        onClose={close}
                        onDone={done}
                      />,
                    ),
                },
              ]}
            />
          </>
        )
      }
    />
  );

  if (!data) {
    return (
      <>
        {header}
        <p className="py-20 text-center text-sm text-zinc-400">{error || '正在扫描各 Agent 的 Skill 目录…'}</p>
      </>
    );
  }

  const sync = (k: SkillInfo) => setDialog(<SyncDialog skill={k} data={data} onClose={close} onDone={done} />);
  const restore = (k: SkillInfo) =>
    setDialog(
      <PlanDialog
        title={`还原「${k.name}」`}
        intro="各 Agent 目录恢复同步前的内容（逐字节），来源目录里的这一份保留。"
        plan={() => skillsApi.restore({ names: [k.name], dryRun: true })}
        run={() => skillsApi.restore({ names: [k.name] })}
        verb="还原"
        danger
        onClose={close}
        onDone={done}
      />,
    );

  return (
    <>
      {header}
      <div className="mx-auto w-full max-w-[1240px] space-y-7 px-8 pt-6 pb-12">
        {error && <p className="text-sm text-red-600">{error}</p>}
        <div className="grid grid-cols-4 gap-4">
          <Stat icon={<Link2 className="size-4" />} label="已统一" value={count('synced')} sub="各 Agent 链接到同一份" />
          <Stat icon={<FolderSync className="size-4" />} label="可以统一" value={count('same')} sub="各处相同，但各是一份副本" />
          <Stat icon={<TriangleAlert className="size-4" />} label="内容不一致" value={count('differs')} sub="需要选一份为准" />
          <Card className="px-5 py-4">
            <div className="text-[13px] text-zinc-500">来源目录</div>
            <div className="mt-2 truncate font-mono text-[13px] text-zinc-700 dark:text-zinc-300" title={data.source}>
              {data.source}
            </div>
            <div className="mt-1 truncate text-xs text-zinc-400">统一后每个 Skill 只在这里存一份</div>
          </Card>
        </div>

        {data.skills.length === 0 && <p className="py-12 text-center text-sm text-zinc-400">没有在各 Agent 的目录里找到 Skill。</p>}

        {groups.map((g) => {
          const rows = data.skills.filter((k) => g.states.includes(k.state));
          if (!rows.length) return null;
          const expanded = open[g.id] ?? !g.collapsed;
          return (
            <section key={g.id}>
              <div className={cx(grid, 'px-5')}>
                <div className="-ml-5 min-w-0">
                  <GroupHeader
                    title={g.title}
                    count={rows.length}
                    hint={g.hint}
                    open={expanded}
                    onToggle={g.collapsed ? () => setOpen((o) => ({ ...o, [g.id]: !expanded })) : undefined}
                  />
                </div>
                {expanded && (
                  <>
                    <span className="mb-2.5 text-xs text-zinc-400">位置</span>
                    <span className="mb-2.5 text-xs text-zinc-400">状态</span>
                    <span />
                  </>
                )}
              </div>
              {expanded && (
                <TableCard>
                  {rows.map((k) => (
                    <SkillRow
                      key={k.name}
                      skill={k}
                      data={data}
                      open={rowOpen === k.name}
                      onToggle={() => setRowOpen(rowOpen === k.name ? null : k.name)}
                      onSync={() => sync(k)}
                      onRestore={() => restore(k)}
                    />
                  ))}
                </TableCard>
              )}
            </section>
          );
        })}
      </div>
      {dialog}
    </>
  );
}

const stateLine: Record<SkillState, [tone: 'green' | 'blue' | 'amber' | 'gray', text: string]> = {
  synced: ['green', '已统一'],
  source: ['gray', '只在来源目录'],
  same: ['blue', '各处相同'],
  differs: ['amber', '内容不一致'],
  single: ['gray', '只在一处'],
  external: ['gray', '外部链接'],
};

function SkillRow({
  skill,
  data,
  open,
  onToggle,
  onSync,
  onRestore,
}: {
  skill: SkillInfo;
  data: SkillStatus;
  open: boolean;
  onToggle: () => void;
  onSync: () => void;
  onRestore: () => void;
}) {
  const copies = skill.copies.filter((c) => c.location !== SOURCE);
  const changed = copies.some((c) => c.changed);
  const [tone, text] = stateLine[skill.state];
  const managed = skill.copies.some((c) => c.managed);
  return (
    <div>
      <div onClick={onToggle} className={cx(grid, 'cursor-pointer px-5 py-3 transition-colors hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40')}>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate text-[15px] font-medium">{skill.name}</span>
            {changed && <Badge tone="red">同步后被改动</Badge>}
          </div>
          <div className="mt-0.5 truncate text-xs text-zinc-400" title={skill.description}>
            {skill.description || '—'}
          </div>
        </div>
        <div className="flex min-w-0 flex-wrap gap-1.5">
          {copies.map((c) => (
            <LocChip key={c.location} copy={c} skill={skill} />
          ))}
        </div>
        <div className="flex items-center gap-2 text-[13px] text-zinc-500">
          <Dot tone={tone} />
          {skill.state === 'differs' ? `${skill.variants} 个版本` : text}
        </div>
        <div className="flex items-center justify-end gap-1" onClick={(e) => e.stopPropagation()}>
          {skill.state === 'differs' && (
            <Button size="sm" onClick={onSync}>
              选择版本
            </Button>
          )}
          {(skill.state === 'same' || skill.state === 'single') && (
            <Button size="sm" icon={<FolderSync className="size-3.5" />} onClick={onSync}>
              统一
            </Button>
          )}
          {(skill.state === 'synced' || skill.state === 'source' || managed) && (
            <Menu
              items={[
                { label: '添加到其他 Agent', icon: <Plus className="size-4" />, onSelect: onSync },
                'sep',
                { label: '还原', hint: '恢复同步前的内容，来源目录保留', icon: <Undo2 className="size-4" />, danger: true, disabled: !managed, onSelect: onRestore },
              ]}
            />
          )}
        </div>
      </div>
      {open && <SkillDetail skill={skill} data={data} />}
    </div>
  );
}

function chipTone(c: SkillCopy): string {
  if (c.changed || c.error || c.kind === 'broken') return 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300';
  if (c.kind === 'linked' || (c.managed && c.match === 'same'))
    return 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300';
  if (c.kind === 'link') return 'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800/60 dark:text-zinc-300';
  if (c.match && c.match !== 'same') return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200';
  return 'border-zinc-200 bg-white text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200';
}

function copyText(c: SkillCopy, skill: SkillInfo): string {
  const variants = skill.variants;
  if (c.error) return '读取失败';
  if (c.changed) return '已改动';
  if (c.kind === 'linked') return '链接';
  if (c.kind === 'broken') return '失效链接';
  if (c.kind === 'link') return '外部链接';
  if (c.managed) return '副本';
  if (c.location === SOURCE) return '来源';
  const m = { same: variants > 1 && skill.reference !== SOURCE ? '最新' : '一致', newer: '较新', older: '较旧', differs: '不同' }[c.match ?? 'same'];
  return c.variant ? `${c.variant} ${m}` : m;
}

function LocChip({ copy, skill }: { copy: SkillCopy; skill: SkillInfo }) {
  const b = brand[copy.location];
  return (
    <span
      title={copy.target ? `${copy.path} → ${copy.target}` : copy.path}
      className={cx('inline-flex h-6 items-center gap-1 rounded-md border px-1.5 text-xs whitespace-nowrap', chipTone(copy))}
    >
      {b ? <BrandMark brand={b} className="size-3" /> : <span className="font-mono text-[11px] opacity-70">~</span>}
      <span className="font-medium">{label(copy.location)}</span>
      <span className="opacity-75">{copyText(copy, skill)}</span>
      {copy.kind === 'link' && <ExternalLink className="size-3 opacity-60" />}
    </span>
  );
}

export function diffText(d?: SkillDiff): string {
  if (!d) return '';
  const part = (name: string, list?: string[]) =>
    list?.length ? `${name} ${list.slice(0, 3).join('、')}${list.length > 3 ? ` 等 ${list.length} 个` : ''}` : '';
  return [part('改动', d.changed), part('多出', d.added), part('缺少', d.removed)].filter(Boolean).join('；');
}

function when(t: string) {
  if (!t || t.startsWith('0001')) return '—';
  return new Date(t).toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
}

function SkillDetail({ skill, data }: { skill: SkillInfo; data: SkillStatus }) {
  const locName = (id: string) => (id === SOURCE ? 'MCPcell 来源目录' : (data.locations.find((l) => l.id === id)?.name ?? id));
  const compared = skill.variants > 1 || skill.reference === SOURCE;
  return (
    <div className="border-t border-zinc-100 bg-zinc-50/60 px-5 py-4 dark:border-zinc-800 dark:bg-zinc-950/40">
      <div className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-xs text-zinc-500">
        {compared && <span>比较基准：{skill.reference === SOURCE ? 'MCPcell 来源目录' : `${locName(skill.reference ?? '')}（最近修改的一份）`}</span>}
        {skill.from && <span>来源取自：{locName(skill.from)}</span>}
      </div>
      <div className="space-y-2">
        {skill.copies.map((c) => {
          const diff = diffText(c.diff);
          return (
            <div key={c.location} className="grid grid-cols-[160px_120px_64px_140px_minmax(0,1fr)] items-start gap-3 text-[13px]">
              <span className="font-medium text-zinc-700 dark:text-zinc-200">{locName(c.location)}</span>
              <span>
                <span className={cx('inline-flex h-5 items-center rounded border px-1.5 text-xs', chipTone(c))}>{copyText(c, skill)}</span>
              </span>
              <span className="text-zinc-500 tabular-nums">{c.files} 个文件</span>
              <span className="text-zinc-500 tabular-nums">{when(c.newest)}</span>
              <div className="min-w-0">
                <div className="truncate font-mono text-xs text-zinc-400" title={c.path}>
                  {c.path}
                  {c.target && ` → ${c.target}`}
                </div>
                {diff && <div className="mt-0.5 text-xs text-amber-700 dark:text-amber-300">与基准相比：{diff}</div>}
                {c.changed && <div className="mt-0.5 text-xs text-red-600">MCPcell 同步之后被改动过，不会再自动覆盖或还原</div>}
                {c.error && <div className="mt-0.5 text-xs text-red-600">{c.error}</div>}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
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

function PlanDialog({
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
function SyncDialog({ skill, data, onClose, onDone }: { skill: SkillInfo; data: SkillStatus; onClose: () => void; onDone: () => void }) {
  const has = (id: string) => skill.copies.find((c) => c.location === id);
  const versions = variantsOf(skill);
  const needChoice = skill.state === 'differs';
  const [from, setFrom] = useState<string>(needChoice ? (versions[0]?.from ?? '') : '');
  const installed = data.locations.filter((l) => l.installed);
  const [to, setTo] = useState<string[]>(() => installed.filter((l) => has(l.id) && has(l.id)!.kind !== 'link' && has(l.id)!.kind !== 'broken').map((l) => l.id));
  const toggle = (id: string, on: boolean) => setTo((t) => (on ? [...t, id] : t.filter((x) => x !== id)));
  const req = { names: [skill.name], to, from: from || undefined };
  const { result, error } = usePlan(() => skillsApi.sync({ ...req, dryRun: true }), [from, to.join(',')]);
  const { busy, ran, go } = useRun(() => skillsApi.sync(req), '同步', onDone);
  const n = changes(result);
  const shown = ran ?? result;

  return (
    <Modal open onClose={onClose} width={720}>
      <h2 className="text-base font-semibold">统一「{skill.name}」</h2>
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
