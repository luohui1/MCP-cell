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
  package?: { name: string; version: string; installedAt: string };
  catalog?: { tools: number; prompts: number; resources: number; fetchedAt: string };
  note?: string;
  lastError?: string;
  lastErrorAt?: string;
  retryAt?: string;
  stderrTail?: string;
  instance?: InstanceSnapshot;
}

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
  idleTimeout: string;
  memoryLimit: string;
  flatten: boolean;
  catalog: boolean;
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
