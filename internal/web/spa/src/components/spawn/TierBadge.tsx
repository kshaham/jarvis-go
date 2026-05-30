import type { CSSProperties } from 'react';

// Static base for the pill variant — hoisted to avoid per-render allocations.
const pillBaseStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: 6,
  padding: '2px 8px',
  borderRadius: 'var(--radius-pill)',
  fontSize: 12,
  fontWeight: 600,
};

export type Tier = 'strategic' | 'worker' | 'support';

type Variant = 'dot' | 'pill';

const TIER_COLORS: Record<Tier, string> = {
  strategic: 'var(--accent)',
  worker: 'var(--status-working)',
  support: 'var(--status-meeting)',
};

const TIER_LABELS: Record<Tier, string> = {
  strategic: 'Strategic',
  worker: 'Worker',
  support: 'Support',
};

export function tierColor(tier: Tier | undefined): string {
  return tier ? TIER_COLORS[tier] : 'var(--text-muted)';
}

export function tierLabel(tier: Tier | undefined): string {
  return tier ? TIER_LABELS[tier] : 'Other';
}

export function TierBadge({
  tier,
  variant = 'dot',
  count,
  style,
}: {
  tier: Tier | undefined;
  variant?: Variant;
  count?: number;
  style?: CSSProperties;
}) {
  const color = tierColor(tier);
  const label = tierLabel(tier);

  if (variant === 'dot') {
    return (
      <span
        aria-label={label}
        title={label}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
          fontSize: 12,
          color: 'var(--text-secondary)',
          ...style,
        }}
      >
        <span
          aria-hidden
          style={{
            display: 'inline-block',
            width: 8,
            height: 8,
            borderRadius: 999,
            background: color,
            boxShadow: '0 0 0 1px rgba(0,0,0,0.4) inset',
          }}
        />
        {typeof count === 'number' ? count : null}
      </span>
    );
  }

  return (
    <span
      aria-label={label}
      style={{
        ...pillBaseStyle,
        background: 'color-mix(in srgb, ' + color + ' 16%, transparent)',
        border: '1px solid color-mix(in srgb, ' + color + ' 36%, transparent)',
        color,
        ...style,
      }}
    >
      <span
        aria-hidden
        style={{
          display: 'inline-block',
          width: 6,
          height: 6,
          borderRadius: 999,
          background: color,
        }}
      />
      {label}
      {typeof count === 'number' ? ` · ${count}` : null}
    </span>
  );
}

// Compact horizontal tier-count summary used on cards and the sticky bar.
export function TierCounts({
  members,
  style,
}: {
  members: { tier?: Tier }[];
  style?: CSSProperties;
}) {
  const counts: Record<Tier, number> = { strategic: 0, worker: 0, support: 0 };
  let other = 0;
  for (const m of members) {
    if (m.tier && m.tier in counts) {
      counts[m.tier]++;
    } else {
      other++;
    }
  }

  return (
    <span style={{ display: 'inline-flex', gap: 10, alignItems: 'center', ...style }}>
      {(['strategic', 'worker', 'support'] as Tier[]).map((t) =>
        counts[t] > 0 ? <TierBadge key={t} tier={t} count={counts[t]} /> : null,
      )}
      {other > 0 ? <TierBadge tier={undefined} count={other} /> : null}
    </span>
  );
}
