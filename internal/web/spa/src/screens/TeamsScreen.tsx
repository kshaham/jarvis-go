import { Link } from 'react-router-dom';
import { useRef, useState } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { toast } from 'sonner';
import { UsersRound, X } from 'lucide-react';
import type { CSSProperties } from 'react';

import { Metric } from '../components/motion/Metric';
import { Modal } from '../components/motion/Modal';
import { StatusDot } from '../components/motion/StatusDot';
import { Button } from '../components/ui/Button';
import { EmptyState } from '../components/ui/EmptyState';
import { Field, Input } from '../components/ui/Field';
import { GlowCard } from '../components/ui/GlowCard';
import { Skeleton } from '../components/ui/Skeleton';
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

// ─── module-scope style constants ─────────────────────────────────────────────
const avatarBoxStyle: CSSProperties = {
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
};

const teamLabelStyle: CSSProperties = {
  fontSize: 12,
  color: 'var(--text-muted)',
  letterSpacing: 0.4,
  textTransform: 'uppercase',
};

const statLabelStyle: CSSProperties = {
  fontSize: 12,
  color: 'var(--text-muted)',
  letterSpacing: 0.4,
  textTransform: 'uppercase',
};

const peerUrlStyle: CSSProperties = {
  fontSize: 12,
  color: 'var(--text-muted)',
  fontFamily: 'var(--font-mono)',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const peerMetaRowStyle: CSSProperties = {
  display: 'flex',
  gap: 12,
  fontSize: 12,
  color: 'var(--text-muted)',
  flexWrap: 'wrap',
};

const peerErrorStyle: CSSProperties = {
  fontSize: 12,
  color: 'var(--status-error)',
  background: 'rgba(255,90,95,0.08)',
  padding: '6px 8px',
  borderRadius: 'var(--radius-sm)',
};

const unreadBadgeStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  minWidth: 22,
  height: 20,
  padding: '0 7px',
  borderRadius: 999,
  background: 'var(--accent-muted)',
  color: 'var(--accent)',
  fontSize: 12,
  fontWeight: 700,
};

const statTextStyle: CSSProperties = {
  fontSize: 14,
  color: 'var(--text-primary)',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
  maxWidth: 280,
};

const tokenSectionStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: 12,
  padding: 14,
  borderRadius: 'var(--radius-md)',
  background: 'var(--bg-surface)',
  border: '1px solid var(--border-subtle)',
};

