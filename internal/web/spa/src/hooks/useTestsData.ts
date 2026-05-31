// useTestsData — the Tests page data layer. Reads per-project coverage +
// vulnerability results (GET /api/tests/results), exposes Run/Scan mutations
// (POST /api/tests/{coverage,scan}), and invalidates the query on the four
// tests:* SSE events so a running→done transition refreshes the tiles live.
import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { TestProject } from '@/api/types';

const TESTS_KEY = ['tests', 'results'] as const;

export function useTestsResults() {
  const qc = useQueryClient();

  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (name.startsWith('tests:')) {
        qc.invalidateQueries({ queryKey: TESTS_KEY });
      }
    });
    return () => sub.close();
  }, [qc]);

  const query = useQuery<{ projects: TestProject[] }>({
    queryKey: TESTS_KEY,
    queryFn: ({ signal }) => fetchJSON<{ projects: TestProject[] }>('/api/tests/results', { signal }),
  });

  const runCoverage = useMutation({
    mutationFn: (projectId: string) =>
      fetchJSON('/api/tests/coverage', { method: 'POST', body: { projectId } }),
    // Optimistic refetch so the tile flips to "running" immediately; the SSE
    // complete event refreshes again with the final value.
    onSuccess: () => qc.invalidateQueries({ queryKey: TESTS_KEY }),
  });

  const runScan = useMutation({
    mutationFn: (projectId: string) =>
      fetchJSON('/api/tests/scan', { method: 'POST', body: { projectId } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: TESTS_KEY }),
  });

  return {
    projects: query.data?.projects ?? [],
    isLoading: query.isLoading,
    error: query.error as Error | null,
    runCoverage,
    runScan,
  };
}
