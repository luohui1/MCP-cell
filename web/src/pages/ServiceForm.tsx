import { useEffect, useMemo, useState } from 'react';
import { Check, ClipboardPaste, Lightbulb, Plus, TriangleAlert, X } from 'lucide-react';
import { api, type ServiceForm } from '../api';
import { useStatus } from '../App';
import { firstLine } from '../format';
import { ServiceAvatar } from '../icons';
import { presets, type Preset } from '../presets';
import { go } from '../router';
import { Button, Card, cx, Field, IconButton, Input, Switch, Textarea, useToast } from '../ui';

const blank: ServiceForm = { name: '', command: '', args: [], env: {}, cwd: '', idleTimeout: '', memoryLimit: '', flatten: true, catalog: true };

interface EnvRow {
  key: string;
  value: string;
  stored: boolean; // value kept on the daemon, not shown here
}

const idleOptions = [
  { value: '', label: '默认（10 分钟）' },
  { value: '5m', label: '5 分钟' },
  { value: '30m', label: '30 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '0', label: '不回收' },
];

export function ServiceFormPage({ name }: { name?: string }) {
  const editing = !!name;
  const { refresh } = useStatus();
  const toast = useToast();
  const [form, setForm] = useState<ServiceForm>(blank);
  const [argsText, setArgsText] = useState('');
  const [env, setEnv] = useState<EnvRow[]>([]);
  const [preset, setPreset] = useState<Preset | null>(null);
  const [pasteOpen, setPasteOpen] = useState(false);
  const [pasteText, setPasteText] = useState('');
  const [loading, setLoading] = useState(editing);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!name) return;
    api
      .configs()
      .then((all) => {
        const f = all.find((c) => c.name === name);
        if (!f) throw new Error('服务不存在');
        load(f);
      })
      .catch((e) => setError((e as Error).message))
      .finally(() => setLoading(false));
  }, [name]);

  function load(f: Partial<ServiceForm>) {
    setForm((prev) => ({ ...prev, ...f, name: editing ? prev.name || f.name || '' : f.name ?? prev.name }));
    if (f.args) setArgsText(f.args.join('\n'));
    if (f.env) setEnv(Object.entries(f.env).map(([key, v]) => ({ key, value: v ?? '', stored: v === null })));
  }

  function pick(p: Preset | null) {
    setPreset(p);
    if (!p) {
      setForm(blank);
      setArgsText('');
      setEnv([]);
      return;
    }
    setForm({ ...blank, name: p.id, command: p.command });
    setArgsText(p.args.join('\n'));
    setEnv((p.env ?? []).map((key) => ({ key, value: '', stored: false })));
  }

  function applyPaste() {
    try {
      const parsed = parsePasted(pasteText);
      load({ ...parsed, name: editing ? undefined : parsed.name });
      setPreset(null);
      setPasteOpen(false);
      setPasteText('');
      toast('已从 JSON 填入');
    } catch (e) {
      toast((e as Error).message, 'error');
    }
  }

  async function submit() {
    setError('');
    const out: ServiceForm = {
      ...form,
      name: form.name.trim(),
      command: form.command.trim(),
      args: argsText.split('\n').map((a) => a.trim()).filter(Boolean),
      env: Object.fromEntries(env.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.stored && !r.value ? null : r.value])),
    };
    if (!out.name) return setError('请填写服务名称');
    if (!out.command) return setError('请填写启动命令');
    setSaving(true);
    try {
      if (editing) await api.save(out);
      else await api.create(out);
      await refresh();
      toast(editing ? `已保存 ${out.name}，下次调用按新配置启动` : `已添加 ${out.name}`);
      go('services', out.name);
    } catch (e) {
      setError(firstLine((e as Error).message));
    } finally {
      setSaving(false);
    }
  }

  const stateful = preset?.stateful;
  const urlPreview = useMemo(() => (form.name ? `/s/${form.name}` : ''), [form.name]);

  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-20 flex h-[72px] shrink-0 items-center gap-4 border-b border-zinc-200 bg-white/90 px-8 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/90">
        <button
          onClick={() => (editing ? go('services', name!) : go('services'))}
          aria-label="返回"
          className="grid size-10 place-items-center rounded-xl border border-zinc-200 text-zinc-600 hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          <X className="size-[18px]" />
        </button>
        <h1 className="text-xl font-semibold tracking-tight">{editing ? `编辑 ${name}` : '添加 MCP 服务'}</h1>
      </header>

      <div className="mx-auto w-full max-w-[960px] flex-1 px-8 py-6">
        {loading ? (
          <p className="py-16 text-center text-sm text-zinc-400">正在读取配置…</p>
        ) : (
          <Card className="p-7">
            {!editing && (
              <>
                <h2 className="mb-3 text-sm font-medium text-zinc-800 dark:text-zinc-200">预设服务</h2>
                <div className="flex flex-wrap gap-2.5">
                  <Chip active={preset === null} onClick={() => pick(null)}>
                    自定义
                  </Chip>
                  {presets.map((p) => (
                    <Chip key={p.id} active={preset?.id === p.id} onClick={() => pick(p)}>
                      {p.name}
                    </Chip>
                  ))}
                </div>
              </>
            )}
            <div className={cx('flex items-center gap-2 text-sm text-zinc-500', !editing && 'mt-4')}>
              <Lightbulb className="size-4 text-amber-500" />
              <span>{preset?.hint ?? '写法与客户端 mcpServers 里的条目相同；也可以直接粘贴那段 JSON。'}</span>
              <button onClick={() => setPasteOpen((v) => !v)} className="ml-auto inline-flex items-center gap-1.5 text-blue-600 hover:underline dark:text-blue-400">
                <ClipboardPaste className="size-4" />
                粘贴 JSON
              </button>
            </div>
            {pasteOpen && (
              <div className="mt-3 rounded-xl border border-zinc-200 p-3 dark:border-zinc-700">
                <Textarea
                  rows={6}
                  autoFocus
                  placeholder={'"context7": {\n  "command": "npx",\n  "args": ["-y", "@upstash/context7-mcp"]\n}'}
                  value={pasteText}
                  onChange={(e) => setPasteText(e.target.value)}
                />
                <div className="mt-2 flex justify-end gap-2">
                  <Button className="h-9" onClick={() => setPasteOpen(false)}>
                    取消
                  </Button>
                  <Button className="h-9" variant="primary" onClick={applyPaste}>
                    填入表单
                  </Button>
                </div>
              </div>
            )}
            {stateful && (
              <div className="mt-3 flex gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
                <TriangleAlert className="mt-1 size-4 shrink-0" />
                这个服务持有浏览器状态。按会话隔离要到 M3 才实现，目前所有会话会共用同一个浏览器。
              </div>
            )}

            <div className="my-7 flex flex-col items-center gap-2">
              <ServiceAvatar name={form.name} size="lg" />
              {urlPreview && <span className="font-mono text-xs text-zinc-400">{urlPreview}</span>}
            </div>

            <div className="grid grid-cols-2 gap-x-6 gap-y-5">
              <Field label="服务名称" hint="客户端里看到的 MCP 名称，也是接入地址的一部分；保存后不能改">
                <Input value={form.name} disabled={editing} placeholder="例如：context7" onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </Field>
              <Field label="工作目录" hint="留空时为用户目录">
                <Input value={form.cwd} placeholder="可选" onChange={(e) => setForm({ ...form, cwd: e.target.value })} />
              </Field>
              <Field label="启动命令" className="col-span-2" hint="npx、uvx、node，或可执行文件的完整路径。Windows 上原样写 cmd /c npx … 也可以">
                <Input className="font-mono" value={form.command} placeholder="npx" onChange={(e) => setForm({ ...form, command: e.target.value })} />
              </Field>
              <Field label="参数" className="col-span-2" hint="每行一个参数">
                <Textarea rows={3} value={argsText} placeholder={'-y\n@upstash/context7-mcp'} onChange={(e) => setArgsText(e.target.value)} />
              </Field>
              <div className="col-span-2">
                <span className="mb-2 block text-sm font-medium text-zinc-800 dark:text-zinc-200">环境变量</span>
                <div className="space-y-2">
                  {env.map((row, i) => (
                    <div key={i} className="flex gap-2">
                      <Input
                        className="w-[38%] font-mono"
                        placeholder="KEY"
                        value={row.key}
                        onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
                      />
                      <Input
                        className="flex-1 font-mono"
                        type="password"
                        autoComplete="off"
                        placeholder={row.stored ? '已保存，不显示；留空则保持不变' : '值'}
                        value={row.value}
                        onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                      />
                      <IconButton label="删除变量" className="size-11" onClick={() => setEnv(env.filter((_, j) => j !== i))}>
                        <X className="size-4" />
                      </IconButton>
                    </div>
                  ))}
                  <Button className="h-9" variant="ghost" icon={<Plus className="size-4" />} onClick={() => setEnv([...env, { key: '', value: '', stored: false }])}>
                    添加变量
                  </Button>
                </div>
                <span className="mt-1.5 block text-xs text-zinc-500">只传给这个服务；值保存在本机配置文件里，管理页不会再显示。</span>
              </div>
            </div>

            <div className="my-7 h-px bg-zinc-100 dark:bg-zinc-800" />
            <h2 className="mb-4 text-sm font-medium text-zinc-800 dark:text-zinc-200">运行方式</h2>
            <div className="grid grid-cols-2 gap-x-6 gap-y-5">
              <Field label="空闲回收" hint="没有请求后多久结束进程，下次调用自动重启">
                <select
                  value={form.idleTimeout}
                  onChange={(e) => setForm({ ...form, idleTimeout: e.target.value })}
                  className="h-11 w-full rounded-xl border border-zinc-200 bg-white px-3 text-sm outline-none focus:border-blue-400 focus:ring-3 focus:ring-blue-100 dark:border-zinc-700 dark:bg-zinc-900"
                >
                  {idleOptions.some((o) => o.value === form.idleTimeout) ? null : <option value={form.idleTimeout}>{form.idleTimeout}</option>}
                  {idleOptions.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="内存上限" hint="整棵进程树的上限，超出时立即终止；留空不限制">
                <Input value={form.memoryLimit} placeholder="例如：1GB" onChange={(e) => setForm({ ...form, memoryLimit: e.target.value })} />
              </Field>
            </div>
            <div className="mt-5 space-y-3">
              <Toggle
                checked={form.flatten}
                onChange={(v) => setForm({ ...form, flatten: v })}
                title="npm 包直接用 node 启动"
                desc="首次安装到 MCPcell 的包缓存并锁定版本，之后不经过 npx：少一个约 130 MB 的进程，启动也快得多"
              />
              <Toggle
                checked={form.catalog}
                onChange={(v) => setForm({ ...form, catalog: v })}
                title="缓存能力目录"
                desc="连接和列出工具时直接用缓存回答，不启动进程。工具列表会在运行中变化、又不发通知的服务才需要关掉"
              />
            </div>
          </Card>
        )}
      </div>

      <footer className="sticky bottom-0 z-20 border-t border-zinc-200 bg-white/90 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/90">
        <div className="mx-auto flex w-full max-w-[960px] items-center gap-3 px-8 py-4">
          {error && <span className="text-sm text-red-600 dark:text-red-400">{error}</span>}
          <div className="ml-auto flex gap-3">
            <Button onClick={() => (editing ? go('services', name!) : go('services'))}>取消</Button>
            <Button variant="primary" busy={saving} disabled={loading} icon={editing ? <Check className="size-4" /> : <Plus className="size-4" />} onClick={submit}>
              {editing ? '保存' : '添加'}
            </Button>
          </div>
        </div>
      </footer>
    </div>
  );
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cx(
        'h-10 rounded-xl px-4 text-sm transition-colors',
        active ? 'bg-brand font-medium text-white shadow-sm' : 'bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700',
      )}
    >
      {children}
    </button>
  );
}

