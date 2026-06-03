import type { CSSProperties, ReactElement } from 'react';
import { SectionCard } from './formPrimitives';
import { estimateDailyCost, formatPerDay } from './costEstimate.utils';
import type { MemberEdit } from './memberEditor.utils';

// Illustrative sparkline — a fixed gentle upward trend, scaled once at module
// load. It does NOT plot real data; it's a visual cue that cost trends with
// activity. Deterministic (no random) so the tile is stable across renders.
const SPARK_W = 132;
const SPARK_H = 36;
const SPARK_PAD = 3;
const SPARK_SERIES = [0.32, 0.48, 0.4, 0.6, 0.52, 0.75, 0.66, 0.9];
const sparkPoints = SPARK_SERIES.map((v, i) => {
  const x = SPARK_PAD + (i * (SPARK_W - SPARK_PAD * 2)) / (SPARK_SERIES.length - 1);
  const y = SPARK_H - SPARK_PAD - v * (SPARK_H - SPARK_PAD * 2);
  return `${x.toFixed(1)},${y.toFixed(1)}`;
}).join(' ');
const sparkAreaPoints = `${SPARK_PAD},${SPARK_H - SPARK_PAD} ${sparkPoints} ${SPARK_W - SPARK_PAD},${SPARK_H - SPARK_PAD}`;

const rowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: 12,
  flexWrap: 'wrap',
};

const valueStyle: CSSProperties = {
  fontSize: 24,
  fontWeight: 700,
  color: 'var(--accent)',
  lineHeight: 1.1,
  fontVariantNumeric: 'tabular-nums',
};

const subStyle: CSSProperties = {
  marginTop: 2,
  fontSize: 12,
  color: 'var(--text-muted)',
};

export function CostEstimate({
  members,
  defaultProvider,
  defaultModel,
  perpetual,
}: {
  members: MemberEdit[];
  defaultProvider: string;
  defaultModel: string;
  perpetual: boolean;
}): ReactElement {
  const est = estimateDailyCost(members, defaultProvider, defaultModel, perpetual);

  return (
    <SectionCard title="Cost estimate" hint="Rough heuristic — not billed actuals.">
      {est.allSubscription ? (
        <div>
          <div style={{ ...valueStyle, fontSize: 18 }}>Included in plan</div>
          <p style={subStyle}>
            {est.memberCount} agent{est.memberCount === 1 ? '' : 's'} on a flat-rate subscription —
            no per-token cost.
          </p>
        </div>
      ) : (
        <div style={rowStyle}>
          <div style={{ minWidth: 0 }}>
            <div style={valueStyle}>{formatPerDay(est.perDay)}</div>
            <p style={subStyle}>
              {est.meteredCount} metered · {est.memberCount} agent
              {est.memberCount === 1 ? '' : 's'}
              {perpetual ? ' · perpetual ×1.5' : ''}
            </p>
          </div>
          <svg
            width={SPARK_W}
            height={SPARK_H}
            viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
            aria-hidden
            style={{ flexShrink: 0 }}
          >
            <polygon points={sparkAreaPoints} fill="var(--accent-muted)" />
            <polyline
              points={sparkPoints}
              fill="none"
              stroke="var(--accent)"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
      )}
    </SectionCard>
  );
}
