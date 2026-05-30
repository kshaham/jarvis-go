import '@xyflow/react/dist/style.css';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useReducedMotion } from 'motion/react';
import {
  Background,
  Controls,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from '@xyflow/react';

import { Metric } from '../components/motion/Metric';
import { Modal } from '../components/motion/Modal';
import { Press } from '../components/motion/Press';
import { StatusDot } from '../components/motion/StatusDot';
import { AgentFlowNode } from '../components/network/AgentFlowNode';
import { settleLayout } from '../lib/forceLayout';
import { useOfficeMessages } from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { AgentStatus, OfficeMember } from '../api/types';
import type { CSSProperties, ReactNode } from 'react';

// ─── module-scope style constants ─────────────────────────────────────────────
const relayoutButtonStyle: CSSProperties = {
  padding: '6px 14px',
  background: 'var(--lg-fill)',
  border: '1px solid var(--lg-rim)',
  borderRadius: 'var(--radius-pill)',
  color: 'var(--text-primary)',
  fontSize: 12,
  fontWeight: 600,
  WebkitBackdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
  boxShadow: 'var(--lg-shadow-rest)',
};

const toggleChipBaseStyle: CSSProperties = {
  padding: '6px 12px',
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 600,
  WebkitBackdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
};

// NetworkScreen — agents as nodes, messages as edges, rendered with
// React Flow. Layout is a one-shot d3-force settle (see settleLayout);
// edge thickness = message volume, opacity = recency. Click a node for
// detail. Re-layout only happens on topology change or a manual
// Relayout — never per SSE message (avoids a re-render storm).
const W = 900;
const H = 560;

// Module-scope so React Flow doesn't re-instantiate nodes each render.
const nodeTypes: NodeTypes = { agent: AgentFlowNode };

type MsgEdge = {
  source: string;
  target: string;
  count: number;
  lastTs: number;
};

export function NetworkScreen() {
  const status = useOfficeStatus();
  const messages = useOfficeMessages();
  const members = useMemo(() => status.data?.members ?? [], [status.data?.members]);

  const [hideIdle, setHideIdle] = useState(false);
  const [activeOnly, setActiveOnly] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [relayoutSeed, setRelayoutSeed] = useState(0);
  const selectedMember = members.find((m) => m.sessionId === selectedId) ?? null;

  // Aggregate messages → edges (undirected, dedup by pair).
  const edges = useMemo<MsgEdge[]>(() => {
    const map = new Map<string, MsgEdge>();
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

  const visibleMembers = useMemo(
    () => (hideIdle ? members.filter((m) => m.status !== 'idle') : members),
    [members, hideIdle],
  );
  const visibleSet = useMemo(
    () => new Set(visibleMembers.map((m) => m.sessionId)),
    [visibleMembers],
  );
  const visibleEdges = useMemo(() => {
    let list = edges.filter((e) => visibleSet.has(e.source) && visibleSet.has(e.target));
    if (activeOnly) {
      const cutoff = Date.now() - 60 * 60_000;
      list = list.filter((e) => e.lastTs >= cutoff);
    }
    return list;
  }, [edges, visibleSet, activeOnly]);

  const maxCount = visibleEdges.reduce((m, e) => Math.max(m, e.count), 1);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Network
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Agents as nodes, messages as edges. Edge thickness = volume; opacity = recency. Drag,
            zoom, and click a node for detail.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <ToggleChip label="Hide idle" active={hideIdle} onClick={() => setHideIdle((v) => !v)} />
          <ToggleChip
            label="Active edges only"
            active={activeOnly}
            onClick={() => setActiveOnly((v) => !v)}
          />
          <Press pressScale={0.96} onClick={() => setRelayoutSeed((s) => s + 1)}>
            <span style={relayoutButtonStyle}>
              Relayout
            </span>
          </Press>
        </div>
      </header>

      <div
        style={{
          display: 'flex',
          gap: 18,
          fontSize: 12,
          color: 'var(--text-muted)',
          padding: '4px 0',
        }}
      >
        <Legend status="working" label="Working" />
        <Legend status="idle" label="Idle" />
        <Legend status="meeting" label="Meeting" />
        <Legend status="stuck" label="Stuck" />
        <span style={{ marginLeft: 'auto' }}>
          <Metric value={visibleMembers.length} size={12} /> nodes ·{' '}
          <Metric value={visibleEdges.length} size={12} /> edges
        </span>
      </div>

      <div
        className="surface-card surface-card--solid"
        style={{ flex: 1, minHeight: 0, overflow: 'hidden', position: 'relative' }}
      >
        <ReactFlowProvider>
          <NetworkGraph
            members={visibleMembers}
            edges={visibleEdges}
            maxCount={maxCount}
            relayoutSeed={relayoutSeed}
            onSelect={setSelectedId}
          />
        </ReactFlowProvider>
      </div>

      <Modal
        open={!!selectedMember}
        onOpenChange={(next) => (next ? null : setSelectedId(null))}
        size="md"
        glow={selectedMember?.status === 'working'}
        title={selectedMember?.name ?? ''}
        description={selectedMember?.role}
      >
        {selectedMember ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{ display: 'flex', gap: 18 }}>
              <Stat label="Status" value={selectedMember.status} />
              <Stat label="Cost" value={`$${(selectedMember.totalCost ?? 0).toFixed(2)}`} />
              {selectedMember.score?.grade ? (
                <Stat label="Grade" value={selectedMember.score.grade} />
              ) : null}
            </div>
            {selectedMember.workingDir ? (
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                <span
                  style={{
                    display: 'block',
                    fontSize: 12,
                    textTransform: 'uppercase',
                    letterSpacing: 0.4,
                  }}
                >
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

// edgeFrom — map an aggregated message edge to a React Flow edge with
// the volume/recency styling. CSS var stroke resolves against :root.
function edgeFrom(e: MsgEdge, maxCount: number): Edge {
  const ageHrs = (Date.now() - e.lastTs) / 3_600_000;
  const opacity = Math.max(0.12, Math.min(0.75, 1 - ageHrs / 24));
  const width = Math.max(1, (e.count / maxCount) * 5);
  return {
    id: `${e.source}-${e.target}`,
    source: e.source,
    target: e.target,
    type: 'straight',
    style: { stroke: 'var(--accent)', strokeOpacity: opacity, strokeWidth: width },
  };
}

function NetworkGraph({
  members,
  edges,
  maxCount,
  relayoutSeed,
  onSelect,
}: {
  members: OfficeMember[];
  edges: MsgEdge[];
  maxCount: number;
  relayoutSeed: number;
  onSelect: (id: string) => void;
}) {
  const reduce = useReducedMotion();
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [rfEdges, setRfEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const { fitView } = useReactFlow();
  const topoRef = useRef('');

  useEffect(() => {
    const nodeIds = members.map((m) => m.sessionId);
    const memberById = new Map(members.map((m) => [m.sessionId, m] as const));
    const topoKey =
      nodeIds.toSorted().join(',') +
      '||' +
      edges.map((e) => `${e.source}-${e.target}`).toSorted().join(',') +
      '||' +
      relayoutSeed;

    if (topoKey !== topoRef.current) {
      // Topology changed (or manual relayout) — recompute positions.
      topoRef.current = topoKey;
      const pos = settleLayout(
        nodeIds,
        edges.map((e) => ({ source: e.source, target: e.target })),
        W,
        H,
      );
      setNodes(
        nodeIds.map((id) => ({
          id,
          type: 'agent',
          position: pos.get(id) ?? { x: W / 2, y: H / 2 },
          data: { member: memberById.get(id) as OfficeMember },
        })),
      );
      setRfEdges(edges.map((e) => edgeFrom(e, maxCount)));
      requestAnimationFrame(() => fitView({ padding: 0.2, duration: reduce ? 0 : 400 }));
    } else {
      // Same topology — refresh node data + edge styling in place,
      // keep positions (and any user drag) stable.
      setNodes((prev) =>
        prev.map((n) => {
          const m = memberById.get(n.id);
          return m ? { ...n, data: { member: m } } : n;
        }),
      );
      setRfEdges(edges.map((e) => edgeFrom(e, maxCount)));
    }
  }, [members, edges, maxCount, relayoutSeed, reduce, setNodes, setRfEdges, fitView]);

  return (
    <ReactFlow
      nodes={nodes}
      edges={rfEdges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      nodeTypes={nodeTypes}
      onNodeClick={(_, node) => onSelect(node.id)}
      fitView
      minZoom={0.2}
      maxZoom={2}
      proOptions={{ hideAttribution: true }}
      nodesConnectable={false}
      style={{ width: '100%', height: '100%' }}
    >
      <Background color="rgba(255,255,255,0.07)" gap={24} />
      <Controls showInteractive={false} />
    </ReactFlow>
  );
}

// PeerEdges — which agents this one has exchanged messages with.
function PeerEdges({
  sessionId,
  edges,
  members,
}: {
  sessionId: string;
  edges: MsgEdge[];
  members: OfficeMember[];
}) {
  const lookup = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of members) m.set(mem.sessionId, mem.name);
    return m;
  }, [members]);
  const peers = edges
    .flatMap((e) => {
      if (e.source !== sessionId && e.target !== sessionId) return [];
      const otherId = e.source === sessionId ? e.target : e.source;
      return [{ name: lookup.get(otherId) ?? otherId.slice(0, 6), count: e.count, lastTs: e.lastTs }];
    })
    .toSorted((a, b) => b.count - a.count);
  if (peers.length === 0) return null;
  return (
    <div>
      <h3
        style={{
          margin: '8px 0',
          fontSize: 12,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
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

function Stat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span style={{ fontSize: 12, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span style={{ fontSize: 14, color: 'var(--text-primary)', fontWeight: 600, textTransform: 'capitalize' }}>
        {value}
      </span>
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
          ...toggleChipBaseStyle,
          background: active ? 'var(--lg-fill-accent)' : 'var(--lg-fill)',
          border: '1px solid ' + (active ? 'rgb(var(--accent-rgb) / 0.6)' : 'var(--lg-rim)'),
          color: active ? 'var(--accent)' : 'var(--text-secondary)',
          boxShadow: active ? 'var(--lg-shadow-rest), var(--glow-violet-sm)' : 'var(--lg-shadow-rest)',
        }}
      >
        {label}
      </span>
    </Press>
  );
}
