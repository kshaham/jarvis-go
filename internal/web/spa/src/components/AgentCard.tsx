import { cn } from '@/lib/cn';
import { StatusPill } from './StatusPill';
import type { OfficeMember } from '@/api/types';

interface Props {
  member: OfficeMember;
}

function formatCost(cents: number): string {
  if (cents >= 100) return '$' + (cents / 100).toFixed(2);
  return Math.round(cents) + 'c';
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

export function AgentCard({ member }: Props) {
  const cost = (member.totalCost || 0) * 100;
  const color = avatarBg[member.avatarColor || 'blue'] || avatarBg.blue;
  const avatar = member.avatarEmoji || member.avatarInitial || member.name?.[0] || '?';

  return (
    <article
      className={cn(
        'group relative flex flex-col gap-3 rounded-xl border border-white/5 bg-white/[0.025] p-4',
        'transition hover:border-white/15 hover:bg-white/[0.04]',
        'shadow-[0_1px_2px_rgba(0,0,0,0.04),0_1px_1px_rgba(0,0,0,0.06)]',
        member.paused && 'opacity-60',
      )}
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
          <div className="truncate text-sm font-semibold text-slate-100">{member.name}</div>
          <div className="truncate text-xs text-slate-400">{member.role}</div>
        </div>
        <StatusPill status={member.status} />
      </div>

      <div className="flex items-center justify-between text-[11px] text-slate-400">
        <span className="ds-tabular">{formatCost(cost)}</span>
        <span>{timeAgo(member.lastActivityAt)}</span>
      </div>

      {member.score?.grade && (
        <div className="flex items-center gap-2 text-[11px]">
          <span className="rounded bg-white/5 px-1.5 py-0.5 font-semibold text-slate-200">
            {member.score.grade}
          </span>
          <span className="text-slate-400 ds-tabular">
            {Math.round((member.score.overallScore || 0) * 100)}% overall
          </span>
        </div>
      )}
    </article>
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
