import type { CSSProperties, ReactNode } from 'react';

import { cn } from '@/lib/cn';

// Section — a titled card on the elevated surface, with an optional hint
// and a trailing slot (e.g. a save button or status). The canonical
// container for grouped controls/content across the office screens.
export function Section({
  title,
  hint,
  trailing,
  children,
  className,
  style,
}: {
  title?: ReactNode;
  hint?: ReactNode;
  trailing?: ReactNode;
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
}) {
  return (
    <section className={cn('surface-card flex flex-col gap-2.5 p-4', className)} style={style}>
      {title || trailing ? (
        <header className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            {title ? (
              <h2 className="text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
                {title}
              </h2>
            ) : null}
            {hint ? (
              <p className="mt-0.5 text-xs leading-relaxed" style={{ color: 'var(--text-muted)' }}>
                {hint}
              </p>
            ) : null}
          </div>
          {trailing ? <div className="shrink-0">{trailing}</div> : null}
        </header>
      ) : null}
      {children}
    </section>
  );
}
