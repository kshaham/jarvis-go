import { Metric } from '../components/motion/Metric';
import { RevealItem } from '../components/motion/RevealItem';
import { GlowCard } from '../components/ui/GlowCard';
import { CostTokensChart } from '../components/charts/CostTokensChart';
import { MetricSparkline } from '../components/charts/MetricSparkline';
import { useMetrics } from '../hooks/useOfficeData';
import { useMetricsHistory, type MetricPoint } from '../hooks/useMetricsHistory';

type Tone = 'accent' | 'success' | 'warning';
type SparkTone = 'accent' | 'success' | 'warn' | 'error';

// MetricsScreen — KPI grid for the office. Reads /api/office/metrics.
// Headline numbers animate via NumberFlow (Metric); a rolling
// client-side history (useMetricsHistory) drives inline sparklines and
// a cost/tokens area chart.
export function MetricsScreen() {
  const metrics = useMetrics();
  const m = metrics.data?.metrics ?? {};
  const history = useMetricsHistory();

  const tiles: {
    label: string;
    value: number;
    format?: Intl.NumberFormatOptions;
    tone?: Tone;
    sparkKey?: keyof MetricPoint;
    sparkTone?: SparkTone;
  }[] = [
    { label: 'Standups completed', value: m.standupsCompleted ?? 0 },
    { label: 'Tasks done', value: m.tasksDone ?? 0, tone: 'success', sparkKey: 'tasksDone', sparkTone: 'success' },
    { label: 'Bugs filed', value: m.bugsFiled ?? 0 },
    { label: 'Bugs fixed', value: m.bugsFixed ?? 0, tone: 'success' },
    { label: 'Recoveries', value: m.recoveries ?? 0 },
    {
      label: 'Stuck alerts',
      value: m.stuckAlerts ?? 0,
      tone: (m.stuckAlerts ?? 0) > 0 ? 'warning' : undefined,
    },
    { label: 'Total messages', value: m.totalMessages ?? 0, sparkKey: 'totalMessages', sparkTone: 'accent' },
  ];

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Metrics
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Cumulative counters since the current team spawned.
        </p>
      </header>

      {/* Headline numbers */}
      <section
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
          gap: 12,
        }}
      >
        {tiles.map((t, i) => (
          <RevealItem key={t.label} index={i}>
            <Tile
              label={t.label}
              value={t.value}
              tone={t.tone}
              spark={t.sparkKey ? { data: history, dataKey: t.sparkKey, tone: t.sparkTone } : undefined}
            />
          </RevealItem>
        ))}
      </section>

      {/* Cost & tokens — headline figures + the live trend chart. */}
      <GlowCard className="flex flex-col gap-4 p-[18px]">
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))',
            gap: 14,
          }}
        >
          <Big label="Total cost" value={m.totalCostUSD ?? 0} currency tone="accent" />
          <Big label="Input tokens" value={m.inputTokens ?? 0} />
          <Big label="Output tokens" value={m.outputTokens ?? 0} />
          <Big label="Avg response %" value={Math.round((m.avgResponsePct ?? 0) * 100) / 100} suffix="%" />
        </div>
        <div style={{ borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
          <div
            style={{
              fontSize: 12,
              color: 'var(--text-muted)',
              letterSpacing: 0.4,
              textTransform: 'uppercase',
              marginBottom: 4,
            }}
          >
            Cost &amp; output tokens over time
          </div>
          <CostTokensChart data={history} />
        </div>
      </GlowCard>

      {/* Uptime per agent */}
      {m.uptimeByAgent && Object.keys(m.uptimeByAgent).length > 0 ? (
        <GlowCard className="p-[18px]">
          <header
            style={{
              display: 'flex',
              alignItems: 'baseline',
              justifyContent: 'space-between',
              marginBottom: 10,
            }}
          >
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>
              Uptime by agent
            </h2>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>seconds since spawn</span>
          </header>
          <ul
            style={{
              listStyle: 'none',
              padding: 0,
              margin: 0,
              display: 'flex',
              flexDirection: 'column',
              gap: 6,
            }}
          >
            {Object.entries(m.uptimeByAgent).map(([name, secs]) => (
              <li
                key={name}
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  padding: '6px 0',
                  borderBottom: '1px solid var(--border-subtle)',
                  fontSize: 12,
                  color: 'var(--text-secondary)',
                }}
              >
                <span style={{ fontFamily: 'var(--font-mono)' }}>{name}</span>
                <Metric value={secs} size={12} tone="muted" />
              </li>
            ))}
          </ul>
        </GlowCard>
      ) : null}
    </section>
  );
}

function Tile({
  label,
  value,
  tone,
  spark,
}: {
  label: string;
  value: number;
  tone?: Tone;
  spark?: { data: MetricPoint[]; dataKey: keyof MetricPoint; tone?: SparkTone };
}) {
  return (
    <GlowCard interactive className="flex flex-col gap-1.5 p-4">
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
      <Metric value={value} size={24} tone={tone ?? 'primary'} pulseOnChange />
      {spark ? <MetricSparkline data={spark.data} dataKey={spark.dataKey} tone={spark.tone} /> : null}
    </GlowCard>
  );
}

function Big({
  label,
  value,
  currency,
  suffix,
  tone,
}: {
  label: string;
  value: number;
  currency?: boolean;
  suffix?: string;
  tone?: 'accent' | 'primary';
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
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
      <Metric
        value={value}
        size={32}
        tone={tone ?? 'primary'}
        pulseOnChange
        format={
          currency ? { style: 'currency', currency: 'USD', minimumFractionDigits: 2 } : undefined
        }
        suffix={suffix}
      />
    </div>
  );
}
