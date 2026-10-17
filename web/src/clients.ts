// How each AI client points an MCP entry at an MCPcell endpoint. Formats come
// from the 2026-10-17 client survey (docs/architecture.md §9.4).

export interface Endpoint {
  name: string;
  url: string;
}

export interface ClientSpec {
  id: string;
  name: string;
  brand: string;
  file: string;
  http: boolean;
  note?: string;
  /** Config text for these endpoints, authorizing with token. */
  snippet?: (eps: Endpoint[], token: string) => string;
}

const auth = (t: string) => `Bearer ${t}`;

function jsonEntries(eps: Endpoint[], entry: (e: Endpoint) => object): string {
  const lines = eps.map((e) => {
    const body = JSON.stringify(entry(e), null, 2).replace(/\n/g, '\n  ');
    return `  ${JSON.stringify(e.name)}: ${body}`;
  });
  return `{\n${lines.join(',\n')}\n}`;
}

function tomlTables(eps: Endpoint[], headerKey: string, token: string): string {
  return eps.map((e) => `[mcp_servers.${e.name}]\nurl = "${e.url}"\n${headerKey} = { Authorization = "${auth(token)}" }`).join('\n\n');
}

export const clients: ClientSpec[] = [
  {
    id: 'claude-code',
    name: 'Claude Code',
    brand: 'claude',
    file: '~/.claude.json 的 mcpServers，或项目里的 .mcp.json',
    http: true,
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ type: 'http', url: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'codex',
    name: 'Codex',
    brand: 'openai',
    file: '~/.codex/config.toml（CLI、IDE 插件、桌面版共用）',
    http: true,
    snippet: (eps, t) => tomlTables(eps, 'http_headers', t),
  },
  {
    id: 'cursor',
    name: 'Cursor',
    brand: 'cursor',
    file: '~/.cursor/mcp.json 的 mcpServers',
    http: true,
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ url: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'vscode',
    name: 'VS Code · Copilot',
    brand: 'vscode',
    file: '用户 mcp.json 或 .vscode/mcp.json 的 servers',
    http: true,
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ type: 'http', url: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'antigravity',
    name: 'Antigravity CLI',
    brand: 'gemini',
    file: '~/.gemini/config/mcp_config.json 的 mcpServers',
    http: true,
    note: '取代 Gemini CLI；旧版 Gemini CLI 把 serverUrl 换成 httpUrl',
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ serverUrl: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'windsurf',
    name: 'Devin Desktop · Windsurf',
    brand: 'windsurf',
    file: 'mcp_config.json 的 mcpServers',
    http: true,
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ serverUrl: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'cline',
    name: 'Cline',
    brand: 'cline',
    file: 'cline_mcp_settings.json 的 mcpServers',
    http: true,
    note: '必须写 type，否则会当成旧版 SSE',
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ type: 'streamableHttp', url: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'opencode',
    name: 'OpenCode',
    brand: 'opencode',
    file: 'opencode.json 的 mcp',
    http: true,
    note: 'oauth: false 可以避免它在 401 时去走 OAuth',
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ type: 'remote', url: e.url, headers: { Authorization: auth(t) }, oauth: false })),
  },
  {
    id: 'kiro',
    name: 'Kiro',
    brand: 'kiro',
    file: '~/.kiro/settings/mcp.json 的 mcpServers',
    http: true,
    snippet: (eps, t) => jsonEntries(eps, (e) => ({ url: e.url, headers: { Authorization: auth(t) } })),
  },
  {
    id: 'grok',
    name: 'Grok CLI',
    brand: '',
    file: '~/.grok/config.toml',
    http: true,
    note: 'Grok 也会读取 ~/.claude.json，接管 Claude Code 后通常不用单独配置',
    snippet: (eps, t) => tomlTables(eps, 'headers', t),
  },
  {
    id: 'claude-desktop',
    name: 'Claude Desktop',
    brand: 'claude',
    file: 'claude_desktop_config.json',
    http: false,
    note: '只支持 stdio，需要 MCPcell 的 shim 才能接入（M3）',
  },
];

export const mainClients = clients.filter((c) => ['claude-code', 'codex', 'cursor'].includes(c.id));

export function masked(token: string) {
  return token.slice(0, 6) + '…';
}
