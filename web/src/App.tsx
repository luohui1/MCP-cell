import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Layers, Puzzle, Settings as SettingsIcon } from 'lucide-react';
import { api, Unauthorized, type Status } from './api';
import { DataProvider, StatusContext, useStatus, useUnified } from './data';
import { useSkillsShown } from './features';
import { Logo } from './icons';
import { mcpView } from './model';
import { href, redirect, useRoute } from './router';
import { Card, cx, ToastProvider } from './ui';
import { HomePage } from './pages/Home';
import { McpPage } from './pages/Mcp';
import { SettingsPage } from './pages/Settings';
import { SkillsPage } from './pages/Skills';

export function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [offline, setOffline] = useState(false);
  const [expired, setExpired] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setStatus(await api.status());
      setOffline(false);
    } catch (e) {
      if (e instanceof Unauthorized) setExpired(true);
      else setOffline(true);
    }
  }, []);

  useEffect(() => {
    refresh();
    const t = setInterval(() => !document.hidden && refresh(), 2000);
    const onVisible = () => !document.hidden && refresh();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      clearInterval(t);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [refresh]);

  if (expired) return <Notice title="管理页登录已失效" text="在终端运行 mcpcell ui，会用新的一次性链接重新打开。" />;
  if (!status) return <Notice title={offline ? '连不上 MCPcell 守护进程' : '正在连接…'} text={offline ? '确认 mcpcell daemon 仍在运行。' : ''} />;

  return (
    <ToastProvider>
      <StatusContext.Provider value={{ status, offline, refresh }}>
        <DataProvider>
          <div className="flex h-full">
            <Sidebar />
            <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
              {offline && (
                <div role="alert" className="bg-red-600 px-8 py-1.5 text-[13px] text-white">
                  守护进程没有响应，下面是最后一次读到的状态。确认 mcpcell daemon 仍在运行。
                </div>
              )}
              <Routes />
            </main>
          </div>
        </DataProvider>
      </StatusContext.Provider>
    </ToastProvider>
  );
}

function Routes() {
  const { parts, query } = useRoute();
  const [page = '', id, sub] = parts;
  const skills = useSkillsShown();
  // Routes of earlier page layouts still work.
  useEffect(() => {
    if (page === 'services') redirect(id ? ['mcp', id] : [], sub === 'edit' ? { edit: '1' } : undefined);
    else if (page === 'sessions') redirect(['settings'], { diag: '1' });
    else if ((page === 'mcp' && !id) || ['agents', 'clients', 'overview'].includes(page) || (page === 'skills' && !skills)) redirect([]);
  }, [page, id, sub, skills]);
  switch (page) {
    case 'mcp':
      if (id) return <McpPage id={id} />;
      break;
    case 'skills':
      if (skills) return <SkillsPage name={id} agent={query.get('agent') ?? ''} />;
      break;
    case 'settings':
      return <SettingsPage diag={query.get('diag') === '1'} />;
  }
  return <HomePage />;
}

function NavItem({ icon, label, badge, active, to }: { icon: ReactNode; label: string; badge?: number; active: boolean; to: string }) {
  return (
    <a
      href={to}
      aria-current={active ? 'page' : undefined}
      className={cx(
        'flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm transition-colors',
        active
          ? 'bg-white font-medium text-zinc-900 shadow-sm ring-1 ring-zinc-200/80 dark:bg-zinc-800 dark:text-white dark:ring-zinc-700'
          : 'text-zinc-600 hover:bg-zinc-200/50 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800/60 dark:hover:text-zinc-100',
      )}
    >
      <span className={cx('grid size-5 place-items-center', active ? 'text-blue-600 dark:text-blue-400' : 'text-zinc-400')}>{icon}</span>
      <span className="flex-1">{label}</span>
      {!!badge && (
        <span title={`${badge} 个打不开`} className="grid h-5 min-w-5 place-items-center rounded-full bg-red-100 px-1.5 text-[11px] font-semibold text-red-700 tabular-nums dark:bg-red-900/60 dark:text-red-200">
          {badge}
        </span>
      )}
    </a>
  );
}

function Sidebar() {
  const { status } = useStatus();
  const { data } = useUnified();
  const [page = ''] = useRoute().parts;
  const broken = data?.rows.filter((r) => mcpView(r, status, data?.auto).kind === 'broken').length ?? 0;
  const skills = useSkillsShown();
  return (
    <aside className="flex w-52 shrink-0 flex-col border-r border-zinc-200/80 bg-zinc-100/60 dark:border-zinc-800 dark:bg-zinc-900/60">
      <div className="flex h-16 items-center gap-2.5 px-5">
        <Logo className="size-7" />
        <span className="text-base font-semibold tracking-tight">MCPcell</span>
      </div>
      <nav className="space-y-0.5 px-3 pt-1">
        <NavItem icon={<Layers className="size-4" />} label="我的 MCP" badge={broken} active={!['skills', 'settings'].includes(page)} to={href([])} />
        {skills && <NavItem icon={<Puzzle className="size-4" />} label="Skill" active={page === 'skills'} to={href(['skills'])} />}
      </nav>
      <div className="mt-auto border-t border-zinc-200/80 px-3 py-3 dark:border-zinc-800">
        <NavItem icon={<SettingsIcon className="size-4" />} label="设置" active={page === 'settings'} to={href(['settings'])} />
      </div>
    </aside>
  );
}

function Notice({ title, text }: { title: string; text: string }) {
  return (
    <div className="grid h-full place-items-center bg-zinc-50 dark:bg-zinc-950">
      <Card className="w-[420px] p-8 text-center">
        <div className="mx-auto mb-4 w-fit">
          <Logo className="size-12" />
        </div>
        <h1 className="text-lg font-semibold">{title}</h1>
        {text && <p className="mt-2 text-sm leading-6 text-zinc-500">{text}</p>}
      </Card>
    </div>
  );
}
