import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { m, useReducedMotion } from 'motion/react';
import { AlertTriangle, Check } from 'lucide-react';

import { cn } from '@/lib/cn';
import { spring } from '@/lib/motion';
import { estimateDailyCost, formatPerDay } from './costEstimate.utils';
import { CostEstimate } from './CostEstimate';
import { ProviderModelSection, TelegramSection, TogglesSection, providerLabel } from './DefaultsSection';
import { EditMemberTile, RosterTile } from './MembersSection';
import { MissionSection } from './MissionSection';
import { ReviewTile } from './ReviewSummary';
import { TemplateChip } from './TemplateChip';
import { WorkdirSection } from './WorkdirSection';
import { sectionValidity } from './reviewSummary.utils';
import type { ChecklistItem, DotState, SectionId } from './reviewSummary.utils';
import type { SavedProject, TeamTemplate } from '../../api/types';
// Type-only import (erased at build → no runtime cycle with SpawnScreen).
import type { SetField, SpawnState } from '../../screens/SpawnScreen';

type Section = { id: SectionId; label: string };

const PRIMARY: Section[] = [
  { id: 'mission', label: 'Mission' },
  { id: 'workspace', label: 'Workspace' },
  { id: 'team', label: 'Team' },
  { id: 'options', label: 'Options' },
  { id: 'review', label: 'Review' },
];

const ADVANCED: Section[] = [
  { id: 'provider', label: 'Provider & model' },
  { id: 'telegram', label: 'Telegram' },
  { id: 'cost', label: 'Cost estimate' },
];

const ALL_SECTIONS = [...PRIMARY, ...ADVANCED];

function baseName(path: string): string {
  const trimmed = path.replace(/\/+$/, '');
  const idx = trimmed.lastIndexOf('/');
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1).trimEnd() + '…' : s;
}

// One-line secondary value per rail row, so the rail doubles as a review surface.
function sectionSummaries(state: SpawnState): Record<SectionId, string> {
  const {
    mission,
    mode,
    workingDir,
    projectName,
    githubRepo,
    zipFile,
    members,
    defaultProvider,
    defaultModel,
    perpetual,
    sevenStep,
    workflowsEnabled,
    telegramEnabled,
    telegramInterval,
  } = state;

  const workspace =
    mode === 'github'
      ? githubRepo.trim()
        ? `GitHub · ${githubRepo.trim()}`
        : 'Not set'
      : mode === 'new'
        ? projectName.trim()
          ? `New · ${projectName.trim()}${zipFile ? ' · zip' : ''}`
          : 'Not set'
        : workingDir.trim()
          ? `Folder · ${baseName(workingDir.trim())}`
          : 'Not set';

  const options =
    [perpetual && 'Perpetual', workflowsEnabled && 'Workflows', sevenStep && '7-step']
      .filter(Boolean)
      .join(' · ') || 'All off';

  const cost = estimateDailyCost(members, defaultProvider, defaultModel, perpetual);

  return {
    mission: mission.trim() ? truncate(mission.trim(), 36) : 'Not set',
    workspace,
    team: `${members.length} agent${members.length === 1 ? '' : 's'}`,
    options,
    review: '',
    provider: `${providerLabel(defaultProvider)}${defaultModel ? ` · ${defaultModel}` : ''}`,
    telegram: telegramEnabled ? `Every ${telegramInterval} min` : 'Off',
    cost: cost.allSubscription ? 'Subscription · $0' : formatPerDay(cost.perDay),
  };
}

function dotSrText(state: DotState, active: boolean): string {
  if (active) return ', current';
  if (state === 'done') return ', completed';
  if (state === 'error') return ', needs attention';
  if (state === 'incomplete') return ', incomplete';
  return '';
}

