import { useMemo, useState } from 'react';
import { motion } from 'motion/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Metric, Press, RevealItem } from '../components/motion';
import { spring } from '../lib/motion';
import { relativeTime } from '../lib/relativeTime';
import { fetchJSON } from '../api/client';
import { useOfficeMessages } from '../hooks/useOfficeData';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import type { OfficeMember, OfficeMessageWire } from '../api/types';

// MessagesScreen — agent-to-agent chat. Left rail: agent list with
// unread / latest preview. Right pane: thread between local viewer
// (operator perspective) and the selected agent. The operator can
// inject a message from any sender to any recipient.
export function MessagesScreen() {
  const messagesQ = useOfficeMessages();
  const status = useOfficeStatus();
  const qc = useQueryClient();

  const members = status.data?.members ?? [];
  const memberLookup = useMemo(() => {
    const map = new Map<string, OfficeMember>();
    for (const m of members) map.set(m.sessionId, m);
    return map;
  }, [members]);

  const allMessages = messagesQ.data?.messages ?? [];

  const [selectedAgent, setSelectedAgent] = useState<string>('');
  const [fromAgent, setFromAgent] = useState<string>(''); // operator's chosen sender

  // Default selection: pick the agent with the most recent message.
  const effectiveSelected = selectedAgent || latestAgentId(allMessages) || members[0]?.sessionId || '';

  // Conversation = every message where the selected agent is either
  // the sender or recipient.
  const conversation = useMemo(() => {
    if (!effectiveSelected) return [];
    return allMessages
      .filter(
        (m) => m.fromSessionId === effectiveSelected || m.toSessionId === effectiveSelected,
      )
      .sort((a, b) => a.timestamp.localeCompare(b.timestamp));
  }, [allMessages, effectiveSelected]);

  // For the left rail, show every agent (not just those who already
  // chatted) so the operator can start fresh threads.
  const railRows = useMemo(() => {
    return members.map((m) => {
      const last = allMessages
        .filter((x) => x.fromSessionId === m.sessionId || x.toSessionId === m.sessionId)
        .sort((a, b) => b.timestamp.localeCompare(a.timestamp))[0];
      return { member: m, last };
    });
  }, [members, allMessages]);

  const sendMut = useMutation({
    mutationFn: (vars: { from: string; to: string; content: string }) =>
      fetchJSON('/api/office/message', {
        method: 'POST',
        body: {
          fromSessionId: vars.from,
          toSessionId: vars.to,
          content: vars.content,
        },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'messages'] }),
  });

  const [text, setText] = useState('');
  const submit = () => {
    const content = text.trim();
    if (!content || !fromAgent || !effectiveSelected) return;
    sendMut.mutate(
      { from: fromAgent, to: effectiveSelected, content },
      { onSuccess: () => setText('') },
    );
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Chat
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            Inter-agent messages. Operator can inject from any sender to any agent.
          </p>
        </div>
        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
          <Metric value={allMessages.length} size={12} /> messages
        </span>
      </header>

      {members.length === 0 ? (
        <EmptyNoTeam />
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
          <aside
            style={{
              padding: 10,
              borderRadius: 'var(--radius-card)',
              background: 'var(--bg-surface)',
              border: '1px solid var(--border-subtle)',
              display: 'flex',
              flexDirection: 'column',
              gap: 4,
              overflowY: 'auto',
            }}
          >
            {railRows.map(({ member, last }) => (
              <AgentRail
                key={member.sessionId}
                member={member}
                last={last}
                selected={member.sessionId === effectiveSelected}
                onSelect={() => setSelectedAgent(member.sessionId)}
              />
            ))}
          </aside>

          <main
            style={{
              padding: 14,
              borderRadius: 'var(--radius-card)',
              background: 'var(--bg-surface)',
              border: '1px solid var(--border-subtle)',
              display: 'flex',
              flexDirection: 'column',
              gap: 10,
              minHeight: 0,
            }}
          >
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
              {conversation.length === 0 ? (
                <EmptyThread
                  name={memberLookup.get(effectiveSelected)?.name ?? '—'}
                />
              ) : (
                conversation.map((m, i) => (
                  <RevealItem key={m.id} index={Math.min(i, 12)}>
                    <Bubble
                      msg={m}
                      isOutboundToSelected={m.toSessionId === effectiveSelected}
                      lookup={memberLookup}
                    />
                  </RevealItem>
                ))
              )}
            </div>

            <Composer
              members={members}
              fromAgent={fromAgent}
              setFromAgent={setFromAgent}
              text={text}
              setText={setText}
              onSend={submit}
              pending={sendMut.isPending}
              recipient={memberLookup.get(effectiveSelected)?.name ?? ''}
            />
          </main>
        </div>
      )}
    </section>
  );
}

