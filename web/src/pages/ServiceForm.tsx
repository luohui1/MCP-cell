import { useEffect, useState, type ReactNode } from 'react';
import { ChevronRight, ClipboardPaste, Lightbulb, Plus, TriangleAlert, X } from 'lucide-react';
import { api, type Service, type ServiceForm } from '../api';
import { useServiceActions } from '../actions';
import { useStatus, useUnified } from '../data';
import { firstLine } from '../format';
import { presets, type Preset } from '../presets';
import { go } from '../router';
import { Badge, Button, Card, cx, Field, IconButton, Input, Segmented, Switch, Textarea, useToast } from '../ui';

// A service's settings: inline in the MCP detail for an existing service,
// or the whole right pane when adding one (with presets and pasted JSON).

const blank: ServiceForm = {
  name: '',
  command: '',
  args: [],
  env: {},
  cwd: '',
  mode: 'shared',
  idleTimeout: '',
  memoryLimit: '',
  flatten: true,
  catalog: true,
  lean: true,
};

interface EnvRow {
  key: string;
  value: string;
  stored: boolean; // value kept on the daemon, not shown here
}

const idleOptions = (mode: string) => [
  { value: '', label: mode === 'per-session' ? '默认（30 分钟）' : '默认（3 分钟）' },
  { value: '10m', label: '10 分钟' },
  { value: '30m', label: '30 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '0', label: '不自动关' },
];

interface Draft {
  form: ServiceForm;
  args: string;
  env: EnvRow[];
}

const draftOf = (f: ServiceForm): Draft => ({
  form: f,
  args: f.args.join('\n'),
  env: Object.entries(f.env).map(([key, v]) => ({ key, value: v ?? '', stored: v === null })),
});

function formOf(d: Draft): ServiceForm {
  return {
    ...d.form,
    name: d.form.name.trim(),
    command: d.form.command.trim(),
    args: d.args
      .split('\n')
      .map((a) => a.trim())
      .filter(Boolean),
    env: Object.fromEntries(d.env.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.stored && !r.value ? null : r.value])),
  };
}

/** The settings of an existing service, saved in place. */
export function ServiceSettings({ name, svc, openAdvanced }: { name: string; svc?: Service; openAdvanced?: boolean }) {
  const { refresh } = useStatus();
  const { reload } = useUnified();
  const toast = useToast();
  const actions = useServiceActions();
  const [saved, setSaved] = useState<Draft | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [advanced, setAdvanced] = useState(!!openAdvanced);

  useEffect(() => {
    setSaved(null);
    setDraft(null);
    setError('');
    api
      .configs()
      .then((all) => {
        const f = all.find((c) => c.name === name);
        if (!f) throw new Error('config.json 里没有这个服务');
        setSaved(draftOf(f));
        setDraft(draftOf(f));
      })
      .catch((e) => setError((e as Error).message));
  }, [name]);

  if (!draft || !saved) return <Card className="p-5 text-sm text-zinc-400">{error || '正在读取配置…'}</Card>;
  const dirty = JSON.stringify(formOf(draft)) !== JSON.stringify(formOf(saved));

  async function save() {
    setError('');
    const out = formOf(draft!);
    if (!out.command) return setError('请填写启动命令');
    setSaving(true);
    try {
      const back = await api.save(out);
      setSaved(draftOf(back));
      setDraft(draftOf(back));
      await Promise.all([refresh(), reload()]);
      toast(`已保存 ${name}，下次打开时按新设置`);
    } catch (e) {
      setError(firstLine((e as Error).message));
    } finally {
      setSaving(false);
    }
  }

  const set = (f: Partial<ServiceForm>) => setDraft({ ...draft, form: { ...draft.form, ...f } });
  return (
    <Card className="divide-y divide-zinc-100 dark:divide-zinc-800">
      <RunFields form={draft.form} set={set} />
      {svc?.package && (
        <SettingRow label="版本" hint="版本已固定，用 node 直接打开，不经过 npx">
          <div className="flex items-center gap-3">
            <span className="min-w-0 truncate font-mono text-[13px]">{svc.package.name}</span>
            <Badge>{svc.package.version}</Badge>
            <Button size="sm" className="ml-auto" busy={actions.busy[name]} onClick={() => actions.update(name)}>
              更新到最新
            </Button>
          </div>
        </SettingRow>
      )}
      <div>
        <button onClick={() => setAdvanced(!advanced)} className="flex w-full items-center gap-2 px-5 py-3 text-left text-sm text-zinc-600 hover:bg-zinc-50 dark:text-zinc-300 dark:hover:bg-zinc-800/40">
          <ChevronRight className={cx('size-4 shrink-0 text-zinc-400 transition-transform', advanced && 'rotate-90')} />
          <span className="shrink-0">启动命令与其他选项</span>
          {!advanced && <span className="ml-2 min-w-0 truncate font-mono text-xs text-zinc-400">{svc?.command ?? draft.form.command}</span>}
        </button>
        {advanced && (
          <div className="px-5 pb-5">
            <LaunchFields draft={draft} setDraft={setDraft} />
            <Toggles form={draft.form} set={set} />
          </div>
        )}
      </div>
      {(dirty || error) && (
        <div className="flex items-center gap-3 bg-zinc-50/80 px-5 py-3 dark:bg-zinc-900">
          {error ? <span className="text-sm text-red-600 dark:text-red-400">{error}</span> : <span className="text-[13px] text-zinc-500">有未保存的修改</span>}
          <div className="ml-auto flex gap-2">
            <Button size="sm" disabled={!dirty || saving} onClick={() => (setDraft(saved), setError(''))}>
              放弃修改
            </Button>
            <Button size="sm" variant="primary" busy={saving} disabled={!dirty} onClick={save}>
              保存
            </Button>
          </div>
        </div>
      )}
    </Card>
  );
}

