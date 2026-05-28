import { cn } from '@/lib/cn';
import type { AgentStatus } from '@/api/types';
import { StatusDot } from '@/components/motion';

const colorMap: Record<AgentStatus, string> = {
  working: 'bg-status-working/15 text-status-working ring-status-working/30',
  idle: 'bg-status-idle/15 text-slate-300 ring-status-idle/30',
  meeting: 'bg-status-meeting/15 text-status-meeting ring-status-meeting/30',
  stuck: 'bg-status-stuck/15 text-status-stuck ring-status-stuck/30',
};

// StatusPill — colored chip with an animated StatusDot. The dot
// breathes when the agent is working, pulses urgently when stuck,
// and ripples on every status crossover (signature beat).
export function StatusPill({ status }: { status: AgentStatus }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ring-1',
        colorMap[status],
      )}
    >
      <StatusDot status={status} size={6} />
      {status}
    </span>
  );
}
