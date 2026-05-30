import type { Tier } from './TierBadge';

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
