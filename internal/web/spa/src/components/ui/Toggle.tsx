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
          background: value ? 'rgb(var(--accent-rgb) / 0.9)' : 'var(--lg-fill)',
          border: '1px solid ' + (value ? 'rgb(var(--accent-rgb))' : 'var(--lg-rim)'),
          boxShadow: value
            ? 'var(--lg-specular-concave), var(--glow-violet-sm)'
            : 'var(--lg-specular-concave)',
          WebkitBackdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
          backdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
          cursor: 'pointer',
          transition: 'background 160ms cubic-bezier(0.16,1,0.3,1), border-color 160ms ease, box-shadow 160ms ease',
        }}
      >
        {/* Glossy convex knob — a light-catching glass bead. */}
        <span
          aria-hidden
          style={{
            position: 'absolute',
            left: value ? 22 : 2,
            top: 2,
            width: 18,
            height: 18,
            borderRadius: 999,
            background: 'linear-gradient(180deg, #ffffff 0%, #dfe3e7 100%)',
            transition: 'left 180ms cubic-bezier(0.16,1,0.3,1)',
            boxShadow:
              '0 1px 2px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.95), inset 0 -2px 3px rgba(0,0,0,0.12)',
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
