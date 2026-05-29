import { cva, type VariantProps } from 'class-variance-authority';
import type { ComponentProps, ReactNode } from 'react';

import { Press } from '@/components/motion/Press';
import { cn } from '@/lib/cn';

// Button — the canonical CTA. Built on Press so it keeps the magnetic
// 0.96 tap + reduced-motion behavior. The liquid-glass material (fill,
// rim, specular sheen, blur) lives in the `.lg-button` class family in
// styles.css; cva just selects the variant modifier + sizing. The pill
// radius comes from `.lg-button`. Replaces ad-hoc <button style={{…}}>.
const button = cva(
  'lg-button inline-flex items-center justify-center gap-2 font-semibold disabled:opacity-50 disabled:cursor-not-allowed',
  {
    variants: {
      variant: {
        // Neutral glass is the base `.lg-button` (used by secondary).
        primary: 'lg-button--primary',
        secondary: '',
        ghost: 'lg-button--ghost',
        danger: 'lg-button--danger',
      },
      size: {
        sm: 'px-3.5 py-1.5 text-xs',
        md: 'px-5 py-2 text-sm',
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
