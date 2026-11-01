import { useEffect, useState, type ReactNode } from 'react';
import { CircleCheck, CircleMinus, Layers, LoaderCircle, Power, ScanSearch, Undo2 } from 'lucide-react';
import { agentBrand, appOf, candidates, isProject, pendingSync } from './agents';
import { useStatus, useUnified } from './data';
import { BrandMark, Logo } from './icons';
import { RunView, summaryText, useTakeoverRun } from './run';
import { agentOrder, mcpView } from './model';
import { agentName } from './timeline';
import { Button, cx } from './ui';

// The guide: what MCPcell does, 一键检测 (read every app's MCPs), 一键托管
// (take over the ones that can be). It covers the whole window on a plain
// background, opens by itself the first time the window opens, and from
// the menus any time after.

const seenKey = 'mcpcell.guide';

function seen(): boolean {
  try {
    return localStorage.getItem(seenKey) === '1';
  } catch {
    return false;
  }
}

function markSeen() {
  try {
    localStorage.setItem(seenKey, '1');
  } catch {
    // it opens again next time
  }
}

/** Opens the guide from anywhere. */
export const openGuide = () => window.dispatchEvent(new Event('mcpcell:guide'));

type Step = 'intro' | 'scan' | 'done';
const steps: { id: Step; label: string }[] = [
  { id: 'intro', label: '介绍' },
  { id: 'scan', label: '检测' },
  { id: 'done', label: '托管' },
];

