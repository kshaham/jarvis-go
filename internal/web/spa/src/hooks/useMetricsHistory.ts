import { useEffect, useRef, useState } from 'react';

import { useMetrics } from './useOfficeData';

export interface MetricPoint {
  t: number;
  totalCostUSD: number;
  inputTokens: number;
  outputTokens: number;
  tasksDone: number;
  totalMessages: number;
}

// useMetricsHistory — the server exposes only cumulative scalar metrics
// (no time series), so we accumulate a short rolling window client-side
// as SSE-driven updates arrive. This gives the sparklines + area chart
// real movement. History resets on reload (it lives in component state).
export function useMetricsHistory(cap = 30): MetricPoint[] {
  const metrics = useMetrics();
  const m = metrics.data?.metrics;
  const [series, setSeries] = useState<MetricPoint[]>([]);
  const lastKey = useRef<string>('');

  useEffect(() => {
    if (!m) return;
    const point: MetricPoint = {
      t: Date.now(),
      totalCostUSD: m.totalCostUSD ?? 0,
      inputTokens: m.inputTokens ?? 0,
      outputTokens: m.outputTokens ?? 0,
      tasksDone: m.tasksDone ?? 0,
      totalMessages: m.totalMessages ?? 0,
    };
    // Only append when a tracked value actually changed (query refetch
    // produces a new object even when values are identical).
    const key = `${point.totalCostUSD}|${point.inputTokens}|${point.outputTokens}|${point.tasksDone}|${point.totalMessages}`;
    if (key === lastKey.current) return;
    lastKey.current = key;
    setSeries((prev) => [...prev, point].slice(-cap));
  }, [m, cap]);

  return series;
}
