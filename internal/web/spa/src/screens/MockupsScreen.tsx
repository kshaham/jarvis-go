import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Image as ImageIcon, Images } from 'lucide-react';

import { Button, EmptyState, GlowCard, SegmentedControl, Skeleton } from '@/components/ui';
import { useMockups } from '@/hooks/useMockups';
import type { MockupKind, MockupProject } from '@/api/types';

const SLOTS = 6;

const tileGridStyle: React.CSSProperties = {
  display: 'grid',
  gap: 10,
  gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
};

// MockupsScreen — per saved project, the spawned team generates 6 distinct AI
// mockups. The user picks the kind: UI (image-to-image redesigns of the running
// screen) or Presentation (text-to-image slide designs from the project's docs).
// Generate is enabled only when a team is spawned on that project's workdir.
export function MockupsScreen() {
  const { projects, isLoading, generate } = useMockups();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 className="page-header">Mockups</h1>
        <p className="page-subtitle">
          Spawn a team on a project, then have it generate 6 distinct AI mockups — a UI redesign or a
          presentation deck.
        </p>
      </header>

      {isLoading ? (
        <Skeleton style={{ height: 200, borderRadius: 'var(--radius-card)' }} />
      ) : projects.length === 0 ? (
        <EmptyState
          icon={<Images size={22} />}
          title="No projects yet"
          description="Save a project on the Spawn page to generate mockups for it here."
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {projects.map((p) => (
            <ProjectMockups
              key={p.id}
              p={p}
              onGenerate={(kind) => generate.mutate({ projectId: p.id, kind })}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function ProjectMockups({
  p,
  onGenerate,
}: {
  p: MockupProject;
  onGenerate: (kind: MockupKind) => void;
}) {
  const navigate = useNavigate();
  const available: MockupKind[] = [];
  if (p.hasUi) available.push('ui');
  if (p.hasDocs) available.push('presentation');

  const [kind, setKind] = useState<MockupKind>(available[0] ?? 'ui');
  useEffect(() => {
    if (available.length && !available.includes(kind)) setKind(available[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.hasUi, p.hasDocs]);

  if (available.length === 0) {
    return (
      <GlowCard className="flex items-center justify-between gap-2 p-4">
        <div className="truncate font-semibold" style={{ color: 'var(--text-primary)' }}>
          {p.name}
        </div>
        <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
          No UI or content to present
        </div>
      </GlowCard>
    );
  }

  const state = kind === 'ui' ? p.ui : p.presentation;
  const running = state.status.status === 'running';
  const bySlot = new Map(state.mockups.map((m) => [m.index, m.url]));
  const noun = kind === 'ui' ? 'mockup' : 'slide';
  const aspect = kind === 'presentation' ? '16 / 9' : '4 / 3';

  return (
    <GlowCard className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-semibold" style={{ color: 'var(--text-primary)' }}>
            {p.name}
          </div>
          <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {running
              ? `Generating ${state.status.received}/${state.status.expected}…`
              : state.mockups.length > 0
                ? `${state.mockups.length} ${noun}${state.mockups.length === 1 ? '' : 's'}`
                : 'No mockups yet'}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {available.length > 1 ? (
            <SegmentedControl<MockupKind>
              ariaLabel="Mockup kind"
              value={kind}
              onChange={setKind}
              options={[
                { value: 'ui', label: 'UI' },
                { value: 'presentation', label: 'Presentation' },
              ]}
            />
          ) : (
            <span
              className="rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
              style={{ color: 'var(--text-muted)', border: '1px solid var(--lg-rim)' }}
            >
              {kind === 'ui' ? 'UI' : 'Presentation'}
            </span>
          )}
          {!p.teamActive ? (
            <button
              onClick={() => navigate('/spawn')}
              className="text-xs underline"
              style={{ color: 'var(--text-muted)', textUnderlineOffset: 3 }}
            >
              Spawn a team for this project
            </button>
          ) : null}
          <Button
            size="sm"
            onClick={() => onGenerate(kind)}
            disabled={!p.teamActive || running}
            title={p.teamActive ? '' : 'Spawn a team for this project first'}
          >
            {running ? `Generating ${state.status.received}/${state.status.expected}…` : 'Generate 6 mockups'}
          </Button>
        </div>
      </div>

      {state.status.status === 'error' ? (
        <div className="text-xs" style={{ color: 'var(--status-error)' }}>
          {state.status.error || 'Generation failed'}
        </div>
      ) : null}

      <div style={tileGridStyle}>
        {Array.from({ length: SLOTS }).map((_, i) => (
          <MockupTile key={i} url={bySlot.get(i)} running={running} aspect={aspect} kind={kind} />
        ))}
      </div>
    </GlowCard>
  );
}

function MockupTile({
  url,
  running,
  aspect,
  kind,
}: {
  url?: string;
  running: boolean;
  aspect: string;
  kind: MockupKind;
}) {
  const frame: React.CSSProperties = {
    aspectRatio: aspect,
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--lg-rim)',
    overflow: 'hidden',
    background: 'rgb(var(--surface-tint-rgb) / 0.04)',
  };
  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer" style={{ ...frame, display: 'block' }} title="Open full size">
        <img
          src={url}
          alt={kind === 'ui' ? 'UI mockup' : 'presentation slide'}
          style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
        />
      </a>
    );
  }
  if (running) {
    return <Skeleton style={{ ...frame, border: 'none' }} />;
  }
  return (
    <div
      style={{ ...frame, display: 'grid', placeItems: 'center', color: 'var(--text-faint)' }}
      aria-label="empty mockup slot"
    >
      <ImageIcon size={18} />
    </div>
  );
}
