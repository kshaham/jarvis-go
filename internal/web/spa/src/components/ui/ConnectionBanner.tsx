import { AnimatePresence, m, useReducedMotion } from 'motion/react';
import type { CSSProperties } from 'react';

import { useSSEConnected } from '@/hooks/useSSEConnected';

const bannerInnerStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  gap: 8,
  padding: '6px 12px',
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--status-warn)',
  background: 'rgb(var(--status-warn-rgb) / 0.12)',
  borderBottom: '1px solid rgb(var(--status-warn-rgb) / 0.25)',
};

// ConnectionBanner — a slim amber strip that slides in under the header
// whenever the SSE stream is down, making "live updates paused" obvious
// (complements the SSEPulse dot + the reconnect toast). Auto-hides on
// reconnect.
export function ConnectionBanner() {
  const connected = useSSEConnected();
  const reduce = useReducedMotion();
  return (
    <AnimatePresence>
      {!connected ? (
        <m.div
          key="offline"
          initial={reduce ? { opacity: 0 } : { opacity: 0, height: 0 }}
          animate={reduce ? { opacity: 1 } : { opacity: 1, height: 'auto' }}
          exit={reduce ? { opacity: 0 } : { opacity: 0, height: 0 }}
          transition={{ duration: 0.2 }}
          style={{ overflow: 'hidden' }}
        >
          <div style={bannerInnerStyle}>
            <m.span
              animate={reduce ? { opacity: 1 } : { opacity: [1, 0.4, 1] }}
              transition={reduce ? { duration: 0 } : { duration: 1.4, repeat: Infinity, ease: 'easeInOut' }}
              style={{ width: 6, height: 6, borderRadius: 999, background: 'var(--status-warn)' }}
            />
            Reconnecting… live updates paused
          </div>
        </m.div>
      ) : null}
    </AnimatePresence>
  );
}
