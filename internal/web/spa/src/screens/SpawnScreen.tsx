import React, { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { LayoutTemplate } from 'lucide-react';

import { Chip, EmptyState, Input, Section, Skeleton } from '@/components/ui';
import { DefaultsSection } from '../components/spawn/DefaultsSection';
import { memberFromTemplate } from '../components/spawn/memberEditor.utils';
import type { MemberEdit } from '../components/spawn/memberEditor.utils';
import { MembersSection } from '../components/spawn/MembersSection';
import { MissionSection } from '../components/spawn/MissionSection';
import { ReviewSummary } from '../components/spawn/ReviewSummary';
import { validateSpawnState } from '../components/spawn/reviewSummary.utils';
import { TemplateCard } from '../components/spawn/TemplateCard';
import { TemplateChip } from '../components/spawn/TemplateChip';
import { WorkdirSection } from '../components/spawn/WorkdirSection';
import { recordRecentWorkdir } from '../components/spawn/workdirSection.utils';
import { fetchJSON } from '../api/client';
import { useTeamTemplates } from '../hooks/useOfficeData';
import type { SettingsAccounts, TeamTemplate, TeamTemplateMember } from '../api/types';

const stepCircleBaseStyle: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: 18,
  height: 18,
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 700,
};

// SpawnScreen — the wizard formerly known as the spawn-team modal.
// Two panes: template picker on the left, configure form + sticky
// review bar on the right. Selecting a template collapses the picker
// to a slim chip; clicking that chip's Change button clears the
// selection and expands the picker back.
export function SpawnScreen() {
  const templates = useTeamTemplates();
  const qc = useQueryClient();
  const navigate = useNavigate();

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
  // OpenAI / Codex custom-endpoint operator settings. Persisted to /api/settings
  // on spawn (the backend reads them from the settings store, not the spawn
  // payload). openaiKeyConfigured prefills the "configured ✓" indicator when a
  // key (operator setting or shared deployment OPENAI_API_KEY) already exists.
  const [openaiBaseUrl, setOpenaiBaseUrl] = useState('');
  const [openaiApiKey, setOpenaiApiKey] = useState('');
  const [openaiKeyConfigured, setOpenaiKeyConfigured] = useState(false);
  const [telegramEnabled, setTelegramEnabled] = useState(false);
  const [telegramInterval, setTelegramInterval] = useState(60);
  // Dynamic Workflows are on by default; this toggle lets the operator
  // disable them for the team (sends disableWorkflows to the backend).
  const [workflowsEnabled, setWorkflowsEnabled] = useState(true);
  const [members, setMembers] = useState<MemberEdit[]>([]);
  const [selectedMemberId, setSelectedMemberId] = useState<string | null>(null);

  // When the operator picks a template, hydrate the per-member
  // overrides + the team-wide defaults from the template.
  // Adjust-state-during-render: React batches all the setters called
  // synchronously during render, so this is a single commit — no
  // cascading re-renders and no effect needed. hydratedForRef is never
  // rendered so we use useRef to avoid an extra re-render on the flag.
  const hydratedForRef = useRef<string | null>(null);
  if (tpl && hydratedForRef.current !== tpl.id) {
    hydratedForRef.current = tpl.id;
    const hydrated = tpl.members.map((m: TeamTemplateMember, i) =>
      memberFromTemplate(m, `${tpl.id}-${i}`),
    );
    setMembers(hydrated);
    setSelectedMemberId(hydrated[0]?.id ?? null);
    if (tpl.defaultProvider) setDefaultProvider(tpl.defaultProvider);
    if (tpl.defaultModel) setDefaultModel(tpl.defaultModel);
    if (typeof tpl.perpetualImprovement === 'boolean') setPerpetual(tpl.perpetualImprovement);
    if (tpl.telegram) {
      setTelegramEnabled(!!tpl.telegram.enabled);
      setTelegramInterval(tpl.telegram.intervalMinutes ?? 60);
    }
  }

  // Prefill the OpenAI/Codex config indicator from settings once on mount.
  // The key value is never returned raw (only openaiKeyConfigured + base URL).
  useEffect(() => {
    let cancelled = false;
    fetchJSON<SettingsAccounts>('/api/settings/accounts')
      .then((accounts) => {
        if (cancelled || !accounts) return;
        setOpenaiKeyConfigured(!!accounts.openaiKeyConfigured);
        if (accounts.openaiBaseUrl) setOpenaiBaseUrl(accounts.openaiBaseUrl);
      })
      .catch(() => {
        /* non-fatal: spawn still works, the indicator just stays unset */
      });
    return () => {
      cancelled = true;
    };
  }, []);

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
    mutationFn: async () => {
      // For the Codex custom-endpoint provider, persist the operator's base
      // URL + API key to settings first so the backend's spawn path can read
      // them (it reads the settings store, not this payload). Skip the API key
      // when it's blank or the masked placeholder so we never clobber a stored
      // key with its display mask.
      const isCodexHarness =
        defaultProvider === 'openai-codex' || defaultProvider === 'openai-custom';
      if (isCodexHarness) {
        const settingsUpdate: Record<string, unknown> = {};
        if (openaiBaseUrl.trim()) settingsUpdate.openaiBaseUrl = openaiBaseUrl.trim();
        if (openaiApiKey.trim() && !openaiApiKey.includes('****')) {
          settingsUpdate.openaiApiKey = openaiApiKey.trim();
        }
        if (Object.keys(settingsUpdate).length > 0) {
          await fetchJSON('/api/settings', { method: 'PUT', body: settingsUpdate });
        }
      }
      const body: Record<string, unknown> = {
        templateId,
        mission,
        workingDir: newProject ? '' : workingDir,
        newProject,
        projectName: newProject ? projectName : '',
        perpetualMode: perpetual,
        telegramEnabled,
        telegramIntervalMin: telegramInterval,
        disableWorkflows: !workflowsEnabled,
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
      if (!newProject && workingDir.trim()) {
        recordRecentWorkdir(workingDir.trim());
      }
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      toast.success('Team spawned');
      navigate('/');
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : 'Failed to spawn team');
    },
  });

  // The new layout pivots on whether a template is chosen. Before
  // selection: left pane = full TemplateStep, right pane empty. After
  // selection: left collapses to a slim TemplateChip, right pane shows
  // Configure (and Review when the operator advances).
  const hasTemplate = !!tpl;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Spawn team
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          Pick a template, configure the mission and members, then spawn.
        </p>
        <StepIndicator step={hasTemplate ? 2 : 1} />
      </header>

      {hasTemplate ? (
        <>
          <div className="spawn-grid">
            {/* Column 1 — team-level setup. */}
            <div className="spawn-col">
              <TemplateChip tpl={tpl} onChange={() => setTemplateId('')} />
              <MissionSection value={mission} onChange={setMission} templateName={tpl.name} />
              <WorkdirSection
                newProject={newProject}
                setNewProject={setNewProject}
                workingDir={workingDir}
                setWorkingDir={setWorkingDir}
                projectName={projectName}
                setProjectName={setProjectName}
              />
              <DefaultsSection
                defaultProvider={defaultProvider}
                setDefaultProvider={setDefaultProvider}
                defaultModel={defaultModel}
                setDefaultModel={setDefaultModel}
                openaiBaseUrl={openaiBaseUrl}
                setOpenaiBaseUrl={setOpenaiBaseUrl}
                openaiApiKey={openaiApiKey}
                setOpenaiApiKey={setOpenaiApiKey}
                openaiKeyConfigured={openaiKeyConfigured}
                perpetual={perpetual}
                setPerpetual={setPerpetual}
                telegramEnabled={telegramEnabled}
                setTelegramEnabled={setTelegramEnabled}
                telegramInterval={telegramInterval}
                setTelegramInterval={setTelegramInterval}
                workflowsEnabled={workflowsEnabled}
                setWorkflowsEnabled={setWorkflowsEnabled}
              />
            </div>
            {/* Column 2 — the roster leads. */}
            <div className="spawn-col">
              <MembersSection
                tpl={tpl}
                members={members}
                setMembers={setMembers}
                selectedMemberId={selectedMemberId}
                setSelectedMemberId={setSelectedMemberId}
                defaultProvider={defaultProvider}
                defaultModel={defaultModel}
              />
            </div>
          </div>
          {/* Full-width sticky review/spawn bar spanning both columns. */}
          <ReviewSummary
            tpl={tpl}
            members={members}
            defaultProvider={defaultProvider}
            defaultModel={defaultModel}
            newProject={newProject}
            workingDir={workingDir}
            projectName={projectName}
            validity={validateSpawnState({
              templateId,
              mission,
              newProject,
              workingDir,
              projectName,
            })}
            pending={spawn.isPending}
            error={spawn.error instanceof Error ? spawn.error.message : null}
            onSpawn={() => spawn.mutate()}
          />
        </>
      ) : (
        <TemplateStep
          query={query}
          setQuery={setQuery}
          category={category}
          setCategory={setCategory}
          categories={categories}
          templates={filteredTemplates}
          selected={templateId}
          onSelect={(id) => setTemplateId(id)}
          loading={templates.isLoading}
        />
      )}
    </section>
  );
}

