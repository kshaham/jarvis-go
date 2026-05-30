import { useMemo } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { ArrowLeftRight } from 'lucide-react';

import { Metric } from '../components/motion/Metric';
import { GlowCard } from '../components/ui/GlowCard';
import { EmptyState } from '../components/ui/EmptyState';
import { useTaskQueues } from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import { relativeTime } from '../lib/relativeTime';

// HandoffsScreen — read-only view of every task currently in
// `status='handoff_pending'`. Agents accept / reject handoffs through
// their own CLI; the operator just gets visibility here so they
// know what's stuck.
export function HandoffsScreen() {
  const queues = useTaskQueues();
  const status = useOfficeStatus();
  // AutoAnimate owns enter/exit/reorder of the live list as handoffs
  // resolve over SSE (respects reduced motion).
  const [listRef] = useAutoAnimate<HTMLUListElement>();
  const nameBySession = useMemo(() => {
    const m = new Map<string, string>();
    for (const member of status.data?.members ?? []) m.set(member.sessionId, member.name);
    return m;
  }, [status.data?.members]);

  const pending = useMemo(() => {
    const out: { fromName: string; toName?: string; taskId: string; title: string; when?: string }[] = [];
    for (const q of queues.data?.queues ?? []) {
      for (const t of q.tasks ?? []) {
        if (t.status !== 'handoff_pending') continue;
        out.push({
          fromName: q.agentName,
          taskId: t.id,
          title: t.title,
          when: t.assignedAt,
          // handoff target is encoded in the BlockedBy / handoff object on
          // the wire; treat anything we can't resolve as unknown rather
          // than guessing.
          toName: undefined,
        });
      }
    }
    return out;
  }, [queues.data?.queues]);

  void nameBySession;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Handoffs
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Pending task transfers. The recipient accepts or rejects through their own session.
        </p>
      </header>

      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
        <Metric value={pending.length} size={12} pulseOnChange /> awaiting decision
      </span>

      {pending.length === 0 ? (
        <EmptyState
          icon={<ArrowLeftRight size={22} />}
          title="No pending handoffs"
          description="When an agent hands a task to a teammate, it shows up here until they accept or reject."
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
          {pending.map((row) => (
            <li key={row.taskId} style={{ listStyle: 'none' }}>
              <GlowCard interactive className="flex items-center gap-3 p-3">
                <span style={{ flex: 1, minWidth: 0 }}>
                  <span
                    style={{
                      display: 'block',
                      fontSize: 13,
                      color: 'var(--text-primary)',
                      fontWeight: 600,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {row.title}
                  </span>
                  <span style={{ display: 'block', fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
                    from <strong style={{ color: 'var(--text-secondary)' }}>{row.fromName}</strong>
                    {row.when ? ` · ${relativeTime(row.when)}` : ''}
                  </span>
                </span>
                <span
                  style={{
                    fontSize: 12,
                    color: 'var(--status-warn)',
                    padding: '4px 10px',
                    background: 'rgba(255,180,84,0.10)',
                    borderRadius: 'var(--radius-sm)',
                  }}
                >
                  Pending
                </span>
              </GlowCard>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
