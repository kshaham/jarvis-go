import { useEffect, useMemo, useRef, useState } from 'react';

import { Metric, Press, StatusDot } from '../components/motion';
import { Modal } from '../components/motion/Modal';
import { useOfficeMessages } from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { AgentStatus, OfficeMember } from '../api/types';

// NetworkScreen — agents as nodes, messages as edges. Force-directed
// layout port of office.js's velocity-Verlet sim (simplified). Color
// reflects status; edge thickness reflects message volume between a
// pair; edge opacity reflects recency. Click a node to open a
// detail panel.
type Node = {
  id: string;
  member: OfficeMember;
  x: number;
  y: number;
  vx: number;
  vy: number;
  fx?: number; // pin (drag)
  fy?: number;
};

type Edge = {
  source: string;
  target: string;
  count: number;
  lastTs: number;
};

const W = 900;
const H = 560;
const TICK_LIMIT = 200;

export function NetworkScreen() {
  const status = useOfficeStatus();
  const messages = useOfficeMessages();
  const members = status.data?.members ?? [];

  const [hideIdle, setHideIdle] = useState(false);
  const [activeOnly, setActiveOnly] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selectedMember = members.find((m) => m.sessionId === selectedId) ?? null;

  // Aggregate messages → edges (undirected, dedup by pair).
  const edges = useMemo<Edge[]>(() => {
    const map = new Map<string, Edge>();
    for (const m of messages.data?.messages ?? []) {
      const a = m.fromSessionId;
      const b = m.toSessionId;
      if (!a || !b || a === b) continue;
      const key = a < b ? `${a}|${b}` : `${b}|${a}`;
      const ts = new Date(m.timestamp).getTime();
      const existing = map.get(key);
      if (existing) {
        existing.count += 1;
        existing.lastTs = Math.max(existing.lastTs, ts);
      } else {
        map.set(key, { source: a, target: b, count: 1, lastTs: ts });
      }
    }
    return Array.from(map.values());
  }, [messages.data?.messages]);

  // Filter members + edges.
  const visibleMembers = useMemo(
    () => (hideIdle ? members.filter((m) => m.status !== 'idle') : members),
    [members, hideIdle],
  );
  const visibleSet = useMemo(() => new Set(visibleMembers.map((m) => m.sessionId)), [visibleMembers]);
  const visibleEdges = useMemo(() => {
    let list = edges.filter((e) => visibleSet.has(e.source) && visibleSet.has(e.target));
    if (activeOnly) {
      // "Active" = touched in the last hour.
      const cutoff = Date.now() - 60 * 60_000;
      list = list.filter((e) => e.lastTs >= cutoff);
    }
    return list;
  }, [edges, visibleSet, activeOnly]);

  // Force-directed layout state (refs because we don't want to re-render every tick).
  const nodesRef = useRef<Map<string, Node>>(new Map());
  const tickRef = useRef<number>(0);
  // bumping `tickFrame` once per RAF triggers an SVG re-render.
  const [, setTickFrame] = useState(0);

  // Sync nodes set with visibleMembers — preserve positions for nodes
  // that survived a re-filter; assign random positions to newcomers.
  useEffect(() => {
    const m = nodesRef.current;
    const ids = new Set(visibleMembers.map((mem) => mem.sessionId));
    for (const k of m.keys()) {
      if (!ids.has(k)) m.delete(k);
    }
    for (const mem of visibleMembers) {
      if (!m.has(mem.sessionId)) {
        m.set(mem.sessionId, {
          id: mem.sessionId,
          member: mem,
          x: W / 2 + (Math.random() - 0.5) * 100,
          y: H / 2 + (Math.random() - 0.5) * 100,
          vx: 0,
          vy: 0,
        });
      } else {
        // Keep position, refresh the member ref so status / cost render fresh.
        const n = m.get(mem.sessionId)!;
        n.member = mem;
      }
    }
    tickRef.current = 0; // restart the cooldown
  }, [visibleMembers]);

  // rAF simulation loop. Cools down after TICK_LIMIT ticks; resumes
  // when membership changes (above). Pure simulation — no React state
  // touches happen here except setTickFrame to schedule re-render.
  useEffect(() => {
    let raf = 0;
    const step = () => {
      const t = tickRef.current;
      if (t < TICK_LIMIT) {
        simulate(nodesRef.current, visibleEdges);
        tickRef.current += 1;
        setTickFrame((v) => (v + 1) & 0xffff);
      }
      raf = requestAnimationFrame(step);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [visibleEdges]);

  const nodes = Array.from(nodesRef.current.values());
  const maxCount = visibleEdges.reduce((m, e) => Math.max(m, e.count), 1);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Network
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Agents as nodes, messages as edges. Edge thickness = volume; opacity = recency.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <ToggleChip label="Hide idle" active={hideIdle} onClick={() => setHideIdle((v) => !v)} />
          <ToggleChip label="Active edges only" active={activeOnly} onClick={() => setActiveOnly((v) => !v)} />
          <Press
            pressScale={0.96}
            onClick={() => {
              tickRef.current = 0;
              // Jitter positions so the layout re-runs.
              for (const n of nodesRef.current.values()) {
                n.x = W / 2 + (Math.random() - 0.5) * 80;
                n.y = H / 2 + (Math.random() - 0.5) * 80;
                n.vx = 0;
                n.vy = 0;
              }
            }}
          >
            <span
              style={{
                padding: '6px 12px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-sm)',
                color: 'var(--text-primary)',
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              Relayout
            </span>
          </Press>
        </div>
      </header>

      <div
        style={{
          display: 'flex',
          gap: 18,
          fontSize: 11,
          color: 'var(--text-muted)',
          padding: '4px 0',
        }}
      >
        <Legend status="working" label="Working" />
        <Legend status="idle" label="Idle" />
        <Legend status="meeting" label="Meeting" />
        <Legend status="stuck" label="Stuck" />
        <span style={{ marginLeft: 'auto' }}>
          <Metric value={nodes.length} size={11} /> nodes ·{' '}
          <Metric value={visibleEdges.length} size={11} /> edges
        </span>
      </div>

      <div
        style={{
          flex: 1,
          minHeight: 0,
          padding: 14,
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          borderRadius: 'var(--radius-card)',
          overflow: 'hidden',
        }}
      >
        <svg viewBox={`0 0 ${W} ${H}`} width="100%" height="100%" style={{ display: 'block' }}>
          {/* Edges */}
          <g>
            {visibleEdges.map((e, i) => {
              const a = nodesRef.current.get(e.source);
              const b = nodesRef.current.get(e.target);
              if (!a || !b) return null;
              const ageHrs = (Date.now() - e.lastTs) / 3_600_000;
              const opacity = Math.max(0.12, Math.min(0.75, 1 - ageHrs / 24));
              const stroke = Math.max(1, (e.count / maxCount) * 5);
              return (
                <line
                  key={i}
                  x1={a.x}
                  y1={a.y}
                  x2={b.x}
                  y2={b.y}
                  stroke="var(--accent)"
                  strokeOpacity={opacity}
                  strokeWidth={stroke}
                />
              );
            })}
          </g>
          {/* Nodes */}
          <g>
            {nodes.map((n) => {
              const color = colorForStatus(n.member.status);
              return (
                <g
                  key={n.id}
                  transform={`translate(${n.x},${n.y})`}
                  style={{ cursor: 'pointer' }}
                  onClick={() => setSelectedId(n.id)}
                >
                  <circle
                    r={16}
                    fill="var(--bg-elevated)"
                    stroke={color}
                    strokeWidth={2}
                  />
                  <text
                    textAnchor="middle"
                    dominantBaseline="middle"
                    fontSize={11}
                    fontWeight={700}
                    fill="var(--text-primary)"
                  >
                    {(n.member.avatarEmoji ||
                      n.member.avatarInitial ||
                      n.member.name?.[0] ||
                      '?').toString().slice(0, 1)}
                  </text>
                  <text
                    y={28}
                    textAnchor="middle"
                    fontSize={10}
                    fill="var(--text-muted)"
                  >
                    {n.member.name}
                  </text>
                </g>
              );
            })}
          </g>
        </svg>
      </div>

      <Modal
        open={!!selectedMember}
        onOpenChange={(next) => (next ? null : setSelectedId(null))}
        size="md"
        title={selectedMember?.name ?? ''}
        description={selectedMember?.role}
      >
        {selectedMember ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{ display: 'flex', gap: 18 }}>
              <Stat label="Status" value={selectedMember.status} />
              <Stat label="Cost" value={`$${(selectedMember.totalCost ?? 0).toFixed(2)}`} />
              {selectedMember.score?.grade ? <Stat label="Grade" value={selectedMember.score.grade} /> : null}
            </div>
            {selectedMember.workingDir ? (
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                <span style={{ display: 'block', fontSize: 10, textTransform: 'uppercase', letterSpacing: 0.4 }}>
                  Working dir
                </span>
                <code style={{ fontFamily: 'var(--font-mono)' }}>{selectedMember.workingDir}</code>
              </div>
            ) : null}
            <PeerEdges sessionId={selectedMember.sessionId} edges={edges} members={members} />
          </div>
        ) : null}
      </Modal>
    </section>
  );
}

// PeerEdges — shows which agents this one has exchanged messages with.
function PeerEdges({
  sessionId,
  edges,
  members,
}: {
  sessionId: string;
  edges: Edge[];
  members: OfficeMember[];
}) {
  const lookup = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of members) m.set(mem.sessionId, mem.name);
    return m;
  }, [members]);
  const peers = edges
    .filter((e) => e.source === sessionId || e.target === sessionId)
    .map((e) => {
      const otherId = e.source === sessionId ? e.target : e.source;
      return { name: lookup.get(otherId) ?? otherId.slice(0, 6), count: e.count, lastTs: e.lastTs };
    })
    .sort((a, b) => b.count - a.count);
  if (peers.length === 0) return null;
  return (
    <div>
      <h3 style={{ margin: '8px 0', fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        Talks to
      </h3>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
        {peers.slice(0, 6).map((p) => (
          <li
            key={p.name}
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              padding: '6px 10px',
              background: 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 'var(--radius-sm)',
              fontSize: 12,
            }}
          >
            <span style={{ color: 'var(--text-primary)' }}>{p.name}</span>
            <Metric value={p.count} size={12} tone="muted" />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span style={{ fontSize: 10, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span style={{ fontSize: 14, color: 'var(--text-primary)', fontWeight: 600 }}>{value}</span>
    </div>
  );
}

function Legend({ status, label }: { status: AgentStatus; label: string }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      <StatusDot status={status} size={8} />
      {label}
    </span>
  );
}

function ToggleChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <Press pressScale={0.94} onClick={onClick}>
      <span
        style={{
          padding: '6px 12px',
          background: active ? 'var(--accent-muted)' : 'var(--bg-surface)',
          border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
          borderRadius: 999,
          color: active ? 'var(--accent)' : 'var(--text-muted)',
          fontSize: 11,
          fontWeight: 600,
        }}
      >
        {label}
      </span>
    </Press>
  );
}

function colorForStatus(s: AgentStatus): string {
  switch (s) {
    case 'working':
      return 'var(--status-working)';
    case 'meeting':
      return 'var(--status-meeting)';
    case 'stuck':
      return 'var(--status-stuck)';
    default:
      return 'var(--status-idle)';
  }
}

// simulate — one tick of a velocity-Verlet-ish force-directed layout.
// Centre gravity + repulsion + spring on each edge. Cheap (O(N^2))
// but fine for the <50 nodes the office shell ever has.
function simulate(nodesMap: Map<string, Node>, edges: Edge[]) {
  const nodes = Array.from(nodesMap.values());
  const k = 0.04; // spring stiffness
  const repulse = 4500;
  const damping = 0.7;
  const centre = { x: W / 2, y: H / 2 };

  // Centre gravity
  for (const n of nodes) {
    n.vx += (centre.x - n.x) * 0.003;
    n.vy += (centre.y - n.y) * 0.003;
  }
  // Repulsion (every pair)
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i];
      const b = nodes[j];
      const dx = a.x - b.x;
      const dy = a.y - b.y;
      const d2 = Math.max(40, dx * dx + dy * dy);
      const f = repulse / d2;
      const dist = Math.sqrt(d2);
      const nx = dx / dist;
      const ny = dy / dist;
      a.vx += nx * f * 0.02;
      a.vy += ny * f * 0.02;
      b.vx -= nx * f * 0.02;
      b.vy -= ny * f * 0.02;
    }
  }
  // Springs on edges
  for (const e of edges) {
    const a = nodesMap.get(e.source);
    const b = nodesMap.get(e.target);
    if (!a || !b) continue;
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const dist = Math.max(0.1, Math.sqrt(dx * dx + dy * dy));
    const target = 120;
    const force = (dist - target) * k;
    const nx = dx / dist;
    const ny = dy / dist;
    a.vx += nx * force;
    a.vy += ny * force;
    b.vx -= nx * force;
    b.vy -= ny * force;
  }
  // Integrate
  for (const n of nodes) {
    n.vx *= damping;
    n.vy *= damping;
    n.x += n.vx;
    n.y += n.vy;
    // Clamp to viewport.
    n.x = Math.max(30, Math.min(W - 30, n.x));
    n.y = Math.max(30, Math.min(H - 40, n.y));
  }
}
