import { useRef, useState } from 'react';
import { motion, useReducedMotion } from 'motion/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Modal } from './motion/Modal';
import { Metric, Press, StatusDot } from './motion';
import { agentAvatarClass, agentAvatarGlyph } from './AgentCard';
import { cn } from '../lib/cn';
import { fetchJSON } from '../api/client';
import { relativeTime } from '../lib/relativeTime';
import type { OfficeMember } from '../api/types';
import { VOICES } from '../lib/voices';

type Tab = 'overview' | 'personality' | 'send' | 'lifecycle';

type Props = {
  member: OfficeMember | null;
  onClose: () => void;
};

// MemberDetailModal — opens when the operator taps an AgentCard.
// `lg` (840px) modal with four tabs:
//   - Overview: status, working dir, current task, recent activity
//   - Personality: edit Character + Role Instructions; PUT to /api/office/member/{sessionId}/personality
//   - Send: inject a prompt straight into the agent's session
//   - Lifecycle: restart, kill, change working dir
//
// Mutations invalidate the office status cache so the underlying card
// reflects the new state on close.
export function MemberDetailModal({ member, onClose }: Props) {
  const open = !!member;
  const reduce = useReducedMotion();
  const [tab, setTab] = useState<Tab>('overview');
  // Reset tab when the modal reopens for a fresh member. Track previous
  // member key in a ref (never rendered) — the parent's state change
  // already triggers this render so no extra re-render cycle is needed.
  const prevMemberKeyRef = useRef<string | null>(null);
  const memberKey = member?.sessionId ?? null;
  if (memberKey !== prevMemberKeyRef.current) {
    prevMemberKeyRef.current = memberKey;
    if (tab !== 'overview') setTab('overview');
  }
  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="lg"
      hideHeader
      glow={member?.status === 'working'}
      title={member?.name ?? 'Agent'}
      description={member?.role}
    >
      {member ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: '18px 22px' }}>
          {/* Header — the avatar is the shared-element morph target: it
              flies in from the clicked AgentCard. */}
          <header style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            <motion.div
              layoutId={reduce ? undefined : `agent-avatar-${member.sessionId}`}
              className={cn(
                'flex h-12 w-12 shrink-0 items-center justify-center rounded-xl text-lg font-semibold',
                agentAvatarClass(member.avatarColor),
              )}
            >
              {agentAvatarGlyph(member)}
            </motion.div>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>
                {member.name}
              </div>
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>{member.role}</div>
            </div>
          </header>
          <TabBar tab={tab} setTab={setTab} />
          {tab === 'overview' && <OverviewTab member={member} />}
          {tab === 'personality' && <PersonalityTab member={member} onSaved={onClose} />}
          {tab === 'send' && <SendTab member={member} />}
          {tab === 'lifecycle' && <LifecycleTab member={member} onAfter={onClose} />}
        </div>
      ) : null}
    </Modal>
  );
}

