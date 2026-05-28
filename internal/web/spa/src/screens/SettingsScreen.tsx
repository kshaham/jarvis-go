import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { Metric, Press, StatusDot } from '../components/motion';
import { SnapshotsModal } from '../components/SnapshotsModal';
import { fetchJSON } from '../api/client';
import { useOfficeStatus } from '../hooks/useOfficeStatus';
import { useBrowserHealth } from '../hooks/useOfficeData';
import type { OfficeSettings } from '../api/types';

// SettingsScreen — operator controls. Mission editor, team lifecycle
// (pause / resume / shutdown), brainstorm trigger, link to identity +
// peer registry. Read settings via /api/office/settings, mutate via
// PUT /api/office/settings.
export function SettingsScreen() {
  const status = useOfficeStatus();
  const qc = useQueryClient();

  const settings = useQuery<OfficeSettings>({
    queryKey: ['office', 'settings'],
    queryFn: ({ signal }) => fetchJSON<OfficeSettings>('/api/office/settings', { signal }),
    staleTime: 30_000,
  });

  const [mission, setMission] = useState('');
  const [hydratedFor, setHydratedFor] = useState<string | null>(null);
  // Adjust-state-during-render: hydrate the form from the loaded
  // settings once. Repeated invalidations don't blow away the
  // operator's edits because hydratedFor pins the previous load.
  if (settings.data && hydratedFor !== (settings.data.mission ?? '')) {
    setMission(settings.data.mission ?? '');
    setHydratedFor(settings.data.mission ?? '');
  }

  const putSettings = useMutation({
    mutationFn: (patch: Partial<OfficeSettings>) =>
      fetchJSON('/api/office/settings', { method: 'PUT', body: patch }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'settings'] });
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
    },
  });
  const pause = useMutation({
    mutationFn: (reason: string) =>
      fetchJSON('/api/office/pause', { method: 'POST', body: { reason } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'status'] }),
  });
  const resume = useMutation({
    mutationFn: () => fetchJSON('/api/office/resume', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'status'] }),
  });
  const shutdown = useMutation({
    mutationFn: () => fetchJSON('/api/office/shutdown-team', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['office', 'status'] }),
  });
  const brainstorm = useMutation({
    mutationFn: (prompt: string) =>
      fetchJSON('/api/office/brainstorm', { method: 'POST', body: { prompt } }),
  });

  const teamSpawned = !!status.data?.teamSpawned;
  const paused = !!status.data?.paused;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18, maxWidth: 880 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Settings
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Mission, team lifecycle, and quick actions.
        </p>
      </header>

      {/* Mission */}
      <Section title="Mission" hint="Shown to every agent in the team's system prompt.">
        <textarea
          value={mission}
          onChange={(e) => setMission(e.target.value)}
          rows={5}
          placeholder="What is this team working on?"
          style={{
            padding: 10,
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
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
          <Press
            pressScale={0.96}
            onClick={() => putSettings.mutate({ mission })}
            disabled={putSettings.isPending}
          >
            <span
              style={{
                padding: '8px 14px',
                background: 'var(--accent)',
                color: '#fff',
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: putSettings.isPending ? 0.6 : 1,
              }}
            >
              {putSettings.isPending ? 'Saving…' : 'Save mission'}
            </span>
          </Press>
        </div>
      </Section>

      {/* Team lifecycle */}
      <Section
        title="Team lifecycle"
        hint={
          teamSpawned
            ? paused
              ? 'Team is paused. Resume to let agents take turns again.'
              : 'Team is live. Pause to freeze every agent at their current turn.'
            : 'No team spawned. Use the classic UI for spawn (port pending).'
        }
      >
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          <Press
            pressScale={0.96}
            disabled={!teamSpawned || paused || pause.isPending}
            onClick={() => pause.mutate('manual pause from settings')}
          >
            <span
              style={{
                padding: '10px 16px',
                background: 'var(--bg-pressed)',
                border: '1px solid var(--border-default)',
                borderRadius: 'var(--radius-md)',
                color: 'var(--text-primary)',
                fontSize: 13,
                fontWeight: 600,
                opacity: !teamSpawned || paused || pause.isPending ? 0.4 : 1,
              }}
            >
              Pause team
            </span>
          </Press>
          <Press
            pressScale={0.96}
            disabled={!paused || resume.isPending}
            onClick={() => resume.mutate()}
          >
            <span
              style={{
                padding: '10px 16px',
                background: paused ? 'var(--accent)' : 'var(--bg-pressed)',
                color: paused ? '#fff' : 'var(--text-muted)',
                border: '1px solid ' + (paused ? 'var(--accent-strong)' : 'var(--border-default)'),
                borderRadius: 'var(--radius-md)',
                fontSize: 13,
                fontWeight: 600,
                opacity: !paused || resume.isPending ? 0.6 : 1,
              }}
            >
              Resume team
            </span>
          </Press>
          <Press
            pressScale={0.96}
            disabled={!teamSpawned || shutdown.isPending}
            onClick={() => {
              if (window.confirm('Shut down team? Every agent is killed; the office is reset.')) {
                shutdown.mutate();
              }
            }}
          >
            <span
              style={{
                padding: '10px 16px',
                background: 'transparent',
                border: '1px solid var(--status-error)',
                borderRadius: 'var(--radius-md)',
                color: 'var(--status-error)',
                fontSize: 13,
                fontWeight: 600,
                opacity: !teamSpawned || shutdown.isPending ? 0.4 : 1,
              }}
            >
              {shutdown.isPending ? 'Shutting down…' : 'Shut down'}
            </span>
          </Press>
          <a href="/office-classic#spawn" style={{ textDecoration: 'none' }}>
            <Press pressScale={0.96}>
              <span
                style={{
                  padding: '10px 16px',
                  background: 'var(--accent)',
                  color: '#fff',
                  borderRadius: 'var(--radius-md)',
                  fontSize: 13,
                  fontWeight: 600,
                  display: 'inline-block',
                }}
              >
                + Spawn (classic)
              </span>
            </Press>
          </a>
        </div>
      </Section>

      {/* Brainstorm */}
      <Section
        title="Brainstorm"
        hint="Kick off an ad-hoc brainstorm. Every agent contributes one idea, then they cross-evaluate."
      >
        <BrainstormForm onSubmit={(prompt) => brainstorm.mutate(prompt)} pending={brainstorm.isPending} />
      </Section>

      {/* Advanced settings */}
      <AdvancedSections settings={settings.data} onSave={(patch) => putSettings.mutate(patch)} pending={putSettings.isPending} />

      {/* Browser health pill */}
      <BrowserHealthRow />

      {/* Snapshots */}
      <SnapshotsRow />

      {/* Quick links */}
      <Section title="Identity + peers" hint="Inter-team relay configuration.">
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          <Link to="/teams" style={{ textDecoration: 'none' }}>
            <Press pressScale={0.96}>
              <span style={navLinkStyle}>Teams (peer registry)</span>
            </Press>
          </Link>
          <Link to="/relay" style={{ textDecoration: 'none' }}>
            <Press pressScale={0.96}>
              <span style={navLinkStyle}>Relay</span>
            </Press>
          </Link>
        </div>
      </Section>

      {/* Status stats */}
      <Section title="Now">
        <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
          <Stat label="Members" value={status.data?.members?.length ?? 0} />
          <Stat label="Working" value={(status.data?.members ?? []).filter((m) => m.status === 'working').length} tone="success" />
          <Stat label="Stuck" value={(status.data?.members ?? []).filter((m) => m.status === 'stuck').length} tone={(status.data?.members ?? []).filter((m) => m.status === 'stuck').length > 0 ? 'danger' : 'muted'} />
        </div>
      </Section>
    </section>
  );
}

