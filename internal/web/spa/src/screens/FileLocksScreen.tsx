import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Lock } from 'lucide-react';
import { toast } from 'sonner';

import { Metric } from '../components/motion/Metric';
import { Button } from '../components/ui/Button';
import { GlowCard } from '../components/ui/GlowCard';
import { EmptyState } from '../components/ui/EmptyState';
import { fetchJSON } from '../api/client';
import { useFileLocks } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';

// FileLocksScreen — list of files currently locked by agents (so two
// agents don't write the same file). Operator can force-release a
// lock if it's stuck.
export function FileLocksScreen() {
  const fl = useFileLocks();
  const qc = useQueryClient();
  // AutoAnimate owns enter/exit/reorder of the live list as locks are
  // acquired or released over SSE (respects reduced motion).
  const [listRef] = useAutoAnimate<HTMLUListElement>();

  const release = useMutation({
    mutationFn: (path: string) =>
      fetchJSON(`/api/office/file-locks/${encodeURIComponent(path)}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'file-locks'] });
      toast.success('Lock released');
    },
    onError: () => toast.error('Failed to release lock'),
  });

  const list = fl.data?.locks ?? [];

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 className="page-header">
            File locks
          </h1>
          <p className="page-subtitle">
            Distributed locks the team uses to serialize concurrent writes. Force-release stuck locks here.
          </p>
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
          <Metric value={list.length} size={12} pulseOnChange /> active
        </span>
      </header>

      {list.length === 0 ? (
        <EmptyState
          icon={<Lock size={22} />}
          title="No active locks"
          description="When an agent locks a file to serialize a write, it appears here until released."
        />
      ) : (
        <ul
          ref={listRef}
          style={{
            listStyle: 'none',
            padding: 0,
            margin: 0,
            display: 'flex',
            flexDirection: 'column',
            gap: 6,
          }}
        >
          {list.map((lock) => (
            <li key={lock.path} style={{ listStyle: 'none' }}>
              <GlowCard interactive className="flex items-center gap-3 p-3">
                <span style={{ flex: 1, minWidth: 0 }}>
                  <span
                    style={{
                      display: 'block',
                      fontSize: 13,
                      fontFamily: 'var(--font-mono)',
                      color: 'var(--text-primary)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {lock.path}
                  </span>
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                    {lock.ownerName ?? lock.owner} · acquired {relativeTime(lock.acquiredAt)}
                    {lock.expiresAt ? ` · expires ${relativeTime(lock.expiresAt)}` : ''}
                  </span>
                </span>
                <Button
                  variant="danger"
                  size="sm"
                  pressScale={0.94}
                  disabled={release.isPending}
                  onClick={() => {
                    if (window.confirm(`Force-release ${lock.path}? Whoever held it loses their lock.`)) {
                      release.mutate(lock.path);
                    }
                  }}
                >
                  Force release
                </Button>
              </GlowCard>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
