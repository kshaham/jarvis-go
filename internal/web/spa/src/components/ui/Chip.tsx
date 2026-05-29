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
      className={cn('chip transition-colors', className)}
      style={{
        background: active ? 'var(--accent-muted)' : 'var(--bg-surface)',
        border: '1px solid ' + (active ? 'rgb(var(--accent-rgb))' : 'var(--border-default)'),
        color: active ? 'var(--accent)' : 'var(--text-muted)',
        cursor: 'pointer',
      }}
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
            onClick={() => onChange(opt.value)}
            title={opt.hint}
            className="chip focus-ring transition-colors"
            style={{
              background: selected ? 'var(--accent-muted)' : 'var(--bg-elevated)',
              border: '1px solid ' + (selected ? 'rgb(var(--accent-rgb))' : 'var(--border-default)'),
              color: selected ? 'var(--accent)' : 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
