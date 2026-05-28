// relativeTime returns "5m", "2h", "3d", "just now". Compact; tuned
// for chronological feeds where a calendar date isn't needed.
export function relativeTime(input: string | number | Date | undefined): string {
  if (!input) return '—';
  const t = typeof input === 'number' ? input : new Date(input).getTime();
  if (!Number.isFinite(t)) return '—';
  const secs = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (secs < 30) return 'just now';
  if (secs < 90) return '1m ago';
  if (secs < 60 * 60) return `${Math.round(secs / 60)}m ago`;
  if (secs < 60 * 60 * 24) return `${Math.round(secs / 3600)}h ago`;
  if (secs < 60 * 60 * 24 * 14) return `${Math.round(secs / 86400)}d ago`;
  return new Date(t).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}