function Toggle({ checked, onChange, title, desc }: { checked: boolean; onChange: (v: boolean) => void; title: string; desc: string }) {
  return (
    <div className="flex items-center gap-4 rounded-xl border border-zinc-200 px-4 py-3 dark:border-zinc-700">
      <div className="flex-1">
        <div className="text-sm font-medium">{title}</div>
        <div className="mt-0.5 text-xs leading-5 text-zinc-500">{desc}</div>
      </div>
      <Switch checked={checked} onChange={onChange} label={title} />
    </div>
  );
}

/** Reads a client's MCP entry: a bare entry, "name": {...}, or a whole
 * mcpServers / servers / mcp object (the first entry is used). */
export function parsePasted(text: string): Partial<ServiceForm> {
  let value: unknown;
  const trimmed = text.trim().replace(/,\s*$/, '');
  try {
    value = JSON.parse(trimmed);
  } catch {
    try {
      value = JSON.parse(`{${trimmed}}`);
    } catch {
      throw new Error('不是有效的 JSON');
    }
  }
  type Entry = { command?: string | string[]; args?: string[]; env?: Record<string, string>; environment?: Record<string, string>; cwd?: string };
  let obj = value as Record<string, unknown>;
  for (const key of ['mcpServers', 'servers', 'mcp']) {
    if (obj && typeof obj[key] === 'object') obj = obj[key] as Record<string, unknown>;
  }
  let name: string | undefined;
  let entry = obj as Entry;
  if (!('command' in obj)) {
    const found = Object.entries(obj).find(([, v]) => v && typeof v === 'object' && 'command' in (v as object));
    if (!found) throw new Error('没找到 command 字段；HTTP 类型的 MCP 不需要接入 MCPcell');
    [name, entry] = [found[0], found[1] as Entry];
  }
  let command = entry.command ?? '';
  let args = entry.args ?? [];
  if (Array.isArray(command)) [command, args] = [command[0] ?? '', [...command.slice(1), ...args]];
  const env = entry.env ?? entry.environment ?? {};
  return { name, command, args, env, cwd: entry.cwd ?? '' };
}
