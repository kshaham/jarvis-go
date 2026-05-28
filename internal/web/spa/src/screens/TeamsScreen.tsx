import { motion } from 'motion/react';
import { Link } from 'react-router-dom';
import { useState } from 'react';

import { Metric, Press, RevealItem, StatusDot } from '../components/motion';
import { Modal } from '../components/motion/Modal';
import { spring } from '../lib/motion';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import {
  useGenerateToken,
  useIdentity,
  usePeers,
  usePingPeer,
  useRemovePeer,
  useUpdateIdentity,
} from '../hooks/usePeers';
import type { Peer, PeerStatus } from '../api/types';

// TeamsScreen — the "all teams in one view" page. Top zone shows this
// jarvis instance's identity + stats; below is the grid of registered
// peer teams. Adding a peer drops the operator into /peers/new.
export function TeamsScreen() {
  const status = useOfficeStatus();
  const identity = useIdentity();
  const peers = usePeers();
  const peerList = peers.data?.peers ?? [];
  const memberCount = status.data?.teamSpawned ? status.data.members?.length || 0 : 0;
  const [editOpen, setEditOpen] = useState(false);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1
            style={{
              margin: 0,
              fontSize: 24,
              fontWeight: 600,
              color: 'var(--text-primary)',
            }}
          >
            Teams
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Your local team and every peer jarvis instance it can talk to.
          </p>
        </div>
        <Link to="/peers/new" style={{ textDecoration: 'none' }}>
          <Press pressScale={0.97}>
            <span
              style={{
                display: 'inline-block',
                padding: '10px 16px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-md)',
                fontSize: 14,
                fontWeight: 600,
              }}
            >
              + Add peer team
            </span>
          </Press>
        </Link>
      </header>

      {/* Local team hero */}
      <motion.section
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={spring.smooth}
        style={{
          padding: 20,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border-subtle)',
          boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.04)',
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <div
            style={{
              width: 56,
              height: 56,
              borderRadius: 14,
              background: 'linear-gradient(135deg, var(--accent) 0%, var(--accent-strong) 100%)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#fff',
              fontSize: 22,
              fontWeight: 700,
            }}
          >
            {identity.data?.displayName?.[0]?.toUpperCase() ?? '?'}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div
              style={{
                fontSize: 11,
                color: 'var(--text-muted)',
                letterSpacing: 0.4,
                textTransform: 'uppercase',
              }}
            >
              This team
            </div>
            <div
              style={{
                fontSize: 20,
                fontWeight: 700,
                color: 'var(--text-primary)',
                marginTop: 2,
              }}
            >
              {identity.data?.displayName || 'Loading…'}
            </div>
            <div
              style={{
                fontSize: 12,
                color: 'var(--text-muted)',
                fontFamily: 'var(--font-mono)',
                marginTop: 2,
              }}
            >
              {identity.data?.peerId || '—'}
            </div>
          </div>
          <Press pressScale={0.96} onClick={() => setEditOpen(true)}>
            <span
              style={{
                display: 'inline-block',
                padding: '8px 14px',
                background: 'var(--bg-pressed)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
              }}
            >
              Edit identity
            </span>
          </Press>
        </div>

        {status.data?.mission ? (
          <div
            style={{
              padding: 12,
              borderRadius: 'var(--radius-md)',
              background: 'var(--bg-surface)',
              border: '1px solid var(--border-subtle)',
              fontSize: 13,
              color: 'var(--text-secondary)',
              lineHeight: 1.5,
            }}
          >
            {status.data.mission}
          </div>
        ) : null}

        <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
          <StatTile label="Members" value={memberCount} />
          <StatTile label="Peers" value={peerList.length} />
          <StatTile
            label="Unread relay"
            value={peerList.reduce((s, p) => s + (p.unreadInbound ?? 0), 0)}
            tone={
              peerList.some((p) => (p.unreadInbound ?? 0) > 0) ? 'accent' : 'muted'
            }
          />
          <StatTile
            label="Public URL"
            text={identity.data?.publicUrl || 'unset'}
          />
        </div>
      </motion.section>

      {/* Peer grid */}
      <section>
        <header
          style={{
            display: 'flex',
            alignItems: 'baseline',
            justifyContent: 'space-between',
            marginBottom: 12,
          }}
        >
          <h2 style={{ margin: 0, fontSize: 17, fontWeight: 600, color: 'var(--text-primary)' }}>
            Peer teams
          </h2>
          <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
            <Metric value={peerList.length} size={12} /> registered
          </span>
        </header>

        {peerList.length === 0 ? (
          <EmptyPeers />
        ) : (
          <div
            style={{
              display: 'grid',
              gap: 14,
              gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
            }}
          >
            {peerList.map((peer, i) => (
              <RevealItem key={peer.peerId} index={i}>
                <PeerCard peer={peer} />
              </RevealItem>
            ))}
          </div>
        )}
      </section>

      <IdentityEditModal open={editOpen} onClose={() => setEditOpen(false)} />
    </section>
  );
}

