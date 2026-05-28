import type { CSSProperties } from 'react';
import { Press } from '../motion';
import { Avatar } from './Avatar';
import { tierColor, type Tier } from './TierBadge';

export function MemberTile({
  name,
  role,
  emoji,
  color,
  tier,
  selected,
  edited,
  onClick,
  style,
}: {
  name: string;
  role: string;
  emoji?: string;
  color?: string;
  tier?: Tier;
  selected?: boolean;
  edited?: boolean;
  onClick?: () => void;
  style?: CSSProperties;
}) {
  const ring = selected ? 'var(--accent)' : 'var(--border-subtle)';
  const bg = selected ? 'var(--accent-muted)' : 'var(--bg-elevated)';

  return (
    <Press pressScale={0.96} onClick={onClick}>
      <article
        aria-label={`${name}, ${role}`}
        aria-pressed={!!selected}
        style={{
          position: 'relative',
          width: 96,
          padding: '10px 8px 8px',
          background: bg,
          border: `1px solid ${ring}`,
          borderRadius: 'var(--radius-md)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 6,
          cursor: 'pointer',
          transition: 'background 120ms ease, border-color 120ms ease',
          ...style,
        }}
      >
        {edited ? (
          <span
            aria-hidden
            title="Edited"
            style={{
              position: 'absolute',
              top: 6,
              right: 6,
              width: 8,
              height: 8,
              borderRadius: 999,
              background: 'var(--status-warn)',
              boxShadow: '0 0 0 2px var(--bg-elevated)',
            }}
          />
        ) : null}
        <Avatar name={name} emoji={emoji} color={color} tier={tier} size="md" ring />
        <span
          style={{
            fontSize: 12,
            fontWeight: 600,
            color: 'var(--text-primary)',
            maxWidth: '100%',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {name}
        </span>
        <span
          style={{
            fontSize: 10,
            color: 'var(--text-muted)',
            maxWidth: '100%',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            textTransform: 'uppercase',
            letterSpacing: 0.4,
          }}
        >
          {role}
        </span>
        {tier ? (
          <span
            aria-hidden
            style={{
              position: 'absolute',
              left: 8,
              top: 8,
              width: 6,
              height: 6,
              borderRadius: 999,
              background: tierColor(tier),
            }}
          />
        ) : null}
      </article>
    </Press>
  );
}
