import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
import { Avatar, AvatarStack } from './Avatar';
import { leadOf } from './templateHelpers';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';

const cardNameStyle: CSSProperties = {
  flex: 1,
  minWidth: 0,
  fontSize: 15,
  fontWeight: 700,
  color: 'var(--text-primary)',
  lineHeight: 1.3,
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical',
  overflow: 'hidden',
};

const cardDescStyle: CSSProperties = {
  margin: 0,
  fontSize: 12,
  color: 'var(--text-secondary)',
  lineHeight: 1.4,
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical',
  overflow: 'hidden',
};

const categoryChipStyle: CSSProperties = {
  padding: '2px 8px',
  background: 'var(--bg-pressed)',
  border: '1px solid var(--border-subtle)',
  borderRadius: 'var(--radius-pill)',
  color: 'var(--text-muted)',
  fontSize: 12,
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: 0.4,
};

export function TemplateCard({
  tpl,
  selected,
  onSelect,
  style,
}: {
  tpl: TeamTemplate;
  selected: boolean;
  onSelect: () => void;
  style?: CSSProperties;
}) {
  const lead = leadOf(tpl);

  // Flat-minimal card. Selection = an accent tint + a 2px inset accent ring
  // (box-shadow, so it never shifts layout); hover is a flat background change
  // handled by the .spawn-template-card CSS — no glow halo.
  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect();
        }
      }}
      className="spawn-template-card focus-ring"
      style={{
        ...(selected
          ? {
              background: 'var(--accent-subtle)',
              boxShadow: 'inset 0 0 0 2px var(--accent-border)',
            }
          : null),
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
        <span style={cardNameStyle}>
          {tpl.name}
        </span>
      </header>

      {tpl.description ? (
        <p style={cardDescStyle}>
          {tpl.description}
        </p>
      ) : null}

      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <AvatarStack
          members={tpl.members.map((m) => ({
            name: m.name,
            emoji: m.emoji,
            color: m.color,
            tier: m.tier as Tier | undefined,
          }))}
          max={5}
          size="sm"
        />
      </div>

      <footer
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 8,
        }}
      >
        <TierCounts members={tpl.members.map((m) => ({ tier: m.tier as Tier | undefined }))} />
        {tpl.category ? (
          <span style={categoryChipStyle}>
            {tpl.category}
          </span>
        ) : null}
      </footer>
    </div>
  );
}
