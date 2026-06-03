import { useMemo } from 'react';
import type { CSSProperties } from 'react';
import { Check, Circle } from 'lucide-react';
import type { TeamTemplate } from '../../api/types';
import { SectionCard } from './formPrimitives';
import { AvatarStack } from './Avatar';
import type { MemberEdit } from './memberEditor.utils';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';
import type { ChecklistItem } from './reviewSummary.utils';

const checklistStyle: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
  listStyle: 'none',
  margin: 0,
  padding: 0,
};

// ReviewTile — the bento "Review" summary: who's on the team and the
// pre-spawn checklist. The Spawn action itself lives in the page command
// strip, and the provider/workdir live in their own tiles, so this stays
// a compact glance in the right column (matching the B2 mockup).
export function ReviewTile({
  tpl,
  members,
  checklist,
}: {
  tpl: TeamTemplate;
  members: MemberEdit[];
  checklist: ChecklistItem[];
}) {
  const baseById = useMemo(
    () => new Map(tpl.members.map((m, i) => [m.id ?? `${tpl.id}-${i}`, m] as const)),
    [tpl],
  );
  const stack = members.map((m) => {
    const base = baseById.get(m.id);
    return {
      id: m.id,
      name: m.name,
      emoji: base?.emoji,
      color: base?.color,
      tier: m.tier ?? (base?.tier as Tier | undefined),
    };
  });
  const tiers = members.map((m) => ({
    tier: m.tier ?? (baseById.get(m.id)?.tier as Tier | undefined),
  }));

  return (
    <SectionCard title="Review" hint="Checked before you spawn.">
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <AvatarStack members={stack} max={6} size="sm" />
          <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
            {members.length} member{members.length === 1 ? '' : 's'}
          </span>
          <TierCounts members={tiers} />
        </div>

        <ul style={checklistStyle}>
          {checklist.map((item) => (
            <li
              key={item.label}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                fontSize: 12,
                fontWeight: 600,
                color: item.ok ? 'var(--status-success)' : 'var(--text-muted)',
              }}
            >
              {item.ok ? (
                <Check size={14} strokeWidth={2.5} aria-hidden />
              ) : (
                <Circle size={14} strokeWidth={2} aria-hidden />
              )}
              {item.label}
            </li>
          ))}
        </ul>
      </div>
    </SectionCard>
  );
}
