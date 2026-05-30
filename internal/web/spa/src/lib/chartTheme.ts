import { useEffect, useState } from 'react';

// Recharts paints SVG via explicit prop values, not CSS classes, so we
// resolve the design tokens to concrete colors at runtime. Re-reads on
// [data-theme] / class changes so charts follow light/dark.

export interface ChartTheme {
  accent: string;
  accentStrong: string;
  success: string;
  warn: string;
  error: string;
  grid: string;
  axis: string;
  text: string;
  tooltipBg: string;
  tooltipBorder: string;
}

const FALLBACK: ChartTheme = {
  accent: '#22c55e',
  accentStrong: '#4ef0a3',
  success: '#3ddc97',
  warn: '#ffb454',
  error: '#ff5a5f',
  grid: 'rgba(255,255,255,0.06)',
  axis: '#3f434b',
  text: '#6e7178',
  tooltipBg: '#11151c',
  tooltipBorder: 'rgba(255,255,255,0.16)',
};

function read(): ChartTheme {
  if (typeof document === 'undefined') return FALLBACK;
  const s = getComputedStyle(document.documentElement);
  const v = (name: string, fallback: string): string => s.getPropertyValue(name).trim() || fallback;
  return {
    accent: v('--accent', FALLBACK.accent),
    accentStrong: v('--accent-strong', FALLBACK.accentStrong),
    success: v('--status-success', FALLBACK.success),
    warn: v('--status-warn', FALLBACK.warn),
    error: v('--status-error', FALLBACK.error),
    grid: v('--border-subtle', FALLBACK.grid),
    axis: v('--text-faint', FALLBACK.axis),
    text: v('--text-muted', FALLBACK.text),
    tooltipBg: v('--bg-elevated', FALLBACK.tooltipBg),
    tooltipBorder: v('--border-strong', FALLBACK.tooltipBorder),
  };
}

export function useChartTheme(): ChartTheme {
  // Lazy initializer: useState(read) reads the tokens synchronously on
  // first render, so no mount effect is needed for initialization — only
  // the MutationObserver needs to subscribe for subsequent theme changes.
  const [theme, setTheme] = useState<ChartTheme>(read);
  useEffect(() => {
    const obs = new MutationObserver(() => setTheme(read()));
    obs.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme', 'class'],
    });
    return () => obs.disconnect();
  }, []);
  return theme;
}
