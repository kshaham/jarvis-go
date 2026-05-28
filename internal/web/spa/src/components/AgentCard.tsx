import { motion, useReducedMotion } from 'motion/react';

import { cn } from '@/lib/cn';
import { StatusPill } from './StatusPill';
import { Metric } from './motion';
import { spring } from '@/lib/motion';
import type { OfficeMember } from '@/api/types';

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

const avatarBg: Record<string, string> = {
  blue: 'bg-blue-500/20 text-blue-300',
  green: 'bg-emerald-500/20 text-emerald-300',
  purple: 'bg-purple-500/20 text-purple-300',
  orange: 'bg-orange-500/20 text-orange-300',
  pink: 'bg-pink-500/20 text-pink-300',
  cyan: 'bg-cyan-500/20 text-cyan-300',
  yellow: 'bg-yellow-500/20 text-yellow-300',
  red: 'bg-red-500/20 text-red-300',
};

export function AgentCard({ member, onSelect }: Props) {
  const cost = member.totalCost || 0;
  const color = avatarBg[member.avatarColor || 'blue'] || avatarBg.blue;
  const avatar = member.avatarEmoji || member.avatarInitial || member.name?.[0] || '?';
  const reduce = useReducedMotion();
  // working status gets a 2px breathing left-edge accent — the
  // "this agent is active right now" signal.
  const accentEdge =
    member.status === 'working' ? 'var(--status-working)' :
    member.status === 'stuck' ? 'var(--status-stuck)' :
    'transparent';

  const isInteractive = !!onSelect;
  return (
    <motion.article
      onClick={isInteractive ? () => onSelect!(member) : undefined}
      whileHover={reduce ? {} : { y: -2, borderColor: 'var(--border-strong)' }}
      transition={spring.smooth}
      className={cn(
        'group relative flex flex-col gap-3 rounded-[14px] border p-4',
        'transition-colors',
        member.paused && 'opacity-60',
      )}
      style={{
        cursor: isInteractive ? 'pointer' : 'default',
        borderColor: 'var(--border-subtle)',
        background: 'var(--bg-elevated)',
        boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.04)',
        // Left-edge accent painted via a pseudo-style background.
        backgroundImage: `linear-gradient(to right, ${accentEdge} 0, ${accentEdge} 2px, transparent 2px)`,
      }}
    >
      <div className="flex items-start gap-3">
        <div
          className={cn(
            'flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-base font-semibold',
            color,
          )}
        >
          {avatar}
        </div>
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

      <div className="flex items-center justify-between text-[11px]" style={{ color: 'var(--text-muted)' }}>
        {/* Cost ticks digit-by-digit as more tokens get billed. */}
        <Metric
          value={cost}
          format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
          size={11}
          tone="muted"
        />
        <span>{timeAgo(member.lastActivityAt)}</span>
      </div>

      {member.score?.grade && (
        <div className="flex items-center gap-2 text-[11px]">
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
    </motion.article>
  );
}

export function AgentCardSkeleton() {
  return (
    <div className="flex animate-pulse flex-col gap-3 rounded-xl border border-white/5 bg-white/[0.025] p-4">
      <div className="flex items-start gap-3">
        <div className="h-10 w-10 shrink-0 rounded-lg bg-white/10" />
        <div className="flex-1 space-y-2">
          <div className="h-3 w-24 rounded bg-white/10" />
          <div className="h-2.5 w-16 rounded bg-white/10" />
        </div>
        <div className="h-5 w-14 rounded-full bg-white/10" />
      </div>
      <div className="h-2.5 w-full rounded bg-white/10" />
    </div>
  );
}
