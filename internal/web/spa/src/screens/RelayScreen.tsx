import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';

import { Metric, Press, RevealItem, StatusDot } from '../components/motion';
import { ComposeRelayModal } from '../components/ComposeRelayModal';
import { spring } from '../lib/motion';
import { useIdentity, usePeers } from '../hooks/usePeers';
import {
  useMarkRelayRead,
  useRelayMessages,
} from '../hooks/useRelayMessages';
import type { Peer, RelayMessage } from '../api/types';

// RelayScreen — inter-team message log. Left rail picks the
// conversation; right pane shows messages between local team and the
// selected peer. Compose CTA opens the wide ComposeRelayModal.
export function RelayScreen() {
  const [params, setParams] = useSearchParams();
  const peerParam = params.get('peer') ?? '';

  const peers = usePeers();
  const identity = useIdentity();
  const peerList = peers.data?.peers ?? [];

  // Auto-select the first peer if the operator hasn't picked one yet.
  const selected = peerParam || peerList[0]?.peerId || '';

  const messages = useRelayMessages(selected || undefined);
  const markRead = useMarkRelayRead();
  const [composeOpen, setComposeOpen] = useState(false);

  // When the operator opens a conversation, mark visible unread inbound
  // messages as read. Cap at 25 so we don't flood the API on first
  // mount of a busy thread.
  useEffect(() => {
    if (!selected || !messages.data) return;
    const unread = messages.data.messages
      .filter((m) => m.direction === 'inbound' && !m.readAt)
      .slice(0, 25);
    for (const m of unread) {
      markRead.mutate(m.id);
    }
    // markRead.mutate is stable; we intentionally do not include it in deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, messages.data]);

  const selectPeer = (peerId: string) => {
    setParams((cur) => {
      const next = new URLSearchParams(cur);
      next.set('peer', peerId);
      return next;
    });
  };

  const conversation = useMemo(() => messages.data?.messages ?? [], [messages.data?.messages]);

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Relay
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Cross-team messages. Pass what your team learned to other teams so they don't
            have to re-learn it.
          </p>
        </div>
        <Press
          pressScale={0.96}
          onClick={() => setComposeOpen(true)}
          disabled={peerList.length === 0}
        >
          <span
            style={{
              display: 'inline-block',
              padding: '10px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 14,
              fontWeight: 600,
              opacity: peerList.length === 0 ? 0.5 : 1,
            }}
          >
            Compose ↗
          </span>
        </Press>
      </header>

      {peerList.length === 0 ? (
        <EmptyNoPeers />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'minmax(220px, 280px) 1fr',
            gap: 18,
            minHeight: 0,
            flex: 1,
          }}
        >
          {/* Left rail: peer list */}
          <aside
            style={{
              padding: 12,
              borderRadius: 'var(--radius-card)',
              background: 'var(--bg-surface)',
              border: '1px solid var(--border-subtle)',
              display: 'flex',
              flexDirection: 'column',
              gap: 6,
              overflowY: 'auto',
            }}
          >
            {peerList.map((peer) => (
              <PeerRail
                key={peer.peerId}
                peer={peer}
                selected={peer.peerId === selected}
                onSelect={() => selectPeer(peer.peerId)}
              />
            ))}
          </aside>

          {/* Right pane: conversation */}
          <main
            style={{
              padding: 18,
              borderRadius: 'var(--radius-card)',
              background: 'var(--bg-surface)',
              border: '1px solid var(--border-subtle)',
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
              minHeight: 0,
              overflow: 'hidden',
            }}
          >
            {selected ? (
              conversation.length === 0 ? (
                <EmptyNoMessages onCompose={() => setComposeOpen(true)} />
              ) : (
                <div
                  style={{
                    flex: 1,
                    overflowY: 'auto',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 12,
                    paddingRight: 6,
                  }}
                >
                  {conversation.map((m, i) => (
                    <RevealItem key={m.id} index={Math.min(i, 12)}>
                      <Bubble
                        message={m}
                        localPeerId={identity.data?.peerId ?? ''}
                        localDisplayName={identity.data?.displayName ?? 'You'}
                      />
                    </RevealItem>
                  ))}
                </div>
              )
            ) : (
              <EmptyNoSelection />
            )}
          </main>
        </div>
      )}

      <ComposeRelayModal
        open={composeOpen}
        onClose={() => setComposeOpen(false)}
        initialPeerId={selected || undefined}
      />
    </section>
  );
}

