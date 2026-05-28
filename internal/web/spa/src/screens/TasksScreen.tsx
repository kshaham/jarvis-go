import { useMemo, useState } from 'react';
import { motion } from 'motion/react';

import { Metric, Press, RevealItem } from '../components/motion';
import { spring } from '../lib/motion';
import { relativeTime } from '../lib/relativeTime';
import { useTaskQueues } from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { Task, TaskPriority, TaskStatus } from '../api/types';

// Kanban columns mirror the vanilla shell: pending → acknowledged →
// in_progress → review-equivalent (failed/handoff) → completed.
// "review" doesn't exist in the canonical TaskStatus enum; we treat
// "blocked" + "handoff_pending" as a single "review-attention" column.
type ColumnId = 'pending' | 'acknowledged' | 'in_progress' | 'attention' | 'completed';

const COLUMNS: { id: ColumnId; label: string; accepts: TaskStatus[] }[] = [
  { id: 'pending', label: 'Backlog', accepts: ['pending'] },
  { id: 'acknowledged', label: 'To Do', accepts: ['acknowledged'] },
  { id: 'in_progress', label: 'Doing', accepts: ['in_progress'] },
  { id: 'attention', label: 'Attention', accepts: ['blocked', 'handoff_pending', 'failed'] },
  { id: 'completed', label: 'Done', accepts: ['completed'] },
];

const PRIORITY_RANK: Record<TaskPriority, number> = { high: 0, medium: 1, low: 2 };

// TasksScreen — Arc/Rauno kanban. Five columns, magnetic-press cards,
// search + priority filter, drag-out-of-scope (drag/drop is Phase 4 of
// the design plan and stays explicit). Density mirrors the vanilla
// dashboard so heavy backlogs still scan.
export function TasksScreen() {
  const queues = useTaskQueues();
  const status = useOfficeStatus();
  const [query, setQuery] = useState('');
  const [priority, setPriority] = useState<'all' | TaskPriority>('all');

  // Member name lookup keyed by sessionId so each card can show its
  // assignee without dragging the full TeamMember object around.
  const nameBySession = useMemo(() => {
    const map = new Map<string, string>();
    for (const m of status.data?.members ?? []) {
      map.set(m.sessionId, m.name);
    }
    return map;
  }, [status.data?.members]);

  // Flatten every agent's queue into one [{task, sessionId}] then
  // bucket by column. Sort within column by priority + assignedAt so
  // the highest-priority items float to the top of each lane.
  type Item = { task: Task; sessionId: string };
  const items = useMemo<Item[]>(() => {
    const out: Item[] = [];
    for (const queue of queues.data?.queues ?? []) {
      for (const task of queue.tasks ?? []) {
        out.push({ task, sessionId: queue.sessionId });
      }
    }
    return out;
  }, [queues.data?.queues]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return items.filter(({ task }) => {
      if (q) {
        if (
          !task.title.toLowerCase().includes(q) &&
          !(task.description ?? '').toLowerCase().includes(q) &&
          !(task.labels ?? []).join(' ').toLowerCase().includes(q)
        ) {
          return false;
        }
      }
      if (priority !== 'all' && task.priority !== priority) return false;
      return true;
    });
  }, [items, query, priority]);

  const buckets = useMemo(() => {
    const grouped: Record<ColumnId, Item[]> = {
      pending: [],
      acknowledged: [],
      in_progress: [],
      attention: [],
      completed: [],
    };
    for (const item of filtered) {
      const col = COLUMNS.find((c) => c.accepts.includes(item.task.status));
      if (col) grouped[col.id].push(item);
    }
    for (const k of Object.keys(grouped) as ColumnId[]) {
      grouped[k].sort((a, b) => {
        const pa = PRIORITY_RANK[(a.task.priority ?? 'medium') as TaskPriority] ?? 1;
        const pb = PRIORITY_RANK[(b.task.priority ?? 'medium') as TaskPriority] ?? 1;
        if (pa !== pb) return pa - pb;
        return (a.task.assignedAt ?? '').localeCompare(b.task.assignedAt ?? '');
      });
    }
    return grouped;
  }, [filtered]);

  const totalTasks = items.length;
  const inProgress = buckets.in_progress.length;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Tasks
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Live task queues across every agent. Drag-reorder is on the roadmap.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 18, alignItems: 'baseline' }}>
          <StatTile label="Total" value={totalTasks} />
          <StatTile label="In flight" value={inProgress} tone="accent" />
          <StatTile label="Attention" value={buckets.attention.length} tone={buckets.attention.length > 0 ? 'warning' : 'muted'} />
        </div>
      </header>

      {/* Filter bar */}
      <div
        style={{
          display: 'flex',
          gap: 10,
          alignItems: 'center',
          flexWrap: 'wrap',
        }}
      >
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search tasks…"
          style={{
            flex: '1 1 240px',
            padding: '8px 12px',
            background: 'var(--bg-surface)',
            border: '1px solid var(--border-default)',
            borderRadius: 'var(--radius-md)',
            color: 'var(--text-primary)',
            fontSize: 13,
            outline: 'none',
          }}
        />
        <div style={{ display: 'flex', gap: 6 }}>
          {(['all', 'high', 'medium', 'low'] as const).map((p) => {
            const active = priority === p;
            return (
              <Press
                key={p}
                pressScale={0.94}
                onClick={() => setPriority(p)}
                style={{
                  padding: '6px 12px',
                  background: active ? 'var(--accent-muted)' : 'var(--bg-surface)',
                  border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
                  borderRadius: 999,
                  color: active ? 'var(--accent)' : 'var(--text-muted)',
                  fontSize: 11,
                  fontWeight: 600,
                  textTransform: 'capitalize',
                  cursor: 'pointer',
                }}
              >
                {p}
              </Press>
            );
          })}
        </div>
      </div>

      {/* Kanban */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(5, minmax(220px, 1fr))',
          gap: 14,
          flex: 1,
          minHeight: 0,
          overflowX: 'auto',
        }}
      >
        {COLUMNS.map((col) => (
          <Column
            key={col.id}
            label={col.label}
            count={buckets[col.id].length}
            items={buckets[col.id]}
            nameBySession={nameBySession}
            tone={col.id === 'attention' ? 'warning' : col.id === 'completed' ? 'success' : 'default'}
          />
        ))}
      </div>
    </section>
  );
}

