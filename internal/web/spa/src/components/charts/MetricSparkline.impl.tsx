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

type Tone = 'accent' | 'success' | 'warn' | 'error';

export default function MetricSparklineImpl({
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
  const [rc, setRc] = useState<Recharts | null>(rechartsCache);

  useEffect(() => {
    if (rc) return;
    rechartsPromise.then(setRc);
  }, [rc]);

  const color =
    tone === 'success'
      ? theme.success
      : tone === 'warn'
        ? theme.warn
        : tone === 'error'
          ? theme.error
          : theme.accent;

  if (data.length < 2) return <div style={{ height }} aria-hidden="true" />;
  if (!rc) return <div style={{ height }} aria-hidden="true" />;

  const { Line, LineChart, ResponsiveContainer } = rc;

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