// Dot — the completion indicator. Decorative (aria-hidden); the row carries an
// .sr-only state phrase so it never signals by color alone. State changes
// animate: "done" scale-pops the check in, "error" shakes on the x-axis with a
// token error glow, "current" rings in via an accent box-shadow. All motion is
// reduced-motion gated — the visual result is identical, only static.
function Dot({ state, active }: { state: DotState; active: boolean }): ReactNode {
  const reduce = useReducedMotion();

  // An accent ring grows in when this dot becomes the current step, and the
  // error state nudges left/right once to draw the eye. The popped scale on
  // "done" is keyed below so a fresh completion re-triggers the pop.
  const animate = reduce
    ? undefined
    : {
        scale: state === 'done' ? [0.7, 1.12, 1] : 1,
        x: state === 'error' ? [0, -2, 2, -1.5, 0] : 0,
        boxShadow:
          active && (state === 'incomplete' || state === 'info')
            ? 'inset 0 0 0 2px var(--accent), var(--glow-violet-sm)'
            : state === 'error'
              ? 'var(--glow-error)'
              : 'inset 0 0 0 0px transparent',
      };

  return (
    <m.span
      className={cn('spawn-dot', `is-${state}`, active && 'is-current')}
      aria-hidden="true"
      animate={animate}
      transition={
        reduce
          ? undefined
          : { scale: spring.snappy, x: { duration: 0.32, ease: 'easeInOut' }, boxShadow: { duration: 0.22 } }
      }
    >
      {state === 'done' ? (
        <m.span
          style={{ display: 'inline-flex' }}
          initial={reduce ? false : { opacity: 0, scale: 0.6 }}
          animate={reduce ? undefined : { opacity: 1, scale: 1 }}
          transition={reduce ? undefined : { duration: 0.2, ease: 'easeOut', delay: 0.05 }}
        >
          <Check size={11} strokeWidth={3} />
        </m.span>
      ) : null}
      {state === 'error' ? <AlertTriangle size={11} strokeWidth={2.5} /> : null}
    </m.span>
  );
}

