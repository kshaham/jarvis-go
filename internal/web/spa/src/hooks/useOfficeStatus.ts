import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { fetchJSON } from '@/api/client';
import type { OfficeStatus } from '@/api/types';
import { subscribeSSE } from '@/api/sse';

const QUERY_KEY = ['office', 'status'] as const;

export function useOfficeStatus() {
  const qc = useQueryClient();
  const q = useQuery<OfficeStatus>({
    queryKey: QUERY_KEY,
    queryFn: ({ signal }) => fetchJSON<OfficeStatus>('/api/office/status', { signal }),
    refetchInterval: 30_000,
  });

  // SSE-driven invalidation: refetch on member / team events so the UI
  // stays current without polling.
  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (
        name === 'office:memberUpdated' ||
        name === 'office:teamSpawned' ||
        name === 'office:teamKilled' ||
        name === 'office:perpetualDurationChanged'
      ) {
        qc.invalidateQueries({ queryKey: QUERY_KEY });
      }
    });
    return () => sub.close();
  }, [qc]);

  return q;
}