function StatTile({
  label,
  value,
  text,
  tone = 'primary',
}: {
  label: string;
  value?: number;
  text?: string;
  tone?: 'primary' | 'muted' | 'accent';
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
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
      {typeof value === 'number' ? (
        <Metric value={value} size={20} tone={tone} />
      ) : (
        <span
          style={{
            fontSize: 14,
            color: 'var(--text-primary)',
            fontFamily: text === 'unset' ? 'var(--font-sans)' : 'var(--font-mono)',
            fontStyle: text === 'unset' ? 'italic' : 'normal',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            maxWidth: 280,
          }}
        >
          {text}
        </span>
      )}
    </div>
  );
}

function PeerCard({ peer }: { peer: Peer }) {
  const ping = usePingPeer();
  const remove = useRemovePeer();
  const dotStatus = mapPeerStatusToDot(peer.status);
  return (
    <motion.article
      whileHover={{ borderColor: 'var(--border-strong)', y: -1 }}
      transition={spring.smooth}
      style={{
        padding: 16,
        borderRadius: 'var(--radius-card)',
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.04)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <StatusDot status={dotStatus} size={8} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 15,
              fontWeight: 600,
              color: 'var(--text-primary)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {peer.displayName}
          </div>
          <div
            style={{
              fontSize: 11,
              color: 'var(--text-muted)',
              fontFamily: 'var(--font-mono)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {peer.publicUrl}
          </div>
        </div>
        {peer.unreadInbound ? (
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              minWidth: 22,
              height: 20,
              padding: '0 7px',
              borderRadius: 999,
              background: 'var(--accent-muted)',
              color: 'var(--accent)',
              fontSize: 11,
              fontWeight: 700,
            }}
          >
            <Metric value={peer.unreadInbound} size={11} tone="accent" />
          </span>
        ) : null}
      </div>
      <div
        style={{
          display: 'flex',
          gap: 12,
          fontSize: 11,
          color: 'var(--text-muted)',
          flexWrap: 'wrap',
        }}
      >
        <span>
          status: <strong style={{ color: peerStatusColor(peer.status) }}>{peer.status}</strong>
        </span>
        <span>
          added:{' '}
          {peer.addedAt
            ? new Date(peer.addedAt).toLocaleDateString(undefined, {
                month: 'short',
                day: 'numeric',
              })
            : '—'}
        </span>
        {peer.lastSeenAt ? (
          <span>last seen: {new Date(peer.lastSeenAt).toLocaleTimeString()}</span>
        ) : null}
      </div>
      {peer.lastError ? (
        <div
          style={{
            fontSize: 11,
            color: 'var(--status-error)',
            background: 'rgba(255,90,95,0.08)',
            padding: '6px 8px',
            borderRadius: 'var(--radius-sm)',
          }}
        >
          {peer.lastError}
        </div>
      ) : null}
      <div style={{ display: 'flex', gap: 8 }}>
        <Press
          pressScale={0.94}
          onClick={() => ping.mutate(peer.peerId)}
          disabled={ping.isPending}
          style={{ flex: 1 } as React.CSSProperties}
        >
          <span
            style={{
              display: 'block',
              padding: '8px 10px',
              background: 'var(--bg-pressed)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--text-primary)',
              fontSize: 12,
              fontWeight: 600,
              textAlign: 'center',
            }}
          >
            {ping.isPending ? 'Pinging…' : 'Ping'}
          </span>
        </Press>
        <Link
          to={`/relay?peer=${encodeURIComponent(peer.peerId)}`}
          style={{ flex: 1, textDecoration: 'none' }}
        >
          <Press pressScale={0.94}>
            <span
              style={{
                display: 'block',
                padding: '8px 10px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-sm)',
                fontSize: 12,
                fontWeight: 600,
                textAlign: 'center',
              }}
            >
              Send message
            </span>
          </Press>
        </Link>
        <Press
          pressScale={0.92}
          onClick={() => {
            if (window.confirm(`Remove peer "${peer.displayName}"?`)) {
              remove.mutate(peer.peerId);
            }
          }}
          disabled={remove.isPending}
        >
          <span
            style={{
              display: 'block',
              padding: '8px 10px',
              background: 'transparent',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--text-muted)',
              fontSize: 12,
              fontWeight: 600,
              minWidth: 32,
              textAlign: 'center',
            }}
          >
            ✕
          </span>
        </Press>
      </div>
    </motion.article>
  );
}

