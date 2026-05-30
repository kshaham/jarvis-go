import { lazy, Suspense } from 'react';
import type { MetricPoint } from '@/hooks/useMetricsHistory';

type Tone = 'accent' | 'success' | 'warn' | 'error';

const MetricSparklineImpl = lazy(() => import('./MetricSparkline.impl'));

// MetricSparkline — a tiny inline trend line under a KPI headline. Needs
// a bounded-height parent (we give it one) or ResponsiveContainer
// collapses to 0. Recharts is code-split via React.lazy.
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
  return (
    <Suspense fallback={<div style={{ height }} aria-hidden="true" />}>
      <MetricSparklineImpl data={data} dataKey={dataKey} tone={tone} height={height} />
    </Suspense>
  );
}
