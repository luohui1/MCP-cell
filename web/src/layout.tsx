import { useEffect, useRef, type ReactNode } from 'react';
import { ChevronDown, ChevronRight, Search, X } from 'lucide-react';
import { Card, cx, Dot, Menu } from './ui';
import type { Tone } from './model';

// Page pieces: a scrolling body, a toolbar row, a detail column and its
// blocks, and list parts.

/** A scrolling page body: one column sized for a small window. */
export function Body({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[880px] space-y-4 px-5 pt-4 pb-8">{children}</div>
    </div>
  );
}

/** The row at the top of a page: what it shows on the left, actions on the right. */
export function Toolbar({ children, actions }: { children?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex min-h-8 items-center gap-2">
      <div className="flex min-w-0 items-center gap-2">{children}</div>
      <div className="ml-auto flex shrink-0 items-center gap-1.5">{actions}</div>
    </div>
  );
}

/** List on the left, the selected item on the right; each scrolls on its own. */
export function Split({ list, detail }: { list: ReactNode; detail: ReactNode }) {
  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex w-[340px] shrink-0 flex-col border-r border-zinc-200/80 bg-white dark:border-zinc-800 dark:bg-zinc-900/40">{list}</div>
      <div className="min-w-0 flex-1 overflow-y-auto">{detail}</div>
    </div>
  );
}

/** The detail pane's content column. */
export function Detail({ children }: { children: ReactNode }) {
  return <div className="mx-auto w-full max-w-[880px] space-y-4 px-5 pt-4 pb-8">{children}</div>;
}

