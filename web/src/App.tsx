import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { House, Layers, Puzzle, Settings as SettingsIcon } from 'lucide-react';
import { api, Unauthorized, type Status } from './api';
import { agentsOf } from './agents';
import { DataProvider, StatusContext, useSkills, useStatus, useUnified } from './data';
import { Logo } from './icons';
import { mcpView, skillView } from './model';
import { href, redirect, useRoute } from './router';
import { Card, cx, ToastProvider } from './ui';
import { McpPage } from './pages/Mcp';
import { OverviewPage } from './pages/Overview';
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
  // Routes of the earlier page layout still work.
  useEffect(() => {
    if (page === 'services') redirect(id ? ['mcp', id] : ['mcp'], sub === 'edit' ? { edit: '1' } : undefined);
    else if (page === 'agents' || page === 'clients') redirect([]);
    else if (page === 'sessions') redirect(['settings'], { diag: '1' });
  }, [page, id, sub]);
  switch (page) {
    case 'mcp':
      return <McpPage id={id} agent={query.get('agent') ?? ''} />;
    case 'skills':
      return <SkillsPage name={id} agent={query.get('agent') ?? ''} />;
    case 'settings':
      return <SettingsPage diag={query.get('diag') === '1'} />;
  }
  return <OverviewPage />;
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
        <span title={`${badge} 项需要处理`} className="grid h-5 min-w-5 place-items-center rounded-full bg-amber-100 px-1.5 text-[11px] font-semibold text-amber-800 tabular-nums dark:bg-amber-900/60 dark:text-amber-200">
          {badge}
        </span>
      )}
    </a>
  );
}

function Sidebar() {
  const { status } = useStatus();
  const { data } = useUnified();
  const skills = useSkills().data;
  const [page = ''] = useRoute().parts;
  const agents = data ? agentsOf(data.agents) : [];
  const mcpAttention = data?.rows.filter((r) => mcpView(r, status, agents).group === 'attention').length ?? 0;
  const skillAttention = skills?.skills.filter((k) => skillView(k, skills).group === 'attention').length ?? 0;
  return (
    <aside className="flex w-52 shrink-0 flex-col border-r border-zinc-200/80 bg-zinc-100/60 dark:border-zinc-800 dark:bg-zinc-900/60">
      <div className="flex h-16 items-center gap-2.5 px-5">
        <Logo className="size-7" />
        <span className="text-base font-semibold tracking-tight">MCPcell</span>
      </div>
      <nav className="space-y-0.5 px-3 pt-1">
        <NavItem icon={<House className="size-4" />} label="概览" active={!['mcp', 'skills', 'settings'].includes(page)} to={href([])} />
        <NavItem icon={<Layers className="size-4" />} label="MCP" badge={mcpAttention} active={page === 'mcp'} to={href(['mcp'])} />
        <NavItem icon={<Puzzle className="size-4" />} label="Skill" badge={skillAttention} active={page === 'skills'} to={href(['skills'])} />
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