function BrainstormForm({
  onSubmit,
  pending,
}: {
  onSubmit: (prompt: string) => void;
  pending: boolean;
}) {
  const [prompt, setPrompt] = useState('');
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <input
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        placeholder="Brainstorm prompt (e.g. 'How should we approach the auth migration?')"
        style={{
          padding: 10,
          background: 'var(--bg-app)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-md)',
          color: 'var(--text-primary)',
          fontSize: 13,
          outline: 'none',
        }}
      />
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Press
          pressScale={0.96}
          onClick={() => prompt.trim() && onSubmit(prompt.trim())}
          disabled={pending || !prompt.trim()}
        >
          <span
            style={{
              padding: '8px 14px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
              opacity: pending || !prompt.trim() ? 0.5 : 1,
            }}
          >
            {pending ? 'Brainstorming…' : 'Start brainstorm'}
          </span>
        </Press>
      </div>
    </div>
  );
}

function Section({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <section
      style={{
        padding: 16,
        background: 'var(--bg-surface)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-card)',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
      }}
    >
      <header>
        <h2 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>
          {title}
        </h2>
        {hint ? (
          <p style={{ margin: '2px 0 0', fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.4 }}>
            {hint}
          </p>
        ) : null}
      </header>
      {children}
    </section>
  );
}