export function Guide() {
  const { data, reload } = useUnified();
  const { status, refresh } = useStatus();
  const [open, setOpen] = useState(() => !seen());
  const [step, setStep] = useState<Step>('intro');
  const [scanning, setScanning] = useState(false);
  const run = useTakeoverRun();

  useEffect(() => {
    const show = () => {
      setStep('intro');
      setOpen(true);
    };
    window.addEventListener('mcpcell:guide', show);
    return () => window.removeEventListener('mcpcell:guide', show);
  }, []);

  useEffect(() => {
    if (!open) return;
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && close();
    document.addEventListener('keydown', esc);
    return () => document.removeEventListener('keydown', esc);
  });

  if (!open) return null;

  function close() {
    if (run.running) return;
    markSeen();
    setOpen(false);
  }

  async function scan() {
    setStep('scan');
    setScanning(true);
    await Promise.all([reload(), new Promise((r) => setTimeout(r, 600))]);
    setScanning(false);
  }

  // The third step shows the run as it goes (run.tsx).
  async function take(list: ReturnType<typeof candidates>) {
    setStep('done');
    await run.run(list, sync);
    await Promise.all([reload(), refresh()]);
  }

  // What the scan found: each app with its MCPs (Claude Code with its projects).
  const loading = scanning || !data;
  const apps = agentOrder.map((app) => {
    const found = (data?.agents ?? []).filter((a) => a.found && appOf(a.id) === app);
    return {
      app,
      found: found.length > 0,
      total: found.reduce((n, a) => n + a.total, 0),
      projects: found.filter((a) => isProject(a.id)).length,
      path: found.find((a) => !isProject(a.id))?.path,
    };
  });
  const installed = apps.filter((a) => a.found);
  const missing = apps.filter((a) => !a.found);
  const views = data ? data.rows.map((r) => mcpView(r, status)) : [];
  const broken = new Set(views.filter((v) => v.kind === 'broken').map((v) => v.row.key));
  const offered = data ? candidates(data).filter((c) => !broken.has(c.row.key)) : [];
  const sync = !!data?.auto?.sync;
  const toSync = sync && data ? pendingSync(data) : 0;
  const work = offered.length > 0 || toSync > 0;
  const counts = [
    { label: '可托管', n: offered.length, tone: 'text-blue-600 dark:text-blue-400' },
    { label: '已托管', n: views.filter((v) => v.managed).length, tone: 'text-emerald-600 dark:text-emerald-400' },
    { label: '打不开', n: broken.size, tone: 'text-red-600 dark:text-red-400' },
    { label: '已忽略', n: views.filter((v) => v.kind === 'other' || v.kind === 'ignored').length, tone: 'text-zinc-500', tip: '远程、内置和无法启动的' },
  ];
  const brands = [...new Map(agentOrder.map((app) => [agentBrand[app] || app, app])).values()];

  return (
    <div role="dialog" aria-modal="true" aria-label="使用引导" className="fixed inset-0 z-50 flex flex-col bg-zinc-50 dark:bg-zinc-950">
      <header className="grid h-12 shrink-0 grid-cols-[1fr_auto_1fr] items-center px-5">
        <span className="flex items-center gap-2">
          <Logo className="size-5" />
          <span className="text-sm font-semibold tracking-tight">MCPcell</span>
        </span>
        <Steps current={step} />
        {step !== 'done' && (
          <button onClick={close} className="justify-self-end text-[13px] text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200">
            跳过
          </button>
        )}
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex min-h-full w-full max-w-[600px] flex-col justify-center px-6 py-6">
          {step === 'intro' && (
            <div className="flex flex-col items-center text-center">
              <Logo className="size-14" />
              <h1 className="mt-4 text-xl font-semibold tracking-tight">欢迎使用 MCPcell</h1>
              <p className="mt-1.5 text-sm text-zinc-500">本机所有 AI 应用共用一套 MCP</p>
              <div className="mt-4 flex flex-wrap justify-center gap-1.5">
                {brands.map((app) => (
                  <span key={app} title={agentName(app)} className="grid size-8 place-items-center rounded-lg border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
                    <BrandMark brand={agentBrand[app] ?? ''} className="size-4" />
                  </span>
                ))}
              </div>
              <div className="mt-7 grid w-full grid-cols-3 gap-3 text-left">
                <Feature icon={<Layers className="size-4" />} title="同一套 MCP">
                  所有应用都有，全机只开一份
                </Feature>
                <Feature icon={<Power className="size-4" />} title="用时启动">
                  没人用不占内存，空闲自动停止
                </Feature>
                <Feature icon={<Undo2 className="size-4" />} title="随时还原">
                  改前备份，一键还原
                </Feature>
              </div>
              <Button className="mt-8 h-10 px-6" variant="primary" icon={<ScanSearch className="size-4" />} onClick={scan}>
                一键检测
              </Button>
            </div>
          )}

          {step === 'scan' && (
            <>
              <h1 className="text-lg font-semibold tracking-tight">{loading ? '检测中…' : `找到 ${installed.length} 个应用`}</h1>
              <div className="mt-3 grid grid-cols-2 gap-2">
                {(loading ? apps.slice(0, 4) : installed).map((a) => (
                  <div key={a.app} title={a.path} className="flex items-center gap-2.5 rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
                    <BrandMark brand={agentBrand[a.app] ?? ''} className="size-4 shrink-0" />
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{agentName(a.app)}</span>
                    <span className="shrink-0 text-xs text-zinc-500 tabular-nums">
                      {loading ? '…' : `${a.total} 个 MCP${a.projects ? ` · ${a.projects} 个项目` : ''}`}
                    </span>
                  </div>
                ))}
              </div>
              {!loading && missing.length > 0 && (
                <p className="mt-2 truncate text-xs text-zinc-400" title={missing.map((a) => agentName(a.app)).join('、')}>
                  未找到：{missing.map((a) => agentName(a.app)).join('、')}
                </p>
              )}
              {!loading && (
                <>
                  <div className="mt-4 grid grid-cols-4 gap-2">
                    {counts.map((c) => (
                      <div key={c.label} title={c.tip} className="rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
                        <div className={cx('text-xl leading-7 font-semibold tabular-nums', c.n ? c.tone : 'text-zinc-300 dark:text-zinc-600')}>{c.n}</div>
                        <div className="text-xs text-zinc-500">{c.label}</div>
                      </div>
                    ))}
                  </div>
                  {sync && work && <p className="mt-3 text-xs text-zinc-500">托管后同步到所有应用，每个应用都有同一套 MCP</p>}
                  {offered.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {offered.map((c) => (
                        <span key={c.row.key} className="rounded-md bg-blue-50 px-2 py-0.5 text-xs text-blue-700 dark:bg-blue-950/50 dark:text-blue-300">
                          {c.row.name}
                        </span>
                      ))}
                    </div>
                  )}
                </>
              )}
              <div className="mt-6 flex justify-end gap-2">
                <Button disabled={loading} onClick={scan}>
                  重新检测
                </Button>
                {work || loading ? (
                  <Button variant="primary" icon={<Layers className="size-4" />} disabled={loading} onClick={() => take(offered)}>
                    一键托管{offered.length ? ` ${offered.length} 个` : ''}
                  </Button>
                ) : (
                  <Button variant="primary" onClick={close}>
                    完成
                  </Button>
                )}
              </div>
            </>
          )}

          {step === 'done' && (
            <>
              <RunHead run={run} />
              <div className="mt-4">
                <RunView run={run} />
              </div>
              <div className="mt-6 flex justify-end">
                <Button className="h-10 px-6" variant="primary" disabled={!run.summary} onClick={close}>
                  完成
                </Button>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/** What the run is doing, then what it did. */
export function RunHead({ run }: { run: ReturnType<typeof useTakeoverRun> }) {
  const s = run.summary && summaryText(run.summary);
  return (
    <div className="flex items-center gap-3">
      {!s ? (
        <LoaderCircle className="size-8 shrink-0 animate-spin text-blue-600" />
      ) : run.summary!.mcps || run.summary!.added ? (
        <CircleCheck className="size-8 shrink-0 text-emerald-500" />
      ) : (
        <CircleMinus className="size-8 shrink-0 text-zinc-400" />
      )}
      <div className="min-w-0">
        <h1 className="text-lg font-semibold tracking-tight">{s ? s.title : '正在托管…'}</h1>
        <p className="truncate text-[13px] text-zinc-500" title={run.summary?.ignored.join('\n')}>
          {s ? s.line : '试启动每个 MCP 后改写配置'}
        </p>
      </div>
    </div>
  );
}

function Steps({ current }: { current: Step }) {
  const at = steps.findIndex((s) => s.id === current);
  return (
    <ol className="flex items-center gap-2 text-xs">
      {steps.map((s, i) => (
        <li key={s.id} className="flex items-center gap-2">
          <span
            className={cx(
              'grid size-5 shrink-0 place-items-center rounded-full text-[11px] font-medium tabular-nums',
              i <= at ? 'bg-blue-600 text-white' : 'bg-zinc-200 text-zinc-500 dark:bg-zinc-800',
            )}
          >
            {i + 1}
          </span>
          <span className={i <= at ? 'font-medium text-zinc-800 dark:text-zinc-100' : 'text-zinc-400'}>{s.label}</span>
          {i < steps.length - 1 && <span className={cx('h-px w-10', i < at ? 'bg-blue-600' : 'bg-zinc-200 dark:bg-zinc-700')} />}
        </li>
      ))}
    </ol>
  );
}

function Feature({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-zinc-200 bg-white px-4 py-3.5 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="grid size-8 place-items-center rounded-lg bg-blue-50 text-blue-600 dark:bg-blue-950/60 dark:text-blue-400">{icon}</div>
      <div className="mt-2.5 text-[13px] font-medium">{title}</div>
      <div className="mt-0.5 text-xs leading-5 text-zinc-500">{children}</div>
    </div>
  );
}
