import { useState } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { FileText } from 'lucide-react';

import { Metric, Press } from '../components/motion';
import { Modal } from '../components/motion/Modal';
import { Button, EmptyState, GlowCard, Section, Skeleton } from '../components/ui';
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
  // AutoAnimate owns enter/exit/reorder as reports are generated.
  const [listRef] = useAutoAnimate<HTMLDivElement>();

  const generate = useMutation({
    mutationFn: () => fetchJSON('/api/office/reports/generate', { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'reports'] });
      toast.success('Report generated');
    },
    onError: () => toast.error('Failed to generate report'),
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

  const copyBody = async () => {
    try {
      await navigator.clipboard.writeText(body);
      toast.success('Copied');
    } catch {
      toast.error('Copy failed');
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
        <Button onClick={() => generate.mutate()} disabled={generate.isPending}>
          {generate.isPending ? 'Generating…' : 'Generate report'}
        </Button>
      </header>

      <Section
        title="Saved reports"
        trailing={
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            <Metric value={list.length} size={11} pulseOnChange /> on disk
          </span>
        }
      >
        {reports.isLoading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {Array.from({ length: 4 }).map((_, i) => (
              <ReportSkeleton key={i} />
            ))}
          </div>
        ) : list.length === 0 ? (
          <EmptyState
            icon={<FileText size={22} />}
            title="No reports saved yet"
            description="Generate a snapshot to capture the team's progress without ending the session."
          />
        ) : (
          <div ref={listRef} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {list.map((r) => (
              <GlowCard key={r.filename} interactive className="p-0">
                <Press
                  pressScale={0.97}
                  onClick={() => openReport(r)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    padding: '10px 12px',
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
                          fontSize: 12,
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
                    <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>
                      {relativeTime(r.generatedAt)}
                    </span>
                  ) : null}
                </Press>
              </GlowCard>
            ))}
          </div>
        )}
      </Section>

      <Modal
        open={!!open}
        onOpenChange={(next) => (next ? null : setOpen(null))}
        title={open?.filename ?? 'Report'}
        description={open?.generatedAt ? `Generated ${relativeTime(open.generatedAt)}` : undefined}
        size="lg"
        footer={
          <>
            <Button variant="secondary" onClick={copyBody}>
              Copy
            </Button>
            <Button onClick={() => setOpen(null)}>Close</Button>
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

function ReportSkeleton() {
  return (
    <div className="surface-card flex items-center gap-3 px-3 py-2.5">
      <div className="flex-1 space-y-2">
        <Skeleton className="h-3 w-1/3" />
        <Skeleton className="h-2.5 w-2/3" />
      </div>
      <Skeleton className="h-2.5 w-12" />
    </div>
  );
}
