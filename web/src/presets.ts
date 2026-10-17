export interface Preset {
  id: string;
  name: string;
  command: string;
  args: string[];
  env?: string[];
  hint?: string;
  /** Holds per-client state; fine to share only until per-session isolation (M3). */
  stateful?: boolean;
}

export const presets: Preset[] = [
  { id: 'context7', name: 'Context7', command: 'npx', args: ['-y', '@upstash/context7-mcp'], hint: '最新的库文档与代码示例' },
  { id: 'shadcn-ui', name: 'shadcn/ui', command: 'npx', args: ['-y', '@jpisnice/shadcn-ui-mcp-server'], hint: 'shadcn/ui 组件、示例与区块' },
  {
    id: 'sequential-thinking',
    name: 'Sequential Thinking',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-sequential-thinking'],
    hint: '分步推理工具',
  },
  { id: 'memory', name: 'Memory', command: 'npx', args: ['-y', '@modelcontextprotocol/server-memory'], hint: '基于知识图谱的长期记忆' },
  {
    id: 'filesystem',
    name: 'Filesystem',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-filesystem', 'D:\\work'],
    hint: '把最后一个参数改成允许访问的目录',
  },
  {
    id: 'playwright',
    name: 'Playwright',
    command: 'npx',
    args: ['@playwright/mcp@latest'],
    hint: '浏览器自动化',
    stateful: true,
  },
  {
    id: 'chrome-devtools',
    name: 'Chrome DevTools',
    command: 'npx',
    args: ['chrome-devtools-mcp@latest'],
    hint: '调试真实的 Chrome',
    stateful: true,
  },
  { id: 'fetch', name: 'Fetch', command: 'uvx', args: ['mcp-server-fetch'], hint: '抓取网页并转成 Markdown，需要安装 uv' },
];
