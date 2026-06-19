import React, { useEffect, useState } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Server, ChevronRight, ExternalLink, RefreshCw } from 'lucide-react';

import { Metric } from '../components/motion/Metric';
import { StatusDot } from '../components/motion/StatusDot';
import { GlowCard } from '../components/ui/GlowCard';
import { EmptyState } from '../components/ui/EmptyState';
import { relativeTime } from '../lib/relativeTime';
import { useDevInstances } from '../hooks/useOfficeData';
import type { DevInstance } from '../api/types';

const statusBadgeBaseStyle: React.CSSProperties = {
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: 0.4,
  padding: '2px 8px',
  borderRadius: 'var(--radius-sm)',
  fontSize: 11,
};

type DotStatus = 'working' | 'stuck' | 'idle';

function dotStatusOf(d: DevInstance): DotStatus {
  if ((d.healthFailures ?? 0) > 0) return 'stuck';
  return d.url ? 'working' : 'idle';
}

// DevInstanceRow renders one dev server as a clickable row. Clicking it
// expands an inline live-preview pane: the raw announced URL
// (http://jarvis:<port>) is docker-internal and unreachable from the
// operator's browser, so the iframe + "open" link point at the reverse-proxy
// path (/api/office/dev-proxy/<slot>/) that jarvis forwards through.
function DevInstanceRow({ d }: { d: DevInstance }): React.ReactElement {
  const [open, setOpen] = useState(false);
  // Bumping nonce remounts the iframe (cache-busted src) so the operator can
  // pull the team's latest edits -- the dev server is static + no-store, so a
  // reload always shows current disk state.
  const [nonce, setNonce] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const dotStatus = dotStatusOf(d);
  const proxyPath = `/api/office/dev-proxy/${d.slot}/`;
  const iframeSrc = `${proxyPath}?_=${nonce}`;
  const hasUrl = !!d.url;
  const label = dotStatus === 'stuck' ? 'Unhealthy' : dotStatus === 'working' ? 'Live' : 'Idle';

  // Auto-refresh the preview every 15s while the pane is open + the toggle is
  // on, so a watching operator sees improvements land without clicking.
  useEffect(() => {
    if (!open || !autoRefresh) return;
    const id = window.setInterval(() => setNonce((n) => n + 1), 15_000);
    return () => window.clearInterval(id);
  }, [open, autoRefresh]);

  return (
    <GlowCard interactive live={dotStatus === 'working'} className="p-0">
      {/* Clickable row header. Toggles the live-preview pane. */}
      <button
        type="button"
        onClick={() => hasUrl && setOpen((v) => !v)}
        aria-expanded={open}
        disabled={!hasUrl}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          width: '100%',
          padding: '12px 14px',
          background: 'transparent',
          border: 'none',
          cursor: hasUrl ? 'pointer' : 'default',
          textAlign: 'left',
          color: 'inherit',
        }}
      >
        <ChevronRight
          size={15}
          style={{
            color: 'var(--text-muted)',
            transition: 'transform 120ms ease',
            transform: open ? 'rotate(90deg)' : 'none',
            opacity: hasUrl ? 1 : 0.3,
            flexShrink: 0,
          }}
        />
        <StatusDot status={dotStatus} size={8} />
        <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', flexShrink: 0 }}>
          {d.slot}
        </span>
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
            flexShrink: 0,
          }}
        >
          {label}
        </span>
        <span
          style={{
            flex: 1,
            minWidth: 0,
            fontSize: 11,
            fontFamily: 'var(--font-mono)',
            color: 'var(--text-muted)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {d.url || '(no URL recorded)'}
        </span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0 }}>
          {typeof d.pid === 'number' ? (
            <span style={{ fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
              pid {d.pid}
            </span>
          ) : null}
          {d.lastCheckAt ? (
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
              {relativeTime(d.lastCheckAt)}
            </span>
          ) : null}
          {hasUrl ? (
            <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--accent)' }}>
              {open ? 'Hide preview' : 'Preview'}
            </span>
          ) : null}
        </span>
      </button>

      {/* Expanded live-preview pane. */}
      {open && hasUrl ? (
        <div
          style={{
            borderTop: '1px solid var(--border-subtle)',
            padding: 12,
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
            <button
              type="button"
              onClick={() => setNonce((n) => n + 1)}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--accent)',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 'var(--radius-sm)',
                padding: '3px 10px',
                cursor: 'pointer',
              }}
            >
              <RefreshCw size={13} /> Refresh
            </button>
            <label
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                fontSize: 12,
                color: 'var(--text-secondary)',
                cursor: 'pointer',
              }}
            >
              <input
                type="checkbox"
                checked={autoRefresh}
                onChange={(e) => setAutoRefresh(e.target.checked)}
              />
              Auto-refresh (15s)
            </label>
            <a
              href={iframeSrc}
              target="_blank"
              rel="noreferrer"
              onClick={(e) => e.stopPropagation()}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                fontSize: 12,
                fontWeight: 600,
                color: 'var(--accent)',
                textDecoration: 'none',
              }}
            >
              Open in new tab <ExternalLink size={13} />
            </a>
            {typeof d.healthFailures === 'number' && d.healthFailures > 0 ? (
              <span style={{ fontSize: 12, color: 'var(--status-error)' }}>
                {d.healthFailures} health failure{d.healthFailures === 1 ? '' : 's'}
              </span>
            ) : null}
            {d.credsSet ? (
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>credentials set</span>
            ) : null}
          </div>
          <iframe
            key={nonce}
            src={iframeSrc}
            title={`live preview - ${d.slot}`}
            style={{
              width: '100%',
              height: 460,
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-sm)',
              background: 'var(--bg-app)',
            }}
          />
        </div>
      ) : null}
    </GlowCard>
  );
}

// DevInstancesScreen — the team's dev servers as a clickable list. Each row
// expands to an embedded live preview (proxied through jarvis, so the operator
// can actually view the running project). Read-only; start/stop is agent-side.
export function DevInstancesScreen() {
  const di = useDevInstances();
  const list = di.data?.instances ?? [];
  // AutoAnimate owns enter/exit/reorder of the live list as instances spin
  // up or down over SSE (respects reduced motion).
  const [listRef] = useAutoAnimate<HTMLDivElement>();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <header>
        <h1 className="page-header">Dev instances</h1>
        <p className="page-subtitle">
          Sibling dev servers the team is running. Click a row to view its live preview.
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
        <div ref={listRef} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {list.map((d) => (
            <DevInstanceRow key={d.slot} d={d} />
          ))}
        </div>
      )}
    </section>
  );
}
