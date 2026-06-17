import { useMemo } from 'react';
import { Input } from '@/components/ui';
import { SectionCard, SegmentedControl } from './formPrimitives';
import { ProjectPicker } from './ProjectPicker';
import type { SavedProject } from '@/api/types';

export type WorkdirMode = 'existing' | 'new' | 'github';

// formatBytes renders a human-friendly file size for the selected-zip chip.
function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[unit]}`;
}

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
  zipFile,
  setZipFile,
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
  zipFile: File | null;
  setZipFile: (f: File | null) => void;
  projects: SavedProject[];
  onPickProject: (p: SavedProject) => void;
  onRemoveProject: (id: string) => void;
}) {
  const projectNameValid = /^[a-zA-Z0-9_-]{1,64}$/.test(projectName);
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

            {/* Optional: seed the new project from an uploaded codebase.
                Empty = blank project (the default). */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 2 }}>
              <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                Starting codebase (optional)
              </span>
              {zipFile ? (
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 8,
                    padding: '6px 10px',
                    borderRadius: 'var(--radius-md)',
                    border: '1px solid var(--lg-rim)',
                    background: 'var(--lg-fill)',
                    fontSize: 12,
                  }}
                >
                  <span
                    style={{
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                      color: 'var(--text-primary)',
                      fontFamily: 'var(--font-mono)',
                    }}
                  >
                    {zipFile.name}{' '}
                    <span style={{ color: 'var(--text-faint)' }}>({formatBytes(zipFile.size)})</span>
                  </span>
                  <button
                    type="button"
                    onClick={() => setZipFile(null)}
                    style={{
                      flexShrink: 0,
                      border: 'none',
                      background: 'transparent',
                      color: 'var(--text-muted)',
                      cursor: 'pointer',
                      fontSize: 12,
                    }}
                  >
                    Remove
                  </button>
                </div>
              ) : (
                <label
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: 'fit-content',
                    padding: '6px 12px',
                    borderRadius: 'var(--radius-md)',
                    border: '1px solid var(--lg-rim)',
                    background: 'var(--lg-fill)',
                    fontSize: 12,
                    color: projectNameValid ? 'var(--text-secondary)' : 'var(--text-faint)',
                    cursor: projectNameValid ? 'pointer' : 'not-allowed',
                    opacity: projectNameValid ? 1 : 0.6,
                  }}
                >
                  Choose .zip
                  <input
                    type="file"
                    accept=".zip,application/zip"
                    disabled={!projectNameValid}
                    onChange={(e) => setZipFile(e.target.files?.[0] ?? null)}
                    style={{ display: 'none' }}
                  />
                </label>
              )}
              <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
                {projectNameValid
                  ? 'Upload a .zip to start the team from that codebase, or leave empty for a blank project.'
                  : 'Enter a project name first to attach a starting codebase.'}
              </span>
            </div>
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