function Column({
  label,
  count,
  items,
  nameBySession,
  tone,
}: {
  label: string;
  count: number;
  items: { task: Task; sessionId: string }[];
  nameBySession: Map<string, string>;
  tone: 'default' | 'warning' | 'success';
}) {
  const borderTone =
    tone === 'warning' ? 'var(--status-warn)' : tone === 'success' ? 'var(--status-success)' : 'var(--border-subtle)';
  return (
    <section
      style={{
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--bg-surface)',
        border: '1px solid var(--border-subtle)',
        borderTop: '2px solid ' + borderTone,
        borderRadius: 'var(--radius-card)',
        padding: 10,
        gap: 10,
        minHeight: 0,
        overflow: 'hidden',
      }}
    >
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '2px 4px',
        }}
      >
        <span
          style={{
            fontSize: 11,
            color: 'var(--text-muted)',
            letterSpacing: 0.6,
            textTransform: 'uppercase',
            fontWeight: 700,
          }}
        >
          {label}
        </span>
        <Metric value={count} size={12} tone="muted" />
      </header>
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
          overflowY: 'auto',
          paddingRight: 2,
        }}
      >
        {items.length === 0 ? (
          <div
            style={{
              padding: 14,
              borderRadius: 'var(--radius-md)',
              border: '1px dashed var(--border-default)',
              color: 'var(--text-faint)',
              fontSize: 11,
              textAlign: 'center',
            }}
          >
            empty
          </div>
        ) : (
          items.map((item, i) => (
            <RevealItem key={item.task.id} index={Math.min(i, 12)}>
              <TaskCard
                task={item.task}
                assignee={nameBySession.get(item.sessionId) ?? item.sessionId.slice(0, 6)}
              />
            </RevealItem>
          ))
        )}
      </div>
    </section>
  );
}

function TaskCard({ task, assignee }: { task: Task; assignee: string }) {
  const priorityColor =
    task.priority === 'high'
      ? 'var(--status-error)'
      : task.priority === 'low'
        ? 'var(--text-muted)'
        : 'var(--status-warn)';
  return (
    <motion.article
      whileHover={{ y: -1, borderColor: 'var(--border-strong)' }}
      transition={spring.smooth}
      style={{
        padding: 10,
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        borderLeft: '2px solid ' + priorityColor,
        borderRadius: 'var(--radius-md)',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.04)',
      }}
    >
      <span
        style={{
          fontSize: 13,
          color: 'var(--text-primary)',
          fontWeight: 600,
          lineHeight: 1.35,
          display: '-webkit-box',
          WebkitLineClamp: 3,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
        }}
      >
        {task.title}
      </span>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 6,
          fontSize: 11,
          color: 'var(--text-muted)',
        }}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {assignee}
        </span>
        <span>{task.assignedAt ? relativeTime(task.assignedAt) : '—'}</span>
      </div>
      {task.labels && task.labels.length > 0 ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
          {task.labels.slice(0, 4).map((lab) => (
            <span
              key={lab}
              style={{
                fontSize: 10,
                padding: '1px 6px',
                borderRadius: 999,
                background: 'var(--bg-pressed)',
                color: 'var(--text-muted)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              {lab}
            </span>
          ))}
        </div>
      ) : null}
      {task.blockerDescription && task.status === 'blocked' ? (
        <span
          style={{
            fontSize: 11,
            color: 'var(--status-warn)',
            background: 'rgba(255,180,84,0.10)',
            padding: '4px 6px',
            borderRadius: 4,
          }}
        >
          🚧 {task.blockerDescription}
        </span>
      ) : null}
    </motion.article>
  );
}

function StatTile({
  label,
  value,
  tone = 'primary',
}: {
  label: string;
  value: number;
  tone?: 'primary' | 'muted' | 'accent' | 'warning';
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span
        style={{
          fontSize: 10,
          color: 'var(--text-muted)',
          textTransform: 'uppercase',
          letterSpacing: 0.4,
        }}
      >
        {label}
      </span>
      <Metric value={value} size={20} tone={tone} />
    </div>
  );
}
