// Motion presets — same spring constants as jarvis-mobile/src/theme/motion.ts.
// A press here should feel like a tap on mobile.
//
// Consumers pass these directly to Motion's `transition` prop:
//   <motion.div animate={{ scale: 1 }} transition={spring.snappy} />

import type { Transition } from 'motion/react';

export const spring = {
  snappy: {
    type: 'spring',
    stiffness: 380,
    damping: 30,
    mass: 0.7,
  } satisfies Transition,
  smooth: {
    type: 'spring',
    stiffness: 220,
    damping: 26,
    mass: 1,
  } satisfies Transition,
  gentle: {
    type: 'spring',
    stiffness: 120,
    damping: 22,
    mass: 1.2,
  } satisfies Transition,
} as const;

export const timing = {
  instant: { duration: 0.08, ease: [0.16, 1, 0.3, 1] } satisfies Transition,
  short: { duration: 0.18, ease: [0.16, 1, 0.3, 1] } satisfies Transition,
  long: { duration: 0.32, ease: [0.16, 1, 0.3, 1] } satisfies Transition,
} as const;

// 24ms stagger between siblings when a list mounts. Multiply by item
// index when manually staggering; or pass directly as
// transition={{ ...spring.smooth, delay: index * STAGGER_LIST }}.
export const STAGGER_LIST = 0.024;
