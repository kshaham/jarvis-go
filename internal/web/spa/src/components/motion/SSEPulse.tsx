import { motion } from 'motion/react';

import { useSSEConnected } from '../../hooks/useSSEConnected';

type Props = {
  size?: number;
};

// SSEPulse — top-right indicator showing whether real-time updates are
// flowing. Violet breathing dot when connected; outlined red dot when
// disconnected. Drop into the page header once; visible on every view.
export function SSEPulse({ size = 6 }: Props) {
  const connected = useSSEConnected();
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: size + 4,
        height: size + 4,
      }}
      aria-label={connected ? 'Real-time connected' : 'Real-time disconnected'}
      title={connected ? 'Real-time connected' : 'Real-time disconnected'}
    >
      <motion.span
        animate={
          connected
            ? { opacity: [1, 0.55, 1] }
            : { opacity: 1 }
        }
        transition={
          connected
            ? { duration: 4, repeat: Infinity, ease: 'easeInOut' }
            : { duration: 0.3 }
        }
        style={{
          width: size,
          height: size,
          borderRadius: '50%',
          background: connected ? 'var(--accent)' : 'transparent',
          border: connected ? 'none' : '1px solid var(--status-error)',
          boxShadow: connected ? '0 0 6px var(--accent)' : 'none',
        }}
      />
    </span>
  );
}
