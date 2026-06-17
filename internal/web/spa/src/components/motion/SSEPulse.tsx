import { m, useReducedMotion } from 'motion/react';

import { useSSEConnected } from '../../hooks/useSSEConnected';

type Props = {
  size?: number;
};

// SSEPulse — top-right indicator showing whether real-time updates are
// flowing. Accent breathing dot inside a faint backdrop ring when
// connected; outlined error dot when disconnected. Drop into the page
// header once; visible on every view.
export function SSEPulse({ size = 7 }: Props): React.ReactElement {
  const connected = useSSEConnected();
  const reduce = useReducedMotion();
  const ring = size + 7;
  return (
    <span
      style={{
        position: 'relative',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: ring,
        height: ring,
      }}
      aria-label={connected ? 'Real-time connected' : 'Real-time disconnected'}
      title={connected ? 'Real-time connected' : 'Real-time disconnected'}
    >
      {/* Faint backdrop ring — reads the heartbeat as intentional, not
          noise. Only rendered while connected. */}
      {connected && (
        <span
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: '50%',
            background: 'rgb(var(--accent-rgb) / 0.08)',
            border: '1px solid rgb(var(--accent-rgb) / 0.18)',
          }}
        />
      )}
      <m.span
        animate={
          connected && !reduce
            ? { opacity: [1, 0.55, 1] }
            : { opacity: 1 }
        }
        transition={
          connected && !reduce
            ? { duration: 4, repeat: Infinity, ease: 'easeInOut' }
            : { duration: 0.3 }
        }
        style={{
          position: 'relative',
          width: size,
          height: size,
          borderRadius: '50%',
          background: connected ? 'var(--accent)' : 'transparent',
          border: connected ? 'none' : '1px solid var(--status-error)',
          boxShadow: connected ? '0 0 6px rgb(var(--accent-rgb) / 0.8)' : 'none',
        }}
      />
    </span>
  );
}