function TabBar({ tab, setTab }: { tab: Tab; setTab: (t: Tab) => void }) {
  const items: { id: Tab; label: string }[] = [
    { id: 'overview', label: 'Overview' },
    { id: 'personality', label: 'Personality' },
    { id: 'send', label: 'Send' },
    { id: 'lifecycle', label: 'Lifecycle' },
  ];
  return (
    <div style={{ display: 'flex', gap: 4, marginBottom: 14, borderBottom: '1px solid var(--border-subtle)' }}>
      {items.map((it) => {
        const active = tab === it.id;
        return (
          <button
            key={it.id}
            onClick={() => setTab(it.id)}
            style={{
              padding: '8px 14px',
              background: 'transparent',
              border: 'none',
              borderBottom: '2px solid ' + (active ? 'var(--accent)' : 'transparent'),
              color: active ? 'var(--accent)' : 'var(--text-muted)',
              fontSize: 13,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            {it.label}
          </button>
        );
      })}
    </div>
  );
}

function OverviewTab({ member }: { member: OfficeMember }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <Row label="Status">
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
          <StatusDot status={member.status} size={8} />
          <span style={{ color: 'var(--text-primary)', fontWeight: 600, textTransform: 'capitalize' }}>
            {member.status}
          </span>
        </span>
      </Row>
      <Row label="Working directory">
        <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>
          {member.workingDir || '—'}
        </code>
      </Row>
      <Row label="Total cost">
        <Metric
          value={member.totalCost ?? 0}
          format={{ style: 'currency', currency: 'USD', minimumFractionDigits: 2 }}
          size={15}
        />
      </Row>
      <Row label="Last activity">
        <span style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
          {member.lastActivityAt ? relativeTime(new Date(member.lastActivityAt)) : '—'}
        </span>
      </Row>
      {member.score?.grade ? (
        <Row label="Grade">
          <span
            style={{
              display: 'inline-block',
              padding: '2px 10px',
              background: 'var(--bg-pressed)',
              borderRadius: 'var(--radius-sm)',
              color: 'var(--text-primary)',
              fontWeight: 700,
              fontSize: 12,
            }}
          >
            {member.score.grade}
          </span>
        </Row>
      ) : null}
    </div>
  );
}

function PersonalityTab({ member, onSaved }: { member: OfficeMember; onSaved: () => void }) {
  const qc = useQueryClient();
  const [character, setCharacter] = useState('');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [voice, setVoice] = useState(member.voice ?? '');
  const save = useMutation({
    mutationFn: () =>
      fetchJSON(`/api/office/member/${encodeURIComponent(member.sessionId)}/personality`, {
        method: 'PUT',
        body: { personality: character, systemPrompt },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      onSaved();
    },
  });
  const saveVoice = useMutation({
    mutationFn: (v: string) =>
      fetchJSON(`/api/office/member/${encodeURIComponent(member.sessionId)}/voice`, {
        method: 'PUT',
        body: { voice: v },
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'status'] }),
    onError: () => {
      setVoice(member.voice ?? '');
    },
  });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Field label="Voice" hint="Voice used for this agent in standup audio summaries.">
        <select
          value={voice}
          onChange={(e) => {
            setVoice(e.target.value);
            saveVoice.mutate(e.target.value);
          }}
          style={selectStyle}
        >
          <option value="" disabled>
            Choose a voice…
          </option>
          {VOICES.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </Field>
      <Field label="Character" hint="Free-form personality notes the agent sees in its system prompt.">
        <textarea
          value={character}
          onChange={(e) => setCharacter(e.target.value)}
          rows={4}
          placeholder="e.g. precise, sceptical, asks for proof before agreeing"
          style={textareaStyle}
        />
      </Field>
      <Field label="Role instructions" hint="Optional: extra direction layered on top of the team mission.">
        <textarea
          value={systemPrompt}
          onChange={(e) => setSystemPrompt(e.target.value)}
          rows={4}
          placeholder="e.g. always cite the file:line you changed and keep diffs minimal."
          style={textareaStyle}
        />
      </Field>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Press pressScale={0.96} onClick={() => save.mutate()} disabled={save.isPending}>
          <span
            style={{
              padding: '8px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
              opacity: save.isPending ? 0.6 : 1,
            }}
          >
            {save.isPending ? 'Saving…' : 'Save personality'}
          </span>
        </Press>
      </div>
    </div>
  );
}

function SendTab({ member }: { member: OfficeMember }) {
  const [prompt, setPrompt] = useState('');
  const inject = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/inject', {
        method: 'POST',
        body: { sessionId: member.sessionId, prompt },
      }),
    onSuccess: () => setPrompt(''),
  });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Field label="Inject prompt" hint="Goes straight into this agent's PTY at the next turn boundary.">
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={6}
          placeholder="What should this agent do?"
          style={textareaStyle}
        />
      </Field>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Press
          pressScale={0.96}
          onClick={() => prompt.trim() && inject.mutate()}
          disabled={inject.isPending || !prompt.trim()}
        >
          <span
            style={{
              padding: '8px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
              opacity: inject.isPending || !prompt.trim() ? 0.5 : 1,
            }}
          >
            {inject.isPending ? 'Injecting…' : 'Inject'}
          </span>
        </Press>
      </div>
    </div>
  );
}

function LifecycleTab({ member, onAfter }: { member: OfficeMember; onAfter: () => void }) {
  const qc = useQueryClient();
  const [workdir, setWorkdir] = useState('');
  const restart = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/restart', { method: 'POST', body: { sessionId: member.sessionId } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      onAfter();
    },
  });
  const kill = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/kill', { method: 'POST', body: { sessionId: member.sessionId } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      onAfter();
    },
  });
  const setDir = useMutation({
    mutationFn: () =>
      fetchJSON('/api/office/agent/workdir', {
        method: 'POST',
        body: { sessionId: member.sessionId, workingDir: workdir },
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      setWorkdir('');
    },
  });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Field label="Change working directory" hint="The agent restarts inside the new directory.">
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            value={workdir}
            onChange={(e) => setWorkdir(e.target.value)}
            placeholder={member.workingDir || '/path/to/dir'}
            style={{ ...textareaStyle, flex: 1 }}
          />
          <Press
            pressScale={0.96}
            onClick={() => workdir.trim() && setDir.mutate()}
            disabled={setDir.isPending || !workdir.trim()}
          >
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-md)',
                color: 'var(--text-primary)',
                fontSize: 13,
                fontWeight: 600,
                opacity: setDir.isPending || !workdir.trim() ? 0.5 : 1,
              }}
            >
              Set
            </span>
          </Press>
        </div>
      </Field>
      <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
        <Press
          pressScale={0.96}
          onClick={() => {
            if (window.confirm('Restart this agent? Its current turn aborts.')) restart.mutate();
          }}
          disabled={restart.isPending}
        >
          <span
            style={{
              padding: '8px 14px',
              background: 'var(--bg-pressed)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-primary)',
              fontSize: 13,
              fontWeight: 600,
            }}
          >
            {restart.isPending ? 'Restarting…' : 'Restart'}
          </span>
        </Press>
        <Press
          pressScale={0.96}
          onClick={() => {
            if (window.confirm('Kill this agent? Their session ends and tasks reassign.')) kill.mutate();
          }}
          disabled={kill.isPending}
        >
          <span
            style={{
              padding: '8px 14px',
              background: 'transparent',
              border: '1px solid var(--status-error)',
              color: 'var(--status-error)',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
            }}
          >
            {kill.isPending ? 'Killing…' : 'Kill'}
          </span>
        </Press>
      </div>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '160px 1fr',
        gap: 14,
        alignItems: 'center',
      }}
    >
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span style={{ minWidth: 0 }}>{children}</span>
    </div>
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
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      {children}
      {hint ? <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span> : null}
    </label>
  );
}

const textareaStyle: React.CSSProperties = {
  padding: 10,
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  resize: 'vertical',
  fontFamily: 'inherit',
};

const selectStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 10px',
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  fontFamily: 'inherit',
};
