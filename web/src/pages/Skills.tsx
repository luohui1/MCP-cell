import { useEffect, useState, type ReactNode } from 'react';
import { ExternalLink, FolderSync, Plus, Puzzle, RefreshCw, Undo2 } from 'lucide-react';
import { skillsApi, SOURCE, type SkillCopy, type SkillInfo, type SkillStatus } from '../api';
import { useSkills } from '../data';
import { AgentFilter, Block, Crumbs, Detail, DetailHead, ListGroup, ListItem, ListScroll, ListTools, Placeholder, SearchBox, Split, StatusLine, type AgentOption } from '../layout';
import { locationName, skillAgents, skillView, type SkillGroup, type SkillView } from '../model';
import { href, redirect } from '../router';
import { diffText, PlanDialog, SyncDialog, when } from '../skill-dialogs';
import { Button, cx, Menu, PageHeader } from '../ui';

// Level 2 and 3 for Skill: every agent skill grouped by whether its copies
// agree, the selected one beside it. Changes go through skill-dialogs.

const groups: { id: SkillGroup; title: string; collapsed?: boolean }[] = [
  { id: 'attention', title: '需要处理' },
  { id: 'synced', title: '已统一' },
  { id: 'single', title: '只在一处', collapsed: true },
  { id: 'external', title: '其他工具管理', collapsed: true },
];

// The agents the MCP page filters by keep their IDs here too.
const agentIds: Record<string, string> = { 'Claude Code': 'claude-code', Codex: 'codex', Cursor: 'cursor' };
const agentName = (id: string) => Object.entries(agentIds).find(([, v]) => v === id)?.[0] ?? id;

const toneRank = { red: 0, amber: 1, blue: 2, green: 3, gray: 4 };

