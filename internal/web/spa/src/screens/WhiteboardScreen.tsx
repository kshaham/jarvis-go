import { Suspense, lazy, useCallback, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { StickyNote as StickyNoteIcon } from 'lucide-react';

import { fetchJSON } from '../api/client';
import { useStickyNotes, useWhiteboard } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';
import { EmptyState } from '../components/ui/EmptyState';
import { Section } from '../components/ui/Section';
import { Skeleton } from '../components/ui/Skeleton';
import type { StickyNote } from '../api/types';

// Excalidraw is large — load it only when this route is visited.
const ExcalidrawCanvas = lazy(() => import('../components/whiteboard/ExcalidrawCanvas'));

// WhiteboardScreen — the team's shared infinite canvas (Excalidraw,
// MIT). The scene persists as JSON in the existing
// /api/office/whiteboard `text` field. Edits are debounced inside the
// canvas, then saved here.
//
// Above the canvas sits the agent sticky-note feed (Feature B): every
// `cm whiteboard "<note>"` posts an immutable colored card here, newest
// first, so the team's running thinking is visible at a glance.
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

  const saveCanvasChange = useCallback(
    (json: string) => {
      if (json === lastSaved.current) return;
      lastSaved.current = json;
      save.mutate(json);
    },
    [save],
  );

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
          <h1 className="page-header">
            Whiteboard
          </h1>
          <p className="page-subtitle">
            Shared infinite canvas + agent sticky notes. Sketch, diagram, and annotate. Every agent in the team can read it.
          </p>
        </div>
        <span
          style={{ fontSize: 12, color: save.isPending ? 'var(--status-warn)' : 'var(--text-muted)' }}
        >
          {status}
        </span>
      </header>

      <StickyNotesPanel />

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

// StickyNotesPanel renders the agent sticky-note feed as colored cards.
// Notes arrive newest-first from the server.
function StickyNotesPanel() {
  const notes = useStickyNotes();
  const list = notes.data?.notes ?? [];

  const trailing = (
    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{list.length} note{list.length === 1 ? '' : 's'}</span>
  );

  return (
    <Section title="Sticky notes" trailing={trailing}>
      {notes.isLoading ? (
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} style={{ width: 200, height: 96, borderRadius: 10 }} />
          ))}
        </div>
      ) : list.length === 0 ? (
        <EmptyState
          icon={<StickyNoteIcon size={22} />}
          title="No sticky notes yet"
          description={'Agents post notes with `cm whiteboard "<note>"` at decisions, blockers, milestones, and hand-offs.'}
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
            gap: 10,
          }}
        >
          {list.map((note) => (
            <StickyNoteCard key={note.id} note={note} />
          ))}
        </div>
      )}
    </Section>
  );
}

function StickyNoteCard({ note }: { note: StickyNote }) {
  const bg = note.color || '#fef3c7';
  return (
    <article
      style={{
        background: bg,
        color: '#1f2937',
        borderRadius: 10,
        padding: '12px 14px',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        minHeight: 96,
        boxShadow: '0 1px 2px rgba(0, 0, 0, 0.18)',
        border: '1px solid rgba(0, 0, 0, 0.06)',
      }}
    >
      <span
        style={{
          fontSize: 13,
          lineHeight: 1.4,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          flex: 1,
        }}
      >
        {note.content}
      </span>
      <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <span
          style={{
            fontSize: 11,
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: 999,
            background: 'rgba(0, 0, 0, 0.08)',
            color: '#111827',
          }}
        >
          {note.author}
        </span>
        <span style={{ fontSize: 11, color: 'rgba(17, 24, 39, 0.6)' }}>
          {relativeTime(note.createdAt)}
        </span>
      </span>
    </article>
  );
}

function CanvasSkeleton() {
  return (
    <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center' }}>
      <Skeleton style={{ width: '92%', height: '88%', borderRadius: 12 }} />
    </div>
  );
}
