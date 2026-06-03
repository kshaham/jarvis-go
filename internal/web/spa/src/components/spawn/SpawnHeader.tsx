import { Fragment } from 'react';
import type { CSSProperties, ReactNode } from 'react';

const stepCircleBaseStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: 18,
  height: 18,
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 700,
};

const headerStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  gap: 16,
  flexWrap: 'wrap',
  paddingBottom: 12,
  borderBottom: '1px solid var(--border-subtle)',
};

const errorBoxStyle: CSSProperties = {
  padding: 10,
  background: 'rgba(255,90,95,0.10)',
  border: '1px solid var(--status-error)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--status-error)',
  fontSize: 12,
};

const STEP_ITEMS: { n: 1 | 2; label: string }[] = [
  { n: 1, label: 'Template' },
  { n: 2, label: 'Configure' },
];

// SpawnHeader — the full-width command strip shared by both spawn steps:
// title + subtitle + step indicator on the left, the Spawn control (step 2)
// on the right, with any spawn error surfaced below.
export function SpawnHeader({
  step,
  subtitle,
  right,
  error,
}: {
  step: 1 | 2;
  subtitle: string;
  right: ReactNode;
  error: string | null;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <header style={headerStyle}>
        <div style={{ minWidth: 0 }}>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Spawn team
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>{subtitle}</p>
          <StepIndicator step={step} />
        </div>
        {right ? (
          <div style={{ display: 'inline-flex', alignItems: 'center', gap: 12, flexShrink: 0 }}>
            {right}
          </div>
        ) : null}
      </header>
      {error ? (
        <div role="alert" style={errorBoxStyle}>
          {error}
        </div>
      ) : null}
    </div>
  );
}

// StepIndicator — orients the two-page flow (Template → Configure). The
// active step is the accent color; a completed step keeps a filled marker.
function StepIndicator({ step }: { step: 1 | 2 }) {
  return (
    <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8, marginTop: 12 }}>
      {STEP_ITEMS.map((s, i) => (
        <Fragment key={s.n}>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              fontSize: 12,
              fontWeight: 600,
              color: step === s.n ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <span
              style={{
                ...stepCircleBaseStyle,
                background: step >= s.n ? 'var(--accent)' : 'var(--bg-elevated)',
                color: step >= s.n ? '#fff' : 'var(--text-muted)',
                border: '1px solid ' + (step >= s.n ? 'var(--accent)' : 'var(--border-default)'),
              }}
            >
              {s.n}
            </span>
            {s.label}
          </span>
          {i < STEP_ITEMS.length - 1 ? <span style={{ color: 'var(--text-faint)' }}>→</span> : null}
        </Fragment>
      ))}
    </div>
  );
}