function AgentRail({
  member,
  last,
  selected,
  onSelect,
}: {
  member: OfficeMember;
  last?: OfficeMessageWire;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      style={{
        textAlign: 'left',
        display: 'flex',
        gap: 10,
        padding: 10,
        borderRadius: 'var(--radius-md)',
        border: '1px solid ' + (selected ? 'var(--accent)' : 'transparent'),
        background: selected ? 'var(--accent-muted)' : 'transparent',
        cursor: 'pointer',
        color: 'inherit',
        font: 'inherit',
      }}
    >
      <div
        style={{
          width: 32,
          height: 32,
          borderRadius: 8,
          background: member.avatarColor ?? 'var(--bg-pressed)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#fff',
          fontSize: 14,
          fontWeight: 700,
          flexShrink: 0,
        }}
      >
        {member.avatarEmoji || member.avatarInitial || member.name?.[0] || '?'}
      </div>
      <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        <span
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--text-primary)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {member.name}
        </span>
        <span
          style={{
            fontSize: 11,
            color: 'var(--text-muted)',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {last
            ? `${last.fromSessionId === member.sessionId ? '→ ' : ''}${last.content}`
            : member.role}
        </span>
      </span>
      {last ? (
        <span style={{ fontSize: 10, color: 'var(--text-faint)' }}>
          {relativeTime(last.timestamp)}
        </span>
      ) : null}
    </button>
  );
}

function Bubble({
  msg,
  isOutboundToSelected,
  lookup,
}: {
  msg: OfficeMessageWire;
  isOutboundToSelected: boolean;
  lookup: Map<string, OfficeMember>;
}) {
  const fromMember = lookup.get(msg.fromSessionId);
  const toMember = msg.toSessionId ? lookup.get(msg.toSessionId) : undefined;
  const fromName = msg.fromName || fromMember?.name || msg.fromSessionId.slice(0, 6);
  const toName = msg.toName || toMember?.name || msg.toSessionId?.slice(0, 6) || 'team';
  return (
    <motion.div
      transition={spring.smooth}
      style={{
        alignSelf: isOutboundToSelected ? 'flex-end' : 'flex-start',
        maxWidth: '78%',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
      }}
    >
      <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
        {fromName} → {toName} · {relativeTime(msg.timestamp)}
      </span>
      <div
        style={{
          padding: '10px 14px',
          borderRadius: 'var(--radius-card)',
          background: isOutboundToSelected ? 'var(--accent)' : 'var(--bg-elevated)',
          color: isOutboundToSelected ? '#fff' : 'var(--text-primary)',
          fontSize: 13,
          lineHeight: 1.55,
          whiteSpace: 'pre-wrap',
          border: '1px solid ' + (isOutboundToSelected ? 'var(--accent-strong)' : 'var(--border-subtle)'),
        }}
      >
        {msg.content}
      </div>
    </motion.div>
  );
}

function Composer({
  members,
  fromAgent,
  setFromAgent,
  text,
  setText,
  onSend,
  pending,
  recipient,
}: {
  members: OfficeMember[];
  fromAgent: string;
  setFromAgent: (id: string) => void;
  text: string;
  setText: (t: string) => void;
  onSend: () => void;
  pending: boolean;
  recipient: string;
}) {
  return (
    <div
      style={{
        padding: 10,
        borderRadius: 'var(--radius-md)',
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
    >
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 12, color: 'var(--text-muted)' }}>
        <span>From</span>
        <select
          value={fromAgent}
          onChange={(e) => setFromAgent(e.target.value)}
          style={{
            padding: '4px 8px',
            background: 'var(--bg-app)',
            border: '1px solid var(--border-default)',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--text-primary)',
            fontSize: 12,
          }}
        >
          <option value="">Select sender…</option>
          {members.map((m) => (
            <option key={m.sessionId} value={m.sessionId}>
              {m.name}
            </option>
          ))}
        </select>
        <span style={{ marginLeft: 'auto' }}>→ {recipient || 'recipient'}</span>
      </div>
      <textarea
        value={text}
        onChange={(e) => setText(e.target.value.slice(0, 500))}
        placeholder="Inject a message into the team channel…"
        rows={2}
        style={{
          padding: '8px 12px',
          background: 'var(--bg-app)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--text-primary)',
          fontSize: 13,
          outline: 'none',
          resize: 'vertical',
          fontFamily: 'inherit',
        }}
      />
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8 }}>
        <span style={{ fontSize: 11, color: text.length > 480 ? 'var(--status-warn)' : 'var(--text-muted)' }}>
          <Metric value={text.length} size={11} /> / 500
        </span>
        <Press
          pressScale={0.96}
          onClick={onSend}
          disabled={pending || !text.trim() || !fromAgent}
        >
          <span
            style={{
              padding: '8px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 12,
              fontWeight: 600,
              opacity: pending || !text.trim() || !fromAgent ? 0.5 : 1,
            }}
          >
            {pending ? 'Sending…' : 'Send'}
          </span>
        </Press>
      </div>
    </div>
  );
}

function latestAgentId(messages: OfficeMessageWire[]): string {
  if (messages.length === 0) return '';
  const last = [...messages].sort((a, b) => b.timestamp.localeCompare(a.timestamp))[0];
  return last.toSessionId || last.fromSessionId;
}

function EmptyNoTeam() {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 10,
        padding: 32,
        borderRadius: 'var(--radius-card)',
        background: 'var(--bg-elevated)',
        border: '1px dashed var(--border-default)',
      }}
    >
      <div style={{ color: 'var(--text-primary)', fontSize: 16, fontWeight: 600 }}>
        No team spawned
      </div>
      <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', maxWidth: 360 }}>
        Spawn a team in the classic UI and inter-agent messages will start flowing here.
      </div>
    </div>
  );
}

function EmptyThread({ name }: { name: string }) {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 6,
        color: 'var(--text-muted)',
        fontSize: 13,
      }}
    >
      No messages with <strong style={{ color: 'var(--text-primary)' }}>{name}</strong> yet.
    </div>
  );
}
