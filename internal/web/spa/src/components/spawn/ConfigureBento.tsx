import { CostEstimate } from './CostEstimate';
import { ProviderModelSection, TogglesSection } from './DefaultsSection';
import { EditMemberTile, RosterTile } from './MembersSection';
import { MissionSection } from './MissionSection';
import { ReviewTile } from './ReviewSummary';
import { TemplateChip } from './TemplateChip';
import { WorkdirSection } from './WorkdirSection';
import type { ChecklistItem } from './reviewSummary.utils';
import type { SavedProject, TeamTemplate } from '../../api/types';
// Type-only import (erased at build → no runtime cycle with SpawnScreen).
import type { SetField, SpawnState } from '../../screens/SpawnScreen';

// ConfigureBento — the "power grid" configure layout (B2). Four visual
// columns: the two left config columns are one auto-balanced multi-column
// region (.bento-config); the Roster (+ Review) and the Edit-member tile are
// independent columns. Purely presentational — all state lives in
// SpawnScreen and flows in via `state` + the `set` setter factory.
export function ConfigureBento({
  state,
  set,
  tpl,
  projects,
  onPickProject,
  onRemoveProject,
  onClearTemplate,
  checklist,
}: {
  state: SpawnState;
  set: SetField;
  tpl: TeamTemplate;
  projects: SavedProject[];
  onPickProject: (p: SavedProject) => void;
  onRemoveProject: (id: string) => void;
  onClearTemplate: () => void;
  checklist: ChecklistItem[];
}) {
  const {
    mission,
    mode,
    workingDir,
    projectName,
    githubRepo,
    defaultProvider,
    defaultModel,
    openaiBaseUrl,
    openaiApiKey,
    openaiKeyConfigured,
    perpetual,
    sevenStep,
    telegramEnabled,
    telegramInterval,
    workflowsEnabled,
    members,
    selectedMemberId,
  } = state;

  return (
    <div className="bento">
      {/* Config tiles — one auto-balanced two-column region. */}
      <div className="bento-config">
        <MissionSection value={mission} onChange={set('mission')} templateName={tpl.name} />
        <TemplateChip tpl={tpl} onChange={onClearTemplate} />
        <WorkdirSection
          mode={mode}
          setMode={set('mode')}
          workingDir={workingDir}
          setWorkingDir={set('workingDir')}
          projectName={projectName}
          setProjectName={set('projectName')}
          githubRepo={githubRepo}
          setGithubRepo={set('githubRepo')}
          projects={projects}
          onPickProject={onPickProject}
          onRemoveProject={onRemoveProject}
        />
        <ProviderModelSection
          defaultProvider={defaultProvider}
          setDefaultProvider={set('defaultProvider')}
          defaultModel={defaultModel}
          setDefaultModel={set('defaultModel')}
          openaiBaseUrl={openaiBaseUrl}
          setOpenaiBaseUrl={set('openaiBaseUrl')}
          openaiApiKey={openaiApiKey}
          setOpenaiApiKey={set('openaiApiKey')}
          openaiKeyConfigured={openaiKeyConfigured}
        />
        <TogglesSection
          defaultProvider={defaultProvider}
          perpetual={perpetual}
          setPerpetual={set('perpetual')}
          sevenStep={sevenStep}
          setSevenStep={set('sevenStep')}
          telegramEnabled={telegramEnabled}
          setTelegramEnabled={set('telegramEnabled')}
          telegramInterval={telegramInterval}
          setTelegramInterval={set('telegramInterval')}
          workflowsEnabled={workflowsEnabled}
          setWorkflowsEnabled={set('workflowsEnabled')}
        />
        <CostEstimate
          members={members}
          defaultProvider={defaultProvider}
          defaultModel={defaultModel}
          perpetual={perpetual}
        />
      </div>

      {/* Roster — tall list + Review (fills out the column). */}
      <div className="bento-col">
        <RosterTile
          tpl={tpl}
          members={members}
          selectedMemberId={selectedMemberId}
          setSelectedMemberId={set('selectedMemberId')}
        />
        <ReviewTile tpl={tpl} members={members} checklist={checklist} />
      </div>

      {/* Edit member. */}
      <div className="bento-col">
        <EditMemberTile
          tpl={tpl}
          members={members}
          setMembers={set('members')}
          selectedMemberId={selectedMemberId}
          defaultProvider={defaultProvider}
          defaultModel={defaultModel}
        />
      </div>
    </div>
  );
}
