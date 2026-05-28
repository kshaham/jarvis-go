import { motion, useReducedMotion } from 'motion/react';
import type { HTMLMotionProps } from 'motion/react';
import type { ReactNode } from 'react';

import { spring, STAGGER_LIST } from '../../lib/motion';

type Props = Omit<HTMLMotionProps<'div'>, 'children'> & {
  // Index in the parent list — drives the per-item stagger delay.
  index: number;
  children: ReactNode;
  // Override the y-translate distance (default 8px).
  yOffset?: number;
};

// Spring-on-mount staggered list item. Wrap every list-row return in
// <RevealItem index={index}> to get the canonical 24ms stagger, 8px
// rise, opacity 0→1 reveal. Mirrors the mobile RevealItem so lists
// feel the same on both surfaces.
export function RevealItem({
  index,
  children,
  yOffset = 8,
  className,
  style,
  ...rest
}: Props) {
  const reduce = useReducedMotion();
  return (
    <motion.div
      initial={reduce ? { opacity: 0 } : { opacity: 0, y: yOffset }}
      animate={reduce ? { opacity: 1 } : { opacity: 1, y: 0 }}
      transition={{ ...spring.smooth, delay: index * STAGGER_LIST }}
      className={className}
      style={style}
      {...rest}
    >
      {children}
    </motion.div>
  );
}
