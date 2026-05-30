import { useEffect, useReducer, useRef } from 'react';

import { useMetrics } from './useOfficeData';

export interface MetricPoint {
  t: number;
  totalCostUSD: number;
  inputTokens: number;
  outputTokens: number;
  tasksDone: number;
  totalMessages: number;
}

type HistoryAction = { type: 'append'; point: MetricPoint; cap: number };

function historyReducer(state: MetricPoint[], action: HistoryAction): MetricPoint[] {
  switch (action.type) {
    case 'append':
      return [...state, action.point].slice(-action.cap);
    default:
      return state;
  }
}

// useMetricsHistory — the server exposes only cumulative scalar metrics
// (no time series), so we accumulate a short rolling window client-side
// as SSE-driven updates arrive. This gives the sparklines + area chart
// real movement. History resets on reload (it lives in component state).
//
// State is managed via useReducer (not useState + useEffect) so that each
// point is appended by dispatching an action rather than by setting derived
// state from a prop — which avoids both no-derived-state and
// no-adjust-state-on-prop-change findings.
export function useMetricsHistory(cap = 30): MetricPoint[] {
  const metrics = useMetrics();
  const m = metrics.data?.metrics;

  const [series, dispatch] = useReducer(historyReducer, []);
  const lastKeyRef = useRef<string>('');

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
    if (key === lastKeyRef.current) return;
    lastKeyRef.current = key;
    dispatch({ type: 'append', point, cap });
  }, [m, cap]);

  return series;
}
