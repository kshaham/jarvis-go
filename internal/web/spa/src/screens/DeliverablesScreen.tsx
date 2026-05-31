import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FileStack, FileText } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

import { Button, EmptyState, GlowCard, Select, Skeleton } from '@/components/ui';
import { Modal } from '@/components/motion/Modal';
import { useDeliverables, useDeliverableDoc } from '@/hooks/useDeliverables';
import { relativeTime } from '@/lib/relativeTime';
import type { Deliverable, DeliverableProject, DeliverableType } from '@/api/types';

// The workflow-action types (research + ops). Keys match the backend.
const TYPE_OPTIONS: { value: DeliverableType; label: string }[] = [
  { value: 'research-brief', label: 'Research brief' },
  { value: 'competitive-analysis', label: 'Competitive analysis' },
  { value: 'findings-memo', label: 'Findings memo' },
  { value: 'literature-review', label: 'Literature review' },
  { value: 'project-plan', label: 'Project plan' },
  { value: 'sop', label: 'SOP' },
  { value: 'decision-memo', label: 'Decision memo' },
  { value: 'status-report', label: 'Status report' },
  { value: 'custom', label: 'Custom…' },
];
const TYPE_LABEL: Record<string, string> = Object.fromEntries(
  TYPE_OPTIONS.map((o) => [o.value, o.label]),
);

type OpenDoc = { project: DeliverableProject; d: Deliverable } | null;

// DeliverablesScreen — per saved project, the team's authored documents
// (briefs, analyses, memos, plans, SOPs). Pick a type from "New deliverable"
// to dispatch the spawned team to write one; it appears here (versioned) and
// renders as markdown. Generation needs a team spawned on the project.
export function DeliverablesScreen() {
  const { projects, isLoading, generate } = useDeliverables();
  const [open, setOpen] = useState<OpenDoc>(null);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>Deliverables</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          The documents your teams produce — briefs, analyses, memos, plans. Pick a type and the spawned team
          writes it.
        </p>
      </header>

      {isLoading ? (
        <Skeleton style={{ height: 160, borderRadius: 'var(--radius-card)' }} />
      ) : projects.length === 0 ? (
        <EmptyState
          icon={<FileStack size={22} />}
          title="No projects yet"
          description="Save a project on the Spawn page to produce deliverables for it here."
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {projects.map((p) => (
            <ProjectDeliverables
              key={p.id}
              p={p}
              onNew={(type) => generate.mutate({ projectId: p.id, type })}
              onNewCustom={(title) => generate.mutate({ projectId: p.id, type: 'custom', title })}
              onOpen={(d) => setOpen({ project: p, d })}
            />
          ))}
        </div>
      )}

      <DeliverableReader open={open} onClose={() => setOpen(null)} />
    </section>
  );
}

