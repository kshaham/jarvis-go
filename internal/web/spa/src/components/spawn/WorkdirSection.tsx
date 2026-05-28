import { useEffect, useState } from 'react';
import { inputStyle, SectionCard, SegmentedControl } from './formPrimitives';

const RECENT_KEY = 'jarvis.spawn.recentWorkdirs';
const RECENT_MAX = 5;

function loadRecents(): string[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) {
      return parsed.filter((s): s is string => typeof s === 'string' && s.length > 0);
    }
  } catch {
    // ignore
  }
  return [];
}

export function recordRecentWorkdir(path: string) {
  if (typeof window === 'undefined' || !path.trim()) return;
  const current = loadRecents().filter((p) => p !== path);
  const next = [path, ...current].slice(0, RECENT_MAX);
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch {
    // ignore
  }
}

export function WorkdirSection({
  newProject,
  setNewProject,
  workingDir,
  setWorkingDir,
  projectName,
  setProjectName,
}: {
  newProject: boolean;
  setNewProject: (b: boolean) => void;
  workingDir: string;
  setWorkingDir: (v: string) => void;
  projectName: string;
  setProjectName: (v: string) => void;
}) {
  const [recents, setRecents] = useState<string[]>([]);
  useEffect(() => {
    setRecents(loadRecents());
  }, []);

  return (
    <SectionCard
      title="Working directory"
      hint="Where the team's agents run. Match this to workingDirAllowlist."
      trailing={
        <SegmentedControl<'existing' | 'new'>
          ariaLabel="Workdir mode"
          options={[
            { value: 'existing', label: 'Existing folder' },
            { value: 'new', label: 'New project' },
          ]}
          value={newProject ? 'new' : 'existing'}
          onChange={(v) => setNewProject(v === 'new')}
        />
      }
    >
      {newProject ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <input
            value={projectName}
            onChange={(e) => {
              const v = e.target.value.replace(/[^a-zA-Z0-9_-]/g, '');
              setProjectName(v.slice(0, 64));
            }}
            placeholder="project-name"
            aria-label="New project name"
            style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
          />
          <span
            style={{
              fontSize: 11,
              color: 'var(--text-muted)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            <span style={{ color: 'var(--text-faint)' }}>workdir →</span>{' '}
            /app/workspace/{projectName || 'project-name'}
          </span>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <input
            value={workingDir}
            onChange={(e) => setWorkingDir(e.target.value)}
            placeholder="/path/to/repo"
            aria-label="Existing folder path"
            style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
          />
          {recents.length > 0 ? (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              <span
                style={{
                  fontSize: 11,
                  color: 'var(--text-faint)',
                  letterSpacing: 0.4,
                  textTransform: 'uppercase',
                  alignSelf: 'center',
                  marginRight: 4,
                }}
              >
                Recent
              </span>
              {recents.map((path) => (
                <button
                  key={path}
                  type="button"
                  onClick={() => setWorkingDir(path)}
                  style={{
                    padding: '4px 8px',
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-default)',
                    borderRadius: 'var(--radius-pill)',
                    color: 'var(--text-secondary)',
                    fontSize: 11,
                    fontFamily: 'var(--font-mono)',
                    cursor: 'pointer',
                    maxWidth: 220,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  title={path}
                >
                  {path}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      )}
    </SectionCard>
  );
}
