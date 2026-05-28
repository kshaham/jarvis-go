import { useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { Metric, Press } from '../components/motion';
import { TemplateCard } from '../components/spawn/TemplateCard';
import { fetchJSON } from '../api/client';
import { useTeamTemplates } from '../hooks/useOfficeData';
import type { TeamTemplate, TeamTemplateMember } from '../api/types';

type Step = 'template' | 'configure' | 'review';

// Per-member overrides the operator can edit in the Configure step.
// Only the fields we expose; the rest fall through to the template's
// default. Mirrors the subset of spawnpayload.MemberOverride that
// matters for the common case — full fidelity is reachable via the
// template editor in the classic UI (not yet ported).
type MemberEdit = {
  id: string;
  name: string;
  role: string;
  personality: string;
  systemPrompt: string;
  maxTurns?: number;
  provider?: string;
  model?: string;
};

// SpawnScreen — the wizard formerly known as the spawn-team modal.
// Three steps with a progress trail at the top.
export function SpawnScreen() {
  const templates = useTeamTemplates();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [step, setStep] = useState<Step>('template');
  const [templateId, setTemplateId] = useState<string>('');
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState<string>('');

  const tpl: TeamTemplate | undefined = useMemo(
    () => (templates.data?.templates ?? []).find((t) => t.id === templateId),
    [templates.data?.templates, templateId],
  );

  // Form state
  const [mission, setMission] = useState('');
  const [workingDir, setWorkingDir] = useState('');
  const [newProject, setNewProject] = useState(false);
  const [projectName, setProjectName] = useState('');
  const [perpetual, setPerpetual] = useState(true);
  const [defaultProvider, setDefaultProvider] = useState('claude-subscription');
  const [defaultModel, setDefaultModel] = useState('');
  const [telegramEnabled, setTelegramEnabled] = useState(false);
  const [telegramInterval, setTelegramInterval] = useState(60);
  const [members, setMembers] = useState<MemberEdit[]>([]);

  // When the operator picks a template, hydrate the per-member
  // overrides + the team-wide defaults from the template.
  const [hydratedFor, setHydratedFor] = useState<string | null>(null);
  if (tpl && hydratedFor !== tpl.id) {
    setMembers(
      tpl.members.map((m: TeamTemplateMember, i) => ({
        id: m.id ?? `${tpl.id}-${i}`,
        name: m.name,
        role: m.role,
        personality: m.personality ?? '',
        systemPrompt: m.systemPrompt ?? '',
        maxTurns: m.maxTurns,
        provider: m.provider,
        model: m.model,
      })),
    );
    if (tpl.defaultProvider) setDefaultProvider(tpl.defaultProvider);
    if (tpl.defaultModel) setDefaultModel(tpl.defaultModel);
    if (typeof tpl.perpetualImprovement === 'boolean') setPerpetual(tpl.perpetualImprovement);
    if (tpl.telegram) {
      setTelegramEnabled(!!tpl.telegram.enabled);
      setTelegramInterval(tpl.telegram.intervalMinutes ?? 60);
    }
    setHydratedFor(tpl.id);
  }

  const filteredTemplates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (templates.data?.templates ?? []).filter((t) => {
      if (category && t.category !== category) return false;
      if (q) {
        const hay = `${t.name} ${t.description ?? ''}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [templates.data?.templates, query, category]);

  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const t of templates.data?.templates ?? []) {
      if (t.category) set.add(t.category);
    }
    return Array.from(set);
  }, [templates.data?.templates]);

  const spawn = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = {
        templateId,
        mission,
        workingDir: newProject ? '' : workingDir,
        newProject,
        projectName: newProject ? projectName : '',
        perpetualMode: perpetual,
        telegramEnabled,
        telegramIntervalMin: telegramInterval,
        memberOverrides: members.map((m) => ({
          id: m.id,
          name: m.name,
          role: m.role,
          personality: m.personality,
          systemPrompt: m.systemPrompt,
          maxTurns: m.maxTurns ?? 0,
          provider: m.provider ?? defaultProvider,
          model: m.model ?? defaultModel,
        })),
      };
      return fetchJSON('/api/office/spawn-team', { method: 'POST', body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      navigate('/');
    },
  });

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Spawn team
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Pick a template, configure the mission and members, then spawn.
        </p>
      </header>

      <StepTrail step={step} setStep={setStep} hasTemplate={!!templateId} />

      {step === 'template' && (
        <TemplateStep
          query={query}
          setQuery={setQuery}
          category={category}
          setCategory={setCategory}
          categories={categories}
          templates={filteredTemplates}
          selected={templateId}
          onSelect={(id) => {
            setTemplateId(id);
            setStep('configure');
          }}
          loading={templates.isLoading}
        />
      )}

      {step === 'configure' && tpl && (
        <ConfigureStep
          tpl={tpl}
          mission={mission}
          setMission={setMission}
          workingDir={workingDir}
          setWorkingDir={setWorkingDir}
          newProject={newProject}
          setNewProject={setNewProject}
          projectName={projectName}
          setProjectName={setProjectName}
          perpetual={perpetual}
          setPerpetual={setPerpetual}
          defaultProvider={defaultProvider}
          setDefaultProvider={setDefaultProvider}
          defaultModel={defaultModel}
          setDefaultModel={setDefaultModel}
          telegramEnabled={telegramEnabled}
          setTelegramEnabled={setTelegramEnabled}
          telegramInterval={telegramInterval}
          setTelegramInterval={setTelegramInterval}
          members={members}
          setMembers={setMembers}
          onBack={() => setStep('template')}
          onNext={() => setStep('review')}
        />
      )}

      {step === 'review' && tpl && (
        <ReviewStep
          tpl={tpl}
          mission={mission}
          workingDir={workingDir}
          newProject={newProject}
          projectName={projectName}
          perpetual={perpetual}
          defaultProvider={defaultProvider}
          defaultModel={defaultModel}
          telegramEnabled={telegramEnabled}
          telegramInterval={telegramInterval}
          members={members}
          onBack={() => setStep('configure')}
          onSpawn={() => spawn.mutate()}
          pending={spawn.isPending}
          error={spawn.error instanceof Error ? spawn.error.message : null}
        />
      )}
    </section>
  );
}

function StepTrail({
  step,
  setStep,
  hasTemplate,
}: {
  step: Step;
  setStep: (s: Step) => void;
  hasTemplate: boolean;
}) {
  const items: { id: Step; label: string }[] = [
    { id: 'template', label: '1. Template' },
    { id: 'configure', label: '2. Configure' },
    { id: 'review', label: '3. Review' },
  ];
  return (
    <nav style={{ display: 'flex', gap: 6 }}>
      {items.map((it, i) => {
        const active = step === it.id;
        const allowed = i === 0 || hasTemplate;
        return (
          <button
            key={it.id}
            onClick={() => allowed && setStep(it.id)}
            disabled={!allowed}
            style={{
              padding: '8px 16px',
              background: active ? 'var(--accent-muted)' : 'var(--bg-surface)',
              border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
              borderRadius: 'var(--radius-md)',
              color: active ? 'var(--accent)' : 'var(--text-muted)',
              fontSize: 13,
              fontWeight: 600,
              cursor: allowed ? 'pointer' : 'not-allowed',
              opacity: allowed ? 1 : 0.4,
            }}
          >
            {it.label}
          </button>
        );
      })}
    </nav>
  );
}

function TemplateStep({
  query,
  setQuery,
  category,
  setCategory,
  categories,
  templates,
  selected,
  onSelect,
  loading,
}: {
  query: string;
  setQuery: (q: string) => void;
  category: string;
  setCategory: (c: string) => void;
  categories: string[];
  templates: TeamTemplate[];
  selected: string;
  onSelect: (id: string) => void;
  loading: boolean;
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
        gap: 14,
      }}
    >
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search templates…"
          style={{
            flex: '1 1 220px',
            padding: 8,
            background: 'var(--bg-app)',
            border: '1px solid var(--border-default)',
            borderRadius: 'var(--radius-md)',
            color: 'var(--text-primary)',
            fontSize: 13,
            outline: 'none',
          }}
        />
        <div style={{ display: 'flex', gap: 6 }}>
          {['', ...categories].map((cat) => {
            const active = cat === category;
            return (
              <Press key={cat || 'all'} pressScale={0.95} onClick={() => setCategory(cat)}>
                <span
                  style={{
                    padding: '6px 12px',
                    background: active ? 'var(--accent-muted)' : 'var(--bg-elevated)',
                    border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border-default)'),
                    borderRadius: 999,
                    color: active ? 'var(--accent)' : 'var(--text-muted)',
                    fontSize: 12,
                    fontWeight: 600,
                  }}
                >
                  {cat || 'All'}
                </span>
              </Press>
            );
          })}
        </div>
      </div>

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading templates…</div>
      ) : templates.length === 0 ? (
        <div style={{ color: 'var(--text-muted)', fontSize: 13, padding: 12 }}>
          No templates match.
        </div>
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
            gap: 10,
          }}
        >
          {templates.map((t) => (
            <TemplateCard
              key={t.id}
              tpl={t}
              selected={t.id === selected}
              onSelect={() => onSelect(t.id)}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function ConfigureStep(props: {
  tpl: TeamTemplate;
  mission: string;
  setMission: (s: string) => void;
  workingDir: string;
  setWorkingDir: (s: string) => void;
  newProject: boolean;
  setNewProject: (b: boolean) => void;
  projectName: string;
  setProjectName: (s: string) => void;
  perpetual: boolean;
  setPerpetual: (b: boolean) => void;
  defaultProvider: string;
  setDefaultProvider: (s: string) => void;
  defaultModel: string;
  setDefaultModel: (s: string) => void;
  telegramEnabled: boolean;
  setTelegramEnabled: (b: boolean) => void;
  telegramInterval: number;
  setTelegramInterval: (n: number) => void;
  members: MemberEdit[];
  setMembers: (m: MemberEdit[]) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  const {
    tpl,
    mission,
    setMission,
    workingDir,
    setWorkingDir,
    newProject,
    setNewProject,
    projectName,
    setProjectName,
    perpetual,
    setPerpetual,
    defaultProvider,
    setDefaultProvider,
    defaultModel,
    setDefaultModel,
    telegramEnabled,
    setTelegramEnabled,
    telegramInterval,
    setTelegramInterval,
    members,
    setMembers,
    onBack,
    onNext,
  } = props;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <SectionCard title="Mission" hint="What's this team working on? Up to 1000 chars.">
        <textarea
          value={mission}
          onChange={(e) => setMission(e.target.value.slice(0, 1000))}
          rows={4}
          placeholder="e.g. Migrate the auth service to the new identity provider."
          style={inputStyle}
        />
      </SectionCard>

      <SectionCard title="Working directory">
        <div style={{ display: 'flex', gap: 16, marginBottom: 8 }}>
          <label style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            <input
              type="radio"
              name="workdir-mode"
              checked={!newProject}
              onChange={() => setNewProject(false)}
              style={{ marginRight: 6 }}
            />
            Existing folder
          </label>
          <label style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            <input
              type="radio"
              name="workdir-mode"
              checked={newProject}
              onChange={() => setNewProject(true)}
              style={{ marginRight: 6 }}
            />
            New project
          </label>
        </div>
        {newProject ? (
          <input
            value={projectName}
            onChange={(e) => {
              const v = e.target.value.replace(/[^a-zA-Z0-9_-]/g, '');
              setProjectName(v.slice(0, 64));
            }}
            placeholder="project-name (alphanum / _ / -, ≤ 64 chars)"
            style={inputStyle}
          />
        ) : (
          <input
            value={workingDir}
            onChange={(e) => setWorkingDir(e.target.value)}
            placeholder="/path/to/repo"
            style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
          />
        )}
      </SectionCard>

      <SectionCard title="Defaults">
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
          <Field label="Provider">
            <select value={defaultProvider} onChange={(e) => setDefaultProvider(e.target.value)} style={inputStyle}>
              <option value="claude-subscription">Claude (subscription)</option>
              <option value="claude-api">Claude API</option>
              <option value="zai-glm">ZAI GLM</option>
            </select>
          </Field>
          <Field label="Model">
            <input
              value={defaultModel}
              onChange={(e) => setDefaultModel(e.target.value)}
              placeholder="auto"
              style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
            />
          </Field>
          <Toggle
            label="Perpetual improvement"
            value={perpetual}
            onChange={setPerpetual}
            hint="Auto-restart agents that finish their queue."
          />
          <Toggle
            label="Telegram notifications"
            value={telegramEnabled}
            onChange={setTelegramEnabled}
          />
          {telegramEnabled ? (
            <Field label="Telegram interval (min)">
              <select
                value={telegramInterval}
                onChange={(e) => setTelegramInterval(Number(e.target.value))}
                style={inputStyle}
              >
                {[30, 60, 120, 240].map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </Field>
          ) : null}
        </div>
      </SectionCard>

      <SectionCard
        title={`Members (${members.length})`}
        hint={`From template "${tpl.name}". Edit per-member overrides below.`}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {members.map((m, i) => (
            <MemberRow
              key={m.id}
              member={m}
              onChange={(next) => {
                const copy = [...members];
                copy[i] = next;
                setMembers(copy);
              }}
            />
          ))}
        </div>
      </SectionCard>

      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <Press pressScale={0.96} onClick={onBack}>
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
            ← Template
          </span>
        </Press>
        <Press
          pressScale={0.96}
          onClick={onNext}
          disabled={!mission.trim()}
        >
          <span
            style={{
              padding: '8px 16px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 13,
              fontWeight: 600,
              opacity: mission.trim() ? 1 : 0.5,
            }}
          >
            Review →
          </span>
        </Press>
      </div>
    </section>
  );
}

function MemberRow({
  member,
  onChange,
}: {
  member: MemberEdit;
  onChange: (next: MemberEdit) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <article
      style={{
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-md)',
        padding: 10,
      }}
    >
      <header
        onClick={() => setOpen((v) => !v)}
        style={{
          display: 'flex',
          gap: 10,
          alignItems: 'center',
          cursor: 'pointer',
        }}
      >
        <span style={{ flex: 1, color: 'var(--text-primary)', fontSize: 13, fontWeight: 600 }}>
          {member.name} · <span style={{ color: 'var(--text-muted)' }}>{member.role}</span>
        </span>
        <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{open ? '▾' : '▸'}</span>
      </header>
      {open ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 10 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 8 }}>
            <Field label="Name">
              <input
                value={member.name}
                onChange={(e) => onChange({ ...member, name: e.target.value })}
                style={inputStyle}
              />
            </Field>
            <Field label="Role">
              <input
                value={member.role}
                onChange={(e) => onChange({ ...member, role: e.target.value })}
                style={inputStyle}
              />
            </Field>
          </div>
          <Field label="Personality">
            <textarea
              value={member.personality}
              onChange={(e) => onChange({ ...member, personality: e.target.value })}
              rows={2}
              style={inputStyle}
            />
          </Field>
          <Field label="System prompt">
            <textarea
              value={member.systemPrompt}
              onChange={(e) => onChange({ ...member, systemPrompt: e.target.value })}
              rows={2}
              style={inputStyle}
            />
          </Field>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8 }}>
            <Field label="Max turns">
              <input
                type="number"
                value={member.maxTurns ?? ''}
                onChange={(e) =>
                  onChange({
                    ...member,
                    maxTurns: e.target.value ? Number(e.target.value) : undefined,
                  })
                }
                placeholder="default"
                style={inputStyle}
              />
            </Field>
            <Field label="Provider">
              <input
                value={member.provider ?? ''}
                onChange={(e) => onChange({ ...member, provider: e.target.value || undefined })}
                placeholder="(team default)"
                style={inputStyle}
              />
            </Field>
            <Field label="Model">
              <input
                value={member.model ?? ''}
                onChange={(e) => onChange({ ...member, model: e.target.value || undefined })}
                placeholder="(team default)"
                style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
              />
            </Field>
          </div>
        </div>
      ) : null}
    </article>
  );
}

function ReviewStep({
  tpl,
  mission,
  workingDir,
  newProject,
  projectName,
  perpetual,
  defaultProvider,
  defaultModel,
  telegramEnabled,
  telegramInterval,
  members,
  onBack,
  onSpawn,
  pending,
  error,
}: {
  tpl: TeamTemplate;
  mission: string;
  workingDir: string;
  newProject: boolean;
  projectName: string;
  perpetual: boolean;
  defaultProvider: string;
  defaultModel: string;
  telegramEnabled: boolean;
  telegramInterval: number;
  members: MemberEdit[];
  onBack: () => void;
  onSpawn: () => void;
  pending: boolean;
  error: string | null;
}) {
  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <SectionCard title="Summary">
        <ReviewRow label="Template" value={tpl.name} />
        <ReviewRow label="Mission" value={mission || '—'} preformatted />
        <ReviewRow label="Workdir" value={newProject ? `new ⇒ /app/workspace/${projectName}` : workingDir || '—'} />
        <ReviewRow label="Provider / model" value={`${defaultProvider}${defaultModel ? ' · ' + defaultModel : ''}`} />
        <ReviewRow label="Perpetual mode" value={perpetual ? 'on' : 'off'} />
        <ReviewRow label="Telegram" value={telegramEnabled ? `every ${telegramInterval}m` : 'off'} />
        <ReviewRow label="Members" value={<Metric value={members.length} size={13} />} />
      </SectionCard>
      <SectionCard title="Members">
        <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {members.map((m) => (
            <li
              key={m.id}
              style={{
                padding: 8,
                background: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
                borderRadius: 'var(--radius-sm)',
                fontSize: 12,
                color: 'var(--text-secondary)',
                display: 'flex',
                gap: 10,
              }}
            >
              <span style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{m.name}</span>
              <span>{m.role}</span>
              {m.model ? (
                <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
                  {m.model}
                </span>
              ) : null}
              {m.maxTurns ? <span>maxTurns: {m.maxTurns}</span> : null}
            </li>
          ))}
        </ul>
      </SectionCard>
      {error ? (
        <div
          style={{
            padding: 10,
            background: 'rgba(255,90,95,0.08)',
            border: '1px solid var(--status-error)',
            borderRadius: 'var(--radius-md)',
            color: 'var(--status-error)',
            fontSize: 12,
          }}
        >
          {error}
        </div>
      ) : null}
      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <Press pressScale={0.96} onClick={onBack}>
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
            ← Configure
          </span>
        </Press>
        <Press pressScale={0.96} onClick={onSpawn} disabled={pending}>
          <span
            style={{
              padding: '10px 18px',
              background: 'var(--accent)',
              color: '#fff',
              borderRadius: 'var(--radius-md)',
              fontSize: 14,
              fontWeight: 700,
              opacity: pending ? 0.6 : 1,
            }}
          >
            {pending ? 'Spawning…' : 'Spawn team'}
          </span>
        </Press>
      </div>
    </section>
  );
}

function SectionCard({
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
        padding: 14,
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
          <p style={{ margin: '2px 0 0', fontSize: 11, color: 'var(--text-muted)' }}>{hint}</p>
        ) : null}
      </header>
      {children}
    </section>
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
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      {children}
      {hint ? <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span> : null}
    </label>
  );
}

function Toggle({
  label,
  value,
  onChange,
  hint,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
  hint?: string;
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
      {hint ? <span style={{ fontSize: 11, color: 'var(--text-faint)', lineHeight: 1.4 }}>{hint}</span> : null}
    </label>
  );
}

function ReviewRow({
  label,
  value,
  preformatted,
}: {
  label: string;
  value: React.ReactNode;
  preformatted?: boolean;
}) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr', gap: 14, alignItems: 'baseline' }}>
      <span style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
        {label}
      </span>
      <span
        style={{
          fontSize: 13,
          color: 'var(--text-primary)',
          whiteSpace: preformatted ? 'pre-wrap' : 'normal',
          fontFamily: 'inherit',
        }}
      >
        {value}
      </span>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  padding: 8,
  background: 'var(--bg-app)',
  border: '1px solid var(--border-default)',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  width: '100%',
  fontFamily: 'inherit',
};
