import type { CSSProperties } from 'react';
import { m, useReducedMotion } from 'motion/react';

import { GlowCard } from '@/components/ui';
import { cn } from '@/lib/cn';
import { Avatar } from './Avatar';
import { TierBadge, type Tier } from './TierBadge';

const editedBadgeStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: 4,
  padding: '2px 8px',
  borderRadius: 'var(--radius-pill)',
  background: 'color-mix(in srgb, var(--status-warn) 16%, transparent)',
  border: '1px solid color-mix(in srgb, var(--status-warn) 36%, transparent)',
  color: 'var(--status-warn)',
  fontSize: 12,
  fontWeight: 600,
};

// MemberTile — a roomy roster card. Shows the agent's avatar plus the
// FULL name and role (wrapping, never truncated) and a tier pill, so an
// operator can read who's on the team at a glance. Built on GlowCard for
// the shared depth/hover/live-glow; selected = accent ring + glow.
export function MemberTile({
  id,
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
  id?: string;
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
  const reduce = useReducedMotion();
  return (
    <GlowCard
      id={id}
      role="option"
      aria-selected={!!selected}
      aria-label={`${name || 'Unnamed'}${role ? `, ${role}` : ''}`}
      interactive
      live={selected}
      onClick={onClick}
      className={cn('flex cursor-pointer items-start gap-3 p-3')}
      style={{
        minHeight: 88,
        background: selected ? 'var(--lg-fill-accent)' : undefined,
        border: selected ? '1px solid rgb(var(--accent-rgb) / 0.6)' : undefined,
        ...style,
      }}
    >
      <Avatar name={name} emoji={emoji} color={color} tier={tier} size="lg" ring />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {/* Full name — wraps, never clipped. */}
        <span
          style={{
            fontSize: 15,
            fontWeight: 600,
            color: 'var(--text-primary)',
            lineHeight: 1.3,
            overflowWrap: 'anywhere',
          }}
        >
          {name || 'Unnamed'}
        </span>
        {role ? (
          <span
            style={{
              fontSize: 12,
              color: 'var(--text-secondary)',
              lineHeight: 1.35,
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
            {role}
          </span>
        ) : null}
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5">
          {tier ? <TierBadge tier={tier} variant="pill" /> : null}
          {edited ? (
            <m.span
              aria-label="Edited"
              title="Overridden from the template"
              initial={reduce ? false : { scale: 0.8, opacity: 0 }}
              animate={reduce ? undefined : { scale: 1, opacity: 1 }}
              transition={{ duration: 0.3, ease: 'easeOut' }}
              style={editedBadgeStyle}
            >
              <span
                aria-hidden
                style={{ width: 5, height: 5, borderRadius: 999, background: 'var(--status-warn)' }}
              />
              Edited
            </m.span>
          ) : null}
        </div>
      </div>
    </GlowCard>
  );
}