function Stat({
  label,
  value,
  tone = 'primary',
}: {
  label: string;
  value: number;
  tone?: 'primary' | 'muted' | 'success' | 'danger';
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.4 }}>
        {label}
      </span>
      <Metric value={value} size={20} tone={tone} />
    </div>
  );
}

const navLinkStyle: React.CSSProperties = {
  padding: '8px 14px',
  background: 'var(--bg-pressed)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  fontWeight: 600,
  display: 'inline-block',
};

// AdvancedSections renders the auto-standup / brainstorm / idle /
// meetings / file-locks form blocks. Each commits independently via
// the shared putSettings mutation so a save only touches one section
// at a time.
function AdvancedSections({
  settings,
  onSave,
  pending,
}: {
  settings: OfficeSettings | undefined;
  onSave: (patch: Partial<OfficeSettings>) => void;
  pending: boolean;
}) {
  // Per-section local state, hydrated from server data on first load.
  const [autoStandupEnabled, setAutoStandupEnabled] = useState(false);
  const [autoStandupHours, setAutoStandupHours] = useState(1);
  const [brainstormCooldown, setBrainstormCooldown] = useState(15);
  const [brainstormTimeout, setBrainstormTimeout] = useState(120);
  const [idleEnabled, setIdleEnabled] = useState(true);
  const [idleThreshold, setIdleThreshold] = useState(10);
  const [meetingInactivity, setMeetingInactivity] = useState(60);
  const [meetingStandupTimeout, setMeetingStandupTimeout] = useState(120);
  const [lockExpiry, setLockExpiry] = useState(15);

  // One-shot hydration on first non-empty server response.
  const [hydrated, setHydrated] = useState(false);
  if (settings && !hydrated) {
    setAutoStandupEnabled(!!settings.autoStandup?.enabled);
    setAutoStandupHours(
      settings.autoStandup?.intervalHours ??
        (settings.autoStandup?.intervalMinutes ? settings.autoStandup.intervalMinutes / 60 : 1),
    );
    setBrainstormCooldown(settings.brainstorm?.cooldownMinutes ?? 15);
    setBrainstormTimeout(settings.brainstorm?.responseTimeoutSeconds ?? 120);
    setIdleEnabled(settings.idleDetection?.enabled ?? true);
    setIdleThreshold(settings.idleDetection?.thresholdMinutes ?? 10);
    setMeetingInactivity(settings.meetings?.inactivityTimeoutSeconds ?? 60);
    setMeetingStandupTimeout(settings.meetings?.standupResponseTimeoutSeconds ?? 120);
    setLockExpiry(settings.fileLocks?.lockExpiryMinutes ?? 15);
    setHydrated(true);
  }

  return (
    <>
      <Section
        title="Auto-standup"
        hint="When enabled, jarvis calls a standup every N hours so the team stays synced."
      >
        <FormRow>
          <Checkbox label="Enabled" value={autoStandupEnabled} onChange={setAutoStandupEnabled} />
          <NumField
            label="Interval (hours)"
            value={autoStandupHours}
            onChange={setAutoStandupHours}
            min={0.5}
            max={24}
            step={0.5}
          />
        </FormRow>
        <SaveBar
          pending={pending}
          onSave={() =>
            onSave({
              autoStandup: {
                enabled: autoStandupEnabled,
                intervalHours: autoStandupHours,
              },
            })
          }
        />
      </Section>

      <Section title="Brainstorm" hint="How long a brainstorm waits + how often a brand-new one can run.">
        <FormRow>
          <NumField
            label="Cooldown (min)"
            value={brainstormCooldown}
            onChange={setBrainstormCooldown}
            min={1}
            max={120}
          />
          <NumField
            label="Response timeout (sec)"
            value={brainstormTimeout}
            onChange={setBrainstormTimeout}
            min={30}
            max={600}
          />
        </FormRow>
        <SaveBar
          pending={pending}
          onSave={() =>
            onSave({
              brainstorm: {
                cooldownMinutes: brainstormCooldown,
                responseTimeoutSeconds: brainstormTimeout,
              },
            })
          }
        />
      </Section>

      <Section title="Idle detection" hint="Flag an agent as idle if they haven't taken a turn in this many minutes.">
        <FormRow>
          <Checkbox label="Enabled" value={idleEnabled} onChange={setIdleEnabled} />
          <NumField
            label="Threshold (min)"
            value={idleThreshold}
            onChange={setIdleThreshold}
            min={1}
            max={60}
          />
        </FormRow>
        <SaveBar
          pending={pending}
          onSave={() =>
            onSave({
              idleDetection: { enabled: idleEnabled, thresholdMinutes: idleThreshold },
            })
          }
        />
      </Section>

      <Section title="Meetings">
        <FormRow>
          <NumField
            label="Inactivity timeout (sec)"
            value={meetingInactivity}
            onChange={setMeetingInactivity}
            min={10}
            max={600}
          />
          <NumField
            label="Standup response timeout (sec)"
            value={meetingStandupTimeout}
            onChange={setMeetingStandupTimeout}
            min={30}
            max={600}
          />
        </FormRow>
        <SaveBar
          pending={pending}
          onSave={() =>
            onSave({
              meetings: {
                inactivityTimeoutSeconds: meetingInactivity,
                standupResponseTimeoutSeconds: meetingStandupTimeout,
              },
            })
          }
        />
      </Section>

      <Section
        title="File-lock policy"
        hint="How long an agent's file lock survives without being touched."
      >
        <FormRow>
          <NumField
            label="Lock expiry (min)"
            value={lockExpiry}
            onChange={setLockExpiry}
            min={1}
            max={120}
          />
        </FormRow>
        <SaveBar
          pending={pending}
          onSave={() =>
            onSave({
              fileLocks: { lockExpiryMinutes: lockExpiry },
            })
          }
        />
      </Section>
    </>
  );
}

