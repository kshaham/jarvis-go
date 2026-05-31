import type { WorkdirMode } from './WorkdirSection';

export type Validity = {
  ok: boolean;
  reason: string;
};

export function validateSpawnState({
  templateId,
  mission,
  mode,
  workingDir,
  projectName,
  githubRepo,
}: {
  templateId: string;
  mission: string;
  mode: WorkdirMode;
  workingDir: string;
  projectName: string;
  githubRepo: string;
}): Validity {
  if (!templateId) return { ok: false, reason: 'Pick a template first' };
  if (!mission.trim()) return { ok: false, reason: 'Add a mission to spawn this team' };
  if (mode === 'new') {
    if (!projectName.trim()) return { ok: false, reason: 'Name the new project' };
    if (!/^[a-zA-Z0-9_-]+$/.test(projectName)) {
      return { ok: false, reason: 'Project name: alphanum / _ / - only' };
    }
  } else if (mode === 'github') {
    if (!githubRepo.trim()) return { ok: false, reason: 'Add a GitHub repo (owner/repo)' };
  } else if (!workingDir.trim()) {
    return { ok: false, reason: 'Pick an existing folder' };
  }
  return { ok: true, reason: '' };
}
