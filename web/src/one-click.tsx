import { useEffect, useMemo, useState } from 'react';
import { Layers } from 'lucide-react';
import { takeover, type AgentSummary, type McpRow } from './api';
import { useStatus } from './App';
import { agentBrand, applyTakeover, AutostartOption, groupOf, takeSteps, type Unified } from './agents';
import { firstLine } from './format';
import { BrandMark, ServiceAvatar } from './icons';
import { Badge, Button, Checkbox, cx, Modal, useToast } from './ui';

/** Rows the one-click flow offers, with their steps for the given agents. */
export function candidates(data: Unified, only?: string) {
  const agents = data.agents.filter((a) => a.found);
  return data.rows
    .map((row) => ({ row, steps: takeSteps(row, agents, only) }))
    .filter((c) => c.steps.length > 0);
}

/** Takes over every MCP that can be shared, across all agents, after one
 * confirmation. Shared ones are preselected; unverified ones are opt-in. */
export function OneClickDialog({
  open,
  onClose,
  data,
  only,
  reload,
}: {
  open: boolean;
  onClose: () => void;
  data: Unified;
  only?: string;
  reload: () => Promise<void>;
}) {
  const { refresh } = useStatus();
  const toast = useToast();
  const list = useMemo(() => candidates(data, only), [data, only]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [auto, setAuto] = useState(true);
  const [busy, setBusy] = useState(false);
  const offerAuto = data.autostart.supported && !data.autostart.enabled;

  useEffect(() => {
    if (open) setPicked(new Set(list.filter((c) => c.steps.every((s) => !s.force)).map((c) => c.row.key)));
  }, [open]);

  const blocked = data.rows.filter((r) => groupOf(r) === 'blocked' && (!only || r.agents[only]));
  const chosen = list.filter((c) => picked.has(c.row.key));
  const touched = [...new Map(chosen.flatMap((c) => c.steps.map((s) => [s.agent.id, s.agent] as [string, AgentSummary]))).values()];

  async function run() {
    setBusy(true);
    try {
      if (offerAuto && auto) await takeover.autostart(true);
      toast(await applyTakeover(chosen.flatMap((c) => c.steps)));
      onClose();
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy(false);
      await Promise.all([reload(), refresh()]);
    }
  }

  const toggle = (key: string, on: boolean) =>
    setPicked((p) => {
      const n = new Set(p);
      if (on) n.add(key);
      else n.delete(key);
      return n;
    });

  return (
    <Modal open={open} onClose={() => !busy && onClose()} width={620}>
      <div className="flex items-start gap-3.5">
        <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-600 text-white shadow-sm shadow-blue-600/30">
          <Layers className="size-5" />
        </div>
        <div>
          <h2 className="text-base font-semibold">{only ? `托管 ${data.agents.find((a) => a.id === only)?.name} 的 MCP` : '一键托管本机 MCP'}</h2>
          <p className="mt-1 text-sm leading-6 text-zinc-500">选中的 MCP 改由 MCPcell 运行：所有 Agent 会话共用一份进程，工具名不变，随时可以还原。</p>
        </div>
      </div>

      {list.length === 0 ? (
        <p className="mt-6 rounded-xl bg-zinc-50 px-4 py-6 text-center text-sm text-zinc-500 dark:bg-zinc-800/50">没有可以托管的 MCP 了。</p>
      ) : (
        <div className="mt-5 max-h-[340px] divide-y divide-zinc-100 overflow-y-auto rounded-xl border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-700">
          {list.map(({ row, steps }) => (
            <CandidateRow key={row.key} row={row} steps={steps} checked={picked.has(row.key)} onChange={(v) => toggle(row.key, v)} />
          ))}
        </div>
      )}

      {blocked.length > 0 && (
        <p className="mt-3 text-[13px] leading-5 text-zinc-400">
          暂不托管：{blocked.map((r) => r.name).join('、')}。它们为每个会话保存状态（浏览器、连接），需要每个会话独立实例，后续版本支持。
        </p>
      )}
      {touched.length > 0 && (
        <p className="mt-2 text-[13px] leading-5 text-zinc-400">
          将修改 {touched.map((a) => a.name).join('、')} 的配置文件，只改选中的条目，改之前先备份整个文件。
        </p>
      )}
      {offerAuto && <AutostartOption checked={auto} onChange={setAuto} />}

      <div className="mt-6 flex items-center justify-end gap-2">
        <Button disabled={busy} onClick={onClose}>
          取消
        </Button>
        <Button variant="primary" busy={busy} disabled={chosen.length === 0} onClick={run}>
          托管 {chosen.length} 个
        </Button>
      </div>
    </Modal>
  );
}

function CandidateRow({
  row,
  steps,
  checked,
  onChange,
}: {
  row: McpRow;
  steps: ReturnType<typeof takeSteps>;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  const unverified = steps.some((s) => s.force);
  return (
    <label className={cx('flex cursor-pointer items-center gap-3.5 px-4 py-3 transition-colors', checked ? 'bg-blue-50/50 dark:bg-blue-950/20' : 'hover:bg-zinc-50 dark:hover:bg-zinc-800/40')}>
      <Checkbox checked={checked} onChange={onChange} />
      <ServiceAvatar name={row.name} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{row.name}</span>
          {unverified ? <Badge tone="amber">未验证</Badge> : <Badge tone="green">推荐</Badge>}
        </div>
        <div className="mt-0.5 truncate font-mono text-xs text-zinc-400">
          {unverified ? '不确定是否保存会话状态，确认可以共享再勾选' : row.launch}
          {!unverified && row.detail && ` ${row.detail}`}
        </div>
      </div>
      <div className="flex items-center gap-1.5" title={steps.map((s) => s.agent.name).join('、')}>
        {steps.map((s) => (
          <span key={s.agent.id} className="grid size-6 place-items-center rounded-full bg-zinc-100 dark:bg-zinc-800">
            <BrandMark brand={agentBrand[s.agent.id]} className="size-3.5" />
          </span>
        ))}
      </div>
    </label>
  );
}
