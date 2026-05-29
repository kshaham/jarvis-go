import { cva, type VariantProps } from 'class-variance-authority';
import type { ComponentProps, ReactNode } from 'react';

import { Press } from '@/components/motion/Press';
import { cn } from '@/lib/cn';

// Button — the canonical CTA. Built on Press so it keeps the magnetic
// 0.96 tap + reduced-motion behavior, with cva variants for the four
// semantic roles. Replaces ad-hoc <button style={{…}}> across screens.
const button = cva(
  'inline-flex items-center justify-center gap-2 rounded-md font-semibold focus-ring transition-colors disabled:opacity-50 disabled:cursor-not-allowed',
  {
    variants: {
      variant: {
        primary: 'bg-accent text-white hover:shadow-glow-sm',
        secondary:
          'border bg-[var(--bg-elevated)] border-[var(--border-default)] text-[var(--text-primary)] hover:border-[var(--border-strong)]',
        ghost: 'text-[var(--text-secondary)] hover:bg-[var(--bg-elevated)]',
        danger:
          'border border-status-error/40 text-status-error hover:bg-status-error hover:text-white',
      },
      size: {
        sm: 'px-3 py-1.5 text-xs',
        md: 'px-4 py-2 text-sm',
      },
    },
    defaultVariants: { variant: 'primary', size: 'md' },
  },
);

type Props = Omit<ComponentProps<typeof Press>, 'children'> &
  VariantProps<typeof button> & {
    children: ReactNode;
  };

export function Button({ variant, size, className, children, ...rest }: Props) {
  return (
    <Press className={cn(button({ variant, size }), className)} {...rest}>
      {children}
    </Press>
  );
}
