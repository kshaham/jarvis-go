import { Metric, RevealItem } from '../components/motion';
import { useMetrics } from '../hooks/useOfficeData';

// MetricsScreen — KPI grid for the office. Reads /api/office/metrics.
// Each tile uses Metric for digit-by-digit animation when values
// update via SSE-driven invalidation.
export function MetricsScreen() {
  const metrics = useMetrics();
  const m = metrics.data?.metrics ?? {};

  const tiles: { label: string; value: number; format?: Intl.NumberFormatOptions; tone?: 'accent' | 'success' | 'warning' }[] = [
    { label: 'Standups completed', value: m.standupsCompleted ?? 0 },
    { label: 'Tasks done', value: m.tasksDone ?? 0, tone: 'success' },
    { label: 'Bugs filed', value: m.bugsFiled ?? 0 },
    { label: 'Bugs fixed', value: m.bugsFixed ?? 0, tone: 'success' },
    { label: 'Recoveries', value: m.recoveries ?? 0 },
    { label: 'Stuck alerts', value: m.stuckAlerts ?? 0, tone: (m.stuckAlerts ?? 0) > 0 ? 'warning' : undefined },
    { label: 'Total messages', value: m.totalMessages ?? 0 },
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
          gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))',
          gap: 10,
        }}
      >
        {tiles.map((t, i) => (
          <RevealItem key={t.label} index={i}>
            <Tile label={t.label} value={t.value} tone={t.tone} />
          </RevealItem>
        ))}
      </section>

      {/* Cost & tokens */}
      <section
        style={{
          padding: 18,
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-card)',
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))',
          gap: 14,
        }}
      >
        <Big label="Total cost" value={m.totalCostUSD ?? 0} currency tone="accent" />
        <Big label="Input tokens" value={m.inputTokens ?? 0} />
        <Big label="Output tokens" value={m.outputTokens ?? 0} />
        <Big label="Avg response %" value={Math.round((m.avgResponsePct ?? 0) * 100) / 100} suffix="%" />
      </section>

      {/* Uptime per agent */}
      {m.uptimeByAgent && Object.keys(m.uptimeByAgent).length > 0 ? (
        <section
          style={{
            padding: 18,
            background: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
          }}
        >
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
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              seconds since spawn
            </span>
          </header>
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
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
        </section>
      ) : null}
    </section>
  );
}

function Tile({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: 'accent' | 'success' | 'warning';
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
      <Metric value={value} size={24} tone={tone ?? 'primary'} />
    </div>
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
          fontSize: 11,
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
        format={
          currency
            ? { style: 'currency', currency: 'USD', minimumFractionDigits: 2 }
            : undefined
        }
        suffix={suffix}
      />
    </div>
  );
}
