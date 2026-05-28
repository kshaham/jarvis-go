import { useMemo } from 'react';
import type { Format } from '@number-flow/react';

import { Metric, RevealItem, StatusDot } from '../components/motion';
import {
  useLabelsRollup,
  useMetrics,
  useTaskQueues,
} from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';

// InsightsScreen — KPI dashboard. Cards mirror the vanilla shell's
// "insights" tab: spend, cost-per-agent leaderboard, token mix,
// blocked-tasks leaderboard, cost-by-label.
export function InsightsScreen() {
  const metrics = useMetrics();
  const status = useOfficeStatus();
  const queues = useTaskQueues();
  const labels = useLabelsRollup();

  const m = metrics.data?.metrics ?? {};
  const members = status.data?.members ?? [];

  // Cost-per-agent leaderboard, sorted descending.
  const costByAgent = useMemo(() => {
    return [...members]
      .map((mem) => ({
        name: mem.name,
        sessionId: mem.sessionId,
        status: mem.status,
        cost: mem.totalCost ?? 0,
      }))
      .sort((a, b) => b.cost - a.cost)
      .slice(0, 8);
  }, [members]);

  const maxCost = costByAgent[0]?.cost ?? 0;

  // Blocked tasks leaderboard.
  const blockedByAgent = useMemo(() => {
    const map = new Map<string, { name: string; count: number }>();
    for (const q of queues.data?.queues ?? []) {
      const blocked = (q.tasks ?? []).filter((t) => t.status === 'blocked').length;
      if (blocked > 0) {
        map.set(q.sessionId, { name: q.agentName, count: blocked });
      }
    }
    return Array.from(map.values()).sort((a, b) => b.count - a.count).slice(0, 6);
  }, [queues.data?.queues]);

  // Cost-by-label top 8.
  const labelRows = useMemo(() => {
    return [...(labels.data?.rollups ?? [])]
      .sort((a, b) => (b.totalCostUSD ?? 0) - (a.totalCostUSD ?? 0))
      .slice(0, 12);
  }, [labels.data?.rollups]);
  const maxLabelCost = labelRows[0]?.totalCostUSD ?? 0;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Insights
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Where the spend goes and what's blocking the team.
        </p>
      </header>

      {/* Top KPIs */}
      <section
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
          gap: 10,
        }}
      >
        <Kpi
          label="Spend"
          value={m.totalCostUSD ?? 0}
          format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
          tone="accent"
        />
        <Kpi label="Tasks done" value={m.tasksDone ?? 0} tone="success" />
        <Kpi label="Input tokens" value={m.inputTokens ?? 0} />
        <Kpi label="Output tokens" value={m.outputTokens ?? 0} />
      </section>

      {/* Cost-per-agent leaderboard */}
      <Card title="Cost by agent" hint="Top 8 by total spend.">
        {costByAgent.length === 0 ? (
          <Empty />
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
            {costByAgent.map((row, i) => {
              const pct = maxCost > 0 ? (row.cost / maxCost) * 100 : 0;
              return (
                <RevealItem key={row.sessionId} index={i}>
                  <li style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 8,
                        fontSize: 12,
                        color: 'var(--text-secondary)',
                      }}
                    >
                      <StatusDot status={row.status} size={6} />
                      <span style={{ flex: 1, fontWeight: 600, color: 'var(--text-primary)' }}>
                        {row.name}
                      </span>
                      <Metric
                        value={row.cost}
                        format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
                        size={12}
                      />
                    </div>
                    <div
                      style={{
                        height: 4,
                        background: 'var(--bg-pressed)',
                        borderRadius: 2,
                        overflow: 'hidden',
                      }}
                    >
                      <div
                        style={{
                          width: `${pct}%`,
                          height: '100%',
                          background: 'var(--accent)',
                        }}
                      />
                    </div>
                  </li>
                </RevealItem>
              );
            })}
          </ul>
        )}
      </Card>

      {/* Blocked-tasks leaderboard */}
      <Card title="Blocked tasks" hint="Agents with blocked work waiting for them.">
        {blockedByAgent.length === 0 ? (
          <Empty message="Nothing blocked right now." />
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {blockedByAgent.map((row, i) => (
              <RevealItem key={row.name} index={i}>
                <li
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    padding: '8px 10px',
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                  }}
                >
                  <span style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 600 }}>
                    {row.name}
                  </span>
                  <Metric value={row.count} size={13} tone="warning" />
                </li>
              </RevealItem>
            ))}
          </ul>
        )}
      </Card>

      {/* Cost by label */}
      <Card title="Cost by label" hint="Top labels by total spend. Tasks pick up labels from the workflow.">
        {labelRows.length === 0 ? (
          <Empty message="No labelled tasks yet." />
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
            {labelRows.map((row, i) => {
              const cost = row.totalCostUSD ?? 0;
              const pct = maxLabelCost > 0 ? (cost / maxLabelCost) * 100 : 0;
              return (
                <RevealItem key={row.label} index={i}>
                  <li style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div
                      style={{
                        display: 'flex',
                        gap: 12,
                        fontSize: 12,
                        color: 'var(--text-secondary)',
                      }}
                    >
                      <span
                        style={{
                          flex: 1,
                          fontFamily: 'var(--font-mono)',
                          color: 'var(--text-primary)',
                        }}
                      >
                        {row.label}
                      </span>
                      <Metric
                        value={cost}
                        format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
                        size={12}
                      />
                      {typeof row.taskCount === 'number' ? (
                        <span style={{ color: 'var(--text-muted)' }}>
                          {row.taskCount} task{row.taskCount === 1 ? '' : 's'}
                        </span>
                      ) : null}
                    </div>
                    <div
                      style={{
                        height: 4,
                        background: 'var(--bg-pressed)',
                        borderRadius: 2,
                        overflow: 'hidden',
                      }}
                    >
                      <div
                        style={{
                          width: `${pct}%`,
                          height: '100%',
                          background: 'var(--status-meeting)',
                        }}
                      />
                    </div>
                  </li>
                </RevealItem>
              );
            })}
          </ul>
        )}
      </Card>
    </section>
  );
}

function Kpi({
  label,
  value,
  format,
  tone,
}: {
  label: string;
  value: number;
  format?: Format;
  tone?: 'accent' | 'success' | 'primary';
}) {
  return (
    <div
      style={{
        padding: 16,
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-md)',
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
      }}
    >
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      <Metric value={value} size={24} tone={tone ?? 'primary'} format={format} />
    </div>
  );
}

function Card({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <section
      style={{
        padding: 18,
        background: 'var(--bg-surface)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-card)',
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
      }}
    >
      <header>
        <h2 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>
          {title}
        </h2>
        {hint ? (
          <p style={{ margin: '2px 0 0', fontSize: 11, color: 'var(--text-muted)' }}>{hint}</p>
        ) : null}
      </header>
      {children}
    </section>
  );
}

function Empty({ message = 'No data yet.' }: { message?: string }) {
  return (
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
      {message}
    </div>
  );
}
