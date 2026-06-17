import { m, useReducedMotion } from 'motion/react';

import { cn } from '@/lib/cn';
import { spring, timing } from '@/lib/motion';
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
  // the spotlight tracks the cursor. Stuck agents escalate with the red
  // alert rim plus the urgent StatusPill so they read across the grid.
  return (
    <GlowCard
      interactive={isInteractive}
      live={member.status === 'working'}
      alert={member.status === 'stuck'}
      spotlight={isInteractive}
      onClick={onSelect ? () => onSelect(member) : undefined}
      className={cn('group relative flex flex-col gap-3 p-4', member.paused && 'opacity-60')}
      style={{ cursor: isInteractive ? 'pointer' : 'default' }}
    >
      {/* Inset top highlight — a hairline of light along the upper edge so
          the tile reads as the same glass material as the rest of the system.
          Token-driven (rim strength) so it tracks every theme. */}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-px rounded-t-[inherit]"
        style={{
          background:
            'linear-gradient(90deg, transparent, var(--lg-rim-strong) 22%, var(--lg-rim-strong) 78%, transparent)',
        }}
      />
      <div className="flex items-start gap-3">
        {/* Shared-element morph target — the avatar flies into the
            MemberDetailModal header on open (gated by reduced motion).
            On hover the tile gives it a 1px lift toward that morph. */}
        <m.div
          layoutId={reduce ? undefined : `agent-avatar-${member.sessionId}`}
          whileHover={isInteractive && !reduce ? { y: -1 } : undefined}
          transition={spring.snappy}
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

      {/* Footer hierarchy: cost is the emphasis (primary text), timeAgo
          recedes to faint. Gap on the 4pt scale. */}
      <div className="flex items-baseline justify-between" style={{ gap: 'var(--space-3)' }}>
        {/* Cost ticks digit-by-digit and flashes when more tokens get billed. */}
        <Metric
          value={cost}
          format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
          size={12}
          tone="primary"
          pulseOnChange
        />
        <span className="text-[11px]" style={{ color: 'var(--text-faint)' }}>
          {timeAgo(member.lastActivityAt)}
        </span>
      </div>

      {member.score?.grade && (
        <div className="flex items-center text-xs" style={{ gap: 'var(--space-3)' }}>
          {/* Grade badge — shared liquid-glass chip with an accent tint, and a
              brief mount glow (scale-in + accent bloom) so it lands with weight
              instead of popping flat. The bloom is a separate overlay that
              fades out, so it interpolates cleanly across themes. Reduced
              motion: static chip, no transform, no bloom. */}
          <m.span
            initial={reduce ? false : { scale: 0.96 }}
            animate={reduce ? undefined : { scale: 1 }}
            transition={spring.snappy}
            className="badge-glass relative font-semibold"
            style={{
              padding: 'var(--space-1) var(--space-3)',
              color: 'var(--text-primary)',
              background:
                'linear-gradient(var(--lg-fill), var(--lg-fill)), rgb(var(--accent-rgb) / 0.1)',
            }}
          >
            {!reduce && (
              <m.span
                aria-hidden
                className="pointer-events-none absolute inset-0 -z-10 rounded-[inherit]"
                initial={{ opacity: 1 }}
                animate={{ opacity: 0 }}
                transition={{ ...timing.long, delay: 0.06 }}
                style={{ boxShadow: 'var(--glow-violet-md)' }}
              />
            )}
            {member.score.grade}
          </m.span>
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
