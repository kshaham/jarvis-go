import { cn } from '@/lib/cn';
import type { AgentStatus } from '@/api/types';

const colorMap: Record<AgentStatus, string> = {
  working: 'bg-status-working/15 text-status-working ring-status-working/30',
  idle: 'bg-status-idle/15 text-slate-300 ring-status-idle/30',
  meeting: 'bg-status-meeting/15 text-status-meeting ring-status-meeting/30',
  stuck: 'bg-status-stuck/15 text-status-stuck ring-status-stuck/30',
};

const dotMap: Record<AgentStatus, string> = {
  working: 'bg-status-working animate-pulse',
  idle: 'bg-status-idle',
  meeting: 'bg-status-meeting animate-pulse',
  stuck: 'bg-status-stuck',
};

export function StatusPill({ status }: { status: AgentStatus }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ring-1',
        colorMap[status],
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', dotMap[status])} />
      {status}
    </span>
  );
}
