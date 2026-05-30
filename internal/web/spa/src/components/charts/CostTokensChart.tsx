import { lazy, Suspense } from 'react';
import type { MetricPoint } from '@/hooks/useMetricsHistory';

const CostTokensChartImpl = lazy(() => import('./CostTokensChart.impl'));

// CostTokensChart — cost over time as a filled area (left axis) with
// output tokens as a line (right axis). Themed to the design tokens via
// useChartTheme. Parent supplies a bounded height. Recharts is code-split
// via React.lazy.
export function CostTokensChart({ data }: { data: MetricPoint[] }) {
  return (
    <Suspense fallback={<div style={{ height: 240 }} />}>
      <CostTokensChartImpl data={data} />
    </Suspense>
  );
}
