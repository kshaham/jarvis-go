// useSavedProjects — the spawn page's saved-project list. Backed by
// GET /api/projects (server-persisted, shared across browsers), with a
// DELETE mutation for removal and SSE invalidation so a project created
// by another browser's spawn shows up live. Replaces the old
// localStorage "recent workdirs" chips.
import { useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import type { SavedProject } from '@/api/types';

const PROJECTS_KEY = ['projects'] as const;

export function useSavedProjects() {
  const qc = useQueryClient();

  useEffect(() => {
    const sub = subscribeSSE(({ name }) => {
      if (name === 'office:projectCreated') {
        qc.invalidateQueries({ queryKey: PROJECTS_KEY });
      }
    });
    return () => sub.close();
  }, [qc]);

  const query = useQuery<{ projects: SavedProject[] }>({
    queryKey: PROJECTS_KEY,
    queryFn: ({ signal }) => fetchJSON<{ projects: SavedProject[] }>('/api/projects', { signal }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => fetchJSON(`/api/projects/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PROJECTS_KEY }),
  });

  return { projects: query.data?.projects ?? [], isLoading: query.isLoading, remove };
}
