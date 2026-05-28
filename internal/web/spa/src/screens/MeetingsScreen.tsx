import { useMemo, useState } from 'react';

import { Metric, RevealItem } from '../components/motion';
import { relativeTime } from '../lib/relativeTime';
import { useStandupDetail, useStandupHistory } from '../hooks/useOfficeData';

// MeetingsScreen — past standups. Master-detail split: list of
// completed standups on the left with search; transcript on the
// right with day/time headers grouped by 5-minute gaps.
export function MeetingsScreen() {
  const history = useStandupHistory();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const list = history.data?.history ?? [];
  // Default-select most recent
  const effectiveId = selectedId || list[0]?.id || null;
  const detail = useStandupDetail(effectiveId);
  const transcript = detail.data?.standup?.transcript ?? [];

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (s) =>
        s.id.toLowerCase().includes(q) ||
        (s.participants ?? []).join(' ').toLowerCase().includes(q),
    );
  }, [list, query]);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Meetings
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Past standup transcripts. Day/time headers appear on 5-minute gaps.
          </p>
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
          <Metric value={list.length} size={12} /> standups
        </span>
      </header>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(240px, 320px) 1fr',
          gap: 18,
          flex: 1,
          minHeight: 0,
        }}
      >
        <aside
          style={{
            padding: 10,
            background: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
            minHeight: 0,
            overflow: 'hidden',
          }}
        >
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search standups…"
            style={{
              padding: 8,
              background: 'var(--bg-app)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-primary)',
              fontSize: 12,
              outline: 'none',
            }}
          />
          <div style={{ overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 4 }}>
            {filtered.length === 0 ? (
              <div style={{ padding: 16, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>
                No standups match.
              </div>
            ) : (
              filtered.map((s) => (
                <button
                  key={s.id}
                  onClick={() => setSelectedId(s.id)}
                  style={{
                    textAlign: 'left',
                    padding: 10,
                    background: s.id === effectiveId ? 'var(--accent-muted)' : 'transparent',
                    border: '1px solid ' + (s.id === effectiveId ? 'var(--accent)' : 'transparent'),
                    borderRadius: 'var(--radius-md)',
                    cursor: 'pointer',
                    color: 'inherit',
                    font: 'inherit',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 2,
                  }}
                >
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)' }}>
                    {new Date(s.startedAt).toLocaleString(undefined, {
                      month: 'short',
                      day: 'numeric',
                      hour: 'numeric',
                      minute: '2-digit',
                    })}
                  </span>
                  <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                    {(s.participants?.length ?? 0)} participant{s.participants?.length === 1 ? '' : 's'} ·{' '}
                    {(s.messageCount ?? 0)} turns ·{' '}
                    {relativeTime(s.startedAt)}
                  </span>
                </button>
              ))
            )}
          </div>
        </aside>

        <main
          style={{
            padding: 18,
            background: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-card)',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
            minHeight: 0,
            overflow: 'hidden',
          }}
        >
          {!effectiveId ? (
            <div
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--text-muted)',
                fontSize: 13,
              }}
            >
              Select a standup to read its transcript.
            </div>
          ) : detail.isLoading ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading…</div>
          ) : transcript.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Empty transcript.</div>
          ) : (
            <div
              style={{
                flex: 1,
                overflowY: 'auto',
                display: 'flex',
                flexDirection: 'column',
                gap: 10,
                paddingRight: 4,
              }}
            >
              {renderTranscript(transcript).map((node, i) =>
                node.kind === 'header' ? (
                  <div
                    key={`hdr-${i}`}
                    style={{
                      padding: '8px 0 4px',
                      borderBottom: '1px solid var(--border-subtle)',
                      fontSize: 11,
                      color: 'var(--text-muted)',
                      letterSpacing: 0.4,
                      textTransform: 'uppercase',
                      fontWeight: 700,
                    }}
                  >
                    {node.label}
                  </div>
                ) : (
                  <RevealItem key={`turn-${i}`} index={Math.min(i, 12)}>
                    <div
                      style={{
                        padding: 12,
                        background: 'var(--bg-elevated)',
                        border: '1px solid var(--border-subtle)',
                        borderRadius: 'var(--radius-md)',
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)' }}>
                        <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>
                          {node.entry.speakerName ?? node.entry.sessionId}
                        </span>
                        <span>{new Date(node.entry.timestamp).toLocaleTimeString()}</span>
                      </div>
                      <div style={{ marginTop: 6, fontSize: 13, color: 'var(--text-secondary)', whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
                        {node.entry.content}
                      </div>
                    </div>
                  </RevealItem>
                ),
              )}
            </div>
          )}
        </main>
      </div>
    </section>
  );
}

// Render the transcript with day/time grouping headers. Mirrors the
// vanilla office.js convention: insert a header when the gap to the
// previous entry exceeds 5 minutes.
type RenderNode =
  | { kind: 'header'; label: string }
  | {
      kind: 'turn';
      entry: { sessionId: string; speakerName?: string; content: string; timestamp: string };
    };

function renderTranscript(
  entries: { sessionId: string; speakerName?: string; content: string; timestamp: string }[],
): RenderNode[] {
  const out: RenderNode[] = [];
  let prev: number | null = null;
  for (const e of entries) {
    const t = new Date(e.timestamp).getTime();
    if (prev === null || t - prev > 5 * 60 * 1000) {
      const d = new Date(e.timestamp);
      const label = d.toLocaleString(undefined, {
        weekday: 'short',
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
      });
      out.push({ kind: 'header', label });
    }
    out.push({ kind: 'turn', entry: e });
    prev = t;
  }
  return out;
}
