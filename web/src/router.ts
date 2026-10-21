import { useEffect, useState } from 'react';

// Hash routes keep the embedded page a plain static bundle. Three levels:
// #/ (overview) › #/mcp, #/skills (lists) › #/mcp/<name>, #/skills/<name>
// (details, shown beside the list). #/mcp/new adds a service, #/settings.
// A query keeps the list's agent filter: #/mcp/context7?agent=codex.

export interface Route {
  parts: string[];
  query: URLSearchParams;
}

function parse(): Route {
  const raw = location.hash.replace(/^#\/?/, '');
  const [path, q = ''] = raw.split('?');
  return {
    parts: path
      .split('/')
      .filter(Boolean)
      .map((p) => decodeURIComponent(p)),
    query: new URLSearchParams(q),
  };
}

export function useRoute(): Route {
  const [route, setRoute] = useState(parse);
  useEffect(() => {
    const onChange = () => setRoute(parse());
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

export function href(parts: string[], query?: Record<string, string | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(query ?? {})) if (v) q.set(k, v);
  const qs = q.toString();
  return '#/' + parts.map(encodeURIComponent).join('/') + (qs ? `?${qs}` : '');
}

export function go(parts: string[], query?: Record<string, string | undefined>) {
  location.hash = href(parts, query);
}

/** Replaces the current history entry (old routes, filters). */
export function redirect(parts: string[], query?: Record<string, string | undefined>) {
  history.replaceState(null, '', href(parts, query));
  window.dispatchEvent(new HashChangeEvent('hashchange'));
}
