import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { Metric, SSEPulse } from '@/components/motion';
import { ThemeToggle } from '@/components/ui';

export function TeamHeader() {
  const { data } = useOfficeStatus();
  const team = data?.teamSpawned ? data : null;
  const memberCount = team?.members?.length || 0;

  return (
    <header
      className="sticky top-0 z-10 border-b backdrop-blur"
      style={{
        borderColor: 'var(--border-subtle)',
        background: 'rgba(6,8,12,0.8)',
      }}
    >
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-6 py-3">
        <div className="flex items-center gap-3">
          {/* Brand mark — electric violet to signature-accent the surface. */}
          <div
            className="h-7 w-7 rounded-md"
            style={{
              background:
                'linear-gradient(135deg, var(--accent) 0%, var(--accent-strong) 100%)',
            }}
          />
          <div>
            <div className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
              Jarvis · Office
            </div>
            <div className="text-[11px]" style={{ color: 'var(--text-muted)' }}>
              React beta /office-v2
            </div>
          </div>
        </div>

        <div className="flex items-center gap-3 text-xs">
          {team?.mission && (
            <span
              className="hidden max-w-[40ch] truncate sm:inline-block"
              style={{ color: 'var(--text-secondary)' }}
            >
              {team.mission}
            </span>
          )}
          {team?.prNumber ? (
            <span
              className="rounded px-2 py-0.5"
              style={{
                background: 'var(--accent-muted)',
                color: 'var(--accent)',
              }}
            >
              PR #{team.prNumber}
            </span>
          ) : null}
          {team?.paused && (
            <span
              className="rounded px-2 py-0.5"
              style={{
                background: 'rgba(255,180,84,0.16)',
                color: 'var(--status-warn)',
              }}
            >
              Paused
            </span>
          )}
          {team && (
            <span className="inline-flex items-baseline gap-1" style={{ color: 'var(--text-secondary)' }}>
              <Metric value={memberCount} size={12} tone="primary" />
              <span style={{ color: 'var(--text-muted)' }}>agents</span>
            </span>
          )}
          {/* Live SSE indicator — the heartbeat of the office. */}
          <SSEPulse />
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}
