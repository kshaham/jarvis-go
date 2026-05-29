import type { ReactNode } from 'react';

// Toggle — accessible switch (keyboard-focusable button with a focus
// ring), token-styled. Optional label + hint compose a labelled field.
export function Toggle({
  label,
  value,
  onChange,
  hint,
}: {
  label?: ReactNode;
  value: boolean;
  onChange: (v: boolean) => void;
  hint?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      {label ? (
        <span className="text-xs font-semibold" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </span>
      ) : null}
      <button
        type="button"
        role="switch"
        aria-checked={value}
        onClick={() => onChange(!value)}
        className="focus-ring relative inline-flex shrink-0 items-center"
        style={{
          width: 44,
          height: 24,
          padding: 2,
          borderRadius: 999,
          background: value ? 'var(--accent)' : 'var(--bg-pressed)',
          border: '1px solid ' + (value ? 'var(--accent)' : 'var(--border-default)'),
          cursor: 'pointer',
          transition: 'background 120ms ease, border-color 120ms ease',
        }}
      >
        <span
          aria-hidden
          style={{
            position: 'absolute',
            left: value ? 22 : 2,
            top: 2,
            width: 18,
            height: 18,
            borderRadius: 999,
            background: '#fff',
            transition: 'left 140ms ease',
            boxShadow: '0 1px 2px rgba(0,0,0,0.3)',
          }}
        />
      </button>
      {hint ? (
        <span className="text-[11px]" style={{ color: 'var(--text-faint)', lineHeight: 1.4 }}>
          {hint}
        </span>
      ) : null}
    </div>
  );
}
