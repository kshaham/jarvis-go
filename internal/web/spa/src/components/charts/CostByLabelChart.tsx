import { useReducedMotion } from 'motion/react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { useChartTheme } from '@/lib/chartTheme';
import type { LabelRollup } from '@/api/types';

const CHART_HEIGHT = 280;

function fmtCost(value: number): string {
  return `$${value.toFixed(2)}`;
}

// CostByLabelChart — horizontal bars of total spend per label, fed by
// useLabelsRollup. Themed to the design tokens via useChartTheme; the
// parent height is bounded so ResponsiveContainer has room. Animation
// is gated off under reduced motion.
export function CostByLabelChart({ rollups }: { rollups: LabelRollup[] }) {
  const theme = useChartTheme();
  const reduce = useReducedMotion();

  if (rollups.length === 0) return null;

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
