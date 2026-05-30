import React from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Server } from 'lucide-react';

import { Metric } from '../components/motion/Metric';
import { StatusDot } from '../components/motion/StatusDot';
import { GlowCard } from '../components/ui/GlowCard';
import { EmptyState } from '../components/ui/EmptyState';
import { relativeTime } from '../lib/relativeTime';
import { useDevInstances } from '../hooks/useOfficeData';

const statusBadgeBaseStyle: React.CSSProperties = {
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: 0.4,
  padding: '2px 8px',
  borderRadius: 'var(--radius-sm)',
  fontSize: 12,
};

// DevInstancesScreen — list the team's dev servers (frontend / api /
// custom slots). Each card shows URL + PID + health and credentials
// the agents use to log in. Read-only mirror of the classic UI's dev
// instances panel; start/stop lives in the per-agent settings.
export function DevInstancesScreen() {
  const di = useDevInstances();
  const list = di.data?.instances ?? [];
  // AutoAnimate owns enter/exit/reorder of the live grid as instances
  // spin up or down over SSE (respects reduced motion).
  const [gridRef] = useAutoAnimate<HTMLDivElement>();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Dev instances
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Sibling dev servers the team uses for live preview + manual smoke testing.
        </p>
      </header>

      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
        <Metric value={list.length} size={12} pulseOnChange /> instances running
      </span>

      {list.length === 0 ? (
        <EmptyState
          icon={<Server size={22} />}
          title="No dev instances"
          description={
            <>
              The team will spin them up via{' '}
              <code style={{ fontFamily: 'var(--font-mono)' }}>cm dev start</code> when needed.
            </>
          }
        />
      ) : (
        <div
          ref={gridRef}
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
            gap: 10,
          }}
        >
          {list.map((d) => {
            const dotStatus = (d.healthFailures ?? 0) > 0 ? 'stuck' : d.url ? 'working' : 'idle';
            return (
              <GlowCard
                key={d.slot}
                interactive
                live={dotStatus === 'working'}
                className="flex flex-col gap-2 p-3.5"
              >
                <header style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <StatusDot status={dotStatus} size={8} />
                  <span style={{ flex: 1, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>
                    {d.slot}
                  </span>
                  {/* Status badge top-right. */}
                  <span
                    style={{
                      ...statusBadgeBaseStyle,
                      background:
                        dotStatus === 'stuck'
                          ? 'rgba(239,68,68,0.12)'
                          : dotStatus === 'working'
                            ? 'var(--accent-muted)'
                            : 'var(--bg-pressed)',
                      color:
                        dotStatus === 'stuck'
                          ? 'var(--status-error)'
                          : dotStatus === 'working'
                            ? 'var(--accent)'
                            : 'var(--text-muted)',
                    }}
                  >
                    {dotStatus === 'stuck' ? 'Unhealthy' : dotStatus === 'working' ? 'Live' : 'Idle'}
                  </span>
                  {typeof d.pid === 'number' ? (
                    <span style={{ fontSize: 12, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
                      pid {d.pid}
                    </span>
                  ) : null}
                </header>
                {d.url ? (
                  <a
                    href={d.url}
                    target="_blank"
                    rel="noreferrer"
                    style={{
                      fontSize: 12,
                      fontFamily: 'var(--font-mono)',
                      color: 'var(--accent)',
                      wordBreak: 'break-all',
                    }}
                  >
                    {d.url}
                  </a>
                ) : (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>(no URL recorded)</span>
                )}
                <div style={{ display: 'flex', gap: 12, fontSize: 12, color: 'var(--text-muted)' }}>
                  {d.lastCheckAt ? <span>last check {relativeTime(d.lastCheckAt)}</span> : null}
                  {typeof d.healthFailures === 'number' && d.healthFailures > 0 ? (
                    <span style={{ color: 'var(--status-error)' }}>
                      {d.healthFailures} health failure{d.healthFailures === 1 ? '' : 's'}
                    </span>
                  ) : null}
                </div>
                {d.credentials && Object.keys(d.credentials).length > 0 ? (
                  <details
                    style={{
                      padding: 8,
                      background: 'var(--bg-app)',
                      borderRadius: 'var(--radius-sm)',
                      fontSize: 12,
                    }}
                  >
                    <summary style={{ cursor: 'pointer', color: 'var(--text-muted)' }}>
                      Credentials ({Object.keys(d.credentials).length})
                    </summary>
                    <ul style={{ listStyle: 'none', padding: '6px 0 0', margin: 0, fontFamily: 'var(--font-mono)' }}>
                      {Object.entries(d.credentials).map(([k, v]) => (
                        <li key={k} style={{ color: 'var(--text-secondary)' }}>
                          {k}: {v}
                        </li>
                      ))}
                    </ul>
                  </details>
                ) : null}
              </GlowCard>
            );
          })}
        </div>
      )}
    </section>
  );
}