const STEP_ITEMS: { n: 1 | 2; label: string }[] = [
  { n: 1, label: 'Template' },
  { n: 2, label: 'Configure' },
];

// StepIndicator — orients the two-page flow (Template → Configure). The
// active step is violet; a completed step keeps a filled marker.
function StepIndicator({ step }: { step: 1 | 2 }) {
  const steps = STEP_ITEMS;
  return (
    <div style={{ display: 'inline-flex', alignItems: 'center', gap: 8, marginTop: 12 }}>
      {steps.map((s, i) => (
        <Fragment key={s.n}>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              fontSize: 12,
              fontWeight: 600,
              color: step === s.n ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <span
              style={{
                ...stepCircleBaseStyle,
                background: step >= s.n ? 'var(--accent)' : 'var(--bg-elevated)',
                color: step >= s.n ? '#fff' : 'var(--text-muted)',
                border: '1px solid ' + (step >= s.n ? 'var(--accent)' : 'var(--border-default)'),
              }}
            >
              {s.n}
            </span>
            {s.label}
          </span>
          {i < steps.length - 1 ? <span style={{ color: 'var(--text-faint)' }}>→</span> : null}
        </Fragment>
      ))}
    </div>
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
    <Section>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search templates…"
          aria-label="Search templates"
          style={{ flex: '1 1 220px', width: 'auto' }}
        />
        <div role="radiogroup" aria-label="Template category" style={{ display: 'flex', gap: 6 }}>
          {['', ...categories].map((cat) => {
            const active = cat === category;
            return (
              <Chip key={cat || 'all'} active={active} onClick={() => setCategory(cat)}>
                {cat || 'All'}
              </Chip>
            );
          })}
        </div>
      </div>

      {loading ? (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: 10,
          }}
        >
          {Array.from({ length: 6 }).map((_, i) => (
            <TemplateCardSkeleton key={i} />
          ))}
        </div>
      ) : templates.length === 0 ? (
        <EmptyState
          icon={<LayoutTemplate size={22} />}
          title="No templates match"
          description="Try clearing the search or switching back to the All category."
        />
      ) : (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
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
    </Section>
  );
}

// Loading placeholder for a single template card — mirrors the card's
// shape (avatar + title, two text lines, footer row) so the grid keeps
// its rhythm while templates fetch.
function TemplateCardSkeleton() {
  return (
    <div className="surface-card flex flex-col gap-2.5 p-3">
      <div className="flex items-center gap-2.5">
        <Skeleton className="size-10 shrink-0 rounded-md" />
        <Skeleton className="h-3.5 w-32" />
      </div>
      <Skeleton className="h-2.5 w-full" />
      <Skeleton className="h-2.5 w-3/4" />
      <div className="mt-1 flex items-center justify-between">
        <Skeleton className="h-4 w-20" />
        <Skeleton className="h-4 w-12 rounded-full" />
      </div>
    </div>
  );
}


