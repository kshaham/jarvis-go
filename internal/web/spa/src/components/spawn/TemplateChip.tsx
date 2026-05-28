import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
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
        </div>
      </header>

      <TierCounts members={tpl.members.map((m) => ({ tier: m.tier as Tier | undefined }))} />

      <button
        type="button"
        onClick={onChange}
        style={{
          padding: '6px 10px',
          background: 'var(--bg-pressed)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-md)',
          color: 'var(--text-primary)',
          fontSize: 12,
          fontWeight: 600,
          cursor: 'pointer',
          alignSelf: 'flex-start',
        }}
      >
        Change template
      </button>
    </aside>
  );
}
