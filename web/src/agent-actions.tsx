import { useState, type ReactNode } from 'react';
import { takeover, unified, type AgentSummary, type AutostartStatus, type McpRow, type TakeoverResult } from './api';
import { useStatus } from './App';
import { firstLine } from './format';
import { Button, Modal, useToast } from './ui';

export const agentOrder = ['claude-code', 'codex', 'cursor'];
export const agentBrand: Record<string, string> = { 'claude-code': 'claude', codex: 'openai', cursor: 'cursor' };
export const restartHint: Record<string, string> = {
  'claude-code': '已经打开的 Claude Code 会话要重启后才生效，新会话立即生效',
  codex: '重启 Codex（包括桌面版）后生效',
  cursor: '在 Cursor 的 MCP 设置里刷新，或重启 Cursor',
};

interface Pending {
  title: string;
  lines: ReactNode[];
  warn?: string;
  confirm: string;
  danger?: boolean;
  run: () => Promise<string>;
}

/** Confirmed actions on agent configurations: take over, restore, add,
 * remove, forget. Every change is shown before it is written. */
export function useAgentActions(reload: () => Promise<void>, auto: AutostartStatus | null) {
  const { refresh } = useStatus();
  const toast = useToast();
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const [enableAuto, setEnableAuto] = useState(true);

  const offerAutostart = auto?.supported && !auto.enabled;

  async function run() {
    if (!pending) return;
    setBusy(true);
    try {
      if (offerAutostart && enableAuto && !pending.danger) await takeover.autostart(true);
      toast(await pending.run());
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy(false);
      setPending(null);
      await Promise.all([reload(), refresh()]);
    }
  }

  function check(results: TakeoverResult[], done: string) {
    const failed = results.filter((r) => !r.ok);
    if (failed.length) throw new Error(`${failed[0].name}：${failed[0].error}`);
    return done;
  }

  const fileLine = (a: AgentSummary) => (
    <>
      只改 <span className="font-mono text-[13px] text-zinc-700 dark:text-zinc-300">{a.path}</span> 里的这些条目，其余内容不动；改之前先备份整个文件
    </>
  );

  const actions = {
    takeoverEntries(a: AgentSummary, entries: { entry: string; service?: string; isNew?: boolean }[], force = false) {
      setPending({
        title: entries.length === 1 ? `把 ${a.name} 的 ${entries[0].entry} 交给 MCPcell 托管` : `托管 ${a.name} 的 ${entries.length} 个 MCP`,
        lines: [
          <span key="l">{entries.map((e) => `${e.entry} → /s/${e.service ?? e.entry}${e.isNew ? '（新建服务）' : ''}`).join('，')}</span>,
          fileLine(a),
          a.id === 'claude-code' ? '通过 Claude Code 自带的 claude mcp 命令修改，不和正在运行的会话抢写文件' : '',
          restartHint[a.id],
        ].filter(Boolean),
        warn: force ? 'MCPcell 不确定它有没有会话状态。如果它为不同客户端保存不同的状态（浏览器页面、连接、登录），共享后会互相干扰；出问题时还原即可。' : undefined,
        confirm: '托管',
        run: async () =>
          check((await takeover.attach(a.id, entries.map((e) => e.entry), force)).results, `已托管 ${entries.length} 个；${restartHint[a.id]}`),
      });
    },
    restoreEntry(a: AgentSummary, entry: string, added?: boolean) {
      setPending({
        title: added ? `从 ${a.name} 移除 ${entry}` : `把 ${a.name} 的 ${entry} 还原为原生`,
        lines: [
          added ? '这是 MCPcell 加进去的条目，移除后这个 Agent 不再有它' : '恢复成接管前的原始配置（逐字节），之后它会在每个会话里自己启动进程',
          '如果条目在托管后被你改过，不会覆盖',
          restartHint[a.id],
        ],
        confirm: added ? '移除' : '还原',
        danger: true,
        run: async () => check((await takeover.restore(a.id, [entry])).results, added ? `已从 ${a.name} 移除 ${entry}` : `已还原 ${entry}；${restartHint[a.id]}`),
      });
    },
    restoreAll(a: AgentSummary, entries: string[]) {
      setPending({
        title: `还原 ${a.name} 的全部 ${entries.length} 个托管`,
        lines: [entries.join('，'), '托管过的条目恢复原样，MCPcell 加进去的条目会被移除', restartHint[a.id]],
        confirm: '还原',
        danger: true,
        run: async () => check((await takeover.restore(a.id, entries)).results, `已还原 ${entries.length} 个；${restartHint[a.id]}`),
      });
    },
    forget(a: AgentSummary, entry: string) {
      setPending({
        title: `忘记 ${a.name} 中 ${entry} 的托管记录`,
        lines: ['这个条目在托管后被改过。保留你现在的配置，只删除 MCPcell 的记录'],
        confirm: '忘记记录',
        run: async () => check((await takeover.restore(a.id, [entry], true)).results, '已忘记记录'),
      });
    },
    add(a: AgentSummary, row: McpRow) {
      const service = row.service ?? row.proposed ?? row.name;
      setPending({
        title: `把 ${row.name} 加入 ${a.name}`,
        lines: [
          `在 ${a.name} 里新增条目 ${service}，连到 MCPcell 的 /s/${service}`,
          row.service ? '' : `会先在 MCPcell 里创建服务 ${service}，配置取自其他 Agent 里的同一个 MCP`,
          fileLine(a),
          restartHint[a.id],
        ].filter(Boolean),
        confirm: '添加',
        run: async () => (await unified.add(a.id, row.key), `已把 ${row.name} 加入 ${a.name}；${restartHint[a.id]}`),
      });
    },
  };

  const dialog = (
    <Modal open={pending !== null} onClose={() => !busy && setPending(null)}>
      {pending && (
        <>
          <h2 className="text-base font-semibold">{pending.title}</h2>
          <ul className="mt-4 list-disc space-y-1.5 pl-5 text-sm leading-6 text-zinc-500">
            {pending.lines.map((l, i) => (
              <li key={i}>{l}</li>
            ))}
          </ul>
          {pending.warn && (
            <p className="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
              {pending.warn}
            </p>
          )}
          {offerAutostart && !pending.danger && (
            <label className="mt-4 flex items-center gap-2 text-sm">
              <input type="checkbox" checked={enableAuto} onChange={(e) => setEnableAuto(e.target.checked)} className="size-4 accent-blue-500" />
              同时开启开机自启（推荐：托管后 Agent 依赖 MCPcell）
            </label>
          )}
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
