import { Bot } from 'lucide-react';
import { brandMarks } from './brand-marks';
import { cx } from './ui';

/** MCPcell mark: one cell whose core serves several sessions. */
export function Logo({ className = 'size-7' }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <defs>
        <linearGradient id="mcpcell-g" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#ff9b45" />
          <stop offset="1" stopColor="#ef5f0f" />
        </linearGradient>
      </defs>
      <path d="M16 2.2 28 9.1v13.8L16 29.8 4 22.9V9.1Z" fill="url(#mcpcell-g)" />
      <path d="M16 16V8.2M16 16l6.8 3.9M16 16l-6.8 3.9" stroke="#fff" strokeWidth="1.8" strokeLinecap="round" opacity=".9" />
      <circle cx="16" cy="16" r="3.6" fill="#fff" />
      <circle cx="16" cy="8.2" r="1.9" fill="#fff" />
      <circle cx="22.8" cy="19.9" r="1.9" fill="#fff" />
      <circle cx="9.2" cy="19.9" r="1.9" fill="#fff" />
    </svg>
  );
}

export function BrandMark({ brand, className = 'size-5' }: { brand: string; className?: string }) {
  const body = brandMarks[brand];
  if (!body) return <Bot className={cx(className, 'text-zinc-500')} />;
  return (
    <svg
      viewBox="0 0 24 24"
      className={cx(className, 'text-zinc-900 dark:text-zinc-100')}
      fill="currentColor"
      aria-hidden="true"
      dangerouslySetInnerHTML={{ __html: body }}
    />
  );
}

/** Maps an MCP clientInfo name to a brand mark. */
export function brandOf(client: string): string {
  const c = client.toLowerCase();
  const table: [string, string][] = [
    ['claude', 'claude'],
    ['codex', 'openai'],
    ['openai', 'openai'],
    ['chatgpt', 'openai'],
    ['cursor', 'cursor'],
    ['copilot', 'copilot'],
    ['visual studio code', 'vscode'],
    ['vscode', 'vscode'],
    ['gemini', 'gemini'],
    ['antigravity', 'gemini'],
    ['windsurf', 'windsurf'],
    ['devin', 'windsurf'],
    ['cline', 'cline'],
    ['kiro', 'kiro'],
    ['opencode', 'opencode'],
  ];
  return table.find(([k]) => c.includes(k))?.[1] ?? '';
}

export function Tile({ children, size = 'md' }: { children: React.ReactNode; size?: 'sm' | 'md' | 'lg' }) {
  return (
    <div
      className={cx(
        'grid shrink-0 place-items-center border border-zinc-200 bg-white shadow-[0_1px_2px_rgba(0,0,0,0.04)] dark:border-zinc-700 dark:bg-zinc-900',
        size === 'lg' ? 'size-20 rounded-3xl' : size === 'sm' ? 'size-10 rounded-xl' : 'size-12 rounded-xl',
      )}
    >
      {children}
    </div>
  );
}

const palette = [
  'bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-300',
  'bg-violet-50 text-violet-600 dark:bg-violet-950 dark:text-violet-300',
  'bg-emerald-50 text-emerald-600 dark:bg-emerald-950 dark:text-emerald-300',
  'bg-amber-50 text-amber-600 dark:bg-amber-950 dark:text-amber-300',
  'bg-rose-50 text-rose-600 dark:bg-rose-950 dark:text-rose-300',
  'bg-cyan-50 text-cyan-600 dark:bg-cyan-950 dark:text-cyan-300',
  'bg-indigo-50 text-indigo-600 dark:bg-indigo-950 dark:text-indigo-300',
  'bg-teal-50 text-teal-600 dark:bg-teal-950 dark:text-teal-300',
];

function hash(s: string) {
  let h = 0;
  for (const ch of s) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return h;
}

/** A stable lettered avatar for an MCP service. */
export function ServiceAvatar({ name, size = 'md' }: { name: string; size?: 'sm' | 'md' | 'lg' }) {
  const letter = (name.match(/[A-Za-z0-9]/)?.[0] ?? '?').toUpperCase();
  return (
    <Tile size={size}>
      <span
        className={cx(
          'grid place-items-center font-semibold',
          palette[hash(name) % palette.length],
          size === 'lg' ? 'size-12 rounded-2xl text-2xl' : size === 'sm' ? 'size-7 rounded-lg text-sm' : 'size-8 rounded-lg text-[15px]',
        )}
      >
        {name ? letter : '+'}
      </span>
    </Tile>
  );
}

export function ClientAvatar({ client }: { client: string }) {
  return (
    <Tile>
      <BrandMark brand={brandOf(client)} className="size-6" />
    </Tile>
  );
}
