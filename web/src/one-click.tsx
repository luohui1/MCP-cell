import { useEffect, useMemo, useState } from 'react';
import { Layers } from 'lucide-react';
import { takeover, type AgentSummary, type McpRow } from './api';
import { agentBrand, applyTakeover, AutostartOption, takeSteps } from './agents';
import { useStatus, type Unified } from './data';
import { firstLine } from './format';
import { BrandMark, ServiceAvatar } from './icons';
import { Badge, Button, Checkbox, cx, Modal, Segmented, useToast } from './ui';

/** Rows the one-click flow offers, with their steps for the given agents. */
export function candidates(data: Unified, only?: string, keys?: string[]) {
  const agents = data.agents.filter((a) => a.found);
  return data.rows
    .filter((row) => !keys || keys.includes(row.key))
    .map((row) => ({ row, steps: takeSteps(row, agents, only) }))
    .filter((c) => c.steps.length > 0);
}

/** Takes over every local MCP across all agents after one confirmation.
 * Known-stateless servers are shared; the rest get one instance per session,
 * which is as safe as running them natively, so everything is preselected.
 * An unverified server can be switched to shared. */
export function OneClickDialog({
  open,
  onClose,
  data,
  only,
  keys,
  reload,
}: {
  open: boolean;
  onClose: () => void;
  data: Unified;
  only?: string;
  /** Offer only these rows (by key). */
  keys?: string[];
  reload: () => Promise<void>;
}) {
  const { refresh } = useStatus();
  const toast = useToast();
  const live = useMemo(() => candidates(data, only, keys), [data, only, keys?.join()]);
  // While it runs, the rows it is taking over leave the live list.
  const [frozen, setFrozen] = useState<typeof live | null>(null);
  const list = frozen ?? live;
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [shared, setShared] = useState<Set<string>>(new Set());
  const [auto, setAuto] = useState(true);
  const [busy, setBusy] = useState(false);
  const offerAuto = data.autostart.supported && !data.autostart.enabled;

  useEffect(() => {
    if (open) {
      setPicked(new Set(list.map((c) => c.row.key)));
      setShared(new Set());
    }
  }, [open]);

  const agents = data.agents.filter((a) => a.found);
  const chosen = list
    .filter((c) => picked.has(c.row.key))
    .map((c) => ({ ...c, steps: takeSteps(c.row, agents, only, shared.has(c.row.key)) }));
  const touched = [...new Map(chosen.flatMap((c) => c.steps.map((s) => [s.agent.id, s.agent] as [string, AgentSummary]))).values()];
  const perSession = chosen.filter((c) => c.steps[0].mode === 'per-session').length;

  async function run() {
    setBusy(true);
    setFrozen(list);
    try {
      if (offerAuto && auto) await takeover.autostart(true);
      toast(await applyTakeover(chosen.flatMap((c) => c.steps)));
      onClose();
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy(false);
      setFrozen(null);
      await Promise.all([reload(), refresh()]);
    }
  }

  const flip = (set: Set<string>, key: string, on: boolean) => {
    const n = new Set(set);
    if (on) n.add(key);
    else n.delete(key);
    return n;
  };

  return (
    <Modal open={open} onClose={() => !busy && onClose()} width={660}>
      <div className="flex items-start gap-3.5">
        <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-blue-600 text-white shadow-sm shadow-blue-600/30">
          <Layers className="size-5" />
        </div>
        <div>
          <h2 className="text-base font-semibold">{only ? `托管 ${data.agents.find((a) => a.id === only)?.name} 的 MCP` : '一键托管本机 MCP'}</h2>
          <p className="mt-1 text-sm leading-6 text-zinc-500">选中的 MCP 改由 MCPcell 运行，工具名不变，随时可以还原。</p>
        </div>
      </div>

      {list.length === 0 ? (
        <p className="mt-6 rounded-xl bg-zinc-50 px-4 py-6 text-center text-sm text-zinc-500 dark:bg-zinc-800/50">没有可以托管的 MCP 了。</p>
      ) : (
        <div className="mt-5 max-h-[360px] divide-y divide-zinc-100 overflow-y-auto rounded-xl border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-700">
          {list.map(({ row, steps }) => (
            <CandidateRow
              key={row.key}
              row={row}
              steps={steps}
              checked={picked.has(row.key)}
              onChange={(v) => setPicked((p) => flip(p, row.key, v))}
              share={shared.has(row.key)}
              onShare={(v) => setShared((s) => flip(s, row.key, v))}
            />
          ))}
        </div>
      )}

      <dl className="mt-4 space-y-1.5 text-[13px] leading-5 text-zinc-500">
        <div className="flex gap-2">
          <dt className="shrink-0">
            <Badge tone="green">共享</Badge>
          </dt>
          <dd>所有会话共用一份进程，空闲后回收。</dd>
        </div>
        <div className="flex gap-2">
          <dt className="shrink-0">
            <Badge tone="blue">按会话</Badge>
          </dt>
          <dd>和原来一样每个会话一份，保留各自的状态；但第一次调用时才启动、会话结束就回收，用不到它的会话不占内存。</dd>
        </div>
      </dl>
      {touched.length > 0 && (
        <p className="mt-3 text-[13px] leading-5 text-zinc-400">
          将修改 {touched.map((a) => a.name).join('、')} 的配置文件，只改选中的条目，改之前先备份整个文件。
          {perSession > 0 && ` 其中 ${perSession} 个按会话运行。`}
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
  share,
  onShare,
}: {
  row: McpRow;
  steps: ReturnType<typeof takeSteps>;
  checked: boolean;
  onChange: (v: boolean) => void;
  share: boolean;
  onShare: (v: boolean) => void;
}) {
  const unverified = row.class === 'unknown';
  const mode = row.class === 'shared' || share ? 'shared' : 'per-session';
  return (
    <label className={cx('flex cursor-pointer items-center gap-3.5 px-4 py-3 transition-colors', checked ? 'bg-blue-50/50 dark:bg-blue-950/20' : 'hover:bg-zinc-50 dark:hover:bg-zinc-800/40')}>
      <Checkbox checked={checked} onChange={onChange} />
      <ServiceAvatar name={row.name} size="sm" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{row.name}</span>
          {!unverified && (mode === 'shared' ? <Badge tone="green">共享</Badge> : <Badge tone="blue">按会话</Badge>)}
        </div>
        <div className="mt-0.5 truncate text-xs text-zinc-400" title={row.reason}>
          {row.class === 'stateful' ? row.reason : unverified ? '未验证：默认按会话；确认它不保存会话状态可以改为共享' : row.launch}
        </div>
      </div>
      {unverified && (
        <div onClick={(e) => e.preventDefault()}>
          <Segmented<string>
            value={mode}
            onChange={(v) => onShare(v === 'shared')}
            options={[
              { value: 'per-session', label: '按会话' },
              { value: 'shared', label: '共享' },
            ]}
          />
        </div>
      )}
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
