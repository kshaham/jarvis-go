import { m, useReducedMotion } from 'motion/react';
import type { CSSProperties } from 'react';

import { cn } from '@/lib/cn';
import type { AgentStatus } from '@/api/types';
import { StatusDot } from '@/components/motion';

// Each pill derives its tint from a single status RGB token so the border,
// fill, and (for active states) the text all share one hue. The idle pill is
// the exception: its grey is too low-contrast to read as text, so its label
// falls back to var(--text-secondary), which the theme re-scopes for legibility
// on every surface.
const TINT_VARS: Record<AgentStatus, string> = {
  working: '--status-working-rgb',
  idle: '--status-idle-rgb',
  meeting: '--status-meeting-rgb',
  stuck: '--status-stuck-rgb',
};

function pillStyle(status: AgentStatus): CSSProperties {
  const rgb = `var(${TINT_VARS[status]})`;
  return {
    // Fill + border share the status hue at low alpha for a cohesive chip.
    backgroundColor: `rgb(${rgb} / 0.15)`,
    border: `1px solid rgb(${rgb} / 0.35)`,
    // Idle grey is illegible as a label, so route it through the text token
    // (re-scoped per theme); active states keep their saturated status hue.
    color: status === 'idle' ? 'var(--text-secondary)' : `rgb(${rgb} / 0.95)`,
    // Explicit 1px top sheen so the pill reads as the same glass material as
    // the tiles it sits on, theme-agnostic (white at low alpha).
    boxShadow: 'inset 0 1px 0 rgb(255 255 255 / 0.14)',
  };
}

// StatusPill — colored chip with an animated StatusDot. The dot breathes when
// the agent is working, pulses urgently when stuck, and ripples on every status
// crossover. When stuck, the whole pill also runs a slow, reduced-motion-safe
// pulse so a blocked agent is pre-attentively obvious in a dense roster.
export function StatusPill({ status }: { status: AgentStatus }): React.ReactElement {
  const reduce = useReducedMotion();
  const isStuck = status === 'stuck';

  return (
    <m.span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium',
      )}
      style={pillStyle(status)}
      animate={
        isStuck && !reduce
          ? { boxShadow: [
              'inset 0 1px 0 rgb(255 255 255 / 0.14), 0 0 0 0 rgb(var(--status-stuck-rgb) / 0.0)',
              'inset 0 1px 0 rgb(255 255 255 / 0.14), 0 0 0 4px rgb(var(--status-stuck-rgb) / 0.18)',
              'inset 0 1px 0 rgb(255 255 255 / 0.14), 0 0 0 0 rgb(var(--status-stuck-rgb) / 0.0)',
            ] }
          : undefined
      }
      transition={
        isStuck && !reduce
          ? { duration: 1.8, repeat: Infinity, ease: 'easeInOut' as const }
          : undefined
      }
    >
      <StatusDot status={status} size={6} />
      {status}
    </m.span>
  );
}
