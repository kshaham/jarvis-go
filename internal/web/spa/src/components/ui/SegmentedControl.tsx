// SegmentedControl — a compact tab/segmented switch on the inset
// surface. Token-styled with keyboard focus rings.
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
        gap: 2,
        background: 'var(--bg-app)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-md)',
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
            className="focus-ring"
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
