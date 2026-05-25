import { useOfficeStatus } from '@/hooks/useOfficeStatus';

export function TeamHeader() {
  const { data } = useOfficeStatus();
  const team = data?.teamSpawned ? data : null;
  const memberCount = team?.members?.length || 0;

  return (
    <header className="sticky top-0 z-10 border-b border-white/5 bg-[#06080C]/80 backdrop-blur supports-[backdrop-filter]:bg-[#06080C]/60">
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-6 py-3">
        <div className="flex items-center gap-3">
          <div className="h-7 w-7 rounded-md bg-gradient-to-br from-sky-400 to-blue-600" />
          <div>
            <div className="text-sm font-semibold text-slate-100">Jarvis · Office</div>
            <div className="text-[11px] text-slate-500">React beta /office-v2</div>
          </div>
        </div>

        {team && (
          <div className="flex items-center gap-3 text-xs text-slate-300">
            {team.mission && (
              <span className="hidden max-w-[40ch] truncate sm:inline-block text-slate-400">
                {team.mission}
              </span>
            )}
            {team.prNumber ? (
              <span className="rounded bg-status-info/15 px-2 py-0.5 text-status-info">
                PR #{team.prNumber}
              </span>
            ) : null}
            {team.paused && (
              <span className="rounded bg-status-warn/15 px-2 py-0.5 text-status-warn">
                Paused
              </span>
            )}
            <span className="ds-tabular text-slate-400">{memberCount} agents</span>
          </div>
        )}
      </div>
    </header>
  );
}
