import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Metric, Press, RevealItem } from '../components/motion';
import { Modal } from '../components/motion/Modal';
import { relativeTime } from '../lib/relativeTime';
import { fetchJSON, fetchText } from '../api/client';
import { useReports } from '../hooks/useOfficeData';
import type { SavedReport } from '../api/types';

// ReportsScreen — saved wrap-up reports. Click a row to open a wide
// modal with the rendered markdown; copy / download from there.
// "Generate report" mutation triggers /api/office/reports/generate.
export function ReportsScreen() {
  const reports = useReports();
  const qc = useQueryClient();
  const [open, setOpen] = useState<SavedReport | null>(null);
  const [body, setBody] = useState<string>('');

  const generate = useMutation({
    mutationFn: () => fetchJSON('/api/office/reports/generate', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'reports'] }),
  });

  const list = reports.data?.reports ?? [];

  const openReport = async (r: SavedReport) => {
    setOpen(r);
    setBody('Loading…');
    try {
      const txt = await fetchText(`/api/office/reports/${encodeURIComponent(r.filename)}`);
      setBody(txt);
    } catch (e) {
      setBody(`Failed to load: ${(e as Error).message}`);
    }
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Reports
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Saved wrap-up reports. Generate a fresh snapshot without ending the session.
          </p>
        </div>
        <Press pressScale={0.96} onClick={() => generate.mutate()} disabled={generate.isPending}>
          <span
            style={{
              padding: '10px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 14,
              fontWeight: 600,
              opacity: generate.isPending ? 0.6 : 1,
            }}
          >
            {generate.isPending ? 'Generating…' : 'Generate report'}
          </span>
        </Press>
      </header>

      <section
        style={{
          padding: 14,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
        }}
      >
        <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0, fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)' }}>
            Saved reports
          </h2>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            <Metric value={list.length} size={11} /> on disk
          </span>
        </header>
        {list.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: 32 }}>
            No reports saved yet.
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {list.map((r, i) => (
              <RevealItem key={r.filename} index={Math.min(i, 12)}>
                <Press
                  pressScale={0.97}
                  onClick={() => openReport(r)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    padding: '10px 12px',
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    width: '100%',
                    cursor: 'pointer',
                  }}
                >
                  <span style={{ flex: 1, minWidth: 0 }}>
                    <span
                      style={{
                        display: 'block',
                        fontSize: 13,
                        color: 'var(--text-primary)',
                        fontFamily: 'var(--font-mono)',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {r.filename}
                    </span>
                    {r.preview ? (
                      <span
                        style={{
                          display: 'block',
                          fontSize: 11,
                          color: 'var(--text-muted)',
                          marginTop: 4,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                      >
                        {r.preview}
                      </span>
                    ) : null}
                  </span>
                  {r.generatedAt ? (
                    <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
                      {relativeTime(r.generatedAt)}
                    </span>
                  ) : null}
                </Press>
              </RevealItem>
            ))}
          </div>
        )}
      </section>

      <Modal
        open={!!open}
        onOpenChange={(next) => (next ? null : setOpen(null))}
        title={open?.filename ?? 'Report'}
        description={open?.generatedAt ? `Generated ${relativeTime(open.generatedAt)}` : undefined}
        size="lg"
        footer={
          <>
            <Press
              pressScale={0.96}
              onClick={async () => {
                if (open) {
                  await navigator.clipboard.writeText(body);
                }
              }}
            >
              <span
                style={{
                  padding: '8px 14px',
                  background: 'var(--bg-pressed)',
                  border: '1px solid var(--border-default)',
                  borderRadius: 'var(--radius-md)',
                  color: 'var(--text-primary)',
                  fontSize: 13,
                  fontWeight: 600,
                }}
              >
                Copy
              </span>
            </Press>
            <Press pressScale={0.96} onClick={() => setOpen(null)}>
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
          </>
        }
      >
        <pre
          style={{
            margin: 0,
            whiteSpace: 'pre-wrap',
            fontFamily: 'var(--font-mono)',
            fontSize: 12,
            color: 'var(--text-primary)',
            lineHeight: 1.55,
          }}
        >
          {body}
        </pre>
      </Modal>
    </section>
  );
}
