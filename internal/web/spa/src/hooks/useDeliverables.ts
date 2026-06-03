// useDeliverables — the Deliverables page data layer. Reads per-project
// deliverables (GET /api/deliverables/results), exposes a generate mutation
// (dispatch the team to write a deliverable), a doc-reader query, and
// invalidates on the deliverables:* SSE so the list refreshes when a doc lands.
import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { DeliverableProject, DeliverableType } from '@/api/types';

const KEY = ['deliverables', 'results'] as const;

export function useDeliverables() {
  const qc = useQueryClient();

  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (name.startsWith('deliverables:')) {
        qc.invalidateQueries({ queryKey: KEY });
      }
    });
    return () => sub.close();
  }, [qc]);

  const query = useQuery<{ projects: DeliverableProject[] }>({
    queryKey: KEY,
    queryFn: ({ signal }) =>
      fetchJSON<{ projects: DeliverableProject[] }>('/api/deliverables/results', { signal }),
  });

  const generate = useMutation({
    mutationFn: (vars: { projectId: string; type: DeliverableType; title?: string }) =>
      fetchJSON('/api/deliverables/generate', { method: 'POST', body: vars }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });

  return {
    projects: query.data?.projects ?? [],
    isLoading: query.isLoading,
    generate,
  };
}

export interface DeliverableDoc {
  title: string;
  type: string;
  content: string;
}

// useDeliverableDoc loads one document's markdown (latest unless version given).
export function useDeliverableDoc(projectId: string | null, slug: string | null, version?: number) {
  return useQuery<DeliverableDoc>({
    enabled: !!projectId && !!slug,
    queryKey: ['deliverables', 'doc', projectId, slug, version ?? 'latest'],
    queryFn: ({ signal }) => {
      if (!projectId || !slug) throw new Error('deliverable doc requires projectId + slug');
      const params = new URLSearchParams({ projectId, slug });
      if (version) params.set('version', String(version));
      return fetchJSON<DeliverableDoc>(`/api/deliverables/doc?${params.toString()}`, { signal });
    },
  });
}
