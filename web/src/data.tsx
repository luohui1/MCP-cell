import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { skillsShown } from './features';
import { skillsApi, unified, type AgentSummary, type AutostartStatus, type McpRow, type SkillStatus, type Status } from './api';

// What every page reads: the daemon's status (polled by App), the MCP rows
// merged across agents (polled here) and the skill scan (loaded on demand:
// it reads every skill directory, about a second; the skill page is hidden
// for now, see features.ts).

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

export type Unified = { agents: AgentSummary[]; rows: McpRow[]; autostart: AutostartStatus };

interface Loaded<T> {
  data: T | null;
  error: string;
  reload: () => Promise<void>;
}

const McpContext = createContext<Loaded<Unified>>({ data: null, error: '', reload: async () => {} });
const SkillsContext = createContext<Loaded<SkillStatus> & { stale: (ms: number) => void }>({
  data: null,
  error: '',
  reload: async () => {},
  stale: () => {},
});

/** /api/mcp: every MCP across MCPcell's services and the agents' configurations. */
export const useUnified = () => useContext(McpContext);

/** /api/skills. Call stale(ms) when a page opens to rescan if older than ms. */
export const useSkills = () => useContext(SkillsContext);

export function DataProvider({ children }: { children: ReactNode }) {
  const [mcp, setMcp] = useState<Unified | null>(null);
  const [mcpError, setMcpError] = useState('');
  const reloadMcp = useCallback(async () => {
    try {
      setMcp(await unified.load());
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

  const [skills, setSkills] = useState<SkillStatus | null>(null);
  const [skillsError, setSkillsError] = useState('');
  const loadedAt = useRef(0);
  const loading = useRef<Promise<void> | null>(null);
  const reloadSkills = useCallback(() => {
    loading.current ??= skillsApi
      .load()
      .then((d) => {
        setSkills(d);
        setSkillsError('');
        loadedAt.current = Date.now();
      })
      .catch((e) => setSkillsError((e as Error).message))
      .finally(() => {
        loading.current = null;
      });
    return loading.current;
  }, []);
  const stale = useCallback((ms: number) => {
    if (Date.now() - loadedAt.current > ms) reloadSkills();
  }, [reloadSkills]);
  useEffect(() => {
    if (skillsShown()) reloadSkills(); // hidden for now: scanned when its page opens
  }, [reloadSkills]);

  return (
    <McpContext.Provider value={{ data: mcp, error: mcpError, reload: reloadMcp }}>
      <SkillsContext.Provider value={{ data: skills, error: skillsError, reload: reloadSkills, stale }}>{children}</SkillsContext.Provider>
    </McpContext.Provider>
  );
}
