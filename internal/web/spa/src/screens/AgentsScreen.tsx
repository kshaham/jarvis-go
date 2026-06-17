import { useMemo, useState } from 'react';
import { LayoutGroup } from 'motion/react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Users } from 'lucide-react';

import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { AgentCard, AgentCardSkeleton } from '@/components/AgentCard';
import { Metric, StatusDot } from '@/components/motion';
import { Chip, EmptyState, SegmentedControl } from '@/components/ui';
import { MemberDetailModal } from '@/components/MemberDetailModal';
import type { AgentStatus, OfficeMember } from '@/api/types';

type StatusFilter = 'all' | AgentStatus;
type SortKey = 'activity' | 'cost' | 'name';

const STATUS_ORDER: { value: AgentStatus; label: string }[] = [
  { value: 'working', label: 'Working' },
  { value: 'idle', label: 'Idle' },
  { value: 'meeting', label: 'Meeting' },
  { value: 'stuck', label: 'Stuck' },
];

export function AgentsScreen() {
  const { data, isLoading, isError, error } = useOfficeStatus();
  const [selectedMember, setSelectedMember] = useState<OfficeMember | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [sort, setSort] = useState<SortKey>('activity');
  // AutoAnimate owns enter/exit/reorder of the live roster as agents
  // join, leave, filter, or re-sort (respects reduced motion).
  const [gridRef] = useAutoAnimate<HTMLDivElement>();

  const members = useMemo(() => data?.members ?? [], [data?.members]);

  const counts = useMemo(() => {
    const c: Record<AgentStatus, number> = { working: 0, idle: 0, meeting: 0, stuck: 0 };
    for (const m of members) c[m.status] = (c[m.status] ?? 0) + 1;
    return c;
  }, [members]);

  const visible = useMemo(() => {
    return members
      .flatMap((m) => (statusFilter === 'all' || m.status === statusFilter ? [m] : []))
      .toSorted((a, b) => {
        if (sort === 'cost') return (b.totalCost ?? 0) - (a.totalCost ?? 0);
        if (sort === 'name') return a.name.localeCompare(b.name);
        return (b.lastActivityAt ?? 0) - (a.lastActivityAt ?? 0);
      });
  }, [members, statusFilter, sort]);

  if (isLoading) {
    return (
      <section>
        <h1 className="mb-4 heading-lg">
          Agents
        </h1>
        <div
          className="grid gap-3"
          style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))' }}
        >
          {Array.from({ length: 6 }).map((_, i) => (
            <AgentCardSkeleton key={i} />
          ))}
        </div>
      </section>
    );
  }

  if (isError) {
    return (
      <section>
        <h1 className="mb-4 heading-lg">
          Agents
        </h1>
        <div className="rounded-lg border border-status-error/30 bg-status-error/10 p-4 text-sm text-status-error">
          {String(error instanceof Error ? error.message : error)}
        </div>
      </section>
    );
  }

  if (!data?.teamSpawned || members.length === 0) {
    return (
      <section>
        <h1 className="mb-4 heading-lg">
          Agents
        </h1>
        <EmptyState
          icon={<Users size={22} />}
          title="No team spawned yet"
          description={
            <>
              Spawn a team from the rail&apos;s <strong>+ Spawn team</strong> button, or open the
              classic UI at <a className="text-accent hover:underline" href="/office">/office</a>.
            </>
          }
        />
      </section>
    );
  }

  return (
    <LayoutGroup>
      <section>
        <div className="mb-4 flex items-center justify-between">
          <h1 className="heading-lg">
            Agents
          </h1>
          <span className="inline-flex items-baseline gap-1 text-xs" style={{ color: 'var(--text-muted)' }}>
            <Metric value={members.length} size={12} tone="primary" pulseOnChange />
            live
          </span>
        </div>

        {/* Filter (also a status summary) + sort toolbar */}
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-1.5">
            <Chip active={statusFilter === 'all'} onClick={() => setStatusFilter('all')}>
              All
              <span style={{ opacity: 0.65 }}>{members.length}</span>
            </Chip>
            {STATUS_ORDER.flatMap((s) =>
              counts[s.value] > 0 ? (
                <Chip
                  key={s.value}
                  active={statusFilter === s.value}
                  onClick={() => setStatusFilter((cur) => (cur === s.value ? 'all' : s.value))}
                >
                  <StatusDot status={s.value} size={6} />
                  {s.label}
                  <span style={{ opacity: 0.65 }}>{counts[s.value]}</span>
                </Chip>
              ) : []
            )}
          </div>
          <SegmentedControl<SortKey>
            ariaLabel="Sort agents"
            value={sort}
            onChange={setSort}
            options={[
              { value: 'activity', label: 'Recent' },
              { value: 'cost', label: 'Cost' },
              { value: 'name', label: 'Name' },
            ]}
          />
        </div>

        <div
          ref={gridRef}
          className="grid gap-3"
          style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))' }}
        >
          {visible.map((m) => (
            <AgentCard key={m.sessionId} member={m} onSelect={setSelectedMember} />
          ))}
        </div>
        {visible.length === 0 ? (
          <EmptyState
            className="mt-3"
            icon={<Users size={20} />}
            title="No agents match this filter"
            description="Clear the status filter to see the whole roster."
          />
        ) : null}

        <MemberDetailModal member={selectedMember} onClose={() => setSelectedMember(null)} />
      </section>
    </LayoutGroup>
  );
}