export function SkillsPage({ name, agent }: { name?: string; agent: string }) {
  const { data, error, reload, stale } = useSkills();
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [dialog, setDialog] = useState<ReactNode>(null);
  useEffect(() => stale(10_000), []);
  const close = () => setDialog(null);
  const done = () => (setDialog(null), reload());

  const views = data ? data.skills.map((k) => skillView(k, data)) : [];
  const selected = name ? views.find((v) => v.skill.name === name) : undefined;
  const readers = data ? new Map(data.skills.map((k) => [k.name, skillAgents(k, data)])) : new Map<string, Set<string>>();
  const options = agentOptions(data, readers);
  const q = search.trim().toLowerCase();
  const shown = views.filter((v) => (!agent || readers.get(v.skill.name)?.has(agentName(agent))) && (!q || v.skill.name.toLowerCase().includes(q)));
  const same = views.filter((v) => v.group === 'attention' && v.skill.state === 'same').map((v) => v.skill.name);
  const managed = data?.skills.some((k) => k.copies.some((c) => c.managed)) ?? false;

  const merge = (names: string[]) =>
    setDialog(
      <PlanDialog
        title={names.length === 1 ? `合并「${names[0]}」` : `合并 ${names.length} 个 Skill`}
        intro="每个 Skill 在 MCPcell 的来源目录放一份，各处的副本先移到备份，再换成指向它的目录链接。内容不变，随时可以还原。"
        plan={() => skillsApi.sync({ names, dryRun: true })}
        run={() => skillsApi.sync({ names })}
        verb="合并"
        onClose={close}
        onDone={done}
      />,
    );
  const restore = (names?: string[]) =>
    setDialog(
      <PlanDialog
        title={names ? `还原「${names[0]}」` : '还原全部 Skill'}
        intro="把合并时移走的目录逐字节移回原处，删除 MCPcell 建的链接；来源目录里的这一份保留，合并后被改动过的不动。"
        plan={() => skillsApi.restore(names ? { names, dryRun: true } : { all: true, dryRun: true })}
        run={() => skillsApi.restore(names ? { names } : { all: true })}
        verb="还原"
        danger
        onClose={close}
        onDone={done}
      />,
    );
  const sync = (k: SkillInfo) => data && setDialog(<SyncDialog skill={k} data={data} onClose={close} onDone={done} />);

  const crumbs = [{ label: 'Skill', href: href(['skills'], { agent }) }];
  if (name) crumbs.push({ label: name, href: '' });

  const list = (
    <>
      <ListTools>
        <AgentFilter value={agent} total={views.length} options={options} onChange={(a) => redirect(name ? ['skills', name] : ['skills'], { agent: a })} />
        <SearchBox value={search} onChange={setSearch} placeholder="搜索 Skill" />
      </ListTools>
      <ListScroll>
        {!data && <p className="py-10 text-center text-sm text-zinc-400">{error || '正在扫描各 Agent 的 Skill 目录…'}</p>}
        {data && shown.length === 0 && <p className="py-10 text-center text-sm text-zinc-400">没有符合条件的 Skill</p>}
        {groups.map((g) => {
          const rows = shown.filter((v) => v.group === g.id).sort((a, b) => toneRank[a.tone] - toneRank[b.tone] || a.skill.name.localeCompare(b.skill.name));
          if (!rows.length) return null;
          const collapsed = g.collapsed && selected?.group !== g.id && !q;
          const expanded = open[g.id] ?? !collapsed;
          const mergeable = g.id === 'attention' ? rows.filter((v) => v.skill.state === 'same').map((v) => v.skill.name) : [];
          return (
            <ListGroup
              key={g.id}
              title={g.title}
              count={rows.length}
              open={expanded}
              onToggle={() => setOpen((o) => ({ ...o, [g.id]: !expanded }))}
              action={
                mergeable.length > 1 && (
                  <Button size="sm" variant="ghost" className="!h-7 !px-2 text-blue-600 dark:text-blue-400" onClick={() => merge(mergeable)}>
                    合并 {mergeable.length} 个
                  </Button>
                )
              }
            >
              {rows.map((v) => (
                <ListItem key={v.skill.name} href={href(['skills', v.skill.name], { agent })} selected={v === selected} name={v.skill.name} tone={v.tone} text={v.text} />
              ))}
            </ListGroup>
          );
        })}
      </ListScroll>
    </>
  );

  let detail: ReactNode;
  if (selected && data) {
    detail = <SkillDetail key={selected.skill.name} view={selected} data={data} onMerge={() => merge([selected.skill.name])} onSync={() => sync(selected.skill)} onRestore={() => restore([selected.skill.name])} />;
  } else if (name && data) {
    detail = (
      <Placeholder icon={<Puzzle className="size-6" />} title={`找不到 ${name}`}>
        它可能已被删除或改名。<a className="text-blue-600 hover:underline" href={href(['skills'], { agent })}>回到列表</a>
      </Placeholder>
    );
  } else {
    const count = (g: SkillGroup) => views.filter((v) => v.group === g).length;
    detail = (
      <Placeholder icon={<Puzzle className="size-6" />} title="选择左侧的一个 Skill">
        {data && (
          <>
            {`共 ${views.length} 个，来自 ${data.locations.filter((l) => l.exists).length} 个目录：${count('attention')} 个需要处理，${count('synced')} 个已统一，${count('single')} 个只在一处，${count('external')} 个由其他工具管理。`}
            <div className="mt-3 text-xs text-zinc-400">
              统一后每个 Skill 只在 <span className="font-mono">{data.source}</span> 存一份，各 Agent 的目录链接到它。
            </div>
          </>
        )}
      </Placeholder>
    );
  }

  return (
    <>
      <PageHeader
        title={<Crumbs items={crumbs} />}
        actions={
          <Menu
            items={[
              { label: '重新扫描', icon: <RefreshCw className="size-4" />, onSelect: reload },
              ...(same.length ? [{ label: `合并 ${same.length} 个相同的`, icon: <FolderSync className="size-4" />, onSelect: () => merge(same) }] : []),
              'sep',
              { label: '全部还原', hint: '各 Agent 目录恢复合并前的内容，来源目录保留', icon: <Undo2 className="size-4" />, danger: true, disabled: !managed, onSelect: () => restore() },
            ]}
          />
        }
      />
      <Split list={list} detail={detail} />
      {dialog}
    </>
  );
}

function agentOptions(data: SkillStatus | null, readers: Map<string, Set<string>>): AgentOption[] {
  if (!data) return [];
  const names = [...new Set(data.locations.filter((l) => l.installed).flatMap((l) => l.agents))];
  const order = (n: string) => (agentIds[n] ? Object.keys(agentIds).indexOf(n) : 10);
  names.sort((a, b) => order(a) - order(b) || a.localeCompare(b));
  return names.map((n) => ({
    id: agentIds[n] ?? n,
    name: n,
    count: [...readers.values()].filter((s) => s.has(n)).length,
  }));
}

const explain: Record<string, string> = {
  same: '这几处是相同的独立副本。合并后 MCPcell 的来源目录放一份，各处换成指向它的目录链接，改一处处处生效；原来的副本先移到备份，随时可以还原。',
  differs: '同名 Skill 在各处的内容不同。选一份为准后合并：其他版本的副本先移到备份，还原时原样放回。',
  synced: '各处都链接到 MCPcell 来源目录里的同一份，改一处处处生效。',
  source: '只在 MCPcell 的来源目录里，还没有链接到任何 Agent。',
  single: '只在一处。添加到其他 Agent 时，先放进 MCPcell 的来源目录，再在选中的位置建目录链接。',
  external: '各处都链接到同一个外部目录，由其他工具管理（例如 bl skill），已经是一份，MCPcell 不改动它。',
};