export function DetailHead({ icon, title, status, actions }: { icon?: ReactNode; title: ReactNode; status?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      {icon}
      <div className="min-w-0 flex-1">
        <h2 className="truncate text-base font-semibold tracking-tight">{title}</h2>
        {status && <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[13px] text-zinc-500">{status}</div>}
      </div>
      <div className="flex shrink-0 items-center gap-2">{actions}</div>
    </div>
  );
}

/** A status sentence with its dot. */
export function StatusLine({ tone, children, title }: { tone: Tone; children: ReactNode; title?: string }) {
  return (
    <span className="flex min-w-0 items-center gap-2" title={title}>
      <Dot tone={tone} />
      <span className={cx('truncate', tone === 'red' && 'text-red-600 dark:text-red-400')}>{children}</span>
    </span>
  );
}

/** A titled block in a detail pane. */
export function Block({ title, extra, children, className, bare }: { title: ReactNode; extra?: ReactNode; children: ReactNode; className?: string; bare?: boolean }) {
  return (
    <section>
      <div className="mb-1.5 flex min-h-6 items-center justify-between gap-4 px-1">
        <h3 className="text-xs font-medium text-zinc-500">{title}</h3>
        {extra}
      </div>
      {bare ? children : <Card className={cx('px-4 py-3', className)}>{children}</Card>}
    </section>
  );
}

// ---- list pane -------------------------------------------------------------

export function ListTools({ children }: { children: ReactNode }) {
  return <div className="space-y-2.5 border-b border-zinc-100 px-4 py-3 dark:border-zinc-800">{children}</div>;
}

export function ListScroll({ children }: { children: ReactNode }) {
  return <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-2.5 pt-3 pb-8">{children}</div>;
}

export function ListGroup({
  title,
  count,
  open,
  onToggle,
  action,
  children,
}: {
  title: string;
  count: number;
  open: boolean;
  onToggle: () => void;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section>
      <div className="flex h-8 items-center gap-2 px-1.5">
        <button
          onClick={onToggle}
          aria-expanded={open}
          className="-ml-0.5 flex min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-[13px] font-semibold text-zinc-700 hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800"
        >
          <ChevronRight className={cx('size-3.5 text-zinc-400 transition-transform', open && 'rotate-90')} />
          {title}
          <span className="font-normal text-zinc-400 tabular-nums">{count}</span>
        </button>
        <div className="ml-auto">{action}</div>
      </div>
      {open && <div className="mt-1 space-y-0.5">{children}</div>}
    </section>
  );
}

export function ListItem({
  href,
  selected,
  icon,
  name,
  tone,
  text,
}: {
  href: string;
  selected: boolean;
  icon?: ReactNode;
  name: string;
  tone: Tone;
  text: string;
}) {
  const ref = useRef<HTMLAnchorElement>(null);
  useEffect(() => {
    if (selected) ref.current?.scrollIntoView({ block: 'nearest' });
  }, [selected]);
  return (
    <a
      ref={ref}
      href={href}
      aria-current={selected ? 'page' : undefined}
      className={cx(
        'flex items-center gap-3 rounded-lg px-2.5 py-2 transition-colors',
        selected ? 'bg-blue-50 ring-1 ring-blue-200 dark:bg-blue-950/50 dark:ring-blue-900' : 'hover:bg-zinc-100/80 dark:hover:bg-zinc-800/60',
      )}
    >
      {icon}
      <div className="min-w-0 flex-1">
        <div className={cx('truncate text-sm', selected ? 'font-semibold text-zinc-900 dark:text-white' : 'font-medium')}>{name}</div>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-zinc-500">
          <Dot tone={tone} />
          <span className={cx('truncate', tone === 'red' && 'text-red-600 dark:text-red-400')} title={text}>
            {text}
          </span>
        </div>
      </div>
    </a>
  );
}

export function SearchBox({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div className="relative">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-zinc-400" />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 w-full rounded-lg border border-zinc-200 bg-zinc-50 pr-7 pl-8 text-[13px] outline-none placeholder:text-zinc-400 focus:border-blue-500 focus:bg-white focus:ring-3 focus:ring-blue-500/15 dark:border-zinc-700 dark:bg-zinc-900"
      />
      {value && (
        <button aria-label="清除" onClick={() => onChange('')} className="absolute top-1/2 right-2 -translate-y-1/2 text-zinc-400 hover:text-zinc-700">
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

export interface AgentOption {
  id: string;
  name: string;
  count: number;
}

/** 全部 plus the main agents as chips; any others under 更多. */
export function AgentFilter({ value, options, total, onChange }: { value: string; options: AgentOption[]; total: number; onChange: (id: string) => void }) {
  const main = options.slice(0, 3);
  const more = options.slice(3);
  const picked = more.find((o) => o.id === value);
  const chip = (active: boolean) =>
    cx(
      'inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-[13px] transition-colors',
      active
        ? 'bg-zinc-900 font-medium text-white dark:bg-white dark:text-zinc-900'
        : 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100',
    );
  return (
    <div className="flex flex-wrap items-center gap-1" role="tablist" aria-label="按 Agent 筛选">
      <button role="tab" aria-selected={!value} className={chip(!value)} onClick={() => onChange('')} title={`${total} 个`}>
        全部
      </button>
      {main.map((o) => (
        <button key={o.id} role="tab" aria-selected={value === o.id} className={chip(value === o.id)} onClick={() => onChange(o.id)} title={`${o.count} 个`}>
          {o.name}
        </button>
      ))}
      {more.length > 0 && (
        <Menu
          align="left"
          items={more.map((o) => ({ label: `${o.name}（${o.count}）`, onSelect: () => onChange(o.id) }))}
          trigger={(toggle) => (
            <button className={chip(!!picked)} onClick={toggle}>
              {picked ? picked.name : '更多'}
              <ChevronDown className="size-3.5 opacity-60" />
            </button>
          )}
        />
      )}
    </div>
  );
}

/** Shown in the detail pane when nothing is selected. */
export function Placeholder({ icon, title, children }: { icon: ReactNode; title: ReactNode; children?: ReactNode }) {
  return (
    <div className="mx-auto flex max-w-[520px] flex-col items-center px-8 pt-24 text-center">
      <div className="grid size-12 place-items-center rounded-2xl bg-zinc-100 text-zinc-400 dark:bg-zinc-800">{icon}</div>
      <div className="mt-4 text-[15px] font-semibold">{title}</div>
      {children && <div className="mt-2 text-sm leading-6 text-zinc-500">{children}</div>}
    </div>
  );
}
