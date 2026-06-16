import type { WorkdirMode } from './WorkdirSection';

export type Validity = {
  ok: boolean;
  reason: string;
};

export interface SpawnFields {
  templateId: string;
  mission: string;
  mode: WorkdirMode;
  workingDir: string;
  projectName: string;
  githubRepo: string;
}

// Single source of truth for "is the workspace fully set?" — shared by the
// validity check and the Review-tile checklist so an all-green checklist
// means exactly the same thing as a spawnable form.
function workspaceValid(s: SpawnFields): boolean {
  if (s.mode === 'new') return !!s.projectName.trim() && /^[a-zA-Z0-9_-]+$/.test(s.projectName);
  if (s.mode === 'github') return !!s.githubRepo.trim();
  return !!s.workingDir.trim();
}

export function validateSpawnState(s: SpawnFields): Validity {
  if (!s.templateId) return { ok: false, reason: 'Pick a template first' };
  if (!s.mission.trim()) return { ok: false, reason: 'Add a mission to spawn this team' };
  if (s.mode === 'new') {
    if (!s.projectName.trim()) return { ok: false, reason: 'Name the new project' };
    if (!/^[a-zA-Z0-9_-]+$/.test(s.projectName)) {
      return { ok: false, reason: 'Project name: alphanum / _ / - only' };
    }
  } else if (s.mode === 'github') {
    if (!s.githubRepo.trim()) return { ok: false, reason: 'Add a GitHub repo (owner/repo)' };
  } else if (!s.workingDir.trim()) {
    return { ok: false, reason: 'Pick an existing folder' };
  }
  return { ok: true, reason: '' };
}

export interface ChecklistItem {
  label: string;
  ok: boolean;
}

// The pre-spawn checklist rendered in the Review tile. Mirrors the same
// predicates as validateSpawnState (above) via workspaceValid.
export function spawnChecklist(s: SpawnFields): ChecklistItem[] {
  return [
    { label: 'Template selected', ok: !!s.templateId },
    { label: 'Mission written', ok: !!s.mission.trim() },
    { label: 'Workspace set', ok: workspaceValid(s) },
  ];
}

// ── Master–detail section model ──────────────────────────────────────────────
// The redesign splits the old bento into a left-rail of sections. SectionId is
// the rail's identity; DotState drives each row's completion indicator.

export type SectionId =
  | 'mission'
  | 'workspace'
  | 'team'
  | 'options'
  | 'review'
  | 'provider'
  | 'telegram'
  | 'cost';

export type DotState = 'done' | 'incomplete' | 'error' | 'info';

// Extra fields (beyond SpawnFields) the per-section validity needs. SpawnState
// is a structural superset, so it satisfies this without an import cycle.
export interface SectionFields extends SpawnFields {
  defaultProvider: string;
  openaiKeyConfigured: boolean;
  openaiApiKey: string;
}

// A Codex provider that bills per-token (codex / custom endpoint) needs an API
// key. The subscription variant authenticates via `codex login`, so no key.
const CODEX_KEYED_PROVIDERS = new Set(['openai-codex', 'openai-custom']);

export function providerNeedsKey(s: SectionFields): boolean {
  return (
    CODEX_KEYED_PROVIDERS.has(s.defaultProvider) &&
    !s.openaiKeyConfigured &&
    !s.openaiApiKey.trim()
  );
}

function workspaceState(s: SpawnFields): DotState {
  if (workspaceValid(s)) return 'done';
  // A new-project name that's been typed but has invalid characters is an
  // error (not merely incomplete) so the rail flags it before spawn.
  if (s.mode === 'new' && s.projectName.trim()) return 'error';
  return 'incomplete';
}

// sectionValidity drives the rail completion dots. Primary sections gate Spawn
// (their dots mirror validateSpawnState); advanced sections are informational
// except provider, which errors when a keyed Codex provider has no key.
export function sectionValidity(s: SectionFields): Record<SectionId, DotState> {
  return {
    mission: s.mission.trim() ? 'done' : 'incomplete',
    workspace: workspaceState(s),
    team: 'done', // seeded from the template; never blocks
    options: 'done', // booleans are always valid
    review: validateSpawnState(s).ok ? 'done' : 'incomplete',
    provider: providerNeedsKey(s) ? 'error' : 'done',
    telegram: 'info',
    cost: 'info',
  };
}
