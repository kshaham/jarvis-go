import { motion, useReducedMotion } from 'motion/react';
import type { HTMLMotionProps } from 'motion/react';
import { useRef } from 'react';
import type { PointerEvent, ReactNode } from 'react';

import { cn } from '@/lib/cn';
import { spring } from '@/lib/motion';

type Props = HTMLMotionProps<'div'> & {
  // Hover lift (y:-2) + box-shadow grow on hover.
  interactive?: boolean;
  // Signature violet glow at rest — for surfaces representing a live /
  // working agent. Grows to the md tier on hover when interactive.
  live?: boolean;
  // Glassmorphic (backdrop-blur) surface instead of the opaque card.
  // Reserve for floating/overlay surfaces — not large always-on panels.
  glass?: boolean;
  // Cursor-follow radial spotlight (auto-disabled under reduced motion).
  spotlight?: boolean;
  children?: ReactNode;
};

// GlowCard — the canonical Aurora surface. Generalizes the bespoke
// motion.article that AgentCard used to inline: depth (surface/glass),
// the violet live-glow, hover lift, and the cursor spotlight, all
// reduced-motion aware. Layer everything else via className.
export function GlowCard({
  interactive = false,
  live = false,
  glass = false,
  spotlight = false,
  className,
  children,
  onPointerMove,
  ...rest
}: Props) {
  const reduce = useReducedMotion();
  const ref = useRef<HTMLDivElement>(null);
  const spotlightOn = spotlight && !reduce;

  function handlePointerMove(e: PointerEvent<HTMLDivElement>) {
    if (spotlightOn && ref.current) {
      const r = ref.current.getBoundingClientRect();
      ref.current.style.setProperty('--mx', `${e.clientX - r.left}px`);
      ref.current.style.setProperty('--my', `${e.clientY - r.top}px`);
    }
    onPointerMove?.(e);
  }

  return (
    <motion.div
      ref={ref}
      onPointerMove={handlePointerMove}
      whileHover={interactive && !reduce ? { y: -2 } : undefined}
      transition={spring.smooth}
      className={cn(
        glass ? 'glass-card' : 'surface-card',
        spotlightOn && 'spotlight',
        interactive && 'transition-shadow duration-200',
        // Live keeps the glass depth shadow AND gains the violet bloom
        // (composite token), so the specular/float aren't lost. Hover
        // rises the whole stack one tier.
        live && 'shadow-lg-live',
        interactive && (live ? 'hover:shadow-lg-live-hover' : 'hover:shadow-lg-hover'),
        className,
      )}
      {...rest}
    >
      {children}
    </motion.div>
  );
}
