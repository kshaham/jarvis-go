import { m, useReducedMotion } from 'motion/react';

import { cn } from '@/lib/cn';
import { StatusPill } from './StatusPill';
import { Metric } from './motion/Metric';
import { GlowCard } from './ui/GlowCard';
import { Skeleton } from './ui/Skeleton';
import type { OfficeMember } from '@/api/types';
import { agentAvatarClass, agentAvatarGlyph } from './agentCard.utils';

interface Props {
  member: OfficeMember;
  onSelect?: (member: OfficeMember) => void;
}

function timeAgo(ts?: number): string {
  if (!ts) return '';
  const diff = Date.now() - ts;
  if (diff < 60_000) return 'just now';
  if (diff < 3_600_000) return Math.round(diff / 60_000) + 'm ago';
  if (diff < 86_400_000) return Math.round(diff / 3_600_000) + 'h ago';
  return Math.round(diff / 86_400_000) + 'd ago';
}

export function AgentCard({ member, onSelect }: Props) {
  const cost = member.totalCost || 0;
  const color = agentAvatarClass(member.avatarColor);
  const avatar = agentAvatarGlyph(member);
  const reduce = useReducedMotion();
  const isInteractive = !!onSelect;
  // Working agents glow violet (replaces the old 2px breathing edge);
  // the spotlight tracks the cursor. Stuck agents are signalled by the
  // urgent red StatusPill.
  return (
    <GlowCard
      interactive={isInteractive}
      live={member.status === 'working'}
      spotlight={isInteractive}
      onClick={onSelect ? () => onSelect(member) : undefined}
      className={cn('group flex flex-col gap-3 p-4', member.paused && 'opacity-60')}
      style={{ cursor: isInteractive ? 'pointer' : 'default' }}
    >
      <div className="flex items-start gap-3">
        {/* Shared-element morph target — the avatar flies into the
            MemberDetailModal header on open (gated by reduced motion). */}
        <m.div
          layoutId={reduce ? undefined : `agent-avatar-${member.sessionId}`}
          className={cn(
            'flex size-10 shrink-0 items-center justify-center rounded-lg text-base font-semibold',
            color,
          )}
        >
          {avatar}
        </m.div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold" style={{ color: 'var(--text-primary)' }}>
            {member.name}
          </div>
          <div className="truncate text-xs" style={{ color: 'var(--text-muted)' }}>
            {member.role}
          </div>
        </div>
        <StatusPill status={member.status} />
      </div>

      <div className="flex items-center justify-between text-xs" style={{ color: 'var(--text-muted)' }}>
        {/* Cost ticks digit-by-digit and flashes when more tokens get billed. */}
        <Metric
          value={cost}
          format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
          size={11}
          tone="muted"
          pulseOnChange
        />
        <span>{timeAgo(member.lastActivityAt)}</span>
      </div>

      {member.score?.grade && (
        <div className="flex items-center gap-2 text-xs">
          <span
            className="rounded px-1.5 py-0.5 font-semibold"
            style={{
              background: 'var(--bg-pressed)',
              color: 'var(--text-primary)',
            }}
          >
            {member.score.grade}
          </span>
          <span className="inline-flex items-baseline gap-1" style={{ color: 'var(--text-muted)' }}>
            <Metric
              value={(member.score.overallScore || 0) * 100}
              format={{ maximumFractionDigits: 0 }}
              suffix="%"
              size={11}
              tone="muted"
            />
            overall
          </span>
        </div>
      )}
    </GlowCard>
  );
}

export function AgentCardSkeleton() {
  return (
    <div className="surface-card flex flex-col gap-3 p-4">
      <div className="flex items-start gap-3">
        <Skeleton className="size-10 shrink-0 rounded-lg" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-2.5 w-16" />
        </div>
        <Skeleton className="h-5 w-14 rounded-full" />
      </div>
      <Skeleton className="h-2.5 w-full" />
    </div>
  );
}
