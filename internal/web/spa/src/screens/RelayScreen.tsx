import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { Inbox, MessagesSquare, Send } from 'lucide-react';

import { Metric, Press, StatusDot } from '../components/motion';
import { ComposeRelayModal } from '../components/ComposeRelayModal';
import { Button, EmptyState } from '../components/ui';
import { spring } from '../lib/motion';
import { useIdentity, usePeers } from '../hooks/usePeers';
import {
  useMarkRelayRead,
  useRelayMessages,
} from '../hooks/useRelayMessages';
import type { Peer, RelayMessage } from '../api/types';
import type { CSSProperties } from 'react';

// ─── module-scope style constants ─────────────────────────────────────────────
const peerRailAsideStyle: CSSProperties = {
  padding: 12,
  borderRadius: 'var(--radius-card)',
  background: 'var(--bg-surface)',
  border: '1px solid var(--border-subtle)',
  display: 'flex',
  flexDirection: 'column',
  gap: 6,
  overflowY: 'auto',
};

const conversationMainStyle: CSSProperties = {
  padding: 18,
  borderRadius: 'var(--radius-card)',
  background: 'var(--bg-surface)',
  border: '1px solid var(--border-subtle)',
  display: 'flex',
  flexDirection: 'column',
  gap: 12,
  minHeight: 0,
  overflow: 'hidden',
};

const peerRailButtonBaseStyle: CSSProperties = {
  textAlign: 'left',
  display: 'flex',
  alignItems: 'center',
  gap: 10,
  padding: '10px 12px',
  borderRadius: 'var(--radius-md)',
  cursor: 'pointer',
  color: 'inherit',
  font: 'inherit',
  width: '100%',
};

const unreadPillStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  minWidth: 20,
  height: 18,
  padding: '0 6px',
  borderRadius: 999,
  background: 'var(--accent)',
  color: '#fff',
  fontSize: 12,
  fontWeight: 700,
};

const bubbleBodyBaseStyle: CSSProperties = {
  padding: '10px 14px',
  borderRadius: 'var(--radius-card)',
  fontSize: 14,
  lineHeight: 1.55,
  whiteSpace: 'pre-wrap',
};

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
  // AutoAnimate owns enter/exit of relay bubbles as new messages land
  // over SSE (respects reduced motion). Replaces the per-item RevealItem
  // so live arrivals slide in without a double animation.
  const [threadRef] = useAutoAnimate<HTMLDivElement>();

  // When the operator opens a conversation, mark visible unread inbound
  // messages as read. Cap at 25 so we don't flood the API on first
  // mount of a busy thread.
  // markRead.mutate is a stable reference from useMutation so including
  // it in deps is correct and doesn't cause extra subscriptions.
  const markReadMutate = markRead.mutate;
  useEffect(() => {
    if (!selected || !messages.data) return;
    const unread = messages.data.messages
      .filter((m) => m.direction === 'inbound' && !m.readAt)
      .slice(0, 25);
    for (const m of unread) {
      markReadMutate(m.id);
    }
  }, [selected, messages.data, markReadMutate]);

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
        <Button
          variant="primary"
          onClick={() => setComposeOpen(true)}
          disabled={peerList.length === 0}
        >
          <Send size={14} /> Compose
        </Button>
      </header>

      {peerList.length === 0 ? (
        <EmptyState
          icon={<Inbox size={22} />}
          title="No peer teams registered"
          description="Inter-team messages flow between registered jarvis instances. Add a peer to start."
          action={
            <Link to="/peers/new" style={{ textDecoration: 'none' }}>
              <Button variant="primary">Add a peer team →</Button>
            </Link>
          }
        />
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
          <aside style={peerRailAsideStyle}>
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
          <main style={conversationMainStyle}>
            {messages.isError ? (
              <ErrorPanel
                message={
                  messages.error instanceof Error ? messages.error.message : String(messages.error)
                }
              />
            ) : selected ? (
              conversation.length === 0 ? (
                <EmptyNoMessages onCompose={() => setComposeOpen(true)} />
              ) : (
                <div
                  ref={threadRef}
                  style={{
                    flex: 1,
                    overflowY: 'auto',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 12,
                    paddingRight: 6,
                  }}
                >
                  {conversation.map((m) => (
                    <Bubble
                      key={m.id}
                      message={m}
                      localPeerId={identity.data?.peerId ?? ''}
                      localDisplayName={identity.data?.displayName ?? 'You'}
                    />
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
    <Press
      pressScale={0.98}
      onClick={onSelect}
      className={selected ? 'shadow-glow-sm transition-shadow' : 'transition-shadow'}
      style={{
        ...peerRailButtonBaseStyle,
        border: '1px solid ' + (selected ? 'rgb(var(--accent-rgb))' : 'transparent'),
        background: selected ? 'var(--accent-muted)' : 'transparent',
      }}
    >
      <StatusDot status={mapStatus(peer.status)} size={8} />
      <span style={{ flex: 1, minWidth: 0 }}>
        <span
          style={{
            display: 'block',
            fontSize: 13,
            fontWeight: 600,
            color: selected ? 'var(--accent)' : 'var(--text-primary)',
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
            fontSize: 12,
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
        <motion.span
          animate={{ scale: [1, 1.12, 1] }}
          transition={{ duration: 2.4, repeat: Infinity, ease: 'easeInOut' }}
          className="shadow-glow-sm"
          style={unreadPillStyle}
        >
          <Metric value={peer.unreadInbound} size={12} />
        </motion.span>
      ) : null}
    </Press>
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
          fontSize: 12,
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
          ...bubbleBodyBaseStyle,
          background: isOutbound ? 'var(--accent)' : 'var(--bg-elevated)',
          color: isOutbound ? '#fff' : 'var(--text-primary)',
          border: '1px solid ' + (isOutbound ? 'var(--accent-strong)' : 'var(--border-subtle)'),
        }}
      >
        {message.content || (
          <span style={{ opacity: 0.7, fontStyle: 'italic' }}>(no body - attachments only)</span>
        )}
        {message.attachments && message.attachments.length > 0 ? (
          <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {message.attachments.map((a) => (
              <details
                key={`${a.kind}:${a.title}`}
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

function ErrorPanel({ message }: { message: string }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={spring.smooth}
      style={{
        padding: 12,
        borderRadius: 'var(--radius-md)',
        background: 'rgba(255,90,95,0.10)',
        border: '1px solid var(--status-error)',
        color: 'var(--status-error)',
        fontSize: 13,
      }}
    >
      Couldn't load this conversation: {message}
    </motion.div>
  );
}

function EmptyNoSelection() {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <EmptyState
        icon={<MessagesSquare size={22} />}
        title="Select a peer to view its conversation"
        description="Pick a team from the rail to read and reply to its relay thread."
      />
    </div>
  );
}

function EmptyNoMessages({ onCompose }: { onCompose: () => void }) {
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <EmptyState
        icon={<Send size={22} />}
        title="No messages exchanged yet"
        description="Send a message: share something your team learned that this peer team could use."
        action={
          <Button variant="primary" onClick={onCompose}>
            Compose first message →
          </Button>
        }
      />
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