function PeerRail({
  peer,
  selected,
  onSelect,
}: {
  peer: Peer;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      style={{
        textAlign: 'left',
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        padding: '10px 12px',
        borderRadius: 'var(--radius-md)',
        border: '1px solid ' + (selected ? 'var(--accent)' : 'transparent'),
        background: selected ? 'var(--accent-muted)' : 'transparent',
        cursor: 'pointer',
        color: 'inherit',
        font: 'inherit',
      }}
    >
      <StatusDot status={mapStatus(peer.status)} size={8} />
      <span style={{ flex: 1, minWidth: 0 }}>
        <span
          style={{
            display: 'block',
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--text-primary)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {peer.displayName}
        </span>
        <span
          style={{
            display: 'block',
            fontSize: 11,
            color: 'var(--text-muted)',
            fontFamily: 'var(--font-mono)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {peer.publicUrl}
        </span>
      </span>
      {peer.unreadInbound ? (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            minWidth: 20,
            height: 18,
            padding: '0 6px',
            borderRadius: 999,
            background: 'var(--accent)',
            color: '#fff',
            fontSize: 11,
            fontWeight: 700,
          }}
        >
          <Metric value={peer.unreadInbound} size={11} />
        </span>
      ) : null}
    </button>
  );
}

function Bubble({
  message,
  localPeerId,
  localDisplayName,
}: {
  message: RelayMessage;
  localPeerId: string;
  localDisplayName: string;
}) {
  const isOutbound = message.direction === 'outbound';
  const senderName = isOutbound ? localDisplayName : message.fromDisplayName;
  void localPeerId;
  return (
    <motion.div
      transition={spring.smooth}
      style={{
        alignSelf: isOutbound ? 'flex-end' : 'flex-start',
        maxWidth: '78%',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
      }}
    >
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.3,
        }}
      >
        {senderName} ·{' '}
        {new Date(message.createdAt).toLocaleString(undefined, {
          month: 'short',
          day: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
        })}
        {isOutbound && !message.deliveredAt ? (
          <span style={{ color: 'var(--status-warn)', marginLeft: 8 }}>queued</span>
        ) : null}
      </span>
      <div
        style={{
          padding: '10px 14px',
          borderRadius: 'var(--radius-card)',
          background: isOutbound ? 'var(--accent)' : 'var(--bg-elevated)',
          color: isOutbound ? '#fff' : 'var(--text-primary)',
          fontSize: 14,
          lineHeight: 1.55,
          whiteSpace: 'pre-wrap',
          border: '1px solid ' + (isOutbound ? 'var(--accent-strong)' : 'var(--border-subtle)'),
        }}
      >
        {message.content || (
          <span style={{ opacity: 0.7, fontStyle: 'italic' }}>(no body — attachments only)</span>
        )}
        {message.attachments && message.attachments.length > 0 ? (
          <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {message.attachments.map((a, i) => (
              <details
                key={i}
                style={{
                  background: isOutbound ? 'rgba(255,255,255,0.12)' : 'var(--bg-surface)',
                  borderRadius: 'var(--radius-sm)',
                  padding: '6px 10px',
                  border: '1px solid ' + (isOutbound ? 'rgba(255,255,255,0.18)' : 'var(--border-subtle)'),
                  color: isOutbound ? '#fff' : 'var(--text-primary)',
                }}
              >
                <summary style={{ cursor: 'pointer', fontSize: 12, fontWeight: 600 }}>
                  {kindBadge(a.kind)} {a.title}
                </summary>
                <div
                  style={{
                    marginTop: 6,
                    whiteSpace: 'pre-wrap',
                    fontSize: 12,
                    opacity: 0.92,
                  }}
                >
                  {a.body}
                </div>
              </details>
            ))}
          </div>
        ) : null}
      </div>
    </motion.div>
  );
}

function EmptyNoSelection() {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
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
        }}
      />
      <div style={{ color: 'var(--text-primary)', fontSize: 16, fontWeight: 600 }}>
        Select a peer to view its conversation
      </div>
    </div>
  );
}

function EmptyNoMessages({ onCompose }: { onCompose: () => void }) {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 14,
      }}
    >
      <div style={{ color: 'var(--text-primary)', fontSize: 16, fontWeight: 600 }}>
        No messages exchanged yet
      </div>
      <p
        style={{
          margin: 0,
          color: 'var(--text-muted)',
          fontSize: 13,
          textAlign: 'center',
          maxWidth: 360,
          lineHeight: 1.5,
        }}
      >
        Send a message — share something your team learned that this peer team could use.
      </p>
      <Press pressScale={0.96} onClick={onCompose}>
        <span
          style={{
            padding: '10px 16px',
            background: 'var(--accent)',
            color: '#fff',
            fontSize: 13,
            fontWeight: 600,
            borderRadius: 'var(--radius-md)',
            display: 'inline-block',
          }}
        >
          Compose first message →
        </span>
      </Press>
    </div>
  );
}

function EmptyNoPeers() {
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
      <div style={{ color: 'var(--text-primary)', fontSize: 17, fontWeight: 600 }}>
        No peer teams registered
      </div>
      <p
        style={{
          margin: 0,
          color: 'var(--text-muted)',
          fontSize: 13,
          textAlign: 'center',
          maxWidth: 380,
          lineHeight: 1.5,
        }}
      >
        Inter-team messages flow between registered jarvis instances. Add a peer to start.
      </p>
      <Link to="/peers/new" style={{ textDecoration: 'none' }}>
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
            Add a peer team →
          </span>
        </Press>
      </Link>
    </div>
  );
}

function mapStatus(s: Peer['status']): 'working' | 'idle' | 'meeting' | 'stuck' {
  switch (s) {
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

function kindBadge(kind: RelayMessage['attachments'] extends Array<infer A>
  ? A extends { kind: infer K }
    ? K
    : string
  : string): string {
  switch (kind) {
    case 'experience':
      return '📚';
    case 'workflow':
      return '🛠';
    case 'task':
      return '📝';
    case 'missionExcerpt':
      return '🎯';
    default:
      return '•';
  }
}