/** Adding a service: a preset, pasted JSON or typed by hand. */
export function NewService() {
  const { refresh } = useStatus();
  const { reload } = useUnified();
  const toast = useToast();
  const [draft, setDraft] = useState<Draft>(draftOf(blank));
  const [preset, setPreset] = useState<Preset | null>(null);
  const [pasteOpen, setPasteOpen] = useState(false);
  const [pasteText, setPasteText] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  function pick(p: Preset | null) {
    setPreset(p);
    if (!p) return setDraft(draftOf(blank));
    setDraft({
      form: { ...blank, name: p.id, command: p.command, mode: p.stateful ? 'per-session' : 'shared' },
      args: p.args.join('\n'),
      env: (p.env ?? []).map((key) => ({ key, value: '', stored: false })),
    });
  }

  function applyPaste() {
    try {
      const parsed = parsePasted(pasteText);
      setDraft((d) => ({
        form: { ...d.form, ...parsed, name: parsed.name ?? d.form.name },
        args: (parsed.args ?? []).join('\n'),
        env: Object.entries(parsed.env ?? {}).map(([key, v]) => ({ key, value: v ?? '', stored: false })),
      }));
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
    const out = formOf(draft);
    if (!out.name) return setError('请填写名称');
    if (!out.command) return setError('请填写启动命令');
    setSaving(true);
    try {
      await api.create(out);
      await Promise.all([refresh(), reload()]);
      toast(`已添加 ${out.name}`);
      go(['mcp', out.name]);
    } catch (e) {
      setError(firstLine((e as Error).message));
    } finally {
      setSaving(false);
    }
  }

  const set = (f: Partial<ServiceForm>) => setDraft({ ...draft, form: { ...draft.form, ...f } });
  return (
    <>
      <Card className="p-5">
        <div className="mb-3 text-sm font-medium">从预设开始</div>
        <div className="flex flex-wrap gap-2">
          <Chip active={preset === null} onClick={() => pick(null)}>
            自定义
          </Chip>
          {presets.map((p) => (
            <Chip key={p.id} active={preset?.id === p.id} onClick={() => pick(p)}>
              {p.name}
            </Chip>
          ))}
        </div>
        <div className="mt-4 flex items-center gap-2 text-sm text-zinc-500">
          <Lightbulb className="size-4 shrink-0 text-amber-500" />
          <span>{preset?.hint ?? '写法和应用配置里 mcpServers 的一项相同；也可以直接粘贴那段 JSON。'}</span>
          <button onClick={() => setPasteOpen((v) => !v)} className="ml-auto inline-flex shrink-0 items-center gap-1.5 text-blue-600 hover:underline dark:text-blue-400">
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
              <Button size="sm" onClick={() => setPasteOpen(false)}>
                取消
              </Button>
              <Button size="sm" onClick={applyPaste}>
                填入
              </Button>
            </div>
          </div>
        )}
        {preset?.stateful && draft.form.mode !== 'per-session' && (
          <div className="mt-3 flex gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
            <TriangleAlert className="mt-1 size-4 shrink-0" />
            它会开着浏览器，建议选「每个对话单独一份」；共用一份时所有对话会用同一个浏览器。
          </div>
        )}
      </Card>

      <Card className="p-5">
        <Field label="名称" hint="应用里看到的 MCP 名称，也是接入地址的一部分；保存后不能改">
          <Input value={draft.form.name} placeholder="例如：context7" onChange={(e) => set({ name: e.target.value })} />
        </Field>
        <div className="mt-5">
          <LaunchFields draft={draft} setDraft={setDraft} />
        </div>
      </Card>

      <Card className="divide-y divide-zinc-100 dark:divide-zinc-800">
        <RunFields form={draft.form} set={set} />
        <div className="px-5 py-4">
          <Toggles form={draft.form} set={set} />
        </div>
      </Card>

      <div className="flex items-center gap-3">
        {error && <span className="text-sm text-red-600 dark:text-red-400">{error}</span>}
        <div className="ml-auto flex gap-2">
          <Button onClick={() => go(['mcp'])}>取消</Button>
          <Button variant="primary" busy={saving} icon={<Plus className="size-4" />} onClick={submit}>
            添加
          </Button>
        </div>
      </div>
    </>
  );
}

