const RECENT_KEY = 'jarvis.spawn.recentWorkdirs';
const RECENT_MAX = 5;

function loadRecents(): string[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) {
      return parsed.filter((s): s is string => typeof s === 'string' && s.length > 0);
    }
  } catch {
    // ignore
  }
  return [];
}

export { loadRecents };

export function recordRecentWorkdir(path: string) {
  if (typeof window === 'undefined' || !path.trim()) return;
  const current = loadRecents().filter((p) => p !== path);
  const next = [path, ...current].slice(0, RECENT_MAX);
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    // ignore
  }
}
