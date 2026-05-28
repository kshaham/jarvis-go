import { motion, useReducedMotion } from 'motion/react';
import type { HTMLMotionProps } from 'motion/react';
import type { ReactNode } from 'react';

import { spring } from '../../lib/motion';

type Props = Omit<HTMLMotionProps<'button'>, 'children'> & {
  children: ReactNode;
  // Scale on press. Default 0.96 — the design language's magnetic-press
  // value. Smaller chips can override down to 0.92 for more bite.
  pressScale?: number;
};

// Press is the canonical interactive surface for the office web UI.
// On press-in it springs to `pressScale` (default 0.96) with the snappy
// spring; the hover state lifts it 1% and warms the border. Mirrors
// the mobile <Press /> behavior so a press here feels like a tap there.
//
// Renders a <button> so it's accessible by default. Layer styles via
// className.
export function Press({
  children,
  pressScale = 0.96,
  className,
  style,
  ...rest
}: Props) {
  const reduce = useReducedMotion();
  const whileHover = reduce ? {} : { scale: 1.01 };
  const whileTap = reduce ? {} : { scale: pressScale };
  return (
    <motion.button
      whileHover={whileHover}
      whileTap={whileTap}
      transition={spring.snappy}
      className={className}
      style={style}
      {...rest}
    >
      {children}
    </motion.button>
  );
}
