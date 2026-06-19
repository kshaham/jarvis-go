// useMockups — the Mockups page data layer. Reads per-project mockup state
// (GET /api/mockups/results), exposes a generate mutation
// (POST /api/mockups/generate), and invalidates on the mockups:* SSE events
// so tiles fill in live as agents upload each image.
import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { MockupKind, MockupProject } from '@/api/types';

const MOCKUPS_KEY = ['mockups', 'results'] as const;

export function useMockups() {
  const qc = useQueryClient();

  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (name.startsWith('mockups:')) {
        qc.invalidateQueries({ queryKey: MOCKUPS_KEY });
      }
    });
    return () => sub.close();
  }, [qc]);

  const query = useQuery<{ projects: MockupProject[] }>({
    queryKey: MOCKUPS_KEY,
    queryFn: ({ signal }) => fetchJSON<{ projects: MockupProject[] }>('/api/mockups/results', { signal }),
  });

  const generate = useMutation({
    mutationFn: ({ projectId, kind }: { projectId: string; kind: MockupKind }) =>
      fetchJSON('/api/mockups/generate', { method: 'POST', body: { projectId, kind } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: MOCKUPS_KEY }),
  });

  const apply = useMutation({
    mutationFn: ({ projectId, index }: { projectId: string; index: number }) =>
      fetchJSON('/api/mockups/apply', { method: 'POST', body: { projectId, kind: 'ui', index } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: MOCKUPS_KEY }),
  });

  return {
    projects: query.data?.projects ?? [],
    isLoading: query.isLoading,
    generate,
    apply,
  };
}
