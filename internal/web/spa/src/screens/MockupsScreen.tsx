import React from 'react';
import { useNavigate } from 'react-router-dom';
import { Image as ImageIcon, Images } from 'lucide-react';

import { Button, EmptyState, GlowCard, Skeleton } from '@/components/ui';
import { useMockups } from '@/hooks/useMockups';
import type { MockupProject } from '@/api/types';

const SLOTS = 6;

const tileGridStyle: React.CSSProperties = {
  display: 'grid',
  gap: 10,
  gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
};

// MockupsScreen — per saved project, the spawned team generates 6 AI UI
// mockups (image-to-image redesigns of the project's current screen). Each
// project section shows its 6 tiles; Generate is enabled only when a team is
// spawned on that project's workdir.
export function MockupsScreen() {
  const { projects, isLoading, generate } = useMockups();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>Mockups</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Spawn a team on a project, then have it generate 6 distinct AI redesigns of the project's UI.
        </p>
      </header>

      {isLoading ? (
        <Skeleton style={{ height: 200, borderRadius: 'var(--radius-card)' }} />
      ) : projects.length === 0 ? (
        <EmptyState
          icon={<Images size={22} />}
          title="No projects yet"
          description="Save a project on the Spawn page to generate UI mockups for it here."
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {projects.map((p) => (
            <ProjectMockups key={p.id} p={p} onGenerate={() => generate.mutate(p.id)} />
          ))}
        </div>
      )}
    </section>
  );
}

function ProjectMockups({ p, onGenerate }: { p: MockupProject; onGenerate: () => void }) {
  const navigate = useNavigate();
  const running = p.status.status === 'running';
  const bySlot = new Map(p.mockups.map((m) => [m.index, m.url]));

  return (
    <GlowCard className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-semibold" style={{ color: 'var(--text-primary)' }}>
            {p.name}
          </div>
          <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {p.hasUi
              ? running
                ? `Generating ${p.status.received}/${p.status.expected}…`
                : p.mockups.length > 0
                  ? `${p.mockups.length} mockup${p.mockups.length === 1 ? '' : 's'}`
                  : 'No mockups yet'
              : 'No UI detected'}
          </div>
        </div>
        {p.hasUi ? (
          <div className="flex items-center gap-2">
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
              onClick={onGenerate}
              disabled={!p.teamActive || running}
              title={p.teamActive ? '' : 'Spawn a team for this project first'}
            >
              {running ? `Generating ${p.status.received}/${p.status.expected}…` : 'Generate 6 mockups'}
            </Button>
          </div>
        ) : null}
      </div>

      {p.status.status === 'error' ? (
        <div className="text-xs" style={{ color: 'var(--status-error)' }}>
          {p.status.error || 'Generation failed'}
        </div>
      ) : null}

      {p.hasUi ? (
        <div style={tileGridStyle}>
          {Array.from({ length: SLOTS }).map((_, i) => {
            const url = bySlot.get(i);
            return <MockupTile key={i} url={url} running={running} />;
          })}
        </div>
      ) : null}
    </GlowCard>
  );
}

function MockupTile({ url, running }: { url?: string; running: boolean }) {
  const frame: React.CSSProperties = {
    aspectRatio: '4 / 3',
    borderRadius: 'var(--radius-md)',
    border: '1px solid var(--lg-rim)',
    overflow: 'hidden',
    background: 'rgb(var(--surface-tint-rgb) / 0.04)',
  };
  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer" style={{ ...frame, display: 'block' }} title="Open full size">
        <img src={url} alt="UI mockup" style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }} />
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
