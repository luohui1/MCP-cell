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
import { ArrowLeft, Check, Copy, Ellipsis, LoaderCircle } from 'lucide-react';

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(' ');
}

type Variant = 'brand' | 'primary' | 'outline' | 'ghost' | 'danger';

const variants: Record<Variant, string> = {
  brand: 'bg-brand text-white shadow-sm hover:bg-brand-hover',
  primary: 'bg-blue-500 text-white shadow-sm hover:bg-blue-600',
  outline:
    'border border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:bg-zinc-800',
  ghost: 'text-zinc-600 hover:bg-zinc-100 dark:text-zinc-300 dark:hover:bg-zinc-800',
  danger: 'bg-red-500 text-white shadow-sm hover:bg-red-600',
};

export function Button({
  variant = 'outline',
  busy,
  icon,
  className,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; busy?: boolean; icon?: ReactNode }) {
  return (
    <button
      {...rest}
      disabled={rest.disabled || busy}
      className={cx(
        'inline-flex h-10 items-center justify-center gap-2 rounded-xl px-4 text-sm font-medium whitespace-nowrap transition-colors',
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
        'grid size-9 place-items-center rounded-lg text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-900',
        'focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-default disabled:opacity-40',
        'dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100',
        className,
      )}
    >
      {busy ? <LoaderCircle className="size-[18px] animate-spin" /> : children}
    </button>
  );
}

type Tone = 'gray' | 'blue' | 'green' | 'amber' | 'red';
const tones: Record<Tone, string> = {
  gray: 'border-zinc-200 bg-white text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300',
  blue: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950 dark:text-blue-300',
  green: 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300',
  amber: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300',
  red: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300',
};

export function Badge({ tone = 'gray', children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span className={cx('inline-flex items-center gap-1 rounded-md border px-1.5 py-px text-xs leading-5 whitespace-nowrap', tones[tone], className)}>
      {children}
    </span>
  );
}

export function Card({ className, children, onClick }: { className?: string; children: ReactNode; onClick?: () => void }) {
  return (
    <div
      onClick={onClick}
      className={cx('rounded-2xl border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900', className)}
    >
      {children}
    </div>
  );
}

export function Section({ title, extra, children }: { title: string; extra?: ReactNode; children: ReactNode }) {
  return (
    <Card className="p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h3 className="text-[15px] font-semibold">{title}</h3>
        {extra}
      </div>
      {children}
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
    <div className="inline-flex rounded-xl bg-zinc-100 p-1 dark:bg-zinc-800" role="tablist">
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={o.value === value}
          onClick={() => onChange(o.value)}
          className={cx(
            'inline-flex h-8 items-center gap-1.5 rounded-lg px-3.5 text-sm transition-colors',
            o.value === value
              ? 'bg-white font-medium text-blue-600 shadow-sm dark:bg-zinc-700 dark:text-blue-300'
              : 'text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-100',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cx(
        'relative h-6 w-10 shrink-0 rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500',
        checked ? 'bg-zinc-800 dark:bg-blue-500' : 'bg-zinc-300 dark:bg-zinc-600',
      )}
    >
      <span
        className={cx('absolute top-0.5 left-0 size-5 rounded-full bg-white shadow transition-transform', checked ? 'translate-x-[18px]' : 'translate-x-0.5')}
      />
    </button>
  );
}

export interface MenuItem {
  label: string;
  icon?: ReactNode;
  danger?: boolean;
  onSelect: () => void;
}

export function Menu({ items, label = '更多' }: { items: (MenuItem | 'sep')[]; label?: string }) {
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
  return (
    <div ref={ref} className="relative" onClick={(e) => e.stopPropagation()}>
      <IconButton label={label} onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <Ellipsis className="size-[18px]" />
      </IconButton>
      {open && (
        <div className="absolute top-10 right-0 z-30 min-w-44 rounded-xl border border-zinc-200 bg-white p-1 shadow-lg dark:border-zinc-700 dark:bg-zinc-900">
          {items.map((it, i) =>
            it === 'sep' ? (
              <div key={i} className="my-1 h-px bg-zinc-100 dark:bg-zinc-800" />
            ) : (
              <button
                key={i}
                onClick={() => {
                  setOpen(false);
                  it.onSelect();
                }}
                className={cx(
                  'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm',
                  it.danger
                    ? 'text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950'
                    : 'text-zinc-700 hover:bg-zinc-100 dark:text-zinc-200 dark:hover:bg-zinc-800',
                )}
              >
                {it.icon}
                {it.label}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}

export function Modal({ open, onClose, children }: { open: boolean; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    if (!open) return;
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', esc);
    return () => document.removeEventListener('keydown', esc);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 p-6" onMouseDown={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        onMouseDown={(e) => e.stopPropagation()}
        className="w-[440px] rounded-2xl border border-zinc-200 bg-white p-6 shadow-xl dark:border-zinc-700 dark:bg-zinc-900"
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
    const t = setTimeout(() => setToast(null), toast.tone === 'error' ? 4000 : 2000);
    return () => clearTimeout(t);
  }, [toast]);
  return (
    <ToastContext.Provider value={show}>
      {children}
      {toast && (
        <div
          role="status"
          className={cx(
            'fixed bottom-6 left-1/2 z-[60] max-w-[560px] -translate-x-1/2 rounded-xl px-4 py-2.5 text-sm text-white shadow-lg',
            toast.tone === 'error' ? 'bg-red-600' : 'bg-zinc-900 dark:bg-zinc-700',
          )}
        >
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
  'w-full rounded-xl border border-zinc-200 bg-white px-3.5 text-sm text-zinc-900 placeholder:text-zinc-400 outline-none transition-colors focus:border-blue-400 focus:ring-3 focus:ring-blue-100 disabled:bg-zinc-50 disabled:text-zinc-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:focus:ring-blue-950';

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={cx(inputBase, 'h-11', props.className)} />;
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
    <header className="sticky top-0 z-20 flex h-[72px] shrink-0 items-center gap-3 border-b border-zinc-200 bg-white/90 px-8 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/90">
      {back && (
        <button
          onClick={back}
          aria-label="返回"
          className="mr-1 grid size-10 place-items-center rounded-xl border border-zinc-200 text-zinc-600 hover:bg-zinc-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
        >
          <ArrowLeft className="size-[18px]" />
        </button>
      )}
      {icon}
      <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
      {subtitle && <span className="text-[15px] text-zinc-500">{subtitle}</span>}
      <div className="ml-auto flex items-center gap-2">{actions}</div>
    </header>
  );
}

export function Dot({ tone }: { tone: 'green' | 'blue' | 'amber' | 'red' | 'gray' }) {
  const c = { green: 'bg-emerald-500', blue: 'bg-blue-500', amber: 'bg-amber-500', red: 'bg-red-500', gray: 'bg-zinc-300 dark:bg-zinc-600' }[tone];
  return <span className={cx('inline-block size-2 shrink-0 rounded-full', c)} />;
}
