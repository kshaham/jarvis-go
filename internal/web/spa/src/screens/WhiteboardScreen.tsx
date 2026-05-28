import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Press } from '../components/motion';
import { fetchJSON } from '../api/client';
import { useWhiteboard } from '../hooks/useOfficeData';
import { relativeTime } from '../lib/relativeTime';

// WhiteboardScreen — the team's shared scratchpad. Single textarea
// backed by GET / POST /api/office/whiteboard. Local draft so live
// SSE updates from teammates don't clobber an in-progress edit.
export function WhiteboardScreen() {
  const wb = useWhiteboard();
  const qc = useQueryClient();
  const remote = wb.data?.whiteboard?.text ?? '';
  const updatedAt = wb.data?.whiteboard?.updatedAt;

  const [draft, setDraft] = useState(remote);
  const [dirty, setDirty] = useState(false);
  const [lastRemote, setLastRemote] = useState(remote);
  if (!dirty && remote !== lastRemote) {
    setDraft(remote);
    setLastRemote(remote);
  }

  const save = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/whiteboard', { method: 'POST', body: { text: draft } }),
    onSuccess: () => {
      setDirty(false);
      qc.invalidateQueries({ queryKey: ['office', 'whiteboard'] });
    },
  });

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Whiteboard
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Shared note pad. Every agent in the team can read it; you can edit and post back from here.
            {updatedAt ? ` Last updated ${relativeTime(updatedAt)}.` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <span style={{ fontSize: 11, color: dirty ? 'var(--status-warn)' : 'var(--text-muted)' }}>
            {dirty ? 'Unsaved changes' : 'In sync'}
          </span>
          <Press
            pressScale={0.96}
            onClick={() => {
              setDraft(remote);
              setDirty(false);
            }}
            disabled={!dirty}
          >
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-default)',
                color: 'var(--text-primary)',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: dirty ? 1 : 0.4,
              }}
            >
              Revert
            </span>
          </Press>
          <Press
            pressScale={0.96}
            onClick={() => save.mutate()}
            disabled={!dirty || save.isPending}
          >
            <span
              style={{
                padding: '8px 16px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: !dirty || save.isPending ? 0.5 : 1,
              }}
            >
              {save.isPending ? 'Saving…' : 'Save'}
            </span>
          </Press>
        </div>
      </header>

      <textarea
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value);
          setDirty(e.target.value !== remote);
        }}
        placeholder="Notes the whole team can see…"
        style={{
          flex: 1,
          padding: 14,
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-card)',
          color: 'var(--text-primary)',
          fontSize: 14,
          fontFamily: 'var(--font-mono)',
          lineHeight: 1.6,
          outline: 'none',
          resize: 'none',
        }}
      />
    </section>
  );
}
