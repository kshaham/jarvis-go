import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
import { Button } from '@/components/ui';
import { Avatar } from './Avatar';
import { leadOf } from './templateHelpers';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';

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
    <aside
      style={{
        padding: 12,
        background: 'var(--bg-surface)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-card)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        ...style,
      }}
    >
      <header style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
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
        <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
          <span
            style={{
              fontSize: 11,
              fontWeight: 600,
              color: 'var(--text-muted)',
              textTransform: 'uppercase',
              letterSpacing: 0.4,
            }}
          >
            Team
          </span>
          <span
            style={{
              fontSize: 15,
              fontWeight: 700,
              color: 'var(--text-primary)',
              lineHeight: 1.3,
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
            {tpl.name}
          </span>
        </div>
      </header>

      <TierCounts members={tpl.members.map((m) => ({ tier: m.tier as Tier | undefined }))} />

      <Button variant="secondary" size="sm" onClick={onChange} style={{ alignSelf: 'flex-start' }}>
        Change template
      </Button>
    </aside>
  );
}
