import { useEffect, useState, type ReactNode } from 'react';
import { ArrowRight, ChevronRight, House, Layers, Puzzle, Undo2 } from 'lucide-react';
import { instancesOf, takeover, type AgentSummary } from '../api';
import { agentBrand, agentsOf, restoreSteps, useAgentActions } from '../agents';
import { useSkills, useStatus, useUnified } from '../data';
import { bytes, firstLine, since } from '../format';
import { BrandMark } from '../icons';
import { Body } from '../layout';
import { mcpView, skillAgents, skillView, type Tone } from '../model';
import { candidates, OneClickDialog } from '../one-click';
import { go, href } from '../router';
import { PlanDialog } from '../skill-dialogs';
import { skillsApi } from '../api';
import { Button, Card, cx, Dot, GroupHeader, Menu, PageHeader, useToast } from '../ui';

// Level 1: is everything fine, and is there anything for me to do.

interface Need {
  key: string;
  tone: Tone;
  title: string;
  text: string;
  action: string;
  run: () => void;
}

export function OverviewPage() {
  const { status } = useStatus();
  const { data, error, reload } = useUnified();
  const skills = useSkills();
  const acts = useAgentActions(reload, data?.autostart ?? null);
  const toast = useToast();
  const [oneClick, setOneClick] = useState<{ only?: string; keys?: string[] } | null>(null);
  const [dialog, setDialog] = useState<ReactNode>(null);
  useEffect(() => skills.stale(30_000), []);

  const agents = data ? agentsOf(data.agents) : [];
  const views = data ? data.rows.map((r) => mcpView(r, status, agents)) : [];
  const skillViews = skills.data ? skills.data.skills.map((k) => skillView(k, skills.data!)) : [];

  // What needs the user, most serious first. Each has one action.
  const needs: Need[] = [];
  for (const v of views.filter((v) => v.group === 'attention')) {
    const broken = v.tone === 'red';
    needs.push({
      key: `mcp:${v.id}`,
      tone: v.tone,
      title: broken ? `${v.row.name} 起不来` : v.row.name,
      text: broken ? v.text.replace(/^起不来：/, '') : v.text,
      action: '查看',
      run: () => go(['mcp', v.id]),
    });
  }
  for (const v of skillViews.filter((v) => v.group === 'attention' && v.skill.state !== 'same')) {
    needs.push({
      key: `skill:${v.skill.name}`,
      tone: v.tone,
      title: v.skill.name,
      text: v.skill.state === 'differs' ? `各处内容不一致，有 ${v.skill.variants} 个版本，选一份为准` : v.text,
      action: '查看',
      run: () => go(['skills', v.skill.name]),
    });
  }
  const untaken = views.filter((v) => v.group === 'unmanaged' && v.takeable);
  if (untaken.length === 1) {
    const v = untaken[0];
    needs.push({ key: 'take', tone: 'blue', title: `${v.row.name} 还没托管`, text: '现在每个会话各起一份进程', action: '查看', run: () => go(['mcp', v.id]) });
  } else if (untaken.length > 1) {
    needs.push({
      key: 'take',
      tone: 'blue',
      title: `${untaken.length} 个 MCP 还没托管`,
      text: `${untaken.map((v) => v.row.name).join('、')}：现在每个会话各起一份进程`,
      action: '一键托管',
      run: () => setOneClick({ keys: untaken.map((v) => v.row.key) }),
    });
  }
  const same = skillViews.filter((v) => v.skill.state === 'same' && v.group === 'attention').map((v) => v.skill);
  if (same.length === 1) {
    needs.push({ key: 'merge', tone: 'blue', title: `${same[0].name} 可以合并`, text: '几处是相同的副本，可以只留一份', action: '查看', run: () => go(['skills', same[0].name]) });
  } else if (same.length > 1) {
    const names = same.map((k) => k.name);
    needs.push({
      key: 'merge',
      tone: 'blue',
      title: `${same.length} 个 Skill 可以合并`,
      text: `${names.join('、')}：各处是相同的副本，可以只留一份`,
      action: '合并',
      run: () =>
        setDialog(
          <PlanDialog
            title={`合并 ${same.length} 个 Skill`}
            intro="每个 Skill 在 MCPcell 的来源目录放一份，各处的副本先移到备份，再换成指向它的目录链接。内容不变，随时可以还原。"
            plan={() => skillsApi.sync({ names, dryRun: true })}
            run={() => skillsApi.sync({ names })}
            verb="合并"
            onClose={() => setDialog(null)}
            onDone={() => (setDialog(null), skills.reload())}
          />,
        ),
    });
  }
  if (data && data.autostart.supported && !data.autostart.enabled && agents.some((a) => a.managed > 0)) {
    needs.push({
      key: 'autostart',
      tone: 'amber',
      title: '开机自启没开',
      text: 'MCPcell 没在运行时，托管的 MCP 在 Agent 里用不了',
      action: '开启',
      run: async () => {
        try {
          await takeover.autostart(true);
          toast('已开启开机自启');
          reload();
        } catch (e) {
          toast((e as Error).message, 'error');
        }
      },
    });
  }

  return (
    <>
      <PageHeader icon={<House className="size-5 text-zinc-500" />} title="概览" />
      <Body width={960}>
        <Health needs={needs.length} />
        {error && <p className="text-sm text-red-600">{error}</p>}
        {needs.length > 0 && (
          <section>
            <GroupHeader title="需要你处理" count={needs.length} />
            <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
              {needs.map((n) => (
                <NeedRow key={n.key} need={n} />
              ))}
            </Card>
          </section>
        )}
        {data && (
          <section>
            <GroupHeader
              title="Agent"
              count={agents.length}
              hint="托管后，Agent 里的 MCP 由 MCPcell 统一运行"
              extra={
                <Menu
                  items={[
                    {
                      label: '全部还原',
                      hint: '所有 Agent 恢复托管前的配置',
                      icon: <Undo2 className="size-4" />,
                      danger: true,
                      disabled: !agents.some((a) => a.managed > 0),
                      onSelect: () => {
                        const all = data.rows.flatMap((r) => restoreSteps(r, agents));
                        acts.restore(all, `还原全部 ${all.length} 个托管`);
                      },
                    },
                  ]}
                />
              }
            />
            <Card className="divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800">
              {agents.map((a) => (
                <AgentRow
                  key={a.id}
                  agent={a}
                  skills={skills.data ? skills.data.skills.filter((k) => skillAgents(k, skills.data!).has(a.name)).length : undefined}
                  offers={candidates(data, a.id).length}
                  onTake={() => setOneClick({ only: a.id })}
                  onRestore={() => {
                    const back = data.rows.flatMap((r) => restoreSteps(r, [a]));
                    acts.restore(back, `还原 ${a.name} 的 ${back.length} 个托管`);
                  }}
                />
              ))}
            </Card>
            <a href={href(['settings'], { manual: '1' })} className="mt-2.5 inline-flex items-center gap-1 px-1 text-[13px] text-zinc-500 hover:text-blue-600">
              其他 Agent 手动接入 <ArrowRight className="size-3.5" />
            </a>
          </section>
        )}
      </Body>
      {data && <OneClickDialog open={oneClick !== null} only={oneClick?.only} keys={oneClick?.keys} onClose={() => setOneClick(null)} data={data} reload={reload} />}
      {acts.dialog}
      {dialog}
    </>
  );
}

