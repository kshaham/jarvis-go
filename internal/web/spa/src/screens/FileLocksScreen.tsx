import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Metric, Press, RevealItem } from '../components/motion';
import { fetchJSON } from '../api/client';
import { useFileLocks } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';

// FileLocksScreen — list of files currently locked by agents (so two
// agents don't write the same file). Operator can force-release a
// lock if it's stuck.
export function FileLocksScreen() {
  const fl = useFileLocks();
  const qc = useQueryClient();

  const release = useMutation({
    mutationFn: (path: string) =>
      fetchJSON(`/api/office/file-locks/${encodeURIComponent(path)}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'file-locks'] }),
  });

  const list = fl.data?.locks ?? [];

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            File locks
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Distributed locks the team uses to serialize concurrent writes. Force-release stuck locks here.
          </p>
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
          <Metric value={list.length} size={12} /> active
        </span>
      </header>

      {list.length === 0 ? (
        <Empty />
      ) : (
        <ul
          style={{
            listStyle: 'none',
            padding: 0,
            margin: 0,
            display: 'flex',
            flexDirection: 'column',
            gap: 6,
          }}
        >
          {list.map((lock, i) => (
            <RevealItem key={lock.path} index={i}>
              <li
                style={{
                  padding: 12,
                  background: 'var(--bg-elevated)',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-md)',
                  display: 'flex',
                  gap: 12,
                  alignItems: 'center',
                }}
              >
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
                  <span style={{ display: 'block', fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                    {lock.ownerName ?? lock.owner} · acquired {relativeTime(lock.acquiredAt)}
                    {lock.expiresAt ? ` · expires ${relativeTime(lock.expiresAt)}` : ''}
                  </span>
                </span>
                <Press
                  pressScale={0.94}
                  onClick={() => {
                    if (window.confirm(`Force-release ${lock.path}? Whoever held it loses their lock.`)) {
                      release.mutate(lock.path);
                    }
                  }}
                  disabled={release.isPending}
                >
                  <span
                    style={{
                      padding: '6px 12px',
                      background: 'transparent',
                      border: '1px solid var(--border-default)',
                      borderRadius: 'var(--radius-sm)',
                      color: 'var(--status-warn)',
                      fontSize: 11,
                      fontWeight: 600,
                    }}
                  >
                    Force release
                  </span>
                </Press>
              </li>
            </RevealItem>
          ))}
        </ul>
      )}
    </section>
  );
}

function Empty() {
  return (
    <div
      style={{
        padding: 32,
        textAlign: 'center',
        color: 'var(--text-muted)',
        fontSize: 13,
        background: 'var(--bg-elevated)',
        border: '1px dashed var(--border-default)',
        borderRadius: 'var(--radius-card)',
      }}
    >
      No active locks.
    </div>
  );
}
