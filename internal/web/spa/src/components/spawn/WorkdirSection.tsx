import { useMemo } from 'react';
import { Input } from '@/components/ui';
import { SectionCard, SegmentedControl } from './formPrimitives';
import { ProjectPicker } from './ProjectPicker';
import type { SavedProject } from '@/api/types';

export type WorkdirMode = 'existing' | 'new' | 'github';

// githubSlug mirrors the backend's normalizeRepoInput slug derivation
// (owner-repo) for the live "workdir →" preview. Best-effort only; the
// server re-validates on spawn.
function githubSlug(input: string): string {
  let s = input.trim();
  for (const p of ['https://github.com/', 'http://github.com/', 'github.com/']) {
    if (s.toLowerCase().startsWith(p)) {
      s = s.slice(p.length);
      break;
    }
  }
  s = s.replace(/\.git$/, '').replace(/^\/+|\/+$/g, '');
  const parts = s.split('/');
  if (parts.length !== 2 || !parts[0] || !parts[1]) return '';
  const clean = (seg: string) => seg.replace(/[^A-Za-z0-9_-]+/g, '-');
  return `${clean(parts[0])}-${clean(parts[1])}`;
}

export function WorkdirSection({
  mode,
  setMode,
  workingDir,
  setWorkingDir,
  projectName,
  setProjectName,
  githubRepo,
  setGithubRepo,
  projects,
  onPickProject,
  onRemoveProject,
}: {
  mode: WorkdirMode;
  setMode: (m: WorkdirMode) => void;
  workingDir: string;
  setWorkingDir: (v: string) => void;
  projectName: string;
  setProjectName: (v: string) => void;
  githubRepo: string;
  setGithubRepo: (v: string) => void;
  projects: SavedProject[];
  onPickProject: (p: SavedProject) => void;
  onRemoveProject: (id: string) => void;
}) {
  const trailing = useMemo(
    () => (
      <SegmentedControl<WorkdirMode>
        ariaLabel="Workdir mode"
        options={[
          { value: 'existing', label: 'Existing folder' },
          { value: 'new', label: 'New project' },
          { value: 'github', label: 'GitHub repo' },
        ]}
        value={mode}
        onChange={setMode}
      />
    ),
    [mode, setMode],
  );

  const githubPreview = githubSlug(githubRepo);

  return (
    <SectionCard
      title="Working directory"
      hint="Where the team's agents run. Match this to workingDirAllowlist."
      trailing={trailing}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <ProjectPicker projects={projects} onPick={onPickProject} onRemove={onRemoveProject} />

        {mode === 'new' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <Input
              value={projectName}
              onChange={(e) => {
                const v = e.target.value.replace(/[^a-zA-Z0-9_-]/g, '');
                setProjectName(v.slice(0, 64));
              }}
              placeholder="project-name"
              aria-label="New project name"
              style={{ fontFamily: 'var(--font-mono)' }}
            />
            <span
              style={{
                fontSize: 12,
                color: 'var(--text-muted)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              <span style={{ color: 'var(--text-faint)' }}>workdir →</span>{' '}
              /app/workspace/{projectName || 'project-name'}
            </span>
          </div>
        ) : mode === 'github' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <Input
              value={githubRepo}
              onChange={(e) => setGithubRepo(e.target.value)}
              placeholder="owner/repo or https://github.com/owner/repo"
              aria-label="GitHub repository"
              style={{ fontFamily: 'var(--font-mono)' }}
            />
            <span
              style={{
                fontSize: 12,
                color: 'var(--text-muted)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              <span style={{ color: 'var(--text-faint)' }}>workdir →</span>{' '}
              /app/workspace/{githubPreview || 'owner-repo'}
            </span>
          </div>
        ) : (
          <Input
            value={workingDir}
            onChange={(e) => setWorkingDir(e.target.value)}
            placeholder="/path/to/repo"
            aria-label="Existing folder path"
            style={{ fontFamily: 'var(--font-mono)' }}
          />
        )}
      </div>
    </SectionCard>
  );
}