/** One sentence on how things are. */
function Health({ needs }: { needs: number }) {
  const { status, offline } = useStatus();
  const runs = status.services.flatMap(instancesOf);
  const procs = runs.reduce((n, i) => n + i.usage.processes, 0);
  const mcpBytes = runs.reduce((n, i) => n + i.usage.privateBytes, 0);
  const conns = status.sessions.length;
  const tone: Tone = offline ? 'red' : needs ? 'amber' : 'green';
  const head = offline ? '守护进程无响应' : needs ? `运行中 · 有 ${needs} 项需要处理` : '运行正常';
  const shared = !conns ? '还没有 Agent 连接' : procs ? `${conns} 个连接共用 ${procs} 个进程` : `${conns} 个连接，MCP 进程都已回收，调用时再启动`;
  return (
    <Card className="flex items-center gap-4 px-6 py-5">
      <span className={cx('grid size-10 shrink-0 place-items-center rounded-full', { green: 'bg-emerald-50 dark:bg-emerald-950', amber: 'bg-amber-50 dark:bg-amber-950', red: 'bg-red-50 dark:bg-red-950' }[tone as 'green'])}>
        <Dot tone={tone} pulse={tone === 'green'} />
      </span>
      <div className="min-w-0">
        <div className="text-[17px] font-semibold tracking-tight">{head}</div>
        <div className="mt-0.5 text-sm text-zinc-500">
          {shared}
          <span title={`守护进程 ${bytes(status.daemon.privateBytes)} · MCP ${mcpBytes ? bytes(mcpBytes) : '0 MB'}（私有提交）`}>
            {' · '}内存 {bytes(status.daemon.privateBytes + mcpBytes)}
          </span>
          {' · '}已运行 {since(status.startedAt)}
        </div>
      </div>
    </Card>
  );
}