// ConfigureLayout — the master–detail "configure" step (replaces the bento). A
// left rail of sections (completion dots + secondary values + the Spawn action)
// and a right panel that renders one section's existing component at a time.
// Purely presentational — all state lives in SpawnScreen and flows in via
// `state` + the `set` setter factory.
export function ConfigureLayout({
  state,
  set,
  tpl,
  projects,
  onPickProject,
  onRemoveProject,
  onClearTemplate,
  checklist,
  activeSection,
  setActiveSection,
  advancedOpen,
  setAdvancedOpen,
  spawnControls,
}: {
  state: SpawnState;
  set: SetField;
  tpl: TeamTemplate;
  projects: SavedProject[];
  onPickProject: (p: SavedProject) => void;
  onRemoveProject: (id: string) => void;
  onClearTemplate: () => void;
  checklist: ChecklistItem[];
  activeSection: SectionId;
  setActiveSection: (s: SectionId) => void;
  advancedOpen: boolean;
  setAdvancedOpen: (b: boolean) => void;
  spawnControls: ReactNode;
}) {
  const validity = sectionValidity(state);
  const summaries = sectionSummaries(state);
  const active = ALL_SECTIONS.find((s) => s.id === activeSection) ?? PRIMARY[0];

  // Move focus to the panel heading when the section changes (no focus trap,
  // no hijack on first mount — guarded by didMount).
  const headingRef = useRef<HTMLHeadingElement>(null);
  const didMount = useRef(false);
  useEffect(() => {
    if (!didMount.current) {
      didMount.current = true;
      return;
    }
    headingRef.current?.focus();
  }, [activeSection]);

  const renderRow = (section: Section) => {
    const isActive = section.id === activeSection;
    const dot = validity[section.id];
    return (
      <button
        key={section.id}
        type="button"
        className={cn('spawn-rail-row', isActive && 'is-active')}
        aria-current={isActive ? 'step' : undefined}
        onClick={() => setActiveSection(section.id)}
      >
        <Dot state={dot} active={isActive} />
        <span className="spawn-rail-row-text">
          <span className="spawn-rail-row-label">{section.label}</span>
          {summaries[section.id] ? (
            <span className="spawn-rail-row-value">{summaries[section.id]}</span>
          ) : null}
        </span>
        <span className="sr-only">{dotSrText(dot, isActive)}</span>
      </button>
    );
  };

  return (
    <div className="spawn-config">
      <nav className="spawn-rail" aria-label="Spawn configuration">
        <TemplateChip tpl={tpl} onChange={onClearTemplate} style={{ marginBottom: 4 }} />

        <div className="spawn-rail-group">{PRIMARY.map(renderRow)}</div>

        <button
          type="button"
          className="spawn-rail-advanced"
          aria-expanded={advancedOpen}
          onClick={() => setAdvancedOpen(!advancedOpen)}
        >
          <span>Advanced</span>
          <span aria-hidden="true">{advancedOpen ? '▾' : '▸'}</span>
        </button>
        {advancedOpen ? <div className="spawn-rail-group">{ADVANCED.map(renderRow)}</div> : null}

        <div className="spawn-rail-spacer" />
        <div className="spawn-rail-spawn">{spawnControls}</div>
      </nav>

      <section className="spawn-panel" role="region" aria-labelledby="spawn-panel-heading">
        <h2 id="spawn-panel-heading" className="sr-only" tabIndex={-1} ref={headingRef}>
          {active.label}
        </h2>
        <div className="spawn-panel-body">{renderSection(activeSection)}</div>
      </section>
    </div>
  );

  function renderSection(id: SectionId): ReactNode {
    switch (id) {
      case 'mission':
        return <MissionSection value={state.mission} onChange={set('mission')} templateName={tpl.name} />;
      case 'workspace':
        return (
          <WorkdirSection
            mode={state.mode}
            setMode={set('mode')}
            workingDir={state.workingDir}
            setWorkingDir={set('workingDir')}
            projectName={state.projectName}
            setProjectName={set('projectName')}
            githubRepo={state.githubRepo}
            setGithubRepo={set('githubRepo')}
            zipFile={state.zipFile}
            setZipFile={set('zipFile')}
            projects={projects}
            onPickProject={onPickProject}
            onRemoveProject={onRemoveProject}
          />
        );
      case 'team':
        return (
          <div className="spawn-team-split">
            <RosterTile
              tpl={tpl}
              members={state.members}
              selectedMemberId={state.selectedMemberId}
              setSelectedMemberId={set('selectedMemberId')}
            />
            <EditMemberTile
              tpl={tpl}
              members={state.members}
              setMembers={set('members')}
              selectedMemberId={state.selectedMemberId}
              defaultProvider={state.defaultProvider}
              defaultModel={state.defaultModel}
            />
          </div>
        );
      case 'options':
        return (
          <TogglesSection
            defaultProvider={state.defaultProvider}
            perpetual={state.perpetual}
            setPerpetual={set('perpetual')}
            sevenStep={state.sevenStep}
            setSevenStep={set('sevenStep')}
            workflowsEnabled={state.workflowsEnabled}
            setWorkflowsEnabled={set('workflowsEnabled')}
          />
        );
      case 'review':
        return <ReviewTile tpl={tpl} members={state.members} checklist={checklist} />;
      case 'provider':
        return (
          <ProviderModelSection
            defaultProvider={state.defaultProvider}
            setDefaultProvider={set('defaultProvider')}
            defaultModel={state.defaultModel}
            setDefaultModel={set('defaultModel')}
            openaiBaseUrl={state.openaiBaseUrl}
            setOpenaiBaseUrl={set('openaiBaseUrl')}
            openaiApiKey={state.openaiApiKey}
            setOpenaiApiKey={set('openaiApiKey')}
            openaiKeyConfigured={state.openaiKeyConfigured}
          />
        );
      case 'telegram':
        return (
          <TelegramSection
            telegramEnabled={state.telegramEnabled}
            setTelegramEnabled={set('telegramEnabled')}
            telegramInterval={state.telegramInterval}
            setTelegramInterval={set('telegramInterval')}
          />
        );
      case 'cost':
        return (
          <CostEstimate
            members={state.members}
            defaultProvider={state.defaultProvider}
            defaultModel={state.defaultModel}
            perpetual={state.perpetual}
          />
        );
      default:
        return null;
    }
  }
}
