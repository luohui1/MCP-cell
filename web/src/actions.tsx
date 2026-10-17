import { useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { api } from './api';
import { useStatus } from './App';
import { firstLine } from './format';
import { go } from './router';
import { Button, Modal, useToast } from './ui';

/** Start / stop / update / delete for services, with busy flags, toasts and
 * a delete confirmation dialog the caller renders. */
export function useServiceActions() {
  const { status, refresh } = useStatus();
  const toast = useToast();
  const [busy, setBusy] = useState<Record<string, boolean>>({});
  const [deleting, setDeleting] = useState<string | null>(null);

  async function run(name: string, task: () => Promise<string>) {
    setBusy((b) => ({ ...b, [name]: true }));
    try {
      toast(await task());
    } catch (e) {
      toast(firstLine((e as Error).message), 'error');
    } finally {
      setBusy((b) => ({ ...b, [name]: false }));
      await refresh();
    }
  }

  const start = (name: string) => run(name, async () => (await api.start(name), `${name} 已启动`));
  const stop = (name: string) => run(name, async () => (await api.stop(name), `已回收 ${name} 的进程`));
  const update = (name: string) => {
    toast(`正在更新 ${name} 的 npm 包，可能需要一分钟…`);
    return run(name, async () => {
      const r = await api.update(name);
      if (r.error) throw new Error(r.error);
      return r.previous && r.previous !== r.version ? `${name} 已从 ${r.previous} 更新到 ${r.version}` : `${name} 已是最新版本 ${r.version}`;
    });
  };

  const sessionsOf = (name: string) => status.sessions.filter((s) => s.service === name).length;

  const deleteDialog = (
    <Modal open={deleting !== null} onClose={() => setDeleting(null)}>
      <div className="flex gap-4">
        <div className="grid size-10 shrink-0 place-items-center rounded-full bg-red-50 text-red-600 dark:bg-red-950">
          <TriangleAlert className="size-5" />
        </div>
        <div>
          <h2 className="text-base font-semibold">删除 {deleting}？</h2>
          <p className="mt-2 text-sm leading-6 text-zinc-500">
            会从 config.json 中移除这个服务
            {deleting && sessionsOf(deleting) > 0 ? `，并断开它的 ${sessionsOf(deleting)} 个会话` : ''}
            。已经改成连 MCPcell 的客户端需要换回原来的配置。
          </p>
        </div>
      </div>
      <div className="mt-6 flex justify-end gap-2">
        <Button onClick={() => setDeleting(null)}>取消</Button>
        <Button
          variant="danger"
          busy={deleting !== null && busy[deleting]}
          onClick={async () => {
            const name = deleting!;
            await run(name, async () => (await api.remove(name), `已删除 ${name}`));
            setDeleting(null);
            go('services');
          }}
        >
          删除
        </Button>
      </div>
    </Modal>
  );

  return { busy, start, stop, update, askDelete: setDeleting, deleteDialog };
}
