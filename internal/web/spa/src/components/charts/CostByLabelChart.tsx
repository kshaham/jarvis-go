import { lazy, Suspense } from 'react';
import type { LabelRollup } from '@/api/types';

const CostByLabelChartImpl = lazy(() => import('./CostByLabelChart.impl'));

// CostByLabelChart — horizontal bars of total spend per label, fed by
// useLabelsRollup. Themed to the design tokens via useChartTheme; the
// parent height is bounded so ResponsiveContainer has room. Animation
// is gated off under reduced motion. Recharts is code-split via React.lazy.
export function CostByLabelChart({ rollups }: { rollups: LabelRollup[] }) {
  return (
    <Suspense fallback={<div style={{ height: 280 }} />}>
      <CostByLabelChartImpl rollups={rollups} />
    </Suspense>
  );
}
