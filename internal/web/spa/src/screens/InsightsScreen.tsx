import { useMemo } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { useReducedMotion } from 'motion/react';
import { BarChart3, CheckCircle2, Tags } from 'lucide-react';
import type { Format } from '@number-flow/react';

import { Metric } from '../components/motion/Metric';
import { StatusDot } from '../components/motion/StatusDot';
import { EmptyState } from '../components/ui/EmptyState';
import { GlowCard } from '../components/ui/GlowCard';
import { Section } from '../components/ui/Section';
import { CostByLabelChart } from '../components/charts/CostByLabelChart';
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

  const [costListRef] = useAutoAnimate<HTMLUListElement>();
  const [blockedListRef] = useAutoAnimate<HTMLUListElement>();

  const m = metrics.data?.metrics ?? {};
  const members = useMemo(() => status.data?.members ?? [], [status.data?.members]);

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

  // Cost-by-label top 12.
  const labelRows = useMemo(() => {
    return (labels.data?.rollups ?? [])
      .toSorted((a, b) => (b.totalCostUSD ?? 0) - (a.totalCostUSD ?? 0))
      .slice(0, 12);
  }, [labels.data?.rollups]);

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
          gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
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
      <Section title="Cost by agent" hint="Top 8 by total spend.">
        {costByAgent.length === 0 ? (
          <EmptyState
            icon={<BarChart3 size={22} />}
            title="No spend yet"
            description="Cost accrues per agent as the team works."
          />
        ) : (
          <ul
            ref={costListRef}
            style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 8 }}
          >
            {costByAgent.map((row) => {
              const pct = maxCost > 0 ? (row.cost / maxCost) * 100 : 0;
              return (
                <li key={row.sessionId}>
                  <GlowCard interactive className="flex flex-col gap-1.5 p-2.5">
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
                        pulseOnChange
                      />
                    </div>
                    <ProgressBar pct={pct} />
                  </GlowCard>
                </li>
              );
            })}
          </ul>
        )}
      </Section>

      {/* Blocked-tasks leaderboard */}
      <Section title="Blocked tasks" hint="Agents with blocked work waiting for them.">
        {blockedByAgent.length === 0 ? (
          <EmptyState
            icon={<CheckCircle2 size={22} />}
            title="Nothing blocked"
            description="No blocked work is waiting on the team right now."
          />
        ) : (
          <ul
            ref={blockedListRef}
            style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}
          >
            {blockedByAgent.map((row) => (
              <li key={row.name}>
                <GlowCard
                  interactive
                  className="flex items-center justify-between p-2.5"
                >
                  <span style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 600 }}>
                    {row.name}
                  </span>
                  <Metric value={row.count} size={13} tone="warning" pulseOnChange />
                </GlowCard>
              </li>
            ))}
          </ul>
        )}
      </Section>

      {/* Cost by label — chart + detail rows */}
      <Section title="Cost by label" hint="Top labels by total spend. Tasks pick up labels from the workflow.">
        {labelRows.length === 0 ? (
          <EmptyState
            icon={<Tags size={22} />}
            title="No labelled tasks yet"
            description="Labels are assigned by the workflow as tasks flow through."
          />
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <CostByLabelChart rollups={labelRows} />
            <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
              {labelRows.map((row) => {
                const cost = row.totalCostUSD ?? 0;
                return (
                  <li
                    key={row.label}
                    style={{
                      display: 'flex',
                      gap: 12,
                      alignItems: 'baseline',
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
                      pulseOnChange
                    />
                    {typeof row.taskCount === 'number' ? (
                      <span style={{ color: 'var(--text-muted)' }}>
                        {row.taskCount} task{row.taskCount === 1 ? '' : 's'}
                      </span>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </Section>
    </section>
  );
}

// ProgressBar — rounded, gradient-accent fill. Width transitions are
// gated off under reduced motion.
function ProgressBar({ pct }: { pct: number }) {
  const reduce = useReducedMotion();
  return (
    <div
      style={{
        height: 7,
        background: 'var(--bg-pressed)',
        borderRadius: 999,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          width: `${pct}%`,
          height: '100%',
          borderRadius: 999,
          background: 'linear-gradient(90deg, var(--accent), var(--accent-strong))',
          transition: reduce ? undefined : 'width 0.4s ease',
        }}
      />
    </div>
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
    <GlowCard interactive className="flex flex-col gap-1 p-4">
      <span
        style={{
          fontSize: 12,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      <Metric value={value} size={24} tone={tone ?? 'primary'} format={format} pulseOnChange />
    </GlowCard>
  );
}