function SettingRow({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] items-start gap-4 px-5 py-3.5">
      <div className="pt-1.5 text-sm text-zinc-500">{label}</div>
      <div className="min-w-0">
        {children}
        {hint && <div className="mt-1.5 text-xs leading-5 text-zinc-400">{hint}</div>}
      </div>
    </div>
  );
}

function RunFields({ form, set }: { form: ServiceForm; set: (f: Partial<ServiceForm>) => void }) {
  return (
    <>
      <SettingRow
        label="运行方式"
        hint={
          form.mode === 'per-session'
            ? '每个对话单独开一份，各记各的（浏览器、连接）；用到时才打开，对话结束就关掉'
            : '所有对话共用一份，适合不为各对话记东西的 MCP'
        }
      >
        <Segmented<string>
          value={form.mode || 'shared'}
          onChange={(v) => set({ mode: v })}
          options={[
            { value: 'shared', label: '所有对话共用一份' },
            { value: 'per-session', label: '每个对话单独一份' },
          ]}
        />
      </SettingRow>
      <SettingRow label="多久关掉" hint="没人用多久之后自动关掉；再用到时自动打开">
        <select
          value={form.idleTimeout}
          onChange={(e) => set({ idleTimeout: e.target.value })}
          className="h-9 w-56 rounded-lg border border-zinc-200 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-3 focus:ring-blue-500/15 dark:border-zinc-700 dark:bg-zinc-900"
        >
          {idleOptions(form.mode).some((o) => o.value === form.idleTimeout) ? null : <option value={form.idleTimeout}>{form.idleTimeout}</option>}
          {idleOptions(form.mode).map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </SettingRow>
      <SettingRow label="内存上限" hint="它和它开的子程序加起来最多占多少内存，超过就立即停掉；留空不限制">
        <Input className="!h-9 w-56" value={form.memoryLimit} placeholder="例如：1GB" onChange={(e) => set({ memoryLimit: e.target.value })} />
      </SettingRow>
    </>
  );
}

function LaunchFields({ draft, setDraft }: { draft: Draft; setDraft: (d: Draft) => void }) {
  const { form, env } = draft;
  const setEnv = (rows: EnvRow[]) => setDraft({ ...draft, env: rows });
  return (
    <div className="grid grid-cols-2 gap-x-6 gap-y-5">
      <Field label="启动命令" className="col-span-2" hint="npx、uvx、node，或可执行文件的完整路径。Windows 上原样写 cmd /c npx … 也可以">
        <Input className="font-mono" value={form.command} placeholder="npx" onChange={(e) => setDraft({ ...draft, form: { ...form, command: e.target.value } })} />
      </Field>
      <Field label="参数" className="col-span-2" hint="每行一个参数">
        <Textarea rows={3} value={draft.args} placeholder={'-y\n@upstash/context7-mcp'} onChange={(e) => setDraft({ ...draft, args: e.target.value })} />
      </Field>
      <div className="col-span-2">
        <span className="mb-2 block text-sm font-medium text-zinc-800 dark:text-zinc-200">环境变量</span>
        <div className="space-y-2">
          {env.map((row, i) => (
            <div key={i} className="flex gap-2">
              <div className="w-[38%] shrink-0">
                <Input className="font-mono" placeholder="KEY" value={row.key} onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))} />
              </div>
              <div className="min-w-0 flex-1">
                <Input
                  className="font-mono"
                  type="password"
                  autoComplete="off"
                  placeholder={row.stored ? '已保存，不显示；留空则保持不变' : '值'}
                  value={row.value}
                  onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                />
              </div>
              <IconButton label="删除变量" className="size-10" onClick={() => setEnv(env.filter((_, j) => j !== i))}>
                <X className="size-4" />
              </IconButton>
            </div>
          ))}
          <Button size="sm" variant="ghost" icon={<Plus className="size-4" />} onClick={() => setEnv([...env, { key: '', value: '', stored: false }])}>
            添加变量
          </Button>
        </div>
        <span className="mt-1.5 block text-xs text-zinc-500">只传给这个 MCP；值保存在本机配置文件里，页面上不再显示。</span>
      </div>
      <Field label="工作目录" className="col-span-2" hint="留空时为用户目录">
        <Input value={form.cwd} placeholder="可选" onChange={(e) => setDraft({ ...draft, form: { ...form, cwd: e.target.value } })} />
      </Field>
    </div>
  );
}

