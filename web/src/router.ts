import { useEffect, useState } from 'react';

// Hash routes keep the embedded page a plain static bundle: #/mcp,
// #/services/<name>, #/services/<name>/edit, #/services/new, #/skills, #/agents,
// #/sessions, #/settings.

function parse(): string[] {
  return location.hash
    .replace(/^#\/?/, '')
    .split('/')
    .filter(Boolean)
    .map((p) => decodeURIComponent(p));
}

export function useRoute(): string[] {
  const [route, setRoute] = useState(parse);
  useEffect(() => {
    const onChange = () => setRoute(parse());
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

export function go(...parts: string[]) {
  location.hash = '#/' + parts.map(encodeURIComponent).join('/');
}

export function href(...parts: string[]) {
  return '#/' + parts.map(encodeURIComponent).join('/');
}
