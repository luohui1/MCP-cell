import { useEffect, useState } from 'react';

// Features the page keeps but does not show by default. The skill page is
// hidden for now; Settings › 诊断 turns it back on for this browser profile.

const key = 'mcpcell.skills';
const listeners = new Set<(on: boolean) => void>();

export function skillsShown(): boolean {
  try {
    return localStorage.getItem(key) === '1';
  } catch {
    return false;
  }
}

export function showSkills(on: boolean) {
  try {
    if (on) localStorage.setItem(key, '1');
    else localStorage.removeItem(key);
  } catch {
    // storage unavailable: stays hidden
  }
  listeners.forEach((fn) => fn(skillsShown()));
}

export function useSkillsShown(): boolean {
  const [on, setOn] = useState(skillsShown);
  useEffect(() => {
    listeners.add(setOn);
    return () => void listeners.delete(setOn);
  }, []);
  return on;
}
