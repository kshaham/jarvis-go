import { useIsFetching } from '@tanstack/react-query';
import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';

// TopProgress — a slim indeterminate bar pinned to the very top of the
// viewport. Shows while react-query has in-flight requests, plus a brief
// pulse on route change, so navigation/loading always has feedback.
export function TopProgress() {
  const fetching = useIsFetching();
  const reduce = useReducedMotion();
  const location = useLocation();
  const [routePulse, setRoutePulse] = useState(false);

  useEffect(() => {
    setRoutePulse(true);
    const t = setTimeout(() => setRoutePulse(false), 600);
    return () => clearTimeout(t);
  }, [location.pathname]);

  const active = fetching > 0 || routePulse;

  return (
    <AnimatePresence>
      {active ? (
        <motion.div
          key="topbar"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            height: 2,
            zIndex: 80,
            pointerEvents: 'none',
            overflow: 'hidden',
          }}
        >
          <motion.div
            animate={reduce ? { x: 0 } : { x: ['-100%', '250%'] }}
            transition={reduce ? { duration: 0 } : { duration: 1.1, repeat: Infinity, ease: 'easeInOut' }}
            style={{
              height: '100%',
              width: '40%',
              background:
                'linear-gradient(90deg, transparent, rgb(var(--accent-rgb)), rgb(var(--accent-strong-rgb)), transparent)',
            }}
          />
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
