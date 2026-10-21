import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import { ArrowLeft, Check, ChevronRight, Copy, Ellipsis, LoaderCircle } from 'lucide-react';

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(' ');
}

type Variant = 'primary' | 'outline' | 'ghost' | 'danger';

const variants: Record<Variant, string> = {
  primary: 'bg-blue-600 text-white shadow-sm shadow-blue-600/20 hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-400',
  outline:
    'border border-zinc-200 bg-white text-zinc-700 shadow-xs hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800',
  ghost: 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800 dark:hover:text-white',
  danger: 'bg-red-600 text-white shadow-sm hover:bg-red-700',
};

export function Button({
  variant = 'outline',
  size = 'md',
  busy,
  icon,
  className,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md'; busy?: boolean; icon?: ReactNode }) {
  return (
    <button
      {...rest}
      disabled={rest.disabled || busy}
      className={cx(
        'inline-flex items-center justify-center font-medium whitespace-nowrap transition-colors',
        size === 'sm' ? 'h-8 gap-1.5 rounded-lg px-3 text-[13px]' : 'h-9 gap-2 rounded-lg px-3.5 text-sm',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 disabled:cursor-default disabled:opacity-50',
        variants[variant],
        className,
      )}
    >
      {busy ? <LoaderCircle className="size-4 animate-spin" /> : icon}
      {children}
    </button>
  );
}

export function IconButton({
  label,
  busy,
  className,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; busy?: boolean }) {
  return (
    <button
      {...rest}
      title={label}
      aria-label={label}
      disabled={rest.disabled || busy}
      className={cx(
        'grid size-8 place-items-center rounded-lg text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-900',
        'focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-default disabled:opacity-40',
        'dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100',
        className,
      )}
    >
      {busy ? <LoaderCircle className="size-4 animate-spin" /> : children}
    </button>
  );
}

type Tone = 'gray' | 'blue' | 'green' | 'amber' | 'red';
const tones: Record<Tone, string> = {
  gray: 'bg-zinc-100 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300',
  blue: 'bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300',
  green: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300',
  amber: 'bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300',
  red: 'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300',
};

export function Badge({ tone = 'gray', children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span className={cx('inline-flex items-center gap-1 rounded-md px-1.5 text-xs leading-5 font-medium whitespace-nowrap', tones[tone], className)}>
      {children}
    </span>
  );
}

export function Card({ className, children, onClick }: { className?: string; children: ReactNode; onClick?: () => void }) {
  return (
    <div
      onClick={onClick}
      className={cx(
        'rounded-2xl border border-zinc-200/80 bg-white shadow-[0_1px_2px_rgba(16,24,40,0.04)] dark:border-zinc-800 dark:bg-zinc-900',
        className,
      )}
    >
      {children}
    </div>
  );
}

/** A titled card for settings and detail pages. */
export function Section({ title, extra, children, className }: { title: string; extra?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section>
      <div className="mb-2.5 flex items-center justify-between gap-4 px-1">
        <h3 className="text-[13px] font-medium text-zinc-500">{title}</h3>
        {extra}
      </div>
      <Card className={cx('p-5', className)}>{children}</Card>
    </section>
  );
}

/** A group heading above a table card; collapsible when onToggle is set. */
export function GroupHeader({
  title,
  count,
  hint,
  open = true,
  onToggle,
  extra,
}: {
  title: string;
  count?: number;
  hint?: ReactNode;
  open?: boolean;
  onToggle?: () => void;
  extra?: ReactNode;
}) {
  const body = (
    <>
      {onToggle && <ChevronRight className={cx('size-4 text-zinc-400 transition-transform', open && 'rotate-90')} />}
      <span className="shrink-0 text-sm font-semibold whitespace-nowrap text-zinc-800 dark:text-zinc-100">{title}</span>
      {count !== undefined && <span className="text-sm text-zinc-400 tabular-nums">{count}</span>}
      {hint && <span className="ml-1 truncate text-[13px] text-zinc-400">{hint}</span>}
    </>
  );
  return (
    <div className="mb-2.5 flex h-7 items-center gap-2 px-1">
      {onToggle ? (
        <button onClick={onToggle} aria-expanded={open} className="-ml-1 flex min-w-0 items-center gap-2 rounded-md px-1 hover:bg-zinc-100 dark:hover:bg-zinc-800">
          {body}
        </button>
      ) : (
        <div className="flex min-w-0 items-center gap-2">{body}</div>
      )}
      <div className="ml-auto flex items-center gap-2">{extra}</div>
    </div>
  );
}

/** Rows separated by hairlines inside one card, like a native table. */
export function TableCard({ children, className }: { children: ReactNode; className?: string }) {
  return <Card className={cx('divide-y divide-zinc-100 overflow-hidden dark:divide-zinc-800', className)}>{children}</Card>;
}

export function Stat({ label, value, sub, icon }: { label: string; value: ReactNode; sub?: ReactNode; icon?: ReactNode }) {
  return (
    <Card className="px-5 py-4">
      <div className="flex items-center gap-2 text-[13px] text-zinc-500">
        {icon}
        {label}
      </div>
      <div className="mt-1.5 text-2xl font-semibold tracking-tight tabular-nums">{value}</div>
      {sub && <div className="mt-0.5 truncate text-xs text-zinc-400">{sub}</div>}
    </Card>
  );
}

export function EmptyState({ icon, title, text, action }: { icon: ReactNode; title: string; text?: ReactNode; action?: ReactNode }) {
  return (
    <Card className="flex flex-col items-center px-8 py-14 text-center">
      <div className="grid size-12 place-items-center rounded-2xl bg-zinc-100 text-zinc-500 dark:bg-zinc-800">{icon}</div>
      <div className="mt-4 text-[15px] font-semibold">{title}</div>
      {text && <p className="mt-1.5 max-w-md text-sm leading-6 text-zinc-500">{text}</p>}
      {action && <div className="mt-5">{action}</div>}
    </Card>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: { value: T; label: ReactNode }[];
  onChange: (v: T) => void;
}) {
  return (
    <div className="inline-flex rounded-lg bg-zinc-100 p-0.5 dark:bg-zinc-800" role="tablist">
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={o.value === value}
          onClick={() => onChange(o.value)}
          className={cx(
            'inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-[13px] transition-colors',
            o.value === value
              ? 'bg-white font-medium text-zinc-900 shadow-sm dark:bg-zinc-700 dark:text-white'
              : 'text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-100',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Switch({
  checked,
  onChange,
  label,
  disabled,
  title,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={title}
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!checked);
      }}
      className={cx(
        'relative h-[22px] w-[38px] shrink-0 rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500',
        'disabled:cursor-default disabled:opacity-40',
        checked ? 'bg-blue-600 dark:bg-blue-500' : 'bg-zinc-300 dark:bg-zinc-600',
      )}
    >
      <span
        className={cx(
          'absolute top-[2px] left-0 size-[18px] rounded-full bg-white shadow-sm transition-transform',
          checked ? 'translate-x-[18px]' : 'translate-x-[2px]',
        )}
      />
    </button>
  );
}

export function Checkbox({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      disabled={disabled}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!checked);
      }}
      className={cx(
        'grid size-[18px] shrink-0 place-items-center rounded-[5px] border transition-colors disabled:cursor-default disabled:opacity-40',
        checked ? 'border-blue-600 bg-blue-600 text-white' : 'border-zinc-300 bg-white dark:border-zinc-600 dark:bg-zinc-900',
      )}
    >
      {checked && <Check className="size-3.5" strokeWidth={3} />}
    </button>
  );
}

