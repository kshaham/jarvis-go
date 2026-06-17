import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { Metric, SSEPulse } from '@/components/motion';
import { ThemeToggle } from '@/components/ui';

export function TeamHeader() {
  const { data } = useOfficeStatus();
  const team = data?.teamSpawned ? data : null;
  const memberCount = team?.members?.length || 0;

  return (
    <header
      className="sticky top-0 z-10 border-b"
      style={{
        borderColor: 'var(--lg-rim)',
        background: 'var(--chrome-bg)',
        WebkitBackdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
        backdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
        boxShadow: '0 1px 0 0 rgba(78,240,163,0.05)',
      }}
    >
      <div className="flex w-full items-center justify-between gap-4 px-4 py-2 sm:px-6 lg:px-8 xl:px-12 2xl:px-16">
        <div className="flex items-center gap-2.5">
          {/* Brand mark — accent glass chip with a top sheen + bloom. */}
          <div
            className="size-6 rounded-md"
            style={{
              background:
                'linear-gradient(135deg, var(--accent) 0%, var(--accent-strong) 100%)',
              boxShadow:
                'inset 0 1px 0 rgba(255,255,255,0.5), 0 2px 10px rgb(var(--accent-rgb) / 0.4)',
            }}
          />
          {/* Single-line brand — title with an inline muted suffix (hidden on
              mobile) keeps the header chrome to one compact row. */}
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
              Jarvis · Office
            </span>
            <span className="hidden text-xs sm:inline" style={{ color: 'var(--text-muted)' }}>
              React beta · /office-v2
            </span>
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
              className="rounded-full px-2 py-0.5 font-medium"
              style={{
                background: 'var(--lg-fill-accent)',
                border: '1px solid rgb(var(--accent-rgb) / 0.4)',
                boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.14)',
                color: 'var(--accent)',
              }}
            >
              PR #{team.prNumber}
            </span>
          ) : null}
          {team?.paused && (
            <span
              className="rounded-full px-2 py-0.5 font-medium"
              style={{
                background: 'rgb(var(--status-warn-rgb) / 0.16)',
                border: '1px solid rgb(var(--status-warn-rgb) / 0.35)',
                boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.14)',
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
