import { useReducedMotion } from 'motion/react';
import { Line, LineChart, ResponsiveContainer } from 'recharts';

import { useChartTheme } from '@/lib/chartTheme';
import type { MetricPoint } from '@/hooks/useMetricsHistory';

type Tone = 'accent' | 'success' | 'warn' | 'error';

// MetricSparkline — a tiny inline trend line under a KPI headline. Needs
// a bounded-height parent (we give it one) or ResponsiveContainer
// collapses to 0. Animation off: it shouldn't fight the NumberFlow
// headline and it keeps re-render cost minimal under SSE.
export function MetricSparkline({
  data,
  dataKey,
  tone = 'accent',
  height = 34,
}: {
  data: MetricPoint[];
  dataKey: keyof MetricPoint;
  tone?: Tone;
  height?: number;
}) {
  const theme = useChartTheme();
  const reduce = useReducedMotion();
  const color =
    tone === 'success'
      ? theme.success
      : tone === 'warn'
        ? theme.warn
        : tone === 'error'
          ? theme.error
          : theme.accent;

  // Need at least two points to draw a line; reserve the space anyway
  // so tiles don't reflow once history accumulates.
  if (data.length < 2) return <div style={{ height }} aria-hidden="true" />;

  return (
    <div style={{ height }} aria-hidden="true">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 3, right: 0, bottom: 3, left: 0 }}>
          <Line
            type="monotone"
            dataKey={dataKey}
            stroke={color}
            strokeWidth={2}
            dot={false}
            isAnimationActive={!reduce}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
