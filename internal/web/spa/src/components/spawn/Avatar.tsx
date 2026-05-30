import type { CSSProperties } from 'react';
import { tierColor, type Tier } from './TierBadge';

const avatarBaseStyle: CSSProperties = {
  position: 'relative',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  flexShrink: 0,
};

const overflowChipBaseStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: 'var(--bg-pressed)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-secondary)',
  fontSize: 12,
  fontWeight: 700,
};

type Size = 'sm' | 'md' | 'lg';

const SIZE_PX: Record<Size, number> = {
  sm: 28,
  md: 40,
  lg: 56,
};

const EMOJI_FONT: Record<Size, number> = {
  sm: 14,
  md: 22,
  lg: 30,
};

const INITIAL_FONT: Record<Size, number> = {
  sm: 12,
  md: 16,
  lg: 22,
};

function monogram(name: string | undefined): string {
  if (!name) return '?';
  const cleaned = name.trim();
  if (!cleaned) return '?';
  const parts = cleaned.split(/\s+/);
  if (parts.length === 1) return parts[0].slice(0, 1).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function Avatar({
  name,
  emoji,
  color,
  tier,
  size = 'md',
  ring = true,
  style,
}: {
  name?: string;
  emoji?: string;
  color?: string;
  tier?: Tier;
  size?: Size;
  ring?: boolean;
  style?: CSSProperties;
}) {
  const px = SIZE_PX[size];
  const ringColor = tier ? tierColor(tier) : 'var(--border-strong)';
  const swatch = color || 'var(--bg-pressed)';
  const showEmoji = Boolean(emoji && emoji.trim());

  return (
    <span
      role="img"
      aria-label={name || 'member avatar'}
      title={name}
      style={{
        ...avatarBaseStyle,
        width: px,
        height: px,
        background: showEmoji
          ? 'var(--bg-elevated)'
          : `color-mix(in srgb, ${swatch} 22%, transparent)`,
        border: ring ? `2px solid ${ringColor}` : `1px solid var(--border-subtle)`,
        borderRadius: 'var(--radius-md)',
        ...style,
      }}
    >
      {showEmoji ? (
        <span style={{ fontSize: EMOJI_FONT[size], lineHeight: 1 }}>{emoji}</span>
      ) : (
        <span
          style={{
            fontSize: INITIAL_FONT[size],
            fontWeight: 700,
            color: color || 'var(--text-primary)',
            letterSpacing: 0.2,
          }}
        >
          {monogram(name)}
        </span>
      )}
    </span>
  );
}

// Stacked horizontal cluster of small avatars. Caps at `max`, with a
// `+N` overflow chip after. Each member's optional `id` is used as
// the React key; falls back to a name+emoji composite so reordering
// the array doesn't shuffle avatars.
export function AvatarStack({
  members,
  max = 5,
  size = 'sm',
  style,
}: {
  members: { id?: string; name?: string; emoji?: string; color?: string; tier?: Tier }[];
  max?: number;
  size?: Size;
  style?: CSSProperties;
}) {
  const shown = members.slice(0, max);
  const overflow = members.length - shown.length;
  const px = SIZE_PX[size];

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        ...style,
      }}
    >
      {shown.map((m, i) => {
        const key = m.id ?? `${m.name ?? '_'}|${m.emoji ?? ''}|${i}`;
        return (
          <span
            key={key}
            style={{
              marginLeft: i === 0 ? 0 : -8,
              zIndex: shown.length - i,
              position: 'relative',
            }}
          >
            <Avatar {...m} size={size} ring />
          </span>
        );
      })}
      {overflow > 0 ? (
        <span
          style={{ ...overflowChipBaseStyle, marginLeft: -8, width: px, height: px }}
        >
          +{overflow}
        </span>
      ) : null}
    </span>
  );
}
