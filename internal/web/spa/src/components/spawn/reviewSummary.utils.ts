export type Validity = {
  ok: boolean;
  reason: string;
};

export function validateSpawnState({
  templateId,
  mission,
  newProject,
  workingDir,
  projectName,
}: {
  templateId: string;
  mission: string;
  newProject: boolean;
  workingDir: string;
  projectName: string;
}): Validity {
  if (!templateId) return { ok: false, reason: 'Pick a template first' };
  if (!mission.trim()) return { ok: false, reason: 'Add a mission to spawn this team' };
  if (newProject) {
    if (!projectName.trim()) return { ok: false, reason: 'Name the new project' };
    if (!/^[a-zA-Z0-9_-]+$/.test(projectName)) {
      return { ok: false, reason: 'Project name: alphanum / _ / - only' };
    }
  } else if (!workingDir.trim()) {
    return { ok: false, reason: 'Pick an existing folder' };
  }
  return { ok: true, reason: '' };
}
