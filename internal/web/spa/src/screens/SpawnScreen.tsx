import { Fragment, useEffect, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { LayoutTemplate } from 'lucide-react';

import { Chip, EmptyState, Input, Section, Skeleton } from '@/components/ui';
import { DefaultsSection } from '../components/spawn/DefaultsSection';
import { memberFromTemplate } from '../components/spawn/MemberEditor';
import type { MemberEdit } from '../components/spawn/MemberEditor';
import { MembersSection } from '../components/spawn/MembersSection';
import { MissionSection } from '../components/spawn/MissionSection';
import { ReviewSummary, validateSpawnState } from '../components/spawn/ReviewSummary';
import { TemplateCard } from '../components/spawn/TemplateCard';
import { TemplateChip } from '../components/spawn/TemplateChip';
import { recordRecentWorkdir, WorkdirSection } from '../components/spawn/WorkdirSection';
import { fetchJSON } from '../api/client';
import { useTeamTemplates } from '../hooks/useOfficeData';
import type { TeamTemplate, TeamTemplateMember } from '../api/types';

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
  const [telegramEnabled, setTelegramEnabled] = useState(false);
  const [telegramInterval, setTelegramInterval] = useState(60);
  const [members, setMembers] = useState<MemberEdit[]>([]);
  const [selectedMemberId, setSelectedMemberId] = useState<string | null>(null);

  // When the operator picks a template, hydrate the per-member
  // overrides + the team-wide defaults from the template.
  // Runs in an effect so React schedules the state updates rather
  // than re-rendering mid-render.
  const [hydratedFor, setHydratedFor] = useState<string | null>(null);
  useEffect(() => {
    if (!tpl || hydratedFor === tpl.id) return;
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
    setHydratedFor(tpl.id);
  }, [tpl, hydratedFor]);

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
        <div className="spawn-shell">
          <TemplateChip tpl={tpl} onChange={() => setTemplateId('')} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16, minWidth: 0 }}>
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
              selectedMemberId={selectedMemberId}
              setSelectedMemberId={setSelectedMemberId}
            />
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
          </div>
        </div>
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

// StepIndicator — orients the two-page flow (Template → Configure). The
// active step is violet; a completed step keeps a filled marker.
function StepIndicator({ step }: { step: 1 | 2 }) {
  const steps: { n: 1 | 2; label: string }[] = [
    { n: 1, label: 'Template' },
    { n: 2, label: 'Configure' },
  ];
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
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 18,
                height: 18,
                borderRadius: 999,
                fontSize: 10,
                fontWeight: 700,
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
        <Skeleton className="h-10 w-10 shrink-0 rounded-md" />
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
  selectedMemberId: string | null;
  setSelectedMemberId: (id: string | null) => void;
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
    selectedMemberId,
    setSelectedMemberId,
  } = props;

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
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
        perpetual={perpetual}
        setPerpetual={setPerpetual}
        telegramEnabled={telegramEnabled}
        setTelegramEnabled={setTelegramEnabled}
        telegramInterval={telegramInterval}
        setTelegramInterval={setTelegramInterval}
      />

      <MembersSection
        tpl={tpl}
        members={members}
        setMembers={setMembers}
        selectedMemberId={selectedMemberId}
        setSelectedMemberId={setSelectedMemberId}
        defaultProvider={defaultProvider}
        defaultModel={defaultModel}
      />
    </section>
  );
}

