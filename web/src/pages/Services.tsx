import { useState } from 'react';
import { ChevronRight, Copy, Gauge, Package, Pencil, Play, Plus, RefreshCw, Server, Square, Trash2 } from 'lucide-react';
import type { Service, Status } from '../api';
import { useStatus } from '../App';
import { useServiceActions } from '../actions';
import { bytes, firstLine, since, until } from '../format';
import { ServiceAvatar } from '../icons';
import { go } from '../router';
import { Badge, Button, Card, cx, Dot, IconButton, Menu, PageHeader, Segmented, useCopy } from '../ui';

type Filter = 'all' | 'running' | 'idle';

export function ServicesPage() {
  const { status, refresh } = useStatus();
  const [filter, setFilter] = useState<Filter>('all');
  const actions = useServiceActions();
  const copy = useCopy();

  const running = status.services.filter((s) => s.instance);
  const shown = status.services.filter((s) => (filter === 'all' ? true : filter === 'running' ? !!s.instance : !s.instance));
  const mcpBytes = running.reduce((n, s) => n + s.instance!.usage.privateBytes, 0);
  const procs = running.reduce((n, s) => n + s.instance!.usage.processes, 0);

  return (
    <>
      <PageHeader
        icon={<Server className="size-6 text-zinc-700 dark:text-zinc-300" />}
        title="MCP 服务"
        subtitle={`${status.services.length} 个`}
        actions={
          <>
            <Button variant="brand" icon={<Plus className="size-[18px]" />} onClick={() => go('services', 'new')}>
              添加服务
            </Button>
            <Menu
              items={[
                { label: '刷新', icon: <RefreshCw className="size-4" />, onSelect: refresh },
                { label: '复制访问令牌', icon: <Copy className="size-4" />, onSelect: () => copy(status.token, '已复制访问令牌') },
              ]}
            />
          </>
        }
      />
      <div className="mx-auto w-full max-w-[1200px] px-8 py-6">
        <div className="mb-5 flex items-center gap-4">
          <Segmented<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: 'all', label: `全部 ${status.services.length}` },
              { value: 'running', label: <>运行中 {running.length}{running.length > 0 && <Dot tone="blue" />}</> },
              { value: 'idle', label: `未运行 ${status.services.length - running.length}` },
            ]}
          />
          <div className="ml-auto flex items-center gap-2 text-sm text-zinc-500">
            <Gauge className="size-4" />
            MCP 内存 <b className="font-medium text-zinc-800 dark:text-zinc-200">{bytes(mcpBytes)}</b>
            <span className="text-zinc-300">·</span>
            {procs} 个进程
            <span className="text-zinc-300">·</span>
            {status.sessions.length} 个会话
          </div>
        </div>

        {status.services.length === 0 ? (
          <Empty />
        ) : (
          <div className="space-y-3">
            {shown.map((s) => (
              <ServiceCard key={s.name} s={s} status={status} actions={actions} />
            ))}
            {shown.length === 0 && <p className="py-16 text-center text-sm text-zinc-400">没有符合条件的服务</p>}
          </div>
        )}
      </div>
      {actions.deleteDialog}
    </>
  );
}

