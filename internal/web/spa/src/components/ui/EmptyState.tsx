import type { ReactNode } from 'react';

import { cn } from '@/lib/cn';

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
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 rounded-[14px] border border-dashed p-10 text-center',
        className,
      )}
      style={{ borderColor: 'var(--border-default)' }}
    >
      {icon ? (
        <div
          className="grid h-12 w-12 place-items-center rounded-full"
          style={{ background: 'var(--accent-muted)', color: 'var(--accent)' }}
        >
          {icon}
        </div>
      ) : null}
      <div className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
        {title}
      </div>
      {description ? (
        <div className="max-w-sm text-xs leading-relaxed" style={{ color: 'var(--text-muted)' }}>
          {description}
        </div>
      ) : null}
      {action ? <div className="mt-1">{action}</div> : null}
    </div>
  );
}