const tokenCodeStyle: CSSProperties = {
  display: 'block',
  marginTop: 8,
  padding: 8,
  fontFamily: 'var(--font-mono)',
  fontSize: 12,
  background: 'var(--bg-app)',
  color: 'var(--accent)',
  borderRadius: 'var(--radius-sm)',
  wordBreak: 'break-all',
};

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
  // AutoAnimate owns enter/exit/reorder of the peer grid as peers are
  // added/removed or reorder over SSE (respects reduced motion).
  const [gridRef] = useAutoAnimate<HTMLDivElement>();

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 className="page-header">
            Teams
          </h1>
          <p className="page-subtitle">
            Your local team and every peer jarvis instance it can talk to.
          </p>
        </div>
        <Link to="/peers/new" style={{ textDecoration: 'none' }}>
          <Button variant="primary">+ Add peer team</Button>
        </Link>
      </header>

      {/* Local team hero */}
      <GlowCard
        live
        spotlight
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={spring.smooth}
        className="flex flex-col gap-4 p-5"
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <div style={avatarBoxStyle}>
            {identity.data?.displayName?.[0]?.toUpperCase() ?? '?'}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={teamLabelStyle}>
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
          <Button variant="secondary" onClick={() => setEditOpen(true)}>
            Edit identity
          </Button>
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
      </GlowCard>

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
          <span className="inline-flex items-baseline gap-1" style={{ color: 'var(--text-muted)', fontSize: 12 }}>
            <Metric value={peerList.length} size={12} tone="muted" pulseOnChange /> registered
          </span>
        </header>

        {peers.isLoading ? (
          <div
            style={{
              display: 'grid',
              gap: 14,
              gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
            }}
          >
            {Array.from({ length: 3 }).map((_, i) => (
              <PeerCardSkeleton key={i} />
            ))}
          </div>
        ) : peerList.length === 0 ? (
          <EmptyState
            icon={<UsersRound size={22} />}
            title="No peer teams yet"
            description="Add another jarvis instance as a peer and you can exchange messages + knowledge between teams."
            action={
              <Link to="/peers/new" style={{ textDecoration: 'none' }}>
                <Button variant="primary">Add a peer →</Button>
              </Link>
            }
          />
        ) : (
          <div
            ref={gridRef}
            style={{
              display: 'grid',
              gap: 14,
              gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
            }}
          >
            {peerList.map((peer) => (
              <PeerCard key={peer.peerId} peer={peer} />
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
      <span style={statLabelStyle}>
        {label}
      </span>
      {typeof value === 'number' ? (
        <Metric value={value} size={20} tone={tone} pulseOnChange />
      ) : (
        <span
          style={{
            ...statTextStyle,
            fontFamily: text === 'unset' ? 'var(--font-sans)' : 'var(--font-mono)',
            fontStyle: text === 'unset' ? 'italic' : 'normal',
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

  const onPing = () => {
    ping.mutate(peer.peerId, {
      onSuccess: (res) => {
        if (res.error) toast.error(`Ping failed: ${res.error}`);
        else toast.success(`${peer.displayName} is reachable`);
      },
      onError: (e) =>
        toast.error(`Ping failed: ${e instanceof Error ? e.message : String(e)}`),
    });
  };

  const onRemove = () => {
    if (!window.confirm(`Remove peer "${peer.displayName}"?`)) return;
    remove.mutate(peer.peerId, {
      onSuccess: () => toast.success(`Removed ${peer.displayName}`),
      onError: (e) =>
        toast.error(`Remove failed: ${e instanceof Error ? e.message : String(e)}`),
    });
  };

  return (
    <GlowCard interactive live={peer.status === 'ok'} className="flex flex-col gap-2.5 p-4">
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
          <div style={peerUrlStyle}>
            {peer.publicUrl}
          </div>
        </div>
        {peer.unreadInbound ? (
          <span className="shadow-glow-sm" style={unreadBadgeStyle}>
            <Metric value={peer.unreadInbound} size={12} tone="accent" pulseOnChange />
          </span>
        ) : null}
      </div>
      <div style={peerMetaRowStyle}>
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
        <div style={peerErrorStyle}>
          {peer.lastError}
        </div>
      ) : null}
      <div style={{ display: 'flex', gap: 8 }}>
        <Button
          variant="secondary"
          size="sm"
          onClick={onPing}
          disabled={ping.isPending}
          className="flex-1"
        >
          {ping.isPending ? 'Pinging…' : 'Ping'}
        </Button>
        <Link
          to={`/relay?peer=${encodeURIComponent(peer.peerId)}`}
          style={{ flex: 1, textDecoration: 'none' }}
        >
          <Button variant="primary" size="sm" className="w-full">
            Send message
          </Button>
        </Link>
        <Button
          variant="danger"
          size="sm"
          onClick={onRemove}
          disabled={remove.isPending}
          aria-label={`Remove ${peer.displayName}`}
        >
          <X size={14} />
        </Button>
      </div>
    </GlowCard>
  );
}

function PeerCardSkeleton() {
  return (
    <div className="surface-card flex flex-col gap-2.5 p-4">
      <div className="flex items-center gap-2.5">
        <Skeleton className="size-2 shrink-0 rounded-full" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3.5 w-32" />
          <Skeleton className="h-2.5 w-40" />
        </div>
      </div>
      <Skeleton className="h-2.5 w-3/4" />
      <div className="flex gap-2">
        <Skeleton className="h-8 flex-1 rounded-md" />
        <Skeleton className="h-8 flex-1 rounded-md" />
        <Skeleton className="h-8 w-9 rounded-md" />
      </div>
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
  // adjust-state-during-render — track the previously hydrated peerId
  // in a ref (never rendered) to avoid an extra re-render cycle.
  const hydratedForRef = useRef<string | null>(null);
  if (open && identity.data && hydratedForRef.current !== identity.data.peerId) {
    hydratedForRef.current = identity.data.peerId;
    setDisplayName(identity.data.displayName ?? '');
    setPublicUrl(identity.data.publicUrl ?? '');
  }

  const submit = async () => {
    try {
      await update.mutateAsync({ displayName, publicUrl });
      toast.success('Identity saved');
      onClose();
    } catch (e) {
      toast.error(`Save failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  };

  const onGenerate = () => {
    genToken.mutate(undefined, {
      onSuccess: () => toast.success('Pair token generated'),
      onError: (e) =>
        toast.error(`Token failed: ${e instanceof Error ? e.message : String(e)}`),
    });
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
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" onClick={submit} disabled={update.isPending}>
            {update.isPending ? 'Saving…' : 'Save'}
          </Button>
        </>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <Field label="Display name" hint="Shown to peers in the relay feed.">
          <Input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Frontend team"
          />
        </Field>
        <Field
          label="Public URL"
          hint="The HTTPS endpoint other operators paste when adding you as a peer. Must reach this jarvis."
        >
          <Input
            value={publicUrl}
            onChange={(e) => setPublicUrl(e.target.value)}
            placeholder="https://frontend-team.example.com"
          />
        </Field>
        <Field label="Peer ID" hint="Immutable. Generated once on first startup.">
          <code
            className="field"
            style={{
              fontFamily: 'var(--font-mono)',
              display: 'block',
              color: 'var(--text-muted)',
              background: 'var(--bg-surface)',
            }}
          >
            {identity.data?.peerId ?? '—'}
          </code>
        </Field>

        <div style={tokenSectionStyle}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>
              Generate pair token
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 2 }}>
              Share this 32-byte token with the operator who wants to register you as a peer.
            </div>
            {genToken.data?.token ? (
              <code style={tokenCodeStyle}>
                {genToken.data.token}
              </code>
            ) : null}
          </div>
          <Button variant="secondary" size="sm" onClick={onGenerate} disabled={genToken.isPending}>
            {genToken.isPending ? 'Generating…' : 'Generate'}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

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
