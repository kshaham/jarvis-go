import { m, useReducedMotion } from 'motion/react';
import type { HTMLMotionProps } from 'motion/react';
import type { ReactNode } from 'react';

import { spring, STAGGER_LIST, timing } from '../../lib/motion';

type Props = Omit<HTMLMotionProps<'div'>, 'children' | 'exit'> & {
  // Index in the parent list — drives the per-item stagger delay.
  index: number;
  children: ReactNode;
  // Override the y-translate distance (default 8px).
  yOffset?: number;
  // Lift on hover (opt-in — keep dense lists calm by default).
  hover?: boolean;
  // Animate out when removed (requires the caller to wrap the list in
  // <AnimatePresence> and key each item by a stable id). Default on.
  withExit?: boolean;
};

// Spring-on-mount staggered list item. Wrap every list-row return in
// <RevealItem index={index}> to get the canonical 24ms stagger, 8px
// rise, opacity 0→1 reveal. Mirrors the mobile RevealItem so lists
// feel the same on both surfaces. With <AnimatePresence>, removed rows
// animate out (opacity + reverse rise).
export function RevealItem({
  index,
  children,
  yOffset = 8,
  hover = false,
  withExit = true,
  className,
  style,
  ...rest
}: Props) {
  const reduce = useReducedMotion();
  const variants = {
    hidden: reduce ? { opacity: 0 } : { opacity: 0, y: yOffset },
    visible: reduce ? { opacity: 1 } : { opacity: 1, y: 0 },
    exit: reduce
      ? { opacity: 0, transition: timing.short }
      : { opacity: 0, y: -yOffset, transition: timing.short },
  };
  return (
    <m.div
      variants={variants}
      initial="hidden"
      animate="visible"
      exit={withExit ? 'exit' : undefined}
      transition={{ ...spring.smooth, delay: index * STAGGER_LIST }}
      whileHover={hover && !reduce ? { y: -2 } : undefined}
      className={className}
      style={style}
      {...rest}
    >
      {children}
    </m.div>
  );
}
