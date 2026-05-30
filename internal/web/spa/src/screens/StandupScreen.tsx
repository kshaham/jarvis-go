import { useEffect, useMemo, useState } from 'react';
import { motion, useReducedMotion } from 'motion/react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { toast } from 'sonner';

import { Metric } from '@/components/motion';
import { Button, GlowCard, Skeleton, Textarea, Toggle } from '@/components/ui';
import { spring } from '@/lib/motion';
import { relativeTime } from '@/lib/relativeTime';
import { fetchJSON } from '@/api/client';
import { subscribeSSE } from '@/api/sse';
import { useOfficeStatus } from '@/hooks/useOfficeStatus';
import { useStandup, useStandupHistory } from '@/hooks/useOfficeData';
import type { OfficeSettings, Standup } from '@/api/types';

// StandupScreen mirrors the mobile standup design with the speaker
// spotlight. Top hero: start/stop button + participant ring. Below:
// transcript (live auto-animated) + composer input that supports a
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
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'standup'] });
      toast.success('Standup started');
    },
    onError: () => toast.error('Could not start standup'),
  });
  const stopMut = useMutation({
    mutationFn: () =>
      fetchJSON<{ standup: Standup }>('/api/office/standup', {
        method: 'POST',
        body: { action: 'stop' },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'standup'] });
      toast.success('Standup ended');
    },
    onError: () => toast.error('Could not end standup'),
  });
  const sendMut = useMutation({
    mutationFn: (vars: { content: string; asTask: boolean }) =>
      fetchJSON('/api/office/standup/user-message', {
        method: 'POST',
        body: { content: vars.content, type: vars.asTask ? 'task' : 'message' },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'standup'] }),
    onError: () => toast.error('Message failed to send'),
  });

  const [audioStage, setAudioStage] = useState<string | null>(null);
  const [audioUrl, setAudioUrl] = useState<string | null>(null);
  const [audioNonce, setAudioNonce] = useState(0);
  const historyQ = useStandupHistory();

  const latestHistoryId = historyQ.data?.history?.[0]?.id ?? null;
  const targetId = data?.id ?? latestHistoryId;
  const hasTranscript = (data?.transcript?.length ?? 0) > 0;
  const completed = !active && !!targetId && (hasTranscript || !!latestHistoryId);

  const settingsQ = useQuery<OfficeSettings>({
    queryKey: ['office', 'settings'],
    queryFn: ({ signal }) => fetchJSON<OfficeSettings>('/api/office/settings', { signal }),
    staleTime: 60_000,
  });
  // Treat "explicitly false" as missing; while loading (undefined) do NOT block the button.
  const keyMissing = settingsQ.data?.openaiKeyConfigured === false;

  useEffect(() => {
    const sub = subscribeSSE((evt) => {
      if (
        evt.name !== 'office:standupAudioProgress' &&
        evt.name !== 'office:standupAudioReady' &&
        evt.name !== 'office:standupAudioError'
      ) {
        return;
      }
      const p = (evt.data ?? {}) as { id?: string; stage?: string; detail?: string; url?: string; message?: string };
      if (evt.name === 'office:standupAudioProgress') {
        const label =
          p.stage === 'voicing'
            ? `Voicing ${p.detail ?? ''}…`.trim()
            : 'Summarizing…';
        setAudioStage(label);
      } else if (evt.name === 'office:standupAudioReady') {
        setAudioStage(null);
        setAudioUrl((p.url ?? (p.id ? `/api/office/standup/${p.id}/audio` : null)) ?? null);
      } else {
        setAudioStage(null);
        toast.error(p.message || 'Audio generation failed');
      }
    });
    return () => sub.close();
  }, []);

  const generateMut = useMutation({
    mutationFn: () =>
      fetchJSON<{ id: string }>(`/api/office/standup/${encodeURIComponent(targetId ?? 'latest')}/audio`, {
        method: 'POST',
      }),
    onMutate: () => {
      setAudioStage('Summarizing…');
      setAudioNonce((n) => n + 1);
    },
    onError: (err: unknown) => {
      setAudioStage(null);
      const msg = err instanceof Error ? err.message : '';
      if (msg.toLowerCase().includes('openai') || msg.toLowerCase().includes('key')) {
        toast.error('Configure your OpenAI key in Settings');
      } else {
        toast.error('Could not start audio generation');
      }
    },
  });

  const [text, setText] = useState('');
  const [asTask, setAsTask] = useState(false);
  const transcript = data?.transcript ?? [];

  const [transcriptRef] = useAutoAnimate<HTMLDivElement>();

  const submit = () => {
    const content = text.trim();
    if (!content) return;
    sendMut.mutate(
      { content, asTask },
      {
        onSuccess: () => {
          setText('');
          toast.success(asTask ? 'Task injected' : 'Message sent');
        },
      },
    );
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
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Button
            variant="secondary"
            disabled={!completed || keyMissing || !!audioStage || generateMut.isPending}
            title={keyMissing ? 'Configure your OpenAI key in Settings' : (completed ? undefined : 'Available after standup ends')}
            onClick={() => generateMut.mutate()}
          >
            {audioStage ?? (audioUrl ? 'Regenerate audio summary' : 'Generate audio summary')}
          </Button>
          <Button
            variant={active ? 'danger' : 'primary'}
            onClick={() => (active ? stopMut.mutate() : startMut.mutate())}
            disabled={startMut.isPending || stopMut.isPending}
          >
            {active
              ? stopMut.isPending
                ? 'Stopping…'
                : 'End standup'
              : startMut.isPending
                ? 'Starting…'
                : 'Start standup'}
          </Button>
        </div>
      </header>

      {/* Audio player */}
      {audioUrl ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <audio controls src={`${audioUrl}?t=${audioNonce}`} style={{ width: '100%' }} />
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Voices are AI-generated.</span>
        </div>
      ) : null}

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
            <Metric value={transcript.length} size={11} pulseOnChange /> turns
          </span>
        </header>
        <div
          ref={transcriptRef}
          style={{
            flex: 1,
            overflowY: 'auto',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
            paddingRight: 4,
          }}
        >
          {standupQ.isLoading ? (
            <TranscriptSkeleton />
          ) : transcript.length === 0 ? (
            <EmptyTranscript active={active} />
          ) : (
            transcript.map((entry, i) => (
              <TranscriptTurn
                key={`${entry.sessionId}-${entry.timestamp}-${i}`}
                entry={entry}
                name={memberLookup.get(entry.sessionId)?.name ?? entry.speakerName ?? entry.sessionId}
                isCurrent={data?.currentSpeakerId === entry.sessionId}
              />
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
            <Toggle label="/task" value={asTask} onChange={setAsTask} />
          </div>
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value.slice(0, 500))}
            placeholder={asTask ? 'Describe a task to assign…' : 'Type a message…'}
            rows={2}
            style={{ resize: 'vertical' }}
          />
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8 }}>
            <span style={{ fontSize: 11, color: text.length > 480 ? 'var(--status-warn)' : 'var(--text-muted)' }}>
              <Metric value={text.length} size={11} /> / 500
            </span>
            <Button onClick={submit} disabled={sendMut.isPending || !text.trim()}>
              {sendMut.isPending ? 'Sending…' : 'Send'}
            </Button>
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
  const reduce = useReducedMotion();
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
            {/* The active speaker's avatar pulses a violet halo so the eye
                jumps to whoever is talking (gated by reduced motion). */}
            <motion.div
              animate={
                isCurrent && !reduce
                  ? {
                      boxShadow: [
                        '0 0 14px rgba(34,197,94,0.35)',
                        '0 0 26px rgba(34,197,94,0.65)',
                        '0 0 14px rgba(34,197,94,0.35)',
                      ],
                    }
                  : { boxShadow: isCurrent ? '0 0 18px rgba(34,197,94,0.4)' : '0 0 0 rgba(0,0,0,0)' }
              }
              transition={
                isCurrent && !reduce
                  ? { duration: 2.4, repeat: Infinity, ease: 'easeInOut' }
                  : { duration: 0.3 }
              }
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
              }}
            >
              {m?.emoji || m?.name?.[0] || '?'}
            </motion.div>
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
    <GlowCard
      live={isCurrent}
      className="flex flex-col gap-1 p-3"
      style={{
        background: isCurrent ? 'var(--accent-muted)' : 'var(--bg-elevated)',
        border: '1px solid ' + (isCurrent ? 'var(--accent)' : 'var(--border-subtle)'),
        borderRadius: 'var(--radius-md)',
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
    </GlowCard>
  );
}

function TranscriptSkeleton() {
  return (
    <div className="flex flex-col gap-2.5">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="flex flex-col gap-2 p-3"
          style={{
            background: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-md)',
          }}
        >
          <Skeleton className="h-3 w-28" />
          <Skeleton className="h-2.5 w-full" />
          <Skeleton className="h-2.5 w-2/3" />
        </div>
      ))}
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