function Toggles({ form, set }: { form: ServiceForm; set: (f: Partial<ServiceForm>) => void }) {
  return (
    <div className="mt-5 space-y-2.5">
      <Toggle
        checked={form.flatten}
        onChange={(v) => set({ flatten: v })}
        title="npm 包直接用 node 启动"
        desc="第一次用时装到 MCPcell 里并固定版本，之后不经过 npx：少开一个约 130 MB 的进程，打开也快得多"
      />
      <Toggle
        checked={form.lean}
        onChange={(v) => set({ lean: v })}
        title="精简 Node 内存"
        desc="给 Node.js 写的 MCP 加上 --max-semi-space-size=1 --optimize-for-size，实测内存少 27%–38%，功能不变"
      />
      <Toggle
        checked={form.catalog}
        onChange={(v) => set({ catalog: v })}
        title="记住工具列表"
        desc="应用连上来、问有哪些工具时，直接用记下的列表回答，不用先把它打开。工具列表会在运行中变、又不通知的 MCP 才需要关掉"
      />
    </div>
  );
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={cx(
        'h-8 rounded-lg border px-3 text-[13px] transition-colors',
        active
          ? 'border-zinc-900 bg-zinc-900 font-medium text-white dark:border-white dark:bg-white dark:text-zinc-900'
          : 'border-zinc-200 bg-white text-zinc-600 hover:border-zinc-300 hover:text-zinc-900 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
      )}
    >
      {children}
    </button>
  );
}

function Toggle({ checked, onChange, title, desc }: { checked: boolean; onChange: (v: boolean) => void; title: string; desc: string }) {
  return (
    <div className="flex items-center gap-6 rounded-xl border border-zinc-200 px-4 py-3 dark:border-zinc-700">
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
