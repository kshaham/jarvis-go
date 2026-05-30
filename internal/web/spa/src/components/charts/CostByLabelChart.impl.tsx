// This module is only ever loaded via React.lazy. Recharts is loaded
// as a dynamic import so it lands in a separate async chunk (the static
// import would trigger the prefer-dynamic-import lint rule).
import { useEffect, useState } from 'react';
import { useReducedMotion } from 'motion/react';

import { useChartTheme } from '@/lib/chartTheme';
import type { LabelRollup } from '@/api/types';

type Recharts = typeof import('recharts');

// Module-level cache so recharts is only loaded once across all renders.
let rechartsCache: Recharts | null = null;
const rechartsPromise = import('recharts').then((m) => { rechartsCache = m; return m; });

const CHART_HEIGHT = 280;

function fmtCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

export default function CostByLabelChartImpl({ rollups }: { rollups: LabelRollup[] }) {
  const theme = useChartTheme();
  const reduce = useReducedMotion();
  const [rc, setRc] = useState<Recharts | null>(rechartsCache);

  useEffect(() => {
    if (rc) return;
    rechartsPromise.then(setRc);
  }, [rc]);

  if (rollups.length === 0) return null;
  if (!rc) return <div style={{ height: CHART_HEIGHT }} />;

  const { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } = rc;

  return (
    <div style={{ height: CHART_HEIGHT, width: '100%' }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          layout="vertical"
          data={rollups}
          margin={{ top: 4, right: 16, bottom: 4, left: 8 }}
          barCategoryGap="28%"
        >
          <defs>
            <linearGradient id="costByLabelFill" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stopColor={theme.accent} stopOpacity={0.55} />
              <stop offset="100%" stopColor={theme.accentStrong} stopOpacity={1} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke={theme.grid} horizontal={false} />
          <XAxis
            type="number"
            tick={{ fill: theme.text, fontSize: 11 }}
            stroke={theme.grid}
            tickFormatter={fmtCost}
          />
          <YAxis
            type="category"
            dataKey="label"
            tick={{ fill: theme.text, fontSize: 11 }}
            stroke={theme.grid}
            width={120}
          />
          <Tooltip
            cursor={{ fill: theme.grid }}
            contentStyle={{
              background: theme.tooltipBg,
              border: `1px solid ${theme.tooltipBorder}`,
              borderRadius: 10,
              color: 'var(--text-primary)',
              fontSize: 12,
            }}
            formatter={(value) => [fmtCost(Number(value)), 'Cost']}
          />
          <Bar
            dataKey="totalCostUSD"
            name="Cost"
            fill="url(#costByLabelFill)"
            radius={[0, 6, 6, 0]}
            isAnimationActive={!reduce}
          />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
