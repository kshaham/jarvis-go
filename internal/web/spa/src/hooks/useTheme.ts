import { useCallback, useEffect, useSyncExternalStore } from 'react';

// Theme preference: dark-first with opt-in light + blue themes (the
// [data-theme='light'] / [data-theme='blue'] blocks in tokens.css) and a
// 'system' option. Persisted to localStorage; 'system' follows the OS
// color scheme live (system never resolves to blue — that's an explicit pick).
export type ThemePref = 'dark' | 'light' | 'blue' | 'graphite' | 'system';

const KEY = 'office-theme';
const ORDER: ThemePref[] = ['dark', 'light', 'blue', 'graphite', 'system'];

function getStored(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    if (v === 'dark' || v === 'light' || v === 'blue' || v === 'graphite' || v === 'system') return v;
  } catch {
    // ignore (private mode / SSR)
  }
  return 'system';
}

function systemIsLight(): boolean {
  return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: light)').matches;
}

// The concrete [data-theme] attribute value for a preference (null = the
// dark :root default). Blue + graphite are their own attributes; system maps to
// light/dark.
function themeAttr(pref: ThemePref): 'light' | 'blue' | 'graphite' | null {
  if (pref === 'blue') return 'blue';
  if (pref === 'graphite') return 'graphite';
  if (pref === 'light') return 'light';
  if (pref === 'system') return systemIsLight() ? 'light' : null;
  return null; // dark
}

// resolve — the concrete light|dark a preference reads as, for any consumer
// that only cares about light-vs-dark. Blue is light; graphite is a dark theme.
function resolve(pref: ThemePref): 'dark' | 'light' {
  if (pref === 'blue') return 'light';
  if (pref === 'graphite') return 'dark';
  return pref === 'system' ? (systemIsLight() ? 'light' : 'dark') : pref;
}

// applyTheme reflects the preference onto <html> via the data-theme
// attribute (removed for dark). Mirrors the no-flash inline script in
// index.html.
export function applyTheme(pref: ThemePref): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  const attr = themeAttr(pref);
  if (attr) root.setAttribute('data-theme', attr);
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

function getThemePref(): ThemePref {
  return current;
}

function setThemePref(pref: ThemePref): void {
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
