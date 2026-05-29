import { useReducedMotion } from 'motion/react';
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { useChartTheme } from '@/lib/chartTheme';
import type { MetricPoint } from '@/hooks/useMetricsHistory';

function fmtTime(t: number): string {
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

// CostTokensChart — cost over time as a filled area (left axis) with
// output tokens as a line (right axis). Themed to the design tokens via
// useChartTheme. Parent supplies a bounded height.
export function CostTokensChart({ data }: { data: MetricPoint[] }) {
  const theme = useChartTheme();
  const reduce = useReducedMotion();

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
