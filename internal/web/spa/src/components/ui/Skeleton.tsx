import type { CSSProperties } from 'react';

import { cn } from '@/lib/cn';

// Skeleton — a single shimmering placeholder block. Use to compose
// loading states (replaces ad-hoc animate-pulse divs). Shimmer is
// killed under prefers-reduced-motion via styles.css.
export function Skeleton({
  className,
  style,
}: {
  className?: string;
  style?: CSSProperties;
}) {
  return <div className={cn('skeleton', className)} style={style} aria-hidden="true" />;
}
