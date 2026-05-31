import { useEffect, useRef, useState } from 'react';
import { ChevronDown, X } from 'lucide-react';

import { cn } from '@/lib/cn';

// SelectOption — one row in the Select popover. sublabel renders as a
// dimmed second line (e.g. a path or repo URL), tag as a small pill
// (e.g. "local" / "github"), and onRemove (when present) adds a trailing
// remove control that does not trigger selection.
export interface SelectOption {
  value: string;
  label: string;
  sublabel?: string;
  tag?: string;
  onRemove?: () => void;
}

// Select — a button + popover listbox styled to the Aurora glass system.
// A native <select> can't render the two-line + remove rows this needs,
// so this is a custom control with listbox semantics. Closes on outside
// click and Escape.
export function Select({
  value,
  options,
  placeholder = 'Select…',
  onChange,
  ariaLabel,
}: {
  value: string | null;
  options: SelectOption[];
  placeholder?: string;
  onChange: (value: string) => void;
  ariaLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const selected = options.find((o) => o.value === value) ?? null;

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        onClick={() => setOpen((v) => !v)}
        className="field flex w-full items-center justify-between gap-2 text-left"
      >
        <span
          className="truncate"
          style={{ color: selected ? 'var(--text-primary)' : 'var(--text-muted)' }}
        >
          {selected ? selected.label : placeholder}
        </span>
        <ChevronDown size={16} style={{ color: 'var(--text-faint)', flexShrink: 0 }} />
      </button>

      {open ? (
        <ul
          role="listbox"
          aria-label={ariaLabel}
          className="surface-card absolute z-20 mt-1 max-h-64 w-full overflow-auto p-1"
          style={{ left: 0 }}
        >
          {options.length === 0 ? (
            <li className="px-2 py-2 text-[11px]" style={{ color: 'var(--text-muted)' }}>
              No options
            </li>
          ) : (
            options.map((opt) => (
              <li key={opt.value} role="option" aria-selected={opt.value === value}>
                <div
                  className={cn(
                    'group flex items-center gap-2 rounded-md px-2 py-1.5',
                    'hover:bg-[var(--bg-elevated)]',
                  )}
                  data-active={opt.value === value}
                >
                  <button
                    type="button"
                    onClick={() => {
                      onChange(opt.value);
                      setOpen(false);
                    }}
                    className="flex min-w-0 flex-1 flex-col text-left"
                  >
                    <span className="flex items-center gap-1.5">
                      <span className="truncate text-[13px]" style={{ color: 'var(--text-primary)' }}>
                        {opt.label}
                      </span>
                      {opt.tag ? (
                        <span
                          className="rounded px-1 text-[10px] uppercase tracking-wide"
                          style={{
                            color: 'var(--text-faint)',
                            background: 'var(--bg-elevated)',
                          }}
                        >
                          {opt.tag}
                        </span>
                      ) : null}
                    </span>
                    {opt.sublabel ? (
                      <span
                        className="truncate font-mono text-[11px]"
                        style={{ color: 'var(--text-muted)' }}
                      >
                        {opt.sublabel}
                      </span>
                    ) : null}
                  </button>
                  {opt.onRemove ? (
                    <button
                      type="button"
                      aria-label={`Remove ${opt.label}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        opt.onRemove?.();
                      }}
                      className="shrink-0 rounded p-1 opacity-0 transition-opacity hover:bg-[var(--bg-elevated)] group-hover:opacity-100"
                      style={{ color: 'var(--text-faint)' }}
                    >
                      <X size={13} />
                    </button>
                  ) : null}
                </div>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
