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
