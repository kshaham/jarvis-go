import { useMemo, useState } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { CalendarClock, SearchX } from 'lucide-react';

import { Metric, RevealItem } from '../components/motion';
import { EmptyState, GlowCard, Input, Skeleton } from '../components/ui';
import { relativeTime } from '../lib/relativeTime';
import { useStandupDetail, useStandupHistory } from '../hooks/useOfficeData';

// MeetingsScreen — past standups. Master-detail split: list of
// completed standups on the left with search; transcript on the
// right with day/time headers grouped by 5-minute gaps.
export function MeetingsScreen() {
  const history = useStandupHistory();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  const [listRef] = useAutoAnimate<HTMLDivElement>();

  const list = useMemo(() => history.data?.history ?? [], [history.data?.history]);
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
          <Metric value={list.length} size={12} pulseOnChange /> standups
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
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search standups…"
          />
          <div
            ref={listRef}
            style={{ overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 6 }}
          >
            {history.isLoading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <Skeleton key={i} style={{ height: 52, borderRadius: 'var(--radius-md)' }} />
              ))
            ) : filtered.length === 0 ? (
              <EmptyState
                icon={<SearchX size={22} />}
                title={query.trim() ? 'No matches' : 'No standups yet'}
                description={
                  query.trim()
                    ? 'No standups match your search.'
                    : 'Completed standups appear here as the team meets.'
                }
              />
            ) : (
              filtered.map((s) => {
                const active = s.id === effectiveId;
                return (
                  <GlowCard
                    key={s.id}
                    interactive
                    live={active}
                    onClick={() => setSelectedId(s.id)}
                    className="cursor-pointer p-2.5"
                    style={{
                      borderColor: active ? 'var(--accent)' : undefined,
                      background: active ? 'var(--accent-muted)' : undefined,
                    }}
                  >
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
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
                    </div>
                  </GlowCard>
                );
              })
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
            <EmptyState
              className="flex-1"
              icon={<CalendarClock size={22} />}
              title="No standup selected"
              description="Select a standup from the list to read its transcript."
            />
          ) : detail.isLoading ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} style={{ height: 72, borderRadius: 'var(--radius-md)' }} />
              ))}
            </div>
          ) : transcript.length === 0 ? (
            <EmptyState
              className="flex-1"
              icon={<CalendarClock size={22} />}
              title="Empty transcript"
              description="This standup has no recorded turns."
            />
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
                    key={`hdr:${node.label}`}
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
                  <RevealItem key={`turn:${node.entry.sessionId}:${node.entry.timestamp}`} index={Math.min(i, 12)}>
                    <GlowCard interactive className="p-3">
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-muted)' }}>
                        <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>
                          {node.entry.speakerName ?? node.entry.sessionId}
                        </span>
                        <span>{new Date(node.entry.timestamp).toLocaleTimeString()}</span>
                      </div>
                      <div style={{ marginTop: 6, fontSize: 13, color: 'var(--text-secondary)', whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
                        {node.entry.content}
                      </div>
                    </GlowCard>
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
