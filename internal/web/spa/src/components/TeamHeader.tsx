import { m, useReducedMotion } from 'motion/react';

import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { Metric, SSEPulse } from '@/components/motion';
import { ThemeToggle } from '@/components/ui';

// Brand block at the TOP of the left sidebar — this replaces the old
// full-width header bar so the chrome is integrated into the UI and the
// content below sits higher. Brand mark + title on the left, the live SSE
// pulse + theme toggle on the right, and a compact team-status row (agent
// count + PR/Paused badges) when a team is running.
export function TeamHeader(): React.ReactElement {
  const { data } = useOfficeStatus();
  const reduce = useReducedMotion();
  const team = data?.teamSpawned ? data : null;
  const memberCount = team?.members?.length || 0;

  return (
    <div
      className="flex flex-col gap-2"
      style={{
        marginBottom: 10,
        paddingBottom: 10,
        borderBottom: '1px solid var(--border-subtle)',
      }}
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {/* Brand mark — accent glass chip with a top sheen + a slow
              breathing bloom so the office reads as alive. The bloom is a
              separate layer animating box-shadow only; static under reduced
              motion. */}
          <div className="relative size-6 shrink-0">
            <m.span
              aria-hidden
              className="absolute inset-0 rounded-md"
              animate={
                reduce
                  ? undefined
                  : {
                      boxShadow: [
                        '0 0 6px rgb(var(--accent-rgb) / 0.25)',
                        '0 0 16px rgb(var(--accent-rgb) / 0.55)',
                        '0 0 6px rgb(var(--accent-rgb) / 0.25)',
                      ],
                    }
              }
              transition={reduce ? undefined : { duration: 4.5, repeat: Infinity, ease: 'easeInOut' }}
              style={reduce ? { boxShadow: '0 0 10px rgb(var(--accent-rgb) / 0.4)' } : undefined}
            />
            <span
              className="absolute inset-0 rounded-md"
              style={{
                background: 'linear-gradient(135deg, var(--accent) 0%, var(--accent-strong) 100%)',
                boxShadow:
                  'inset 0 1px 0 rgba(255,255,255,0.5), 0 2px 10px rgb(var(--accent-rgb) / 0.4)',
              }}
            />
          </div>
          <span
            className="truncate text-sm font-semibold"
            style={{ color: 'var(--text-primary)' }}
          >
            Jarvis · Office
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {/* Live SSE indicator — the heartbeat of the office. */}
          <SSEPulse />
          <ThemeToggle />
        </div>
      </div>

      {team && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="inline-flex items-baseline gap-1" style={{ color: 'var(--text-secondary)' }}>
            <Metric value={memberCount} size={12} tone="primary" />
            <span style={{ color: 'var(--text-muted)' }}>agents</span>
          </span>
          {team.prNumber ? (
            <m.span
              className="badge-glass px-2 py-0.5 font-medium"
              initial={reduce ? false : { opacity: 0, scale: 0.85 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{ type: 'spring', stiffness: 520, damping: 26 }}
              style={{
                background: 'var(--lg-fill-accent)',
                border: '1px solid rgb(var(--accent-rgb) / 0.4)',
                color: 'var(--accent)',
              }}
            >
              PR #{team.prNumber}
            </m.span>
          ) : null}
          {team.paused ? (
            <m.span
              className="badge-glass px-2 py-0.5 font-medium"
              initial={reduce ? false : { opacity: 0, scale: 0.85 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{ type: 'spring', stiffness: 520, damping: 26 }}
              style={{
                background: 'rgb(var(--status-warn-rgb) / 0.16)',
                border: '1px solid rgb(var(--status-warn-rgb) / 0.35)',
                color: 'var(--status-warn)',
              }}
            >
              Paused
            </m.span>
          ) : null}
        </div>
      )}
    </div>
  );
}
