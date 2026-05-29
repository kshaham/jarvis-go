import { useCallback, useEffect, useSyncExternalStore } from 'react';

// Theme preference: dark-first with an opt-in light theme (the
// [data-theme='light'] block in tokens.css) and a 'system' option.
// Persisted to localStorage; 'system' follows the OS color scheme live.
export type ThemePref = 'dark' | 'light' | 'system';

const KEY = 'office-theme';
const ORDER: ThemePref[] = ['dark', 'light', 'system'];

function getStored(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    if (v === 'dark' || v === 'light' || v === 'system') return v;
  } catch {
    // ignore (private mode / SSR)
  }
  return 'system';
}

function systemIsLight(): boolean {
  return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: light)').matches;
}

function resolve(pref: ThemePref): 'dark' | 'light' {
  return pref === 'system' ? (systemIsLight() ? 'light' : 'dark') : pref;
}

// applyTheme reflects the resolved theme onto <html>: set data-theme
// for light, remove it for dark (the :root default is dark). Mirrors the
// no-flash inline script in index.html.
export function applyTheme(pref: ThemePref): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (resolve(pref) === 'light') root.setAttribute('data-theme', 'light');
  else root.removeAttribute('data-theme');
}

let current: ThemePref = getStored();
const listeners = new Set<() => void>();

function emit(): void {
  for (const l of listeners) l();
}

let watching = false;
function ensureSystemWatch(): void {
  if (watching || typeof matchMedia === 'undefined') return;
  watching = true;
  matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (current === 'system') {
      applyTheme('system');
      emit();
    }
  });
}

export function getThemePref(): ThemePref {
  return current;
}

export function setThemePref(pref: ThemePref): void {
  current = pref;
  try {
    localStorage.setItem(KEY, pref);
  } catch {
    // ignore
  }
  applyTheme(pref);
  emit();
}

// useTheme — read/set the theme preference. `resolved` is the concrete
// dark|light currently shown; `cycle` advances dark → light → system.
export function useTheme() {
  const theme = useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => {
        listeners.delete(cb);
      };
    },
    getThemePref,
    () => 'system' as ThemePref,
  );

  useEffect(() => {
    ensureSystemWatch();
    applyTheme(getThemePref());
  }, []);

  const setTheme = useCallback((p: ThemePref) => setThemePref(p), []);
  const cycle = useCallback(() => {
    setThemePref(ORDER[(ORDER.indexOf(current) + 1) % ORDER.length]);
  }, []);

  return { theme, resolved: resolve(theme), setTheme, cycle };
}
