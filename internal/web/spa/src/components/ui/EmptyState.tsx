import type { ReactNode } from 'react';
import type { Variants } from 'motion/react';
import { m, useReducedMotion } from 'motion/react';

import { cn } from '@/lib/cn';

// Mount stagger: the container orchestrates its children so the icon
// settles first, then the copy, then the action — a calm, intentional
// reveal. Reduced motion collapses every variant to a static, opaque
// resting state (no transform, no fade).
const containerVariants: Variants = {
  hidden: {},
  shown: { transition: { staggerChildren: 0.06, delayChildren: 0.04 } },
};

const iconVariants: Variants = {
  hidden: { opacity: 0, scale: 0.82 },
  shown: { opacity: 1, scale: 1, transition: { duration: 0.36, ease: [0.22, 1, 0.36, 1] } },
};

const itemVariants: Variants = {
  hidden: { opacity: 0, y: 6 },
  shown: { opacity: 1, y: 0, transition: { duration: 0.32, ease: [0.22, 1, 0.36, 1] } },
};

// EmptyState — the canonical "nothing here yet" panel: dashed token
// border, optional accent-tinted icon, title, description, and an
// optional action. Standardizes the scattered empty panels.
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: ReactNode;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  const reduceMotion = useReducedMotion();

  return (
    <m.div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-[14px] border border-dashed p-10 text-center',
        className,
      )}
      style={{ borderColor: 'var(--lg-rim-strong)' }}
      variants={containerVariants}
      // Reduced motion starts already at rest, so nothing animates in.
      initial={reduceMotion ? 'shown' : 'hidden'}
      animate="shown"
    >
      {icon ? (
        // Convex glass bead — inner highlight + accent bloom simulate depth
        // without nesting a second backdrop-filter (perf + artifact-safe).
        <m.div
          className="grid size-12 place-items-center rounded-full"
          style={{
            background: 'var(--lg-fill-accent)',
            border: '1px solid rgb(var(--accent-rgb) / 0.35)',
            boxShadow: 'var(--lg-specular-convex), 0 4px 16px rgb(var(--accent-rgb) / 0.2)',
            color: 'var(--accent)',
          }}
          variants={iconVariants}
        >
          {icon}
        </m.div>
      ) : null}
      <m.div className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }} variants={itemVariants}>
        {title}
      </m.div>
      {description ? (
        <m.div
          className="max-w-sm text-xs leading-relaxed"
          style={{ color: 'var(--text-muted)' }}
          variants={itemVariants}
        >
          {description}
        </m.div>
      ) : null}
      {action ? (
        <m.div className="mt-1" variants={itemVariants}>
          {action}
        </m.div>
      ) : null}
    </m.div>
  );
}
