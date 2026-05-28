import { useMemo, useState } from 'react';

import { Metric, Press, RevealItem } from '../components/motion';
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
          <Metric value={filtered.length} size={20} /> events
        </span>
      </header>

      <div
        style={{
          display: 'flex',
          gap: 6,
          flexWrap: 'wrap',
          padding: '4px 0',
          position: 'sticky',
          top: 0,
          background: 'var(--bg-app)',
          zIndex: 1,
        }}
      >
        {KIND_CHIPS.map((chip) => {
          const active = kind === chip.id;
          return (
            <Press
              key={chip.id || 'all'}
              pressScale={0.94}
              onClick={() => setKind(chip.id)}
              style={{
                padding: '6px 12px',
                background: active ? 'var(--accent-muted)' : 'var(--bg-surface)',
                border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
                borderRadius: 999,
                color: active ? 'var(--accent)' : 'var(--text-muted)',
                fontSize: 11,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              {chip.label}
            </Press>
          );
        })}
      </div>

      {filtered.length === 0 ? (
        <Empty />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {filtered.map((entry, i) => (
            <RevealItem key={`${entry.timestamp}-${i}`} index={Math.min(i, 12)}>
              <Entry entry={entry} />
            </RevealItem>
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
        <span
          style={{
            position: 'relative',
            display: 'block',
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: color,
            margin: '0 auto',
          }}
        />
      </div>
      <article
        style={{
          flex: 1,
          padding: 12,
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-md)',
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
        }}
      >
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
      </article>
    </div>
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
      No activity yet.
    </div>
  );
}