function SkillDetail({ view, data, onMerge, onSync, onRestore }: { view: SkillView; data: SkillStatus; onMerge: () => void; onSync: () => void; onRestore: () => void }) {
  const k = view.skill;
  const managed = k.copies.some((c) => c.managed);
  const compared = k.variants > 1 || k.reference === SOURCE;
  let primary: ReactNode = null;
  if (k.state === 'differs')
    primary = (
      <Button variant="primary" onClick={onSync}>
        选择版本
      </Button>
    );
  else if (k.state === 'same')
    primary = (
      <Button variant="primary" icon={<FolderSync className="size-4" />} onClick={onMerge}>
        合并
      </Button>
    );
  else if (k.state !== 'external')
    primary = (
      <Button icon={<Plus className="size-4" />} onClick={onSync}>
        添加到其他 Agent
      </Button>
    );
  return (
    <Detail>
      <DetailHead
        title={k.name}
        status={<StatusLine tone={view.tone}>{view.text}</StatusLine>}
        actions={
          <>
            {primary}
            <Menu
              items={[
                ...(k.state === 'differs' || k.state === 'same' ? [{ label: '添加到其他 Agent', icon: <Plus className="size-4" />, onSelect: onSync }] : []),
                { label: '还原', hint: '各处恢复合并前的内容，来源目录保留', icon: <Undo2 className="size-4" />, danger: true, disabled: !managed, onSelect: onRestore },
              ]}
            />
          </>
        }
      />
      {k.description && <p className="text-sm leading-6 text-zinc-600 dark:text-zinc-300">{k.description}</p>}
      <p className="text-[13px] leading-6 text-zinc-500">{explain[k.state]}</p>

      <Block
        title={`在哪里 · ${k.copies.length} 处`}
        extra={
          compared && (
            <span className="text-xs text-zinc-400">
              比较基准：{k.reference === SOURCE ? 'MCPcell 来源目录' : `${locationName(data, k.reference ?? '')}（最近修改的一份）`}
            </span>
          )
        }
        className="p-0"
      >
        <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
          {k.copies.map((c) => (
            <CopyRow key={c.location} copy={c} skill={k} data={data} />
          ))}
        </div>
      </Block>
    </Detail>
  );
}

function CopyRow({ copy: c, skill, data }: { copy: SkillCopy; skill: SkillInfo; data: SkillStatus }) {
  const loc = data.locations.find((l) => l.id === c.location);
  const diff = diffText(c.diff);
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-1 px-5 py-3.5">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{locationName(data, c.location)}</span>
          <span className={cx('inline-flex h-5 items-center gap-1 rounded border px-1.5 text-xs', chipTone(c))}>
            {copyText(c, skill)}
            {c.kind === 'link' && <ExternalLink className="size-3 opacity-60" />}
          </span>
        </div>
        {loc && loc.agents.length > 0 && <div className="mt-0.5 text-xs text-zinc-500">{loc.agents.join('、')} 读取这里</div>}
      </div>
      <div className="text-right text-xs text-zinc-500 tabular-nums">
        {c.files} 个文件 · {when(c.newest)}
      </div>
      <div className="col-span-2 min-w-0">
        <div className="truncate font-mono text-xs text-zinc-400" title={c.target ? `${c.path} → ${c.target}` : c.path}>
          {c.path}
          {c.target && ` → ${c.target}`}
        </div>
        {diff && <div className="mt-1 text-xs text-amber-700 dark:text-amber-300">与基准相比：{diff}</div>}
        {c.changed && <div className="mt-1 text-xs text-red-600">MCPcell 合并之后被改过，不会再自动覆盖或还原</div>}
        {c.error && <div className="mt-1 text-xs text-red-600">{c.error}</div>}
      </div>
    </div>
  );
}

function chipTone(c: SkillCopy): string {
  if (c.changed || c.error || c.kind === 'broken') return 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300';
  if (c.kind === 'linked' || (c.managed && c.match === 'same')) return 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300';
  if (c.kind === 'link') return 'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800/60 dark:text-zinc-300';
  if (c.match && c.match !== 'same') return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200';
  return 'border-zinc-200 bg-white text-zinc-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200';
}

function copyText(c: SkillCopy, skill: SkillInfo): string {
  if (c.error) return '读取失败';
  if (c.changed) return '已改动';
  if (c.kind === 'linked') return '链接到来源';
  if (c.kind === 'broken') return '链接失效';
  if (c.kind === 'link') return '外部链接';
  if (c.managed) return 'MCPcell 的副本';
  if (c.location === SOURCE) return '来源';
  const m = { same: skill.variants > 1 && skill.reference !== SOURCE ? '最新' : '一致', newer: '较新', older: '较旧', differs: '不同' }[c.match ?? 'same'];
  return c.variant ? `版本 ${c.variant} · ${m}` : `独立副本 · ${m}`;
}

