import { motion, useReducedMotion } from 'motion/react';
import type { HTMLMotionProps } from 'motion/react';
import { useRef, useState } from 'react';
import type { PointerEvent, ReactNode } from 'react';

import { cn } from '../../lib/cn';
import { spring } from '../../lib/motion';

type Ripple = { id: number; x: number; y: number };

type Props = Omit<HTMLMotionProps<'button'>, 'children'> & {
  children: ReactNode;
  // Scale on press. Default 0.96 — the design language's magnetic-press
  // value. Smaller chips can override down to 0.92 for more bite.
  pressScale?: number;
  // Violet glow box-shadow on hover.
  glow?: boolean;
  // Lift up 1px on hover (for raised CTAs).
  lift?: boolean;
  // Radial ripple from the pointer on press (auto-off under reduced motion).
  ripple?: boolean;
};

// Press is the canonical interactive surface for the office web UI.
// Press-in springs to `pressScale` (default 0.96); hover lifts 1% and
// can warm with a glow/lift; an optional ripple radiates from the tap.
// Renders an accessible <button> with a keyboard focus ring by default.
export function Press({
  children,
  pressScale = 0.96,
  glow = false,
  lift = false,
  ripple = false,
  className,
  style,
  onPointerDown,
  ...rest
}: Props) {
  const reduce = useReducedMotion();
  const idRef = useRef(0);
  const [ripples, setRipples] = useState<Ripple[]>([]);

  const whileHover = reduce
    ? undefined
    : {
        scale: 1.01,
        ...(lift ? { y: -1 } : {}),
        ...(glow ? { boxShadow: 'var(--glow-violet-sm)' } : {}),
      };
  const whileTap = reduce ? undefined : { scale: pressScale };

  function handlePointerDown(e: PointerEvent<HTMLButtonElement>) {
    if (ripple && !reduce) {
      const r = e.currentTarget.getBoundingClientRect();
      idRef.current += 1;
      setRipples((prev) => [
        ...prev,
        { id: idRef.current, x: e.clientX - r.left, y: e.clientY - r.top },
      ]);
    }
    onPointerDown?.(e);
  }

  return (
    <motion.button
      whileHover={whileHover}
      whileTap={whileTap}
      transition={spring.snappy}
      onPointerDown={handlePointerDown}
      className={cn('focus-ring', className)}
      style={ripple ? { position: 'relative', overflow: 'hidden', ...style } : style}
      {...rest}
    >
      {children}
      {ripple && !reduce
        ? ripples.map((r) => (
            <motion.span
              key={r.id}
              initial={{ scale: 0, opacity: 0.4 }}
              animate={{ scale: 2.5, opacity: 0 }}
              transition={{ duration: 0.5, ease: 'easeOut' }}
              onAnimationComplete={() =>
                setRipples((prev) => prev.filter((p) => p.id !== r.id))
              }
              style={{
                position: 'absolute',
                left: r.x,
                top: r.y,
                width: 14,
                height: 14,
                marginLeft: -7,
                marginTop: -7,
                borderRadius: '50%',
                background: 'rgb(var(--accent-rgb) / 0.5)',
                pointerEvents: 'none',
              }}
            />
          ))
        : null}
    </motion.button>
  );
}
