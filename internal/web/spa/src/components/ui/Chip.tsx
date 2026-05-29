import type { ReactNode } from 'react';

import { Press } from '@/components/motion/Press';
import { cn } from '@/lib/cn';

// Chip — a single pill toggle/filter (active/inactive). Press gives the
// magnetic tap + a focus ring. Use for filter rows (hide-idle,
// priority, kind filters).
export function Chip({
  active = false,
  onClick,
  children,
  className,
}: {
  active?: boolean;
  onClick?: () => void;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Press
      pressScale={0.94}
      onClick={onClick}
      data-active={active}
      className={cn('chip lg-chip', className)}
    >
      {children}
    </Press>
  );
}

// ChipGroup — single-select pill group (radiogroup semantics).
export function ChipGroup<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: { value: T; label: string; hint?: string }[];
  value: T;
  onChange: (v: T) => void;
  ariaLabel?: string;
}) {
  return (
    <div role="radiogroup" aria-label={ariaLabel} className="flex flex-wrap gap-1.5">
      {options.map((opt) => {
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={selected}
            data-active={selected}
            onClick={() => onChange(opt.value)}
            title={opt.hint}
            className="chip lg-chip focus-ring"
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
