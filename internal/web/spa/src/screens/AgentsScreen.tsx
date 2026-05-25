import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { AgentCard, AgentCardSkeleton } from '@/components/AgentCard';

export function AgentsScreen() {
  const { data, isLoading, isError, error } = useOfficeStatus();

  if (isLoading) {
    return (
      <section>
        <h1 className="mb-4 text-lg font-semibold text-slate-100">Agents</h1>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <AgentCardSkeleton key={i} />
          ))}
        </div>
      </section>
    );
  }

  if (isError) {
    return (
      <section>
        <h1 className="mb-4 text-lg font-semibold text-slate-100">Agents</h1>
        <div className="rounded-lg border border-status-error/30 bg-status-error/10 p-4 text-sm text-status-error">
          {String(error instanceof Error ? error.message : error)}
        </div>
      </section>
    );
  }

  const members = data?.members || [];

  if (!data?.teamSpawned || members.length === 0) {
    return (
      <section>
        <h1 className="mb-4 text-lg font-semibold text-slate-100">Agents</h1>
        <div className="rounded-xl border border-dashed border-white/10 p-10 text-center text-sm text-slate-400">
          No team spawned yet. Use the classic UI at{' '}
          <a className="text-sky-400 hover:underline" href="/office">
            /office
          </a>{' '}
          to spawn one.
        </div>
      </section>
    );
  }

  return (
    <section>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold text-slate-100">Agents</h1>
        <span className="text-xs text-slate-500 ds-tabular">{members.length} live</span>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {members.map((m) => (
          <AgentCard key={m.sessionId} member={m} />
        ))}
      </div>
    </section>
  );
}
