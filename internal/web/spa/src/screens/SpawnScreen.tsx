import { useEffect, useMemo, useReducer, useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { Button } from '@/components/ui';
import { ConfigureLayout } from '../components/spawn/ConfigureLayout';
import { memberFromTemplate } from '../components/spawn/memberEditor.utils';
import type { MemberEdit } from '../components/spawn/memberEditor.utils';
import { sectionValidity, spawnChecklist, validateSpawnState } from '../components/spawn/reviewSummary.utils';
import type { SectionId } from '../components/spawn/reviewSummary.utils';
import { SpawnHeader } from '../components/spawn/SpawnHeader';
import { TemplateStep } from '../components/spawn/TemplateStep';
import type { WorkdirMode } from '../components/spawn/WorkdirSection';
import { fetchJSON } from '../api/client';
import { useTeamTemplates } from '../hooks/useOfficeData';
import { useSavedProjects } from '../hooks/useSavedProjects';
import type { SavedProject } from '../api/types';
import type { SettingsAccounts, TeamTemplate, TeamTemplateMember } from '../api/types';

// SpawnScreen — the spawn-team wizard orchestrator. It owns all state (the
// reducer below), the spawn mutation, and the two-step layout: step 1 is the
// template gallery (TemplateStep), step 2 is the master–detail configure
// layout (ConfigureLayout) — a left rail of sections (which hosts the Spawn
// action) beside a one-section-at-a-time panel. The presentational pieces live
// in components/spawn/*; this file stays focused on state + flow.
export interface SpawnState {
  templateId: string;
  query: string;
  category: string;
  mission: string;
  workingDir: string;
  mode: WorkdirMode;
  projectName: string;
  githubRepo: string;
  perpetual: boolean;
  sevenStep: boolean;
  defaultProvider: string;
  defaultModel: string;
  openaiBaseUrl: string;
  openaiApiKey: string;
  openaiKeyConfigured: boolean;
  telegramEnabled: boolean;
  telegramInterval: number;
  workflowsEnabled: boolean;
  members: MemberEdit[];
  selectedMemberId: string | null;
  // Master–detail config UI state (which rail section is open; advanced group).
  activeSection: SectionId;
  advancedOpen: boolean;
}

// SetField — the curried setter factory the bento tiles consume:
// set('mission')(value) dispatches a typed field update.
export type SetField = <K extends keyof SpawnState>(field: K) => (value: SpawnState[K]) => void;

type SpawnAction =
  | { type: 'set'; field: keyof SpawnState; value: SpawnState[keyof SpawnState] }
  | { type: 'hydrateTemplate'; tpl: TeamTemplate; members: MemberEdit[] }
  | { type: 'hydrateAccounts'; keyConfigured: boolean; baseUrl?: string };

const SPAWN_DEFAULT: SpawnState = {
  templateId: '',
  query: '',
  category: '',
  mission: '',
  workingDir: '',
  mode: 'existing',
  projectName: '',
  githubRepo: '',
  perpetual: true,
  sevenStep: false,
  defaultProvider: 'claude-subscription',
  defaultModel: '',
  openaiBaseUrl: '',
  openaiApiKey: '',
  openaiKeyConfigured: false,
  telegramEnabled: false,
  telegramInterval: 60,
  workflowsEnabled: true,
  members: [],
  selectedMemberId: null,
  activeSection: 'mission',
  advancedOpen: false,
};

function spawnReducer(state: SpawnState, action: SpawnAction): SpawnState {
  if (action.type === 'set') return { ...state, [action.field]: action.value };
  if (action.type === 'hydrateTemplate') {
    const { tpl, members } = action;
    return {
      ...state,
      members,
      selectedMemberId: members[0]?.id ?? null,
      activeSection: 'mission',
      defaultProvider: tpl.defaultProvider ?? state.defaultProvider,
      defaultModel: tpl.defaultModel ?? state.defaultModel,
      perpetual: typeof tpl.perpetualImprovement === 'boolean' ? tpl.perpetualImprovement : state.perpetual,
      sevenStep: typeof tpl.sevenStep === 'boolean' ? tpl.sevenStep : state.sevenStep,
      telegramEnabled: tpl.telegram ? !!tpl.telegram.enabled : state.telegramEnabled,
      telegramInterval: tpl.telegram?.intervalMinutes ?? state.telegramInterval,
    };
  }
  if (action.type === 'hydrateAccounts') {
    return {
      ...state,
      openaiKeyConfigured: action.keyConfigured,
      openaiBaseUrl: action.baseUrl ?? state.openaiBaseUrl,
    };
  }
  return state;
}

export function SpawnScreen() {
  const templates = useTeamTemplates();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [state, dispatch] = useReducer(spawnReducer, SPAWN_DEFAULT);
  const {
    templateId,
    query,
    category,
    mission,
    workingDir,
    mode,
    projectName,
    githubRepo,
    perpetual,
    sevenStep,
    defaultProvider,
    defaultModel,
    openaiBaseUrl,
    openaiApiKey,
    telegramEnabled,
    telegramInterval,
    workflowsEnabled,
    members,
  } = state;
  const set: SetField =
    <K extends keyof SpawnState>(field: K) =>
    (value: SpawnState[K]) =>
      dispatch({ type: 'set', field, value });

  const savedProjects = useSavedProjects();

  // Picking a saved project sets the matching mode and fills the field:
  // github → repo URL, local → folder path.
  const onPickProject = (p: SavedProject) => {
    if (p.kind === 'github') {
      dispatch({ type: 'set', field: 'mode', value: 'github' });
      dispatch({ type: 'set', field: 'githubRepo', value: p.repoUrl ?? '' });
    } else {
      dispatch({ type: 'set', field: 'mode', value: 'existing' });
      dispatch({ type: 'set', field: 'workingDir', value: p.path });
    }
  };

  const tpl: TeamTemplate | undefined = useMemo(
    () => (templates.data?.templates ?? []).find((t) => t.id === templateId),
    [templates.data?.templates, templateId],
  );

  // When the operator picks a template, hydrate the per-member overrides +
  // the team-wide defaults from the template. Adjust-state-during-render:
  // React batches all the setters called synchronously during render, so
  // this is a single commit. hydratedForRef is never rendered so we use
  // useRef to avoid an extra re-render on the flag.
  const hydratedForRef = useRef<string | null>(null);
  if (tpl && hydratedForRef.current !== tpl.id) {
    hydratedForRef.current = tpl.id;
    const hydrated = tpl.members.map((m: TeamTemplateMember, i) =>
      memberFromTemplate(m, `${tpl.id}-${i}`),
    );
    dispatch({ type: 'hydrateTemplate', tpl, members: hydrated });
  }

  // Prefill the OpenAI/Codex config indicator from settings once on mount.
  // The key value is never returned raw (only openaiKeyConfigured + base URL).
  useEffect(() => {
    let cancelled = false;
    fetchJSON<SettingsAccounts>('/api/settings/accounts')
      .then((accounts) => {
        if (cancelled || !accounts) return;
        dispatch({
          type: 'hydrateAccounts',
          keyConfigured: !!accounts.openaiKeyConfigured,
          baseUrl: accounts.openaiBaseUrl,
        });
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
        workingDir: mode === 'existing' ? workingDir : '',
        newProject: mode === 'new',
        projectName: mode === 'new' ? projectName : '',
        githubRepo: mode === 'github' ? githubRepo.trim() : '',
        perpetualMode: perpetual,
        sevenStepMode: sevenStep,
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
      // The backend persists the working dir as a saved project; refresh
      // the dropdown so the new entry shows up.
      qc.invalidateQueries({ queryKey: ['projects'] });
      qc.invalidateQueries({ queryKey: ['office', 'status'] });
      toast.success('Team spawned');
      navigate('/');
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : 'Failed to spawn team');
    },
  });

  const hasTemplate = !!tpl;

  // Shared validity + checklist (only meaningful once a template is chosen).
  const validity = validateSpawnState({ templateId, mission, mode, workingDir, projectName, githubRepo });
  const checklist = spawnChecklist({ templateId, mission, mode, workingDir, projectName, githubRepo });
  const spawnDisabled = !validity.ok || spawn.isPending;

  // The Spawn control lives at the bottom of the config rail. It stays
  // focusable while invalid (aria-disabled, not native disabled); pressing it
  // when incomplete routes the operator to the first unfinished section.
  const onSpawnClick = () => {
    if (spawn.isPending) return;
    if (!validity.ok) {
      const v = sectionValidity(state);
      const target = (['mission', 'workspace'] as SectionId[]).find((id) => v[id] !== 'done');
      if (target) set('activeSection')(target);
      return;
    }
    spawn.mutate();
  };

  const spawnControls = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: '100%' }}>
      <Button
        variant="primary"
        onClick={onSpawnClick}
        aria-disabled={spawnDisabled}
        title={validity.ok ? '' : validity.reason}
        style={{ width: '100%', opacity: !validity.ok && !spawn.isPending ? 0.6 : 1 }}
      >
        {spawn.isPending ? 'Spawning…' : 'Spawn team'}
      </Button>
      {!validity.ok ? (
        <span style={{ fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.4 }}>{validity.reason}</span>
      ) : null}
    </div>
  );

  return (
    <section className="spawn-shell" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <SpawnHeader
        step={hasTemplate ? 2 : 1}
        subtitle={
          hasTemplate
            ? 'Configure the mission, workspace, and roster, then spawn.'
            : 'Pick a template to start.'
        }
        right={null}
        error={hasTemplate && spawn.error instanceof Error ? spawn.error.message : null}
      />

      {hasTemplate ? (
        <ConfigureLayout
          state={state}
          set={set}
          tpl={tpl}
          projects={savedProjects.projects}
          onPickProject={onPickProject}
          onRemoveProject={(id) => savedProjects.remove.mutate(id)}
          onClearTemplate={() => dispatch({ type: 'set', field: 'templateId', value: '' })}
          checklist={checklist}
          activeSection={state.activeSection}
          setActiveSection={set('activeSection')}
          advancedOpen={state.advancedOpen}
          setAdvancedOpen={set('advancedOpen')}
          spawnControls={spawnControls}
        />
      ) : (
        <TemplateStep
          query={query}
          setQuery={set('query')}
          category={category}
          setCategory={set('category')}
          categories={categories}
          templates={filteredTemplates}
          selected={templateId}
          onSelect={set('templateId')}
          loading={templates.isLoading}
        />
      )}
    </section>
  );
}