export interface MenuItem {
  label: string;
  hint?: string;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

export function Menu({
  items,
  label = '更多',
  trigger,
  header,
  align = 'right',
}: {
  items: (MenuItem | 'sep')[];
  label?: string;
  /** Custom trigger; receives the toggle handler. */
  trigger?: (toggle: () => void, open: boolean) => ReactNode;
  header?: ReactNode;
  align?: 'left' | 'right';
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', esc);
    };
  }, [open]);
  const toggle = () => setOpen((v) => !v);
  return (
    <div ref={ref} className="relative" onClick={(e) => e.stopPropagation()}>
      {trigger ? (
        trigger(toggle, open)
      ) : (
        <IconButton label={label} onClick={toggle} aria-expanded={open}>
          <Ellipsis className="size-[18px]" />
        </IconButton>
      )}
      {open && (
        <div
          className={cx(
            'absolute top-full z-30 mt-1.5 min-w-48 rounded-xl border border-zinc-200 bg-white p-1 shadow-lg shadow-zinc-900/10 dark:border-zinc-700 dark:bg-zinc-900',
            align === 'right' ? 'right-0' : 'left-0',
          )}
        >
          {header && <div className="border-b border-zinc-100 px-3 pt-2 pb-2.5 dark:border-zinc-800">{header}</div>}
          {items.map((it, i) =>
            it === 'sep' ? (
              <div key={i} className="my-1 h-px bg-zinc-100 dark:bg-zinc-800" />
            ) : (
              <button
                key={i}
                disabled={it.disabled}
                onClick={() => {
                  setOpen(false);
                  it.onSelect();
                }}
                className={cx(
                  'flex w-full items-start gap-2.5 rounded-lg px-3 py-2 text-left text-sm disabled:cursor-default disabled:opacity-40',
                  it.danger
                    ? 'text-red-600 enabled:hover:bg-red-50 dark:text-red-400 dark:enabled:hover:bg-red-950'
                    : 'text-zinc-700 enabled:hover:bg-zinc-100 dark:text-zinc-200 dark:enabled:hover:bg-zinc-800',
                )}
              >
                {it.icon && <span className="mt-0.5">{it.icon}</span>}
                <span className="min-w-0">
                  <span className="block whitespace-nowrap">{it.label}</span>
                  {it.hint && <span className="mt-0.5 block text-xs leading-5 text-zinc-400">{it.hint}</span>}
                </span>
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}

export function Modal({
  open,
  onClose,
  children,
  width = 460,
}: {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
  width?: number;
}) {
  useEffect(() => {
    if (!open) return;
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', esc);
    return () => document.removeEventListener('keydown', esc);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-zinc-950/30 p-6 backdrop-blur-[2px]" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        onMouseDown={(e) => e.stopPropagation()}
        style={{ width }}
        className="max-h-[calc(100vh-48px)] overflow-y-auto rounded-2xl border border-zinc-200 bg-white p-6 shadow-2xl shadow-zinc-900/20 dark:border-zinc-700 dark:bg-zinc-900"
      >
        {children}
      </div>
    </div>
  );
}

const ToastContext = createContext<(text: string, tone?: 'ok' | 'error') => void>(() => {});
export const useToast = () => useContext(ToastContext);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<{ text: string; tone: 'ok' | 'error'; id: number } | null>(null);
  const show = useCallback((text: string, tone: 'ok' | 'error' = 'ok') => setToast({ text, tone, id: Date.now() }), []);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), toast.tone === 'error' ? 5000 : 2500);
    return () => clearTimeout(t);
  }, [toast]);
  return (
    <ToastContext.Provider value={show}>
      {children}
      {toast && (
        <div
          role="status"
          className={cx(
            'fixed bottom-6 left-1/2 z-[60] flex max-w-[600px] -translate-x-1/2 items-center gap-2 rounded-xl px-4 py-2.5 text-sm text-white shadow-lg',
            toast.tone === 'error' ? 'bg-red-600' : 'bg-zinc-900 dark:bg-zinc-700',
          )}
        >
          {toast.tone === 'ok' && <Check className="size-4 shrink-0 text-emerald-400" />}
          {toast.text}
        </div>
      )}
    </ToastContext.Provider>
  );
}

export function useCopy() {
  const toast = useToast();
  return async (text: string, what = '已复制') => {
    try {
      await navigator.clipboard.writeText(text);
      toast(what);
    } catch {
      toast('复制失败，请手动选择', 'error');
    }
  };
}

export function CopyButton({ text, label = '复制' }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  const copy = useCopy();
  return (
    <IconButton
      label={label}
      onClick={async (e) => {
        e.stopPropagation();
        await copy(text);
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? <Check className="size-4 text-emerald-600" /> : <Copy className="size-4" />}
    </IconButton>
  );
}

export function CodeBlock({ shown, copy }: { shown: string; copy: string }) {
  return (
    <div className="relative rounded-xl border border-zinc-200 bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-950">
      <pre className="overflow-x-auto px-4 py-3.5 pr-14 font-mono text-[13px] leading-6 text-zinc-800 dark:text-zinc-200">{shown}</pre>
      <div className="absolute top-2 right-2">
        <CopyButton text={copy} />
      </div>
    </div>
  );
}

export function Field({ label, hint, children, className }: { label: string; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <label className={cx('block', className)}>
      <span className="mb-2 block text-sm font-medium text-zinc-800 dark:text-zinc-200">{label}</span>
      {children}
      {hint && <span className="mt-1.5 block text-xs leading-5 text-zinc-500">{hint}</span>}
    </label>
  );
}

const inputBase =
  'w-full rounded-lg border border-zinc-200 bg-white px-3 text-sm text-zinc-900 placeholder:text-zinc-400 outline-none transition-colors focus:border-blue-500 focus:ring-3 focus:ring-blue-500/15 disabled:bg-zinc-50 disabled:text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100';

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cx(inputBase, 'h-10', props.className)} />;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea {...props} className={cx(inputBase, 'py-2.5 font-mono text-[13px] leading-6', props.className)} />;
}

