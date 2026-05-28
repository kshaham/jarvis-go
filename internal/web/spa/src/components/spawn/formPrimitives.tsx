import type { CSSProperties, ReactNode } from 'react';

export const inputStyle: CSSProperties = {
  padding: 8,
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
};

export function SectionCard({
  title,
  hint,
  trailing,
  children,
  style,
}: {
  title: string;
  hint?: string;
  trailing?: ReactNode;
  children: ReactNode;
  style?: CSSProperties;
}) {
  return (
    <section
      style={{
        padding: 14,
        background: 'var(--bg-surface)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-card)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        ...style,
      }}
    >
      <header
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: 12,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <h2 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>
            {title}
          </h2>
          {hint ? (
            <p style={{ margin: '2px 0 0', fontSize: 11, color: 'var(--text-muted)' }}>{hint}</p>
          ) : null}
        </div>
        {trailing ? <div style={{ flexShrink: 0 }}>{trailing}</div> : null}
      </header>
      {children}
    </section>
  );
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      {children}
      {hint ? (
        <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span>
      ) : null}
    </label>
  );
}

export function Toggle({
  label,
  value,
  onChange,
  hint,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
  hint?: string;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      <span
        role="switch"
        aria-checked={value}
        onClick={() => onChange(!value)}
        style={{
          position: 'relative',
          display: 'inline-flex',
          alignItems: 'center',
          width: 44,
          height: 24,
          padding: 2,
          background: value ? 'var(--accent)' : 'var(--bg-pressed)',
          border: '1px solid ' + (value ? 'var(--accent)' : 'var(--border-default)'),
          borderRadius: 999,
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
      </span>
      {hint ? (
        <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span>
      ) : null}
    </label>
  );
}

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
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}
    >
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
            style={{
              padding: '6px 12px',
              background: selected ? 'var(--accent-muted)' : 'var(--bg-elevated)',
              border: '1px solid ' + (selected ? 'var(--accent)' : 'var(--border-default)'),
              borderRadius: 'var(--radius-pill)',
              color: selected ? 'var(--accent)' : 'var(--text-secondary)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
              transition: 'background 120ms ease, color 120ms ease, border-color 120ms ease',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  ariaLabel?: string;
}) {
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        padding: 3,
        background: 'var(--bg-app)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-md)',
        gap: 2,
      }}
    >
      {options.map((opt) => {
        const selected = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="tab"
            aria-selected={selected}
            onClick={() => onChange(opt.value)}
            style={{
              padding: '6px 14px',
              background: selected ? 'var(--bg-elevated)' : 'transparent',
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              color: selected ? 'var(--text-primary)' : 'var(--text-muted)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
              transition: 'background 120ms ease, color 120ms ease',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
