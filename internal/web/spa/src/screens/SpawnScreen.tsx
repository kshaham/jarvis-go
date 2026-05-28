import { useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { Press } from '../components/motion';
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
  const [hydratedFor, setHydratedFor] = useState<string | null>(null);
  if (tpl && hydratedFor !== tpl.id) {
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
      if (!newProject && workingDir.trim()) {
        recordRecentWorkdir(workingDir.trim());
      }
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      navigate('/');
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
      </header>

      {hasTemplate ? (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'minmax(220px, 280px) minmax(0, 1fr)',
            gap: 16,
            alignItems: 'flex-start',
          }}
          className="spawn-shell"
        >
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

