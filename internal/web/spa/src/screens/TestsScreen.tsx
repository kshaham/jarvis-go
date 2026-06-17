import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { FlaskConical, ShieldCheck } from 'lucide-react';

import { Button, EmptyState, GlowCard, SegmentedControl, Skeleton } from '@/components/ui';
import { useTestsResults } from '@/hooks/useTestsData';
import { relativeTime } from '@/lib/relativeTime';
import type { QualityStatus, TestProject } from '@/api/types';

type Tab = 'coverage' | 'vulnerabilities';

const gridStyle: React.CSSProperties = {
  display: 'grid',
  gap: 12,
  gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
};

// TestsScreen — the TDD dashboard. Two tabs over the saved-projects list:
// Coverage (run the suite, show total %) and Vulnerabilities (Trivy scan,
// show counts). Each project is a tile with a Run/Scan button; status flows
// live from the tests:* SSE events.
export function TestsScreen() {
  const [tab, setTab] = useState<Tab>('coverage');
  const { projects, isLoading, runCoverage, runScan, runReact } = useTestsResults();
  const navigate = useNavigate();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>Tests</h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Test-driven development at a glance — coverage and dependency vulnerabilities for every project.
        </p>
      </header>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <SegmentedControl<Tab>
          ariaLabel="Tests view"
          value={tab}
          onChange={setTab}
          options={[
            { value: 'coverage', label: 'Coverage' },
            { value: 'vulnerabilities', label: 'Vulnerabilities' },
          ]}
        />
        {tab === 'vulnerabilities' && projects.length > 0 ? (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => projects.forEach((p) => runScan.mutate(p.id))}
          >
            Scan all
          </Button>
        ) : null}
      </div>

      {isLoading ? (
        <div style={gridStyle}>
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} style={{ height: 132, borderRadius: 'var(--radius-card)' }} />
          ))}
        </div>
      ) : projects.length === 0 ? (
        <EmptyState
          icon={<FlaskConical size={22} />}
          title="No projects yet"
          description="Save a project on the Spawn page to track its test coverage and vulnerabilities here."
          action={<Button onClick={() => navigate('/spawn')}>Go to Spawn</Button>}
        />
      ) : (
        <div style={gridStyle}>
          {projects.map((p) =>
            tab === 'coverage' ? (
              <CoverageTile
                key={p.id}
                p={p}
                onRun={() => runCoverage.mutate(p.id)}
                onRunReact={() => runReact.mutate(p.id)}
              />
            ) : (
              <VulnTile key={p.id} p={p} onScan={() => runScan.mutate(p.id)} />
            ),
          )}
        </div>
      )}
    </section>
  );
}

// ── tiles ────────────────────────────────────────────────────────────────

function CoverageTile({
  p,
  onRun,
  onRunReact,
}: {
  p: TestProject;
  onRun: () => void;
  onRunReact: () => void;
}) {
  const { coverage, react } = p;
  const running = coverage.status === 'running';
  const pct = coverage.pct;
  const color =
    pct == null
      ? 'var(--text-faint)'
      : pct >= 80
        ? 'var(--status-success)'
        : pct >= 50
          ? 'var(--status-warn)'
          : 'var(--status-error)';
  // react-doctor only applies to React projects (backend marks the rest
  // 'unsupported' so the badge stays hidden for non-React tiles).
  const isReact = react.status !== 'unsupported';
  return (
    <GlowCard interactive className="flex flex-col gap-3 p-4">
      <TileHeader name={p.name} sub={langLabel(coverage.language, p.kind)} kind={p.kind} />
      <div className="flex items-end justify-between">
        <div className="ds-tabular flex items-baseline" style={{ color }}>
          {pct == null ? (
            <span style={{ fontSize: 30, fontWeight: 700 }}>—</span>
          ) : (
            <>
              <span style={{ fontSize: 30, fontWeight: 700, lineHeight: 1 }}>{Math.round(pct)}</span>
              <span style={{ fontSize: 15, fontWeight: 600, marginLeft: 2 }}>%</span>
            </>
          )}
        </div>
        <Button size="sm" variant="secondary" onClick={onRun} disabled={running}>
          {running ? 'Running…' : pct == null ? 'Run' : 'Re-run'}
        </Button>
      </div>
      <StatusLine status={coverage.status} error={coverage.error} ts={coverage.lastRunAt} verb="Ran" />
      {isReact ? <ReactDoctorRow react={react} onRun={onRunReact} /> : null}
    </GlowCard>
  );
}

// ReactDoctorRow — the react-doctor health score badge inside a Coverage tile.
function ReactDoctorRow({
  react,
  onRun,
}: {
  react: TestProject['react'];
  onRun: () => void;
}) {
  const running = react.status === 'running';
  const score = react.score;
  const color =
    score == null
      ? 'var(--text-faint)'
      : score >= 75
        ? 'var(--status-success)'
        : score >= 50
          ? 'var(--status-warn)'
          : 'var(--status-error)';
  return (
    <div
      className="flex items-center justify-between gap-2 pt-2.5"
      style={{ borderTop: '1px solid var(--lg-rim)' }}
    >
      <div className="flex items-baseline gap-2 min-w-0">
        <span className="text-[11px] font-semibold uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>
          react-doctor
        </span>
        {react.status === 'ok' && score != null ? (
          <span className="ds-tabular flex items-baseline gap-1" style={{ color }}>
            <span style={{ fontSize: 17, fontWeight: 700, lineHeight: 1 }}>{score}</span>
            {react.label ? (
              <span style={{ fontSize: 11, fontWeight: 600 }}>{react.label}</span>
            ) : null}
          </span>
        ) : running ? (
          <span className="text-xs" style={{ color: 'var(--accent)' }}>
            Scanning…
          </span>
        ) : react.status === 'failed' ? (
          <span className="truncate text-xs" style={{ color: 'var(--status-error)' }} title={react.error}>
            Failed
          </span>
        ) : (
          <span className="text-xs" style={{ color: 'var(--text-faint)' }}>
            Not run
          </span>
        )}
      </div>
      <Button size="sm" variant="ghost" onClick={onRun} disabled={running}>
        {running ? 'Scanning…' : score == null ? 'Run' : 'Re-run'}
      </Button>
    </div>
  );
}

