import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
import { Button } from '@/components/ui';
import { Avatar } from './Avatar';
import { leadOf } from './templateHelpers';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';

const chipAsideStyle: CSSProperties = {
  padding: 12,
  background: 'var(--lg-fill)',
  border: '1px solid var(--lg-rim)',
  borderRadius: 'var(--radius-card)',
  WebkitBackdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  boxShadow: 'var(--lg-shadow-rest)',
  display: 'flex',
  alignItems: 'center',
  gap: 12,
  flexWrap: 'wrap' as const,
};

const teamLabelStyle: CSSProperties = {
  fontSize: 12,
  fontWeight: 600,
  color: 'var(--text-muted)',
  textTransform: 'uppercase' as const,
  letterSpacing: 0.4,
};

const chipNameStyle: CSSProperties = {
  fontSize: 15,
  fontWeight: 700,
  color: 'var(--text-primary)',
  lineHeight: 1.3,
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical',
  overflow: 'hidden',
};

export function TemplateChip({
  tpl,
  onChange,
  style,
}: {
  tpl: TeamTemplate;
  onChange: () => void;
  style?: CSSProperties;
}) {
  const lead = leadOf(tpl);
  return (
    <aside style={{ ...chipAsideStyle, ...style }}>
      {lead ? (
        <Avatar
          name={lead.name}
          emoji={lead.emoji}
          color={lead.color}
          tier={lead.tier as Tier | undefined}
          size="md"
          ring
        />
      ) : null}
      <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: '1 1 160px' }}>
        <span style={teamLabelStyle}>Team</span>
        <span style={chipNameStyle}>{tpl.name}</span>
      </div>

      <TierCounts members={tpl.members.map((m) => ({ tier: m.tier as Tier | undefined }))} />

      <Button variant="secondary" size="sm" onClick={onChange}>
        Change template
      </Button>
    </aside>
  );
}
