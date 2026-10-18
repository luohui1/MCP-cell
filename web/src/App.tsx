import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { Activity, Bot, Layers, Settings as SettingsIcon } from 'lucide-react';
import { api, instancesOf, Unauthorized, type Status } from './api';
import { bytes, since } from './format';
import { Logo } from './icons';
import { go, useRoute } from './router';
import { Card, cx, Dot, ToastProvider } from './ui';
import { McpPage } from './pages/Mcp';
import { AgentsPage } from './pages/Agents';
import { ServiceDetail } from './pages/ServiceDetail';
import { ServiceFormPage } from './pages/ServiceForm';
import { SessionsPage } from './pages/Sessions';
import { SettingsPage } from './pages/Settings';

interface StatusCtx {
  status: Status;
  refresh: () => Promise<void>;
}

const StatusContext = createContext<StatusCtx | null>(null);

export function useStatus(): StatusCtx {
  const ctx = useContext(StatusContext);
  if (!ctx) throw new Error('status not loaded');
  return ctx;
}

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
      <StatusContext.Provider value={{ status, refresh }}>
        <div className="flex h-full">
          <Sidebar status={status} offline={offline} />
          <main className="flex min-w-0 flex-1 flex-col overflow-y-auto">
            <Routes />
          </main>
        </div>
      </StatusContext.Provider>
    </ToastProvider>
  );
}

function Routes() {
  const route = useRoute();
  const [page = 'mcp', id, sub] = route;
  switch (page) {
    case 'services':
      if (id === 'new') return <ServiceFormPage />;
      if (id && sub === 'edit') return <ServiceFormPage name={id} />;
      if (id) return <ServiceDetail name={id} />;
      return <McpPage />;
    case 'agents':
    case 'clients':
      return <AgentsPage />;
    case 'sessions':
      return <SessionsPage />;
    case 'settings':
      return <SettingsPage />;
  }
  return <McpPage />;
}

function NavItem({ icon, label, count, active, onClick }: { icon: ReactNode; label: string; count?: number; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={cx(
        'flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm transition-colors',
        active
          ? 'bg-white font-medium text-zinc-900 shadow-sm ring-1 ring-zinc-200/80 dark:bg-zinc-800 dark:text-white dark:ring-zinc-700'
          : 'text-zinc-600 hover:bg-zinc-200/50 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800/60 dark:hover:text-zinc-100',
      )}
    >
      <span className={cx('grid size-5 place-items-center', active ? 'text-blue-600 dark:text-blue-400' : 'text-zinc-400')}>{icon}</span>
      <span className="flex-1">{label}</span>
      {count !== undefined && count > 0 && <span className="text-xs font-normal text-zinc-400 tabular-nums">{count}</span>}
    </button>
  );
}

function Sidebar({ status, offline }: { status: Status; offline: boolean }) {
  const [page = 'mcp'] = useRoute();
  const running = status.services.flatMap(instancesOf).length;
  return (
    <aside className="flex w-56 shrink-0 flex-col border-r border-zinc-200/80 bg-zinc-100/60 dark:border-zinc-800 dark:bg-zinc-900/60">
      <div className="flex h-16 items-center gap-2.5 px-5">
        <Logo className="size-7" />
        <span className="text-base font-semibold tracking-tight">MCPcell</span>
      </div>
      <nav className="space-y-0.5 px-3 pt-1">
        <NavItem icon={<Layers className="size-4" />} label="MCP" active={page === 'mcp' || page === 'services'} onClick={() => go('mcp')} />
        <NavItem icon={<Bot className="size-4" />} label="Agents" active={page === 'agents' || page === 'clients'} onClick={() => go('agents')} />
        <NavItem icon={<Activity className="size-4" />} label="会话" count={status.sessions.length} active={page === 'sessions'} onClick={() => go('sessions')} />
      </nav>
      <div className="mt-auto px-3 pb-3">
        <Card className="mb-3 px-3.5 py-3">
          <div className="flex items-center gap-2 text-[13px] font-medium">
            <Dot tone={offline ? 'red' : 'green'} pulse={!offline} />
            {offline ? '守护进程无响应' : '后台运行中'}
          </div>
          <div className="mt-1 text-xs leading-5 text-zinc-500">
            {running} 个实例 · {bytes(status.daemon.privateBytes)} · {since(status.startedAt)}
          </div>
        </Card>
        <div className="border-t border-zinc-200/80 pt-3 dark:border-zinc-800">
          <NavItem icon={<SettingsIcon className="size-4" />} label="设置" active={page === 'settings'} onClick={() => go('settings')} />
        </div>
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
