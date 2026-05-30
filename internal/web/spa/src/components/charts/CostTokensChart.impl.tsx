// This module is only ever loaded via React.lazy. Recharts is loaded
// as a dynamic import so it lands in a separate async chunk (the static
// import would trigger the prefer-dynamic-import lint rule).
import { useEffect, useState } from 'react';
import { useReducedMotion } from 'motion/react';

import { useChartTheme } from '@/lib/chartTheme';
import type { MetricPoint } from '@/hooks/useMetricsHistory';

type Recharts = typeof import('recharts');

// Module-level cache so recharts is only loaded once across all renders.
let rechartsCache: Recharts | null = null;
const rechartsPromise = import('recharts').then((m) => { rechartsCache = m; return m; });

function fmtTime(t: number): string {
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export default function costTokensChart({ data }: { data: MetricPoint[] }) {
  const theme = useChartTheme();
  const reduce = useReducedMotion();
  const [rc, setRc] = useState<Recharts | null>(rechartsCache);

  useEffect(() => {
    if (rc) return;
    rechartsPromise.then(setRc);
  }, [rc]);

  if (data.length < 2) {
    return (
      <div
        className="grid place-items-center text-xs"
        style={{ height: 240, color: 'var(--text-faint)' }}
      >
        Collecting live data… the trend appears as metrics update.
      </div>
    );
  }

  if (!rc) return <div style={{ height: 240 }} />;

  const {
    Area, CartesianGrid, ComposedChart, Line,
    ResponsiveContainer, Tooltip, XAxis, YAxis,
  } = rc;

  return (
    <div style={{ height: 240, width: '100%' }}>
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={data} margin={{ top: 8, right: 8, bottom: 4, left: 0 }}>
          <defs>
            <linearGradient id="costFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={theme.accent} stopOpacity={0.45} />
              <stop offset="100%" stopColor={theme.accent} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke={theme.grid} vertical={false} />
          <XAxis
            dataKey="t"
            tickFormatter={fmtTime}
            tick={{ fill: theme.text, fontSize: 11 }}
            stroke={theme.grid}
            minTickGap={32}
          />
          <YAxis
            yAxisId="cost"
            tick={{ fill: theme.text, fontSize: 11 }}
            stroke={theme.grid}
            width={48}
            tickFormatter={(v: number) => `$${v.toFixed(2)}`}
          />
          <YAxis
            yAxisId="tokens"
            orientation="right"
            tick={{ fill: theme.text, fontSize: 11 }}
            stroke={theme.grid}
            width={48}
            tickFormatter={(v: number) => (v >= 1000 ? `${Math.round(v / 1000)}k` : String(v))}
          />
          <Tooltip
            contentStyle={{
              background: theme.tooltipBg,
              border: `1px solid ${theme.tooltipBorder}`,
              borderRadius: 10,
              color: 'var(--text-primary)',
              fontSize: 12,
            }}
            labelFormatter={(label) => fmtTime(Number(label))}
            formatter={(value, name) =>
              name === 'Cost' ? [`$${Number(value).toFixed(2)}`, name] : [value, name]
            }
          />
          <Area
            yAxisId="cost"
            type="monotone"
            dataKey="totalCostUSD"
            name="Cost"
            stroke={theme.accent}
            strokeWidth={2}
            fill="url(#costFill)"
            isAnimationActive={!reduce}
          />
          <Line
            yAxisId="tokens"
            type="monotone"
            dataKey="outputTokens"
            name="Output tokens"
            stroke={theme.success}
            strokeWidth={2}
            dot={false}
            isAnimationActive={!reduce}
          />
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}