function ServiceCard({ s, status, actions }: { s: Service; status: Status; actions: ReturnType<typeof useServiceActions> }) {
  const copy = useCopy();
  const sessions = status.sessions.filter((x) => x.service === s.name).length;
  const inst = s.instance;
  const busy = actions.busy[s.name] || s.state === 'starting';

  let line1: React.ReactNode = '调用时启动';
  let line2: React.ReactNode = sessions ? `${sessions} 个会话已连接` : s.catalog ? '连接和列出工具不占进程' : '首次启动后缓存能力目录';
  if (inst) {
    line1 = (
      <>
        <b className="font-medium text-zinc-800 dark:text-zinc-200">{bytes(inst.usage.privateBytes)}</b> · {inst.usage.processes} 个进程
      </>
    );
    line2 = `${sessions} 个会话 · ${inst.inflight ? `处理中 ${inst.inflight}` : `空闲 ${since(inst.lastActiveAt)}`}`;
  } else if (s.state === 'starting') {
    line1 = '正在启动…';
    line2 = s.package ? '' : '首次运行 npx 服务会先安装 npm 包';
  } else if (s.state === 'failed') {
    line1 = <span className="text-red-600 dark:text-red-400">{firstLine(s.lastError)}</span>;
    line2 = s.retryAt ? `${until(s.retryAt)}后可重试` : '';
  }

  return (
    <Card
      onClick={() => go('services', s.name)}
      className={cx(
        'group flex cursor-pointer items-center gap-4 px-5 py-4 transition-all',
        inst
          ? 'border-blue-500 bg-[#e9f2ff] ring-1 ring-blue-500/30 hover:bg-[#e2edff] dark:border-blue-500 dark:bg-blue-950/40'
          : s.state === 'failed'
            ? 'border-red-200 bg-red-50/30 dark:border-red-900 dark:bg-red-950/20'
            : 'hover:border-zinc-300 hover:shadow-[0_2px_8px_rgba(0,0,0,0.04)] dark:hover:border-zinc-700',
      )}
    >
      <ServiceAvatar name={s.name} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-base font-semibold">{s.name}</span>
          <Badge>共享</Badge>
          {s.package && <Badge>npm {s.package.version}</Badge>}
          {s.catalog && <Badge>{s.catalog.tools} 个工具</Badge>}
          {s.state === 'failed' && <Badge tone="red">启动失败</Badge>}
          {s.note && <Badge tone="amber">经 npx 运行</Badge>}
        </div>
        <div className="mt-1 truncate font-mono text-[13px] text-zinc-500" title={s.command}>
          {s.command}
        </div>
      </div>
      <div className="max-w-[300px] shrink-0 text-right text-sm leading-6 text-zinc-500">
        <div className="truncate">{line1}</div>
        {line2 && <div className="truncate">{line2}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-0.5" onClick={(e) => e.stopPropagation()}>
        {inst ? (
          <>
            <span className="mr-2 ml-3 inline-flex items-center gap-1.5 text-sm font-medium text-blue-600 dark:text-blue-400">
              <Dot tone="blue" />
              运行中
            </span>
            <IconButton label="回收进程（会话保留，下次调用自动启动）" busy={busy} onClick={() => actions.stop(s.name)}>
              <Square className="size-4" />
            </IconButton>
          </>
        ) : (
          <IconButton label="立即启动" busy={busy} onClick={() => actions.start(s.name)}>
            <Play className="size-[18px]" />
          </IconButton>
        )}
        <IconButton label="编辑" onClick={() => go('services', s.name, 'edit')}>
          <Pencil className="size-[17px]" />
        </IconButton>
        <Menu
          items={[
            { label: '详情与接入', icon: <ChevronRight className="size-4" />, onSelect: () => go('services', s.name) },
            { label: '复制接入地址', icon: <Copy className="size-4" />, onSelect: () => copy(`${status.baseUrl}/s/${s.name}`, '已复制接入地址') },
            ...(s.package ? [{ label: '更新 npm 包', icon: <Package className="size-4" />, onSelect: () => actions.update(s.name) }] : []),
            'sep' as const,
            { label: '删除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => actions.askDelete(s.name) },
          ]}
        />
      </div>
    </Card>
  );
}

function Empty() {
  return (
    <Card className="px-8 py-16 text-center">
      <div className="mx-auto grid size-14 place-items-center rounded-2xl bg-zinc-100 text-zinc-500 dark:bg-zinc-800">
        <Server className="size-7" />
      </div>
      <h2 className="mt-5 text-lg font-semibold">还没有 MCP 服务</h2>
      <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-zinc-500">
        添加后，所有客户端会话共用同一个进程；连接时不启动，调用时才启动，空闲后自动回收。
      </p>
      <Button variant="brand" className="mt-6" icon={<Plus className="size-[18px]" />} onClick={() => go('services', 'new')}>
        添加服务
      </Button>
    </Card>
  );
}