function BrowserHealthRow() {
  const bh = useBrowserHealth();
  const ok = bh.data?.ok;
  const tone: 'working' | 'stuck' | 'idle' =
    ok === true ? 'working' : ok === false ? 'stuck' : 'idle';
  return (
    <Section title="Browser health" hint="The sibling browser-automation container the team uses for live tests.">
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <StatusDot status={tone} size={8} />
        <span style={{ fontSize: 13, color: 'var(--text-primary)', fontWeight: 600 }}>
          {bh.data?.status ?? 'unknown'}
        </span>
        {bh.data?.message ? (
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{bh.data.message}</span>
        ) : null}
      </div>
    </Section>
  );
}

function SnapshotsRow() {
  const [open, setOpen] = useState(false);
  return (
    <Section title="Snapshots" hint="Save the current team's state so you can restore it later.">
      <Press pressScale={0.96} onClick={() => setOpen(true)}>
        <span style={navLinkStyle}>Open snapshots</span>
      </Press>
      <SnapshotsModal open={open} onClose={() => setOpen(false)} />
    </Section>
  );
}

function FormRow({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
        gap: 14,
      }}
    >
      {children}
    </div>
  );
}

function NumField({
  label,
  value,
  onChange,
  min,
  max,
  step,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <input
        type="number"
        value={value}
        min={min}
        max={max}
        step={step ?? 1}
        onChange={(e) => onChange(Number(e.target.value))}
        style={{
          padding: 8,
          background: 'var(--bg-app)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-md)',
          color: 'var(--text-primary)',
          fontSize: 13,
          outline: 'none',
        }}
      />
    </label>
  );
}

function Checkbox({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (b: boolean) => void;
}) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-primary)' }}>
        <input type="checkbox" checked={value} onChange={(e) => onChange(e.target.checked)} />
        {value ? 'On' : 'Off'}
      </span>
    </label>
  );
}

function SaveBar({ onSave, pending }: { onSave: () => void; pending: boolean }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
      <Press pressScale={0.96} onClick={onSave} disabled={pending}>
        <span
          style={{
            padding: '8px 14px',
            background: 'var(--accent)',
            color: '#fff',
            borderRadius: 'var(--radius-md)',
            fontSize: 13,
            fontWeight: 600,
            opacity: pending ? 0.6 : 1,
          }}
        >
          {pending ? 'Saving…' : 'Save'}
        </span>
      </Press>
    </div>
  );
}
