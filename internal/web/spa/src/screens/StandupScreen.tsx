import { useMemo, useState } from 'react';
import { motion } from 'motion/react';

import { Metric, Press, RevealItem } from '../components/motion';
import { spring } from '../lib/motion';
import { relativeTime } from '../lib/relativeTime';
import { fetchJSON } from '../api/client';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import { useStandup } from '../hooks/useOfficeData';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { Standup } from '../api/types';

// StandupScreen mirrors the mobile standup design with the speaker
// spotlight. Top hero: start/stop button + participant ring. Below:
// transcript (with reveal stagger) + composer input that supports a
// /task prefix to inject a task instead of a chat line.
export function StandupScreen() {
  const standupQ = useStandup();
  const status = useOfficeStatus();
  const qc = useQueryClient();
  const data = standupQ.data?.standup ?? null;
  const active = !!data?.active;

  const memberLookup = useMemo(() => {
    const map = new Map<string, { name: string; color?: string; emoji?: string }>();
    for (const m of status.data?.members ?? []) {
      map.set(m.sessionId, {
        name: m.name,
        color: m.avatarColor,
        emoji: m.avatarEmoji,
      });
    }
    return map;
  }, [status.data?.members]);

  const startMut = useMutation({
    mutationFn: () =>
      fetchJSON<{ standup: Standup }>('/api/office/standup', {
        method: 'POST',
        body: { action: 'start' },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'standup'] }),
  });
  const stopMut = useMutation({
    mutationFn: () =>
      fetchJSON<{ standup: Standup }>('/api/office/standup', {
        method: 'POST',
        body: { action: 'stop' },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'standup'] }),
  });
  const sendMut = useMutation({
    mutationFn: (vars: { content: string; asTask: boolean }) =>
      fetchJSON('/api/office/standup/user-message', {
        method: 'POST',
        body: { content: vars.content, type: vars.asTask ? 'task' : 'message' },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'standup'] }),
  });

  const [text, setText] = useState('');
  const [asTask, setAsTask] = useState(false);
  const transcript = data?.transcript ?? [];

  const submit = () => {
    const content = text.trim();
    if (!content) return;
    sendMut.mutate({ content, asTask }, {
      onSuccess: () => setText(''),
    });
  };

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18, height: '100%' }}>
      <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
            Standup
          </h1>
          <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
            {active
              ? 'Live — agents are responding in turn. Inject a message or /task below.'
              : 'No standup running. Hit Start to call the team together.'}
          </p>
        </div>
        <Press
          pressScale={0.96}
          onClick={() => (active ? stopMut.mutate() : startMut.mutate())}
          disabled={startMut.isPending || stopMut.isPending}
        >
          <span
            style={{
              display: 'inline-block',
              padding: '10px 18px',
              background: active ? 'var(--status-error)' : 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 14,
              fontWeight: 600,
              opacity: startMut.isPending || stopMut.isPending ? 0.7 : 1,
            }}
          >
            {active
              ? stopMut.isPending
                ? 'Stopping…'
                : 'End standup'
              : startMut.isPending
                ? 'Starting…'
                : 'Start standup'}
          </span>
        </Press>
      </header>

      {/* Speaker spotlight */}
      {active && data?.participants?.length ? (
        <SpotlightRow standup={data} memberLookup={memberLookup} />
      ) : null}

      {/* Transcript */}
      <section
        style={{
          flex: 1,
          minHeight: 0,
          padding: 16,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          overflow: 'hidden',
        }}
      >
        <header style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0, fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)' }}>
            Transcript
          </h2>
          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>
            <Metric value={transcript.length} size={11} /> turns
          </span>
        </header>
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
          {transcript.length === 0 ? (
            <EmptyTranscript active={active} />
          ) : (
            transcript.map((entry, i) => (
              <RevealItem key={`${entry.sessionId}-${entry.timestamp}-${i}`} index={Math.min(i, 12)}>
                <TranscriptTurn
                  entry={entry}
                  name={memberLookup.get(entry.sessionId)?.name ?? entry.speakerName ?? entry.sessionId}
                  isCurrent={data?.currentSpeakerId === entry.sessionId}
                />
              </RevealItem>
            ))
          )}
        </div>
      </section>

      {/* Composer */}
      {active ? (
        <div
          style={{
            padding: 12,
            borderRadius: 'var(--radius-card)',
            background: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
              {asTask ? 'Injecting a task' : 'Sending a message'}
            </span>
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-muted)' }}>
              <input type="checkbox" checked={asTask} onChange={(e) => setAsTask(e.target.checked)} />
              /task
            </label>
          </div>
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value.slice(0, 500))}
            placeholder={asTask ? 'Describe a task to assign…' : 'Type a message…'}
            rows={2}
            style={{
              padding: '8px 12px',
              background: 'var(--bg-app)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
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
              onClick={submit}
              disabled={sendMut.isPending || !text.trim()}
            >
              <span
                style={{
                  padding: '8px 16px',
                  background: 'var(--accent)',
                  color: '#fff',
                  borderRadius: 'var(--radius-md)',
                  fontSize: 13,
                  fontWeight: 600,
                  opacity: sendMut.isPending || !text.trim() ? 0.5 : 1,
                }}
              >
                {sendMut.isPending ? 'Sending…' : 'Send'}
              </span>
            </Press>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function SpotlightRow({
  standup,
  memberLookup,
}: {
  standup: Standup;
  memberLookup: Map<string, { name: string; color?: string; emoji?: string }>;
}) {
  return (
    <section
      style={{
        padding: 18,
        borderRadius: 'var(--radius-card)',
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        display: 'flex',
        gap: 18,
        alignItems: 'center',
        flexWrap: 'wrap',
      }}
    >
      {standup.participants.map((pid) => {
        const m = memberLookup.get(pid);
        const isCurrent = standup.currentSpeakerId === pid;
        const size = isCurrent ? 56 : 36;
        return (
          <motion.div
            key={pid}
            animate={{ scale: isCurrent ? 1.05 : 1, opacity: isCurrent ? 1 : 0.55 }}
            transition={spring.smooth}
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: 6,
            }}
          >
            <div
              style={{
                width: size,
                height: size,
                borderRadius: size / 2,
                background:
                  m?.color ??
                  'linear-gradient(135deg, var(--accent), var(--accent-strong))',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: '#fff',
                fontSize: isCurrent ? 18 : 13,
                fontWeight: 700,
                border: isCurrent ? '2px solid var(--accent)' : '1px solid var(--border-strong)',
                boxShadow: isCurrent ? '0 0 18px rgba(124,92,255,0.4)' : 'none',
              }}
            >
              {m?.emoji || m?.name?.[0] || '?'}
            </div>
            <span
              style={{
                fontSize: 11,
                color: isCurrent ? 'var(--text-primary)' : 'var(--text-muted)',
                fontWeight: isCurrent ? 600 : 500,
                maxWidth: 80,
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {m?.name ?? pid.slice(0, 6)}
            </span>
          </motion.div>
        );
      })}
    </section>
  );
}

function TranscriptTurn({
  entry,
  name,
  isCurrent,
}: {
  entry: Standup['transcript'][number];
  name: string;
  isCurrent: boolean;
}) {
  return (
    <div
      style={{
        padding: 12,
        borderRadius: 'var(--radius-md)',
        background: isCurrent ? 'var(--accent-muted)' : 'var(--bg-elevated)',
        border: '1px solid ' + (isCurrent ? 'var(--accent)' : 'var(--border-subtle)'),
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          fontSize: 11,
          color: 'var(--text-muted)',
        }}
      >
        <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{name}</span>
        <span>{relativeTime(entry.timestamp)}</span>
      </div>
      <div
        style={{
          fontSize: 13,
          color: 'var(--text-secondary)',
          whiteSpace: 'pre-wrap',
          lineHeight: 1.5,
        }}
      >
        {entry.content}
      </div>
    </div>
  );
}

function EmptyTranscript({ active }: { active: boolean }) {
  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 10,
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
      <div style={{ color: 'var(--text-primary)', fontSize: 15, fontWeight: 600 }}>
        {active ? 'Waiting for agents to respond…' : 'No standup running'}
      </div>
      <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>
        {active ? 'Their next turns show up here automatically.' : 'Tap Start standup to begin.'}
      </div>
    </div>
  );
}
