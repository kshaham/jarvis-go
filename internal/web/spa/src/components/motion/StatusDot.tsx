import { motion, useReducedMotion } from 'motion/react';
import { useEffect, useState } from 'react';

import { spring } from '../../lib/motion';

type Status = 'working' | 'idle' | 'meeting' | 'stuck';

const COLOR_VARS: Record<Status, string> = {
  working: 'var(--status-working)',
  idle: 'var(--status-idle)',
  meeting: 'var(--status-meeting)',
  stuck: 'var(--status-stuck)',
};

type Props = {
  status: Status;
  size?: number;
};

// StatusDot — animated colored dot keyed to one of four agent statuses.
//   - working   → 4s breathing pulse
//   - stuck     → 1s urgent pulse
//   - any flip  → 200ms radial ripple in the new status's color
// Same visual grammar as the mobile StatusDot — the design language's
// "something just happened" beat.
export function StatusDot({ status, size = 8 }: Props) {
  const reduce = useReducedMotion();
  // rippleKey changes every time the status prop changes, which
  // remounts the ripple element and replays the animation once.
  const [rippleKey, setRippleKey] = useState(0);
  useEffect(() => {
    setRippleKey((k) => k + 1);
  }, [status]);

  const color = COLOR_VARS[status];

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
        <motion.span
          key={rippleKey}
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
      <motion.span
        animate={reduce ? { opacity: 1 } : breatheAnimate}
        transition={reduce ? { duration: 0 } : breatheTransition}
        style={{
          position: 'absolute',
          inset: 0,
          borderRadius: '50%',
          background: color,
        }}
      />
    </span>
  );
}
