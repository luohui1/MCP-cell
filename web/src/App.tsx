import { useCallback, useEffect, useState } from 'react';
import { Settings as SettingsIcon } from 'lucide-react';
import { api, Unauthorized, type Status } from './api';
import { DataProvider, StatusContext, useStatus, useUnified } from './data';
import { Guide } from './guide';
import { Logo } from './icons';
import { mcpView } from './model';
import { href, redirect, useRoute } from './router';
import { Card, cx, ToastProvider } from './ui';
import { HomePage } from './pages/Home';
import { ActivityPage } from './pages/Activity';
import { McpPage } from './pages/Mcp';
import { MemoryPage } from './pages/Memory';
import { SettingsPage } from './pages/Settings';

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

  if (expired) return <Notice title="登录已失效" text="运行 mcpcell open 重新打开。" />;
  if (!status) return <Notice title={offline ? '连不上 MCPcell' : '正在连接…'} text={offline ? '运行 mcpcell open 重新打开。' : ''} />;

  return (
    <ToastProvider>
      <StatusContext.Provider value={{ status, offline, refresh }}>
        <DataProvider>
          <div className="flex h-full flex-col">
            <TopBar />
            {offline && (
              <div role="alert" className="bg-red-600 px-5 py-1 text-center text-xs text-white">
                MCPcell 没有响应
              </div>
            )}
            <Routes />
          </div>
          <Guide />
        </DataProvider>
      </StatusContext.Provider>
    </ToastProvider>
  );
}

function Routes() {
  const { parts, query } = useRoute();
  const [page = '', id, sub] = parts;
  // Routes of earlier page layouts still work.
  useEffect(() => {
    if (page === 'services') redirect(id ? ['mcp', id] : [], sub === 'edit' ? { edit: '1' } : undefined);
    else if (page === 'sessions') redirect(['settings'], { diag: '1' });
    else if ((page === 'mcp' && !id) || ['agents', 'clients', 'overview', 'skills'].includes(page)) redirect([]);
  }, [page, id, sub]);
  switch (page) {
    case 'mcp':
      if (id) return <McpPage id={id} />;
      break;
    case 'memory':
      return <MemoryPage />;
    case 'activity':
      return <ActivityPage />;
    case 'settings':
      return <SettingsPage diag={query.get('diag') === '1'} />;
  }
  return <HomePage />;
}

/** The app's one bar: name, the three pages as tabs, settings. */
function TopBar() {
  const { status } = useStatus();
  const { data } = useUnified();
  const [page = ''] = useRoute().parts;
  const broken = data?.rows.filter((r) => mcpView(r, status).kind === 'broken').length ?? 0;
  const tabs = [
    { label: 'MCP', to: href([]), active: page === '' || page === 'mcp', badge: broken },
    { label: '内存', to: href(['memory']), active: page === 'memory' },
    { label: '动态', to: href(['activity']), active: page === 'activity' },
  ];
  return (
    <header className="grid h-12 shrink-0 grid-cols-[1fr_auto_1fr] items-center border-b border-zinc-200/70 bg-white/80 px-4 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/80">
      <a href={href([])} className="flex items-center gap-2 justify-self-start">
        <Logo className="size-5" />
        <span className="text-sm font-semibold tracking-tight">MCPcell</span>
      </a>
      <nav className="flex rounded-lg bg-zinc-100 p-0.5 dark:bg-zinc-800/80">
        {tabs.map((t) => (
          <a
            key={t.label}
            href={t.to}
            aria-current={t.active ? 'page' : undefined}
            className={cx(
              'flex h-7 items-center gap-1.5 rounded-md px-3.5 text-[13px] transition-colors',
              t.active ? 'bg-white font-medium text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white' : 'text-zinc-500 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100',
            )}
          >
            {t.label}
            {!!t.badge && <span title={`${t.badge} 个打不开`} className="size-1.5 rounded-full bg-red-500" />}
          </a>
        ))}
      </nav>
      <a
        href={href(['settings'])}
        title="设置"
        aria-label="设置"
        className={cx(
          'grid size-8 place-items-center justify-self-end rounded-lg transition-colors hover:bg-zinc-100 dark:hover:bg-zinc-800',
          page === 'settings' ? 'text-blue-600 dark:text-blue-400' : 'text-zinc-500',
        )}
      >
        <SettingsIcon className="size-4" />
      </a>
    </header>
  );
}

function Notice({ title, text }: { title: string; text: string }) {
  return (
    <div className="grid h-full place-items-center bg-zinc-50 dark:bg-zinc-950">
      <Card className="w-[340px] p-7 text-center">
        <div className="mx-auto mb-3 w-fit">
          <Logo className="size-10" />
        </div>
        <h1 className="text-base font-semibold">{title}</h1>
        {text && <p className="mt-1.5 text-[13px] text-zinc-500">{text}</p>}
      </Card>
    </div>
  );
}