function VulnTile({ p, onScan }: { p: TestProject; onScan: () => void }) {
  const { vuln } = p;
  const running = vuln.status === 'running';
  const scanned = vuln.status === 'ok';
  const color = !scanned
    ? 'var(--text-faint)'
    : vuln.total === 0
      ? 'var(--status-success)'
      : vuln.critical > 0 || vuln.high > 0
        ? 'var(--status-error)'
        : 'var(--status-warn)';
  return (
    <GlowCard interactive className="flex flex-col gap-3 p-4">
      <TileHeader name={p.name} sub={`${p.kind}`} kind={p.kind} />
      <div className="flex items-end justify-between">
        <div className="ds-tabular flex items-baseline gap-1.5" style={{ color }}>
          <span style={{ fontSize: 30, fontWeight: 700, lineHeight: 1 }}>{scanned ? vuln.total : '—'}</span>
          <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-muted)' }}>
            {scanned ? (vuln.total === 1 ? 'vuln' : 'vulns') : ''}
          </span>
        </div>
        <Button size="sm" variant="secondary" onClick={onScan} disabled={running}>
          {running ? 'Scanning…' : scanned ? 'Re-scan' : 'Scan'}
        </Button>
      </div>
      {scanned && vuln.total > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          <SevChip label="Critical" n={vuln.critical} c="var(--status-error)" />
          <SevChip label="High" n={vuln.high} c="var(--status-error)" />
          <SevChip label="Medium" n={vuln.medium} c="var(--status-warn)" />
          <SevChip label="Low" n={vuln.low} c="var(--text-muted)" />
        </div>
      ) : scanned ? (
        <div className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--status-success)' }}>
          <ShieldCheck size={13} /> No known vulnerabilities
        </div>
      ) : null}
      <StatusLine status={vuln.status} error={vuln.error} ts={vuln.lastScanAt} verb="Scanned" />
    </GlowCard>
  );
}

// ── shared bits ────────────────────────────────────────────────────────────

function TileHeader({ name, sub, kind }: { name: string; sub: string; kind: string }) {
  return (
    <div className="flex items-start justify-between gap-2">
      <div className="min-w-0">
        <div className="truncate font-semibold" style={{ color: 'var(--text-primary)' }}>
          {name}
        </div>
        <div className="text-xs capitalize" style={{ color: 'var(--text-muted)' }}>
          {sub}
        </div>
      </div>
      <span
        className="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
        style={{
          color: 'var(--text-muted)',
          background: 'rgb(var(--surface-tint-rgb) / 0.06)',
          border: '1px solid var(--lg-rim)',
        }}
      >
        {kind}
      </span>
    </div>
  );
}

function SevChip({ label, n, c }: { label: string; n: number; c: string }) {
  const zero = n === 0;
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-semibold"
      style={{
        color: zero ? 'var(--text-faint)' : c,
        background: zero ? 'transparent' : 'rgb(var(--surface-tint-rgb) / 0.06)',
        border: '1px solid var(--lg-rim)',
        opacity: zero ? 0.6 : 1,
      }}
      title={`${n} ${label.toLowerCase()}`}
    >
      <span className="ds-tabular">{n}</span> {label}
    </span>
  );
}

function StatusLine({
  status,
  error,
  ts,
  verb,
}: {
  status: QualityStatus;
  error?: string;
  ts?: string;
  verb: string;
}) {
  if (status === 'running') {
    return (
      <div className="text-xs" style={{ color: 'var(--accent)' }}>
        {verb === 'Ran' ? 'Running tests…' : 'Scanning…'}
      </div>
    );
  }
  if (status === 'failed') {
    return (
      <div className="truncate text-xs" style={{ color: 'var(--status-error)' }} title={error}>
        Failed{error ? `: ${firstLine(error)}` : ''}
      </div>
    );
  }
  if (status === 'unsupported') {
    return (
      <div className="text-xs" style={{ color: 'var(--text-faint)' }} title={error}>
        Unsupported project
      </div>
    );
  }
  if (status === 'idle' || !ts) {
    return (
      <div className="text-xs" style={{ color: 'var(--text-faint)' }}>
        Never run
      </div>
    );
  }
  return (
    <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
      {verb} {relativeTime(ts)}
    </div>
  );
}

function langLabel(language: string, kind: string): string {
  switch (language) {
    case 'go':
      return 'Go';
    case 'node':
      return 'Node / TypeScript';
    case 'python':
      return 'Python';
    default:
      return kind;
  }
}

function firstLine(s: string): string {
  const i = s.indexOf('\n');
  return i >= 0 ? s.slice(0, i) : s;
}
