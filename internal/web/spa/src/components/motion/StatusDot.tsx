import { m, useReducedMotion } from 'motion/react';
import { useRef } from 'react';

import { spring } from '../../lib/motion';

type Status = 'working' | 'idle' | 'meeting' | 'stuck';

const COLOR_VARS: Record<Status, string> = {
  working: 'var(--status-working)',
  idle: 'var(--status-idle)',
  meeting: 'var(--status-meeting)',
  stuck: 'var(--status-stuck)',
};

const RGB_VARS: Record<Status, string> = {
  working: 'var(--status-working-rgb)',
  idle: 'var(--status-idle-rgb)',
  meeting: 'var(--status-meeting-rgb)',
  stuck: 'var(--status-stuck-rgb)',
};

// Every status carries a soft glow halo so the dot reads as a lit indicator,
// scaled by intensity: working is brightest (it is the "alive" state), stuck
// is hot, meeting is warm, idle is the subtlest ember. Token-colored so it
// stays correct in every theme.
const GLOW_ALPHA: Record<Status, number> = {
  working: 0.55,
  stuck: 0.5,
  meeting: 0.42,
  idle: 0.22,
};

type Props = {
  status: Status;
  size?: number;
};

// StatusDot — animated colored dot keyed to one of four agent statuses.
//   - working   → 2.8s breathing pulse
//   - stuck     → 1s urgent pulse
//   - any flip  → 360ms radial ripple in the new status's color
// Same visual grammar as the mobile StatusDot — the design language's
// "something just happened" beat.
export function StatusDot({ status, size = 8 }: Props): React.ReactElement {
  const reduce = useReducedMotion();
  // rippleKey changes every time the status prop changes, which
  // remounts the ripple element and replays the animation once.
  // We derive the key by counting status transitions during render
  // instead of storing it in an effect-driven state.
  const prevStatus = useRef(status);
  const rippleKey = useRef(0);
  if (prevStatus.current !== status) {
    rippleKey.current += 1;
    prevStatus.current = status;
  }

  const color = COLOR_VARS[status];
  // Glow radius tracks intensity so brighter states bloom a touch wider.
  const glowRadius = Math.round(size * (status === 'working' ? 1.1 : 0.85));
  const glow = `0 0 ${glowRadius}px rgb(${RGB_VARS[status]} / ${GLOW_ALPHA[status]})`;

  // The base dot breathes for working/stuck statuses. For idle/meeting
  // it sits at full opacity.
  const breatheAnimate =
    status === 'working'
      ? { opacity: [1, 0.55, 1] }
      : status === 'stuck'
        ? { opacity: [1, 0.4, 1] }
        : { opacity: 1 };
  const breatheTransition =
    status === 'working'
      ? { duration: 2.8, repeat: Infinity, ease: 'easeInOut' as const }
      : status === 'stuck'
        ? { duration: 1, repeat: Infinity, ease: 'easeInOut' as const }
        : { duration: 0.3 };

  return (
    <span
      style={{
        position: 'relative',
        display: 'inline-block',
        width: size,
        height: size,
      }}
    >
      {/* Ripple: scales 1 → 2.4 with opacity 0.6 → 0 on every status flip. */}
      {!reduce && (
        <m.span
          key={rippleKey.current}
          initial={{ scale: 1, opacity: 0.6 }}
          animate={{ scale: 2.4, opacity: 0 }}
          transition={{ ...spring.smooth, duration: 0.36 }}
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            background: color,
            pointerEvents: 'none',
          }}
        />
      )}
      <m.span
        animate={reduce ? { opacity: 1 } : breatheAnimate}
        transition={reduce ? { duration: 0 } : breatheTransition}
        style={{
          position: 'absolute',
          inset: 0,
          borderRadius: '50%',
          background: color,
          // Soft glow halo on every state, scaled by status intensity.
          boxShadow: glow,
        }}
      />
    </span>
  );
}
