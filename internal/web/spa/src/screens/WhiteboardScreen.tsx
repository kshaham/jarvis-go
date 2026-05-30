import { Suspense, lazy, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { fetchJSON } from '../api/client';
import { useWhiteboard } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';
import { Skeleton } from '../components/ui';

// Excalidraw is large — load it only when this route is visited.
const ExcalidrawCanvas = lazy(() => import('../components/whiteboard/ExcalidrawCanvas'));

// WhiteboardScreen — the team's shared infinite canvas (Excalidraw,
// MIT). The scene persists as JSON in the existing
// /api/office/whiteboard `text` field. Edits are debounced inside the
// canvas, then saved here.
export function WhiteboardScreen() {
  const wb = useWhiteboard();
  const qc = useQueryClient();
  const remote = wb.data?.whiteboard?.text ?? '';
  const updatedAt = wb.data?.whiteboard?.updatedAt;
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const lastSaved = useRef<string>('');

  const save = useMutation({
    mutationFn: (text: string) =>
      fetchJSON('/api/office/whiteboard', { method: 'POST', body: { text } }),
    onSuccess: () => {
      setSavedAt(Date.now());
      qc.invalidateQueries({ queryKey: ['office', 'whiteboard'] });
    },
  });

  function saveCanvasChange(json: string) {
    if (json === lastSaved.current) return;
    lastSaved.current = json;
    save.mutate(json);
  }

  const status = save.isPending
    ? 'Saving…'
    : savedAt
      ? `Saved ${relativeTime(new Date(savedAt))}`
      : updatedAt
        ? `Last updated ${relativeTime(updatedAt)}`
        : 'In sync';

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Whiteboard
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Shared infinite canvas. Sketch, diagram, and annotate. Every agent in the team can read it.
          </p>
        </div>
        <span
          style={{ fontSize: 12, color: save.isPending ? 'var(--status-warn)' : 'var(--text-muted)' }}
        >
          {status}
        </span>
      </header>

      <div
        className="surface-card surface-card--solid"
        style={{ position: 'relative', flex: 1, minHeight: 0, overflow: 'hidden' }}
      >
        {wb.isLoading ? (
          <CanvasSkeleton />
        ) : (
          <Suspense fallback={<CanvasSkeleton />}>
            <ExcalidrawCanvas initialDocument={remote || undefined} onChange={saveCanvasChange} />
          </Suspense>
        )}
      </div>
    </section>
  );
}

function CanvasSkeleton() {
  return (
    <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>
      <Skeleton style={{ width: '92%', height: '88%', borderRadius: 12 }} />
    </div>
  );
}
