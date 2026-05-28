import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
import { Press } from '../motion';
import { Avatar, AvatarStack } from './Avatar';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';

// Pick the avatar that "represents" the template: prefer a strategic
// member (the lead), fall back to the first member.
function leadOf(tpl: TeamTemplate) {
  return (
    tpl.members.find((m) => (m.tier as Tier | undefined) === 'strategic') ??
    tpl.members[0]
  );
}

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
  const ring = selected ? 'var(--accent)' : 'var(--border-subtle)';
  const bg = selected ? 'var(--accent-muted)' : 'var(--bg-elevated)';

  return (
    <Press pressScale={0.98} onClick={onSelect}>
      <article
        aria-pressed={selected}
        style={{
          padding: 12,
          background: bg,
          border: `1px solid ${ring}`,
          borderRadius: 'var(--radius-card)',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          cursor: 'pointer',
          transition: 'background 120ms ease, border-color 120ms ease',
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
          <span
            style={{
              flex: 1,
              minWidth: 0,
              fontSize: 14,
              fontWeight: 700,
              color: 'var(--text-primary)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {tpl.name}
          </span>
        </header>

        {tpl.description ? (
          <p
            style={{
              margin: 0,
              fontSize: 12,
              color: 'var(--text-secondary)',
              lineHeight: 1.4,
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
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
            <span
              style={{
                padding: '2px 8px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 'var(--radius-pill)',
                color: 'var(--text-muted)',
                fontSize: 11,
                fontWeight: 600,
                textTransform: 'uppercase',
                letterSpacing: 0.4,
              }}
            >
              {tpl.category}
            </span>
          ) : null}
        </footer>
      </article>
    </Press>
  );
}