export function PageHeader({
  icon,
  title,
  subtitle,
  back,
  actions,
}: {
  icon?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  back?: () => void;
  actions?: ReactNode;
}) {
  return (
    <header className="sticky top-0 z-20 flex h-16 shrink-0 items-center gap-3 border-b border-zinc-200/80 bg-white/85 px-8 backdrop-blur-md dark:border-zinc-800 dark:bg-zinc-950/85">
      {back && (
        <IconButton label="返回" onClick={back} className="-ml-2 mr-0.5">
          <ArrowLeft className="size-[18px]" />
        </IconButton>
      )}
      {icon}
      <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
      {subtitle && <span className="text-sm text-zinc-500">{subtitle}</span>}
      <div className="ml-auto flex items-center gap-2">{actions}</div>
    </header>
  );
}

export function Dot({ tone, pulse }: { tone: 'green' | 'blue' | 'amber' | 'red' | 'gray'; pulse?: boolean }) {
  const c = { green: 'bg-emerald-500', blue: 'bg-blue-500', amber: 'bg-amber-500', red: 'bg-red-500', gray: 'bg-zinc-300 dark:bg-zinc-600' }[tone];
  return (
    <span className="relative inline-flex size-2 shrink-0">
      {pulse && <span className={cx('absolute inset-0 animate-ping rounded-full opacity-60', c)} />}
      <span className={cx('relative inline-block size-2 rounded-full', c)} />
    </span>
  );
}
