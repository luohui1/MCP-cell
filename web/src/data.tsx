import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { autoApi, unified, type AgentSummary, type AutoStatus, type AutostartStatus, type McpRow, type Status } from './api';

// What every page reads: the daemon's status (polled by App) and the MCP rows
// merged across agents (polled here).

interface StatusCtx {
  status: Status;
  offline: boolean;
  refresh: () => Promise<void>;
}

export const StatusContext = createContext<StatusCtx | null>(null);

export function useStatus(): StatusCtx {
  const ctx = useContext(StatusContext);
  if (!ctx) throw new Error('status not loaded');
  return ctx;
}

export type Unified = { agents: AgentSummary[]; rows: McpRow[]; autostart: AutostartStatus; auto?: AutoStatus };

interface Loaded<T> {
  data: T | null;
  error: string;
  reload: () => Promise<void>;
}

const McpContext = createContext<Loaded<Unified>>({ data: null, error: '', reload: async () => {} });

/** /api/mcp: every MCP across MCPcell's services and the agents' configurations. */
export const useUnified = () => useContext(McpContext);

export function DataProvider({ children }: { children: ReactNode }) {
  const [mcp, setMcp] = useState<Unified | null>(null);
  const [mcpError, setMcpError] = useState('');
  const reloadMcp = useCallback(async () => {
    try {
      const [data, auto] = await Promise.all([unified.load(), autoApi.status().catch(() => undefined)]);
      setMcp({ ...data, auto });
      setMcpError('');
    } catch (e) {
      setMcpError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    reloadMcp();
    const t = setInterval(() => !document.hidden && reloadMcp(), 4000);
    return () => clearInterval(t);
  }, [reloadMcp]);

  return (
    <McpContext.Provider value={{ data: mcp, error: mcpError, reload: reloadMcp }}>{children}</McpContext.Provider>
  );
}

/** Loads now and every ms while the page is visible. */
export function usePolled<T>(load: () => Promise<T>, ms: number): T | null {
  const [data, setData] = useState<T | null>(null);
  const run = useCallback(() => load().then(setData, () => undefined), [load]);
  useEffect(() => {
    run();
    const t = setInterval(() => !document.hidden && run(), ms);
    return () => clearInterval(t);
  }, [run, ms]);
  return data;
}
