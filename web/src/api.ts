// Types mirror the daemon's /api JSON (internal/httpd, internal/pool).

export interface Usage {
  processes: number;
  privateBytes: number;
  workingSetBytes: number;
}

export interface InstanceSnapshot {
  pid: number;
  protocolVersion: string;
  serverName?: string;
  serverVersion?: string;
  startedAt: string;
  lastActiveAt: string;
  inflight: number;
  sessions: number;
  calls: number;
  failures: number;
  usage: Usage;
}

export type ServiceState = 'cold' | 'starting' | 'ready' | 'failed';

export interface Service {
  name: string;
  mode: string;
  state: ServiceState;
  command: string;
  envNames?: string[];
  idleTimeoutSec: number;
  memoryLimit?: number;
  starts: number;
  direct: boolean;
  lean: boolean;
  package?: { name: string; version: string; installedAt: string };
  catalog?: { tools: number; prompts: number; resources: number; fetchedAt: string };
  note?: string;
  lastError?: string;
  lastErrorAt?: string;
  retryAt?: string;
  stderrTail?: string;
  instance?: InstanceSnapshot; // a shared service's instance
  instances?: (InstanceSnapshot & { session: string })[]; // a per-session service's instances
}

/** Every running instance of a service, shared or per session. */
export const instancesOf = (s: Service): InstanceSnapshot[] => (s.instance ? [s.instance] : (s.instances ?? []));

export interface SessionInfo {
  id: string;
  service: string;
  client: string;
  protocolVersion: string;
  createdAt: string;
  lastSeenAt: string;
  inflight: number;
  streaming: boolean;
}

export interface Status {
  version: string;
  pid: number;
  port: number;
  startedAt: string;
  baseUrl: string;
  token: string;
  configPath: string;
  maxInstances: number;
  daemon: Usage;
  services: Service[];
  sessions: SessionInfo[];
}

/** A service definition as the edit form sees it. Env values are never sent
 * to the page: null means "keep what is stored". */
export interface ServiceForm {
  name: string;
  command: string;
  args: string[];
  env: Record<string, string | null>;
  cwd: string;
  mode: string; // shared | per-session
  idleTimeout: string;
  memoryLimit: string;
  flatten: boolean;
  catalog: boolean;
  lean: boolean;
}

export class Unauthorized extends Error {}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    cache: 'no-store',
    // Cookie-authenticated writes must carry this header (CSRF guard).
    headers: { 'X-MCPcell': '1', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) throw new Unauthorized('未授权');
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data as { error?: string }).error || `请求失败（${res.status}）`);
  return data as T;
}

export const api = {
  status: () => call<Status>('GET', '/api/status'),
  configs: () => call<ServiceForm[]>('GET', '/api/configs'),
  create: (f: ServiceForm) => call<ServiceForm>('POST', '/api/services', f),
  save: (f: ServiceForm) => call<ServiceForm>('PUT', `/api/services/${encodeURIComponent(f.name)}`, f),
  remove: (name: string) => call<{ deleted: boolean }>('DELETE', `/api/services/${encodeURIComponent(name)}`),
  start: (name: string) => call<{ started: boolean }>('POST', `/api/services/${encodeURIComponent(name)}/start`),
  stop: (name: string) => call<{ stopped: boolean }>('POST', `/api/services/${encodeURIComponent(name)}/stop`),
  update: (name: string) =>
    call<{ version: string; previous?: string; error?: string }>('POST', `/api/services/${encodeURIComponent(name)}/update`),
};

export type EntryClass = 'shared' | 'stateful' | 'unknown' | 'remote' | 'managed' | 'internal';

export interface ClientEntry {
  client: string;
  name: string;
  kind: string;
  command?: string;
  args?: string[];
  envKeys?: string[];
  url?: string;
  enabled: boolean;
  envVars?: string[];
  class: EntryClass;
  reason: string;
  package?: string;
  service?: string;
  serviceExists: boolean;
  takenOver: boolean;
  takenAt?: string;
  changed: boolean;
}

export interface ClientView {
  id: string;
  name: string;
  path: string;
  found: boolean;
  error?: string;
  entries: ClientEntry[];
}

export interface AutostartStatus {
  supported: boolean;
  enabled: boolean;
  command?: string;
  error?: string;
}

export interface TakeoverResult {
  name: string;
  ok: boolean;
  service?: string;
  created?: boolean;
  error?: string;
}

export const takeover = {
  overview: () => call<{ clients: ClientView[]; autostart: AutostartStatus }>('GET', '/api/clients'),
  attach: (client: string, names: string[], force = false) =>
    call<{ results: TakeoverResult[] }>('POST', `/api/clients/${encodeURIComponent(client)}/attach`, { names, force }),
  restore: (client: string, names: string[], forget = false) =>
    call<{ results: TakeoverResult[] }>('POST', `/api/clients/${encodeURIComponent(client)}/restore${forget ? '?forget=1' : ''}`, { names }),
  autostart: (enabled: boolean) => call<AutostartStatus>('POST', '/api/autostart', { enabled }),
  autostartStatus: () => call<AutostartStatus>('GET', '/api/autostart'),
};

export type CellState = 'managed' | 'native' | 'changed' | 'remote' | 'internal';

export interface AgentCell {
  state: CellState;
  entry: string;
  enabled: boolean;
  added?: boolean;
  class: EntryClass;
  reason?: string;
  takeable?: boolean;
  /** Managed, but the entry was edited since: how it differs from its service. */
  drift?: string;
}

/** One MCP server, merged across MCPcell's services and every agent. */
export interface McpRow {
  key: string;
  name: string;
  kind: string;
  launch: string;
  mode: string; // the service's mode, or the one a takeover would choose
  detail?: string;
  class: EntryClass;
  reason: string;
  service?: string;
  proposed?: string;
  agents: Record<string, AgentCell>;
  canAdd: boolean;
}

export interface AgentSummary {
  id: string;
  name: string;
  path: string;
  found: boolean;
  error?: string;
  total: number;
  managed: number;
  shareable: number;
}

export interface DesktopStatus {
  supported: boolean;
  webview: boolean;
  webviewError?: string;
  desktopShortcut: boolean;
  startMenuShortcut: boolean;
}

export const desktop = {
  status: () => call<DesktopStatus>('GET', '/api/desktop'),
  set: (p: { desktopShortcut?: boolean; startMenuShortcut?: boolean }) => call<DesktopStatus>('POST', '/api/desktop', p),
  open: () => call<{ ok: boolean }>('POST', '/api/window'),
};

export const unified = {
  load: () => call<{ agents: AgentSummary[]; rows: McpRow[]; autostart: AutostartStatus }>('GET', '/api/mcp'),
  add: (client: string, row: string) => call<{ service: string }>('POST', `/api/agents/${encodeURIComponent(client)}/add`, { row }),
};