function ProjectDeliverables({
  p,
  onNew,
  onNewCustom,
  onOpen,
}: {
  p: DeliverableProject;
  onNew: (type: DeliverableType) => void;
  onNewCustom: (title: string) => void;
  onOpen: (d: Deliverable) => void;
}) {
  const navigate = useNavigate();
  const [picker, setPicker] = useState('');

  function handlePick(value: string) {
    setPicker('');
    if (!value) return;
    if (value === 'custom') {
      const title = window.prompt('Title for the custom document?')?.trim();
      if (title) onNewCustom(title);
      return;
    }
    onNew(value as DeliverableType);
  }

  return (
    <GlowCard className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate font-semibold" style={{ color: 'var(--text-primary)' }}>
            {p.name}
          </div>
          <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {p.drafting
              ? `Team is drafting ${TYPE_LABEL[p.drafting.type] ?? 'a document'}…`
              : p.deliverables.length > 0
                ? `${p.deliverables.length} deliverable${p.deliverables.length === 1 ? '' : 's'}`
                : 'No deliverables yet'}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!p.teamActive ? (
            <button
              onClick={() => navigate('/spawn')}
              className="text-xs underline"
              style={{ color: 'var(--text-muted)', textUnderlineOffset: 3 }}
            >
              Spawn a team for this project
            </button>
          ) : null}
          <div style={{ minWidth: 190, opacity: p.teamActive ? 1 : 0.5, pointerEvents: p.teamActive ? 'auto' : 'none' }}>
            <Select
              value={picker}
              onChange={handlePick}
              placeholder="+ New deliverable"
              ariaLabel="New deliverable type"
              options={TYPE_OPTIONS}
            />
          </div>
        </div>
      </div>

      {p.deliverables.length > 0 ? (
        <div className="flex flex-col gap-1.5">
          {p.deliverables.map((d) => (
            <button
              key={d.slug}
              onClick={() => onOpen(d)}
              className="nav-tile flex items-center justify-between gap-3 rounded-md px-3 py-2 text-left"
              style={{ border: '1px solid transparent' }}
            >
              <div className="flex min-w-0 items-center gap-2">
                <FileText size={15} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
                <span className="truncate font-medium" style={{ color: 'var(--text-primary)' }}>
                  {d.title || d.slug}
                </span>
                <span
                  className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                  style={{ color: 'var(--text-muted)', background: 'rgb(var(--surface-tint-rgb) / 0.06)' }}
                >
                  {TYPE_LABEL[d.type] ?? d.type}
                </span>
              </div>
              <div className="flex shrink-0 items-center gap-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                {d.versions.length > 1 ? <span>v{d.versions.length}</span> : null}
                {d.updatedAt ? <span>{relativeTime(d.updatedAt)}</span> : null}
              </div>
            </button>
          ))}
        </div>
      ) : null}
    </GlowCard>
  );
}

function DeliverableReader({ open, onClose }: { open: OpenDoc; onClose: () => void }) {
  const [version, setVersion] = useState<number | undefined>(undefined);
  const projectId = open?.project.id ?? null;
  const slug = open?.d.slug ?? null;
  const doc = useDeliverableDoc(projectId, slug, version);

  // Reset the selected version when a different doc opens.
  const key = `${projectId}:${slug}`;
  const [lastKey, setLastKey] = useState('');
  if (open && key !== lastKey) {
    setLastKey(key);
    setVersion(undefined);
  }

  const versions = open?.d.versions ?? [];

  return (
    <Modal
      open={!!open}
      onOpenChange={(next) => (next ? null : onClose())}
      title={open?.d.title ?? 'Deliverable'}
      description={open ? (TYPE_LABEL[open.d.type] ?? open.d.type) : undefined}
      size="lg"
      footer={
        <>
          {doc.data ? (
            <Button
              variant="secondary"
              onClick={() => navigator.clipboard?.writeText(doc.data!.content)}
            >
              Copy
            </Button>
          ) : null}
          <Button onClick={onClose}>Close</Button>
        </>
      }
    >
      {versions.length > 1 ? (
        <div className="mb-3" style={{ maxWidth: 240 }}>
          <Select
            ariaLabel="Version"
            value={String(version ?? versions[0])}
            onChange={(v) => setVersion(Number(v))}
            options={versions.map((ts, i) => ({
              value: String(ts),
              label: i === 0 ? `Latest · ${relativeTimeUnix(ts)}` : `v${versions.length - i} · ${relativeTimeUnix(ts)}`,
            }))}
          />
        </div>
      ) : null}

      {doc.isLoading ? (
        <Skeleton style={{ height: 240 }} />
      ) : doc.error ? (
        <div className="text-sm" style={{ color: 'var(--status-error)' }}>
          Could not load this document.
        </div>
      ) : (
        <div className="deliverable-md">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{doc.data?.content ?? ''}</ReactMarkdown>
        </div>
      )}
    </Modal>
  );
}

function relativeTimeUnix(ts: number): string {
  return relativeTime(new Date(ts * 1000).toISOString());
}
