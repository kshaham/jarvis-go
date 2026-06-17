import { m, useReducedMotion } from 'motion/react';

import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { Metric, SSEPulse } from '@/components/motion';
import { ThemeToggle } from '@/components/ui';

export function TeamHeader(): React.ReactElement {
  const { data } = useOfficeStatus();
  const reduce = useReducedMotion();
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
        boxShadow: '0 1px 0 0 rgb(var(--accent-rgb) / 0.05)',
      }}
    >
      <div className="flex w-full items-center justify-between gap-4 px-4 py-1">
        <div className="flex items-center gap-2.5">
          {/* Brand mark — accent glass chip with a top sheen + a slow
              breathing bloom so the office reads as alive. The bloom is a
              separate layer animating box-shadow only; static under
              reduced motion. */}
          <div className="relative size-6">
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
              transition={
                reduce
                  ? undefined
                  : { duration: 4.5, repeat: Infinity, ease: 'easeInOut' }
              }
              style={
                reduce
                  ? { boxShadow: '0 0 10px rgb(var(--accent-rgb) / 0.4)' }
                  : undefined
              }
            />
            <span
              className="absolute inset-0 rounded-md"
              style={{
                background:
                  'linear-gradient(135deg, var(--accent) 0%, var(--accent-strong) 100%)',
                boxShadow:
                  'inset 0 1px 0 rgba(255,255,255,0.5), 0 2px 10px rgb(var(--accent-rgb) / 0.4)',
              }}
            />
          </div>
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
              style={{
                color: 'var(--text-secondary)',
                // Gradient fade-out at the trailing edge so the truncation
                // reads as a deliberate falloff rather than a hard clip.
                WebkitMaskImage:
                  'linear-gradient(to right, #000 calc(100% - 2.5ch), transparent 100%)',
                maskImage:
                  'linear-gradient(to right, #000 calc(100% - 2.5ch), transparent 100%)',
              }}
            >
              {team.mission}
            </span>
          )}
          {team?.prNumber ? (
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
          {team?.paused && (
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