function EmptyPeers() {
  return (
    <div
      style={{
        padding: '48px 24px',
        borderRadius: 'var(--radius-card)',
        background: 'var(--bg-elevated)',
        border: '1px dashed var(--border-default)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 12,
      }}
    >
      <motion.div
        animate={{ scale: [1, 0.92, 1], opacity: [0.6, 0.85, 0.6] }}
        transition={{ duration: 4, repeat: Infinity }}
        style={{
          width: 48,
          height: 48,
          borderRadius: 14,
          border: '1px solid var(--border-strong)',
          background: 'transparent',
        }}
      />
      <div style={{ color: 'var(--text-primary)', fontSize: 17, fontWeight: 600 }}>
        No peer teams yet
      </div>
      <div
        style={{
          color: 'var(--text-muted)',
          fontSize: 13,
          textAlign: 'center',
          maxWidth: 360,
          lineHeight: 1.5,
        }}
      >
        Add another jarvis instance as a peer and you can exchange messages + knowledge
        between teams.
      </div>
      <Link to="/peers/new" style={{ textDecoration: 'none', marginTop: 8 }}>
        <Press pressScale={0.96}>
          <span
            style={{
              display: 'inline-block',
              padding: '10px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 14,
              fontWeight: 600,
            }}
          >
            Add a peer →
          </span>
        </Press>
      </Link>
    </div>
  );
}

function IdentityEditModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const identity = useIdentity();
  const update = useUpdateIdentity();
  const genToken = useGenerateToken();
  const [displayName, setDisplayName] = useState('');
  const [publicUrl, setPublicUrl] = useState('');

  // Reset the form when the identity loads or the modal reopens. Use
  // adjust-state-during-render so React 19's lint stays happy.
  const [hydratedFor, setHydratedFor] = useState<string | null>(null);
  if (open && identity.data && hydratedFor !== identity.data.peerId) {
    setDisplayName(identity.data.displayName ?? '');
    setPublicUrl(identity.data.publicUrl ?? '');
    setHydratedFor(identity.data.peerId);
  }

  const submit = async () => {
    await update.mutateAsync({ displayName, publicUrl });
    onClose();
  };

  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      title="Edit identity"
      description="How peer teams see you. PeerID is immutable; displayName and publicUrl are operator-set."
      size="lg"
      footer={
        <>
          <Press pressScale={0.96} onClick={onClose}>
            <span
              style={{
                padding: '8px 14px',
                background: 'transparent',
                color: 'var(--text-muted)',
                fontSize: 13,
                fontWeight: 600,
                borderRadius: 'var(--radius-md)',
              }}
            >
              Cancel
            </span>
          </Press>
          <Press pressScale={0.96} onClick={submit} disabled={update.isPending}>
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--accent)',
                color: '#fff',
                fontSize: 13,
                fontWeight: 600,
                borderRadius: 'var(--radius-md)',
              }}
            >
              {update.isPending ? 'Saving…' : 'Save'}
            </span>
          </Press>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <Field
          label="Display name"
          hint="Shown to peers in the relay feed."
        >
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Frontend team"
            style={inputStyle}
          />
        </Field>
        <Field
          label="Public URL"
          hint="The HTTPS endpoint other operators paste when adding you as a peer. Must reach this jarvis."
        >
          <input
            value={publicUrl}
            onChange={(e) => setPublicUrl(e.target.value)}
            placeholder="https://frontend-team.example.com"
            style={inputStyle}
          />
        </Field>
        <Field
          label="Peer ID"
          hint="Immutable. Generated once on first startup."
        >
          <code
            style={{
              ...inputStyle,
              fontFamily: 'var(--font-mono)',
              display: 'block',
              color: 'var(--text-muted)',
              background: 'var(--bg-surface)',
            }}
          >
            {identity.data?.peerId ?? '—'}
          </code>
        </Field>

        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: 14,
            borderRadius: 'var(--radius-md)',
            background: 'var(--bg-surface)',
            border: '1px solid var(--border-subtle)',
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
              Generate pair token
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
              Share this 32-byte token with the operator who wants to register you as a peer.
            </div>
            {genToken.data?.token ? (
              <code
                style={{
                  display: 'block',
                  marginTop: 8,
                  padding: 8,
                  fontFamily: 'var(--font-mono)',
                  fontSize: 11,
                  background: 'var(--bg-app)',
                  color: 'var(--accent)',
                  borderRadius: 'var(--radius-sm)',
                  wordBreak: 'break-all',
                }}
              >
                {genToken.data.token}
              </code>
            ) : null}
          </div>
          <Press pressScale={0.94} onClick={() => genToken.mutate()} disabled={genToken.isPending}>
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-md)',
                color: 'var(--text-primary)',
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              {genToken.isPending ? 'Generating…' : 'Generate'}
            </span>
          </Press>
        </div>
      </div>
    </Modal>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
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
      {children}
      {hint ? (
        <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.5 }}>{hint}</span>
      ) : null}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  padding: '10px 12px',
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 14,
  outline: 'none',
  width: '100%',
};

function mapPeerStatusToDot(status: PeerStatus): 'working' | 'idle' | 'meeting' | 'stuck' {
  switch (status) {
    case 'ok':
      return 'working';
    case 'unreachable':
      return 'stuck';
    case 'unauthorized':
      return 'meeting';
    default:
      return 'idle';
  }
}

function peerStatusColor(status: PeerStatus): string {
  switch (status) {
    case 'ok':
      return 'var(--status-success)';
    case 'unreachable':
      return 'var(--status-error)';
    case 'unauthorized':
      return 'var(--status-warn)';
    default:
      return 'var(--text-muted)';
  }
}
