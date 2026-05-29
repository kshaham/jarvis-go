import { useMemo, useState } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Activity } from 'lucide-react';

import { Metric } from '../components/motion';
import { Chip, EmptyState, GlowCard, Skeleton } from '../components/ui';
import { relativeTime } from '../lib/relativeTime';
import { useTimeline } from '../hooks/useOfficeData';
import type { TimelineEntry } from '../api/types';

const KIND_CHIPS: { id: string; label: string }[] = [
  { id: '', label: 'All' },
  { id: 'task', label: 'Tasks' },
  { id: 'message', label: 'Messages' },
  { id: 'standup', label: 'Standup' },
  { id: 'audit', label: 'Audit' },
  { id: 'taskReviewed', label: 'Reviews' },
];

const KIND_COLOR: Record<string, string> = {
  task: 'var(--accent)',
  message: 'var(--status-meeting)',
  standup: 'var(--status-success)',
  audit: 'var(--text-muted)',
  taskReviewed: 'var(--status-warn)',
};

// TimelineScreen — single unified feed of every office event. Sticky
// filter chips, time-spine on the left, NumberFlow on the total count.
export function TimelineScreen() {
  const timeline = useTimeline(200);
  const [kind, setKind] = useState('');
  // AutoAnimate owns enter/exit/reorder as new events stream in over
  // SSE (respects reduced motion). Replaces the per-row RevealItem so
  // live mutations animate without double-staggering.
  const [listRef] = useAutoAnimate<HTMLDivElement>();

  const entries = useMemo(() => timeline.data?.entries ?? [], [timeline.data?.entries]);
  const filtered = useMemo(() => {
    if (!kind) return entries;
    return entries.filter((e) => e.kind === kind);
  }, [entries, kind]);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16, minHeight: 0 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Activity
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Chronological feed of every event the team produces.
          </p>
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
          {/* Count pulses as new events stream in. */}
          <Metric value={filtered.length} size={20} pulseOnChange /> events
        </span>
      </header>

      <div
        className="flex flex-wrap gap-1.5"
        style={{
          padding: '4px 0',
          position: 'sticky',
          top: 0,
          background: 'var(--bg-app)',
          zIndex: 1,
        }}
      >
        {KIND_CHIPS.map((chip) => (
          <Chip key={chip.id || 'all'} active={kind === chip.id} onClick={() => setKind(chip.id)}>
            {chip.label}
          </Chip>
        ))}
      </div>

      {timeline.isLoading ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {Array.from({ length: 5 }).map((_, i) => (
            <EntrySkeleton key={i} />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Activity size={22} />}
          title="No activity yet"
          description="Events appear here as the team produces tasks, messages, standups, and reviews."
        />
      ) : (
        <div ref={listRef} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {filtered.map((entry, i) => (
            <Entry key={`${entry.timestamp}-${i}`} entry={entry} />
          ))}
        </div>
      )}
    </section>
  );
}

function Entry({ entry }: { entry: TimelineEntry }) {
  const color = KIND_COLOR[entry.kind] ?? 'var(--text-muted)';
  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <div
        style={{
          width: 16,
          paddingTop: 14,
          position: 'relative',
          flexShrink: 0,
        }}
      >
        <span
          style={{
            position: 'absolute',
            top: 0,
            bottom: 0,
            left: 7,
            width: 1,
            background: 'var(--border-subtle)',
          }}
        />
        {/* The spine dot glows in its kind color for emphasis. */}
        <span
          style={{
            position: 'relative',
            display: 'block',
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: color,
            margin: '0 auto',
            boxShadow: `0 0 6px ${color}`,
          }}
        />
      </div>
      <GlowCard interactive className="flex flex-1 flex-col gap-1 p-3">
        <header style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
          <span
            style={{
              fontSize: 11,
              color,
              fontWeight: 700,
              letterSpacing: 0.4,
              textTransform: 'uppercase',
            }}
          >
            {entry.kind}
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            {relativeTime(entry.timestamp)}
          </span>
        </header>
        <div style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 600 }}>
          {entry.title}
        </div>
        {entry.agentName ? (
          <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>{entry.agentName}</div>
        ) : null}
        {entry.summary ? (
          <div style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
            {entry.summary}
          </div>
        ) : null}
      </GlowCard>
    </div>
  );
}

function EntrySkeleton() {
  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <div style={{ width: 16, paddingTop: 14, position: 'relative', flexShrink: 0 }}>
        <span
          style={{
            position: 'absolute',
            top: 0,
            bottom: 0,
            left: 7,
            width: 1,
            background: 'var(--border-subtle)',
          }}
        />
        <Skeleton className="relative mx-auto h-2 w-2 rounded-full" />
      </div>
      <div className="surface-card flex flex-1 flex-col gap-2 p-3">
        <div className="flex justify-between gap-8">
          <Skeleton className="h-2.5 w-16" />
          <Skeleton className="h-2.5 w-12" />
        </div>
        <Skeleton className="h-3 w-2/3" />
        <Skeleton className="h-2.5 w-full" />
      </div>
    </div>
  );
}
