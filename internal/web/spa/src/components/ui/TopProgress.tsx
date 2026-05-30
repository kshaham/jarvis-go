import { useIsFetching } from '@tanstack/react-query';
import { AnimatePresence, m, useReducedMotion } from 'motion/react';
import type { CSSProperties } from 'react';
import { useLocation } from 'react-router-dom';

const barContainerStyle: CSSProperties = {
  position: 'fixed',
  top: 0,
  left: 0,
  right: 0,
  height: 2,
  zIndex: 80,
  pointerEvents: 'none',
  overflow: 'hidden',
};

// TopProgress — a slim indeterminate bar pinned to the very top of the
// viewport. Shows while react-query has in-flight requests, plus a brief
// pulse on route change, so navigation/loading always has feedback.
//
// Route-change pulse is driven by a CSS animation keyed to the location
// object (new reference per navigation) rather than a setState chain,
// eliminating the cascading-set-state pattern.
export function TopProgress() {
  const fetching = useIsFetching();
  const reduce = useReducedMotion();
  const location = useLocation();

  // location is a new object reference on every route change (React Router
  // guarantees this), so it's safe to use as a dep without reading .pathname.
  // The pulse duration is encoded purely in the CSS animation below.
  const active = fetching > 0;

  return (
    <AnimatePresence>
      {/* Fetching bar — visible while react-query has in-flight requests */}
      {active ? (
        <m.div
          key="topbar-fetch"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          style={barContainerStyle}
        >
          <m.div
            animate={reduce ? { x: 0 } : { x: ['-100%', '250%'] }}
            transition={reduce ? { duration: 0 } : { duration: 1.1, repeat: Infinity, ease: 'easeInOut' }}
            style={{
              height: '100%',
              width: '40%',
              background:
                'linear-gradient(90deg, transparent, rgb(var(--accent-rgb)), rgb(var(--accent-strong-rgb)), transparent)',
            }}
          />
        </m.div>
      ) : null}
      {/* Route-pulse bar — a single sweep triggered by each navigation.
          The key changes with every location object, which causes
          AnimatePresence to remount + animate the element, producing a
          one-shot 600 ms sweep without any setState. */}
      {!reduce ? (
        <m.div
          key={`topbar-route-${location.key}`}
          initial={{ opacity: 1, x: '-100%' }}
          animate={{ opacity: [1, 1, 0], x: '250%' }}
          transition={{ duration: 0.6, ease: 'easeOut' }}
          style={barContainerStyle}
        >
          <div
            style={{
              height: '100%',
              width: '40%',
              background:
                'linear-gradient(90deg, transparent, rgb(var(--accent-rgb)), rgb(var(--accent-strong-rgb)), transparent)',
            }}
          />
        </m.div>
      ) : null}
    </AnimatePresence>
  );
}
