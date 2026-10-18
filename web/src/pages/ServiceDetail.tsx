import { useState, type ReactNode } from 'react';
import { ArrowRight, Package, Pencil, Play, Square, Trash2 } from 'lucide-react';
import { useStatus } from '../App';
import { useServiceActions } from '../actions';
import { mainClients, masked } from '../clients';
import { bytes, clock, firstLine, since, span, until } from '../format';
import { ClientAvatar, ServiceAvatar } from '../icons';
import { go, href } from '../router';
import { Badge, Button, Card, CodeBlock, CopyButton, Dot, Menu, PageHeader, Section, Segmented } from '../ui';

export function ServiceDetail({ name }: { name: string }) {
  const { status } = useStatus();
  const actions = useServiceActions();
  const [client, setClient] = useState(mainClients[0].id);
  const s = status.services.find((x) => x.name === name);

  if (!s) {
    return (
      <>
        <PageHeader back={() => go('mcp')} title={name} />
        <p className="px-8 py-16 text-center text-sm text-zinc-500">这个服务不存在或已被删除。</p>
      </>
    );
  }

  const inst = s.instance;
  const sessions = status.sessions.filter((x) => x.service === name);
  const url = `${status.baseUrl}/s/${name}`;
  const spec = mainClients.find((c) => c.id === client)!;
  const busy = actions.busy[name] || s.state === 'starting';
  const stateLine = inst
    ? { tone: 'blue' as const, text: '运行中' }
    : s.state === 'starting'
      ? { tone: 'amber' as const, text: '启动中' }
      : s.state === 'failed'
        ? { tone: 'red' as const, text: '启动失败' }
        : { tone: 'gray' as const, text: '未运行 · 调用时启动' };

  return (
    <>
      <PageHeader
        back={() => go('mcp')}
        icon={<ServiceAvatar name={name} />}
        title={name}
        subtitle={
          <span className="inline-flex items-center gap-1.5">
            <Dot tone={stateLine.tone} />
            {stateLine.text}
          </span>
        }
        actions={
          <>
            {inst ? (
              <Button icon={<Square className="size-4" />} busy={busy} onClick={() => actions.stop(name)}>
                回收进程
              </Button>
            ) : (
              <Button icon={<Play className="size-4" />} busy={busy} onClick={() => actions.start(name)}>
                立即启动
              </Button>
            )}
            <Button icon={<Pencil className="size-4" />} onClick={() => go('services', name, 'edit')}>
              编辑
            </Button>
            <Menu
              items={[
                ...(s.package ? [{ label: '更新 npm 包', icon: <Package className="size-4" />, onSelect: () => actions.update(name) }] : []),
                { label: '删除', icon: <Trash2 className="size-4" />, danger: true, onSelect: () => actions.askDelete(name) },
              ]}
            />
          </>
        }
      />
      <div className="mx-auto w-full max-w-[1100px] space-y-5 px-8 py-6">
        <Section
          title="接入"
          extra={
            <a href={href('clients')} className="inline-flex items-center gap-1 text-sm text-blue-600 hover:underline dark:text-blue-400">
              全部客户端 <ArrowRight className="size-3.5" />
            </a>
          }
        >
          <div className="mb-4 flex items-center gap-2">
            <code className="rounded-lg bg-zinc-100 px-3 py-2 font-mono text-[13px] dark:bg-zinc-800">{url}</code>
            <CopyButton text={url} label="复制接入地址" />
          </div>
          <Segmented value={client} onChange={setClient} options={mainClients.map((c) => ({ value: c.id, label: c.name }))} />
          <p className="mt-4 mb-2 text-sm text-zinc-500">
            替换 <span className="text-zinc-800 dark:text-zinc-200">{spec.file}</span> 里同名的条目，然后重启客户端。工具全名保持不变。
          </p>
          <CodeBlock shown={spec.snippet!([{ name, url }], masked(status.token))} copy={spec.snippet!([{ name, url }], status.token)} />
          <p className="mt-2 text-xs text-zinc-400">复制的内容包含完整访问令牌。</p>
        </Section>

        <Section title="运行状态">
          {inst ? (
            <div className="grid grid-cols-4 gap-3">
              <Stat label="私有提交" value={bytes(inst.usage.privateBytes)} sub={`工作集 ${bytes(inst.usage.workingSetBytes)}`} />
              <Stat label="进程" value={`${inst.usage.processes} 个`} sub={`PID ${inst.pid}${s.direct ? ' · node 直接启动' : ''}`} />
              <Stat label="会话" value={`${sessions.length} 个`} sub={inst.inflight ? `处理中 ${inst.inflight} 个请求` : '没有进行中的请求'} />
              <Stat label="调用" value={`${inst.calls} 次`} sub={inst.failures ? `失败 ${inst.failures} 次` : '没有失败'} />
              <Stat label="已运行" value={since(inst.startedAt)} sub={`空闲 ${since(inst.lastActiveAt)}`} />
              <Stat label="协议" value={inst.protocolVersion} sub={inst.serverName ? `${inst.serverName} ${inst.serverVersion ?? ''}` : '上游未报告名称'} />
              <Stat label="空闲回收" value={s.idleTimeoutSec ? span(s.idleTimeoutSec * 1000) : '不回收'} sub="无请求后自动结束进程" />
              <Stat label="累计启动" value={`${s.starts} 次`} sub={s.memoryLimit ? `内存上限 ${bytes(s.memoryLimit)}` : '未设内存上限'} />
            </div>
          ) : (
            <p className="text-sm leading-6 text-zinc-500">
              {s.state === 'failed'
                ? `启动失败，${s.retryAt ? `${until(s.retryAt)}后可以重试` : '可以手动重试'}。`
                : '当前没有进程。客户端连接和列出工具由能力目录直接回答，第一次调用工具时才会启动。'}
            </p>
          )}
        </Section>

        <Section title="按需与资源">
          <Rows>
            <Row label="能力目录">
              {s.catalog ? (
                <>
                  {[`${s.catalog.tools} 个工具`, s.catalog.prompts && `${s.catalog.prompts} 个提示`, s.catalog.resources && `${s.catalog.resources} 个资源`]
                    .filter(Boolean)
                    .join('、')}
                  <span className="text-zinc-400"> · {since(s.catalog.fetchedAt)}前缓存，连接和列出工具不占进程</span>
                </>
              ) : (
                <span className="text-zinc-500">尚未缓存，首次启动后建立</span>
              )}
            </Row>
            <Row label="启动方式">
              {s.package ? (
                <span className="flex items-center gap-3">
                  <span>
                    <span className="font-mono">{s.package.name}</span> <Badge>{s.package.version}</Badge>
                    <span className="text-zinc-400"> · 已锁定版本，用 node 直接启动，不经过 npx</span>
                  </span>
                  <Button className="h-8 px-3" busy={actions.busy[name]} onClick={() => actions.update(name)}>
                    更新到最新
                  </Button>
                </span>
              ) : (
                <span className="font-mono text-[13px]">{s.command}</span>
              )}
            </Row>
            {s.note && (
              <Row label="提示">
                <span className="text-amber-600 dark:text-amber-400">{s.note}</span>
              </Row>
            )}
            <Row label="Node 内存">{s.lean ? '精简（--max-semi-space-size=1 --optimize-for-size）' : '默认'}</Row>
            <Row label="空闲回收">{s.idleTimeoutSec ? `空闲 ${span(s.idleTimeoutSec * 1000)}后结束进程` : '不回收'}</Row>
            <Row label="内存上限">{s.memoryLimit ? `${bytes(s.memoryLimit)}，超出时立即终止整棵进程树` : '不限制'}</Row>
            {s.envNames && s.envNames.length > 0 && <Row label="环境变量">{s.envNames.join('、')}（值已隐藏）</Row>}
          </Rows>
        </Section>

        {sessions.length > 0 && (
          <Section title={`会话 · ${sessions.length}`}>
            <div className="divide-y divide-zinc-100 dark:divide-zinc-800">
              {sessions.map((x) => (
                <div key={x.id} className="flex items-center gap-4 py-3 first:pt-0 last:pb-0">
                  <ClientAvatar client={x.client} />
                  <div className="min-w-0 flex-1">
                    <div className="font-medium">{x.client || '未知客户端'}</div>
                    <div className="text-sm text-zinc-500">
                      连接于 {clock(x.createdAt)} · 协议 <span className="font-mono">{x.protocolVersion}</span>
                    </div>
                  </div>
                  <div className="text-right text-sm text-zinc-500">
                    <div>最近活动 {since(x.lastSeenAt)}前</div>
                    <div>{x.inflight ? `处理中 ${x.inflight}` : x.streaming ? '已建立通知流' : '空闲'}</div>
                  </div>
                </div>
              ))}
            </div>
          </Section>
        )}

        {s.lastError && (
          <Card className="border-red-200 bg-red-50/40 p-5 dark:border-red-900 dark:bg-red-950/20">
            <h3 className="text-[15px] font-semibold text-red-700 dark:text-red-400">
              最近错误{s.lastErrorAt && <span className="ml-2 text-sm font-normal text-red-500">{since(s.lastErrorAt)}前</span>}
            </h3>
            <p className="mt-2 text-sm text-red-700 dark:text-red-300">{firstLine(s.lastError)}</p>
            {s.stderrTail && (
              <pre className="mt-3 overflow-x-auto rounded-xl bg-white/70 p-3 font-mono text-xs leading-5 whitespace-pre-wrap text-zinc-700 dark:bg-zinc-900 dark:text-zinc-300">
                {s.stderrTail}
              </pre>
            )}
          </Card>
        )}
      </div>
      {actions.deleteDialog}
    </>
  );
}

function Stat({ label, value, sub }: { label: string; value: ReactNode; sub: ReactNode }) {
  return (
    <div className="rounded-xl bg-zinc-50 px-4 py-3 dark:bg-zinc-800/60">
      <div className="text-xs text-zinc-500">{label}</div>
      <div className="mt-1 truncate text-lg font-semibold tabular-nums">{value}</div>
      <div className="mt-0.5 truncate text-xs text-zinc-400">{sub}</div>
    </div>
  );
}

export function Rows({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-zinc-100 dark:divide-zinc-800">{children}</div>;
}

export function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start gap-6 py-3 text-sm first:pt-0 last:pb-0">
      <div className="w-24 shrink-0 text-zinc-500">{label}</div>
      <div className="min-w-0 flex-1 leading-6">{children}</div>
    </div>
  );
}