const needIcon: Record<Tone, string> = {
  red: 'bg-red-500',
  amber: 'bg-amber-500',
  blue: 'bg-blue-500',
  green: 'bg-emerald-500',
  gray: 'bg-zinc-400',
};

function NeedRow({ need }: { need: Need }) {
  return (
    <div className="flex items-center gap-4 px-5 py-3.5">
      <span className={cx('size-2 shrink-0 rounded-full', needIcon[need.tone])} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{need.title}</div>
        <div className="mt-0.5 truncate text-[13px] text-zinc-500" title={need.text}>
          {firstLine(need.text)}
        </div>
      </div>
      <Button size="sm" onClick={need.run}>
        {need.action}
      </Button>
    </div>
  );
}

function AgentRow({
  agent,
  skills,
  offers,
  onTake,
  onRestore,
}: {
  agent: AgentSummary;
  skills?: number;
  offers: number;
  onTake: () => void;
  onRestore: () => void;
}) {
  const parts = [`MCP ${agent.total} 个，已托管 ${agent.managed} 个`, skills !== undefined && `Skill ${skills} 个`].filter(Boolean).join(' · ');
  return (
    <div className="group flex items-center gap-4 px-5 py-3.5">
      <a href={href(['mcp'], { agent: agent.id })} title={agent.path} className="flex min-w-0 flex-1 items-center gap-3.5">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-900">
          <BrandMark brand={agentBrand[agent.id]} className="size-[18px]" />
        </span>
        <div className="min-w-0">
          <div className="flex items-center gap-1 text-sm font-medium group-hover:text-blue-600">
            {agent.name}
            <ChevronRight className="size-3.5 text-zinc-300 group-hover:text-blue-500" />
          </div>
          <div className="mt-0.5 truncate text-[13px] text-zinc-500">{parts}</div>
          {agent.error && <div className="mt-0.5 truncate text-xs text-red-600">{agent.error}</div>}
        </div>
      </a>
      {offers > 0 && (
        <Button size="sm" icon={<Layers className="size-3.5" />} onClick={onTake}>
          托管 {offers} 个
        </Button>
      )}
      <Menu
        items={[
          { label: '查看 MCP', icon: <Layers className="size-4" />, onSelect: () => go(['mcp'], { agent: agent.id }) },
          { label: '查看 Skill', icon: <Puzzle className="size-4" />, onSelect: () => go(['skills'], { agent: agent.id }) },
          'sep',
          {
            label: '全部还原',
            hint: `${agent.name} 恢复托管前的配置`,
            icon: <Undo2 className="size-4" />,
            danger: true,
            disabled: agent.managed === 0,
            onSelect: onRestore,
          },
        ]}
      />
    </div>
  );
}
