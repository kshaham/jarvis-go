import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Modal } from './motion/Modal';
import { Metric, Press, RevealItem } from './motion';
import { fetchJSON } from '../api/client';
import { useSnapshots } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';

type Props = {
  open: boolean;
  onClose: () => void;
};

// SnapshotsModal — list / create / restore / delete saved team
// snapshots. A snapshot captures the current team's mission +
// members + state; restoring re-spawns that team. Opens from the
// SettingsScreen.
export function SnapshotsModal({ open, onClose }: Props) {
  const snapshots = useSnapshots();
  const qc = useQueryClient();
  const [name, setName] = useState('');

  const create = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/snapshots', { method: 'POST', body: { name } }),
    onSuccess: () => {
      setName('');
      qc.invalidateQueries({ queryKey: ['office', 'snapshots'] });
    },
  });
  const restore = useMutation({
    mutationFn: (snapName: string) =>
      fetchJSON(`/api/office/snapshots/${encodeURIComponent(snapName)}/restore`, {
        method: 'POST',
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'snapshots'] });
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
    },
  });
  const remove = useMutation({
    mutationFn: (snapName: string) =>
      fetchJSON(`/api/office/snapshots/${encodeURIComponent(snapName)}`, {
        method: 'DELETE',
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'snapshots'] }),
  });

  const list = snapshots.data?.snapshots ?? [];

  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="md"
      title="Snapshots"
      description="Save a team you can later restore. Useful for switching between projects."
      footer={
        <Press pressScale={0.96} onClick={onClose}>
          <span
            style={{
              padding: '8px 14px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
            }}
          >
            Close
          </span>
        </Press>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="snapshot name (e.g. 'pre-migration')"
            style={{
              flex: 1,
              padding: 8,
              background: 'var(--bg-app)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-primary)',
              fontSize: 13,
              outline: 'none',
            }}
          />
          <Press
            pressScale={0.96}
            onClick={() => name.trim() && create.mutate()}
            disabled={create.isPending || !name.trim()}
          >
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: create.isPending || !name.trim() ? 0.5 : 1,
              }}
            >
              {create.isPending ? 'Saving…' : 'Snapshot'}
            </span>
          </Press>
        </div>

        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
          <span
            style={{
              fontSize: 11,
              color: 'var(--text-muted)',
              letterSpacing: 0.4,
              textTransform: 'uppercase',
            }}
          >
            Saved
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            <Metric value={list.length} size={11} /> snapshots
          </span>
        </div>
        {list.length === 0 ? (
          <div
            style={{
              padding: 20,
              textAlign: 'center',
              color: 'var(--text-muted)',
              fontSize: 12,
              border: '1px dashed var(--border-default)',
              borderRadius: 'var(--radius-md)',
            }}
          >
            No snapshots yet.
          </div>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {list.map((s, i) => (
              <RevealItem key={s.name} index={Math.min(i, 12)}>
                <li
                  style={{
                    padding: 10,
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                  }}
                >
                  <span style={{ flex: 1, minWidth: 0 }}>
                    <span
                      style={{
                        display: 'block',
                        fontSize: 13,
                        fontWeight: 600,
                        color: 'var(--text-primary)',
                        fontFamily: 'var(--font-mono)',
                      }}
                    >
                      {s.name}
                    </span>
                    {s.mission ? (
                      <span
                        style={{
                          display: 'block',
                          fontSize: 11,
                          color: 'var(--text-muted)',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {s.mission}
                      </span>
                    ) : null}
                  </span>
                  {s.createdAt ? (
                    <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
                      {relativeTime(s.createdAt)}
                    </span>
                  ) : null}
                  <Press
                    pressScale={0.94}
                    onClick={() => {
                      if (window.confirm(`Restore "${s.name}"? This replaces the current team.`)) {
                        restore.mutate(s.name);
                      }
                    }}
                    disabled={restore.isPending}
                  >
                    <span
                      style={{
                        padding: '6px 10px',
                        background: 'var(--accent-muted)',
                        color: 'var(--accent)',
                        borderRadius: 'var(--radius-sm)',
                        fontSize: 11,
                        fontWeight: 600,
                      }}
                    >
                      Restore
                    </span>
                  </Press>
                  <Press
                    pressScale={0.92}
                    onClick={() => {
                      if (window.confirm(`Delete snapshot "${s.name}"?`)) remove.mutate(s.name);
                    }}
                    disabled={remove.isPending}
                  >
                    <span
                      style={{
                        padding: '6px 8px',
                        background: 'transparent',
                        border: '1px solid var(--border-default)',
                        color: 'var(--text-muted)',
                        borderRadius: 'var(--radius-sm)',
                        fontSize: 11,
                        fontWeight: 600,
                      }}
                    >
                      ✕
                    </span>
                  </Press>
                </li>
              </RevealItem>
            ))}
          </ul>
        )}
      </div>
    </Modal>
  );
}
