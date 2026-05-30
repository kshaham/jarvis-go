import { useState } from 'react';
import { Chip, Input } from '@/components/ui';
import { SectionCard, SegmentedControl } from './formPrimitives';
import { loadRecents } from './workdirSection.utils';

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
  // Lazy initializer: loadRecents() reads localStorage, so passing it
  // as a factory avoids running it on every re-render AND avoids the
  // mount-time effect flash that no-initialize-state flags.
  const [recents] = useState<string[]>(loadRecents);

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
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Input
            value={workingDir}
            onChange={(e) => setWorkingDir(e.target.value)}
            placeholder="/path/to/repo"
            aria-label="Existing folder path"
            style={{ fontFamily: 'var(--font-mono)' }}
          />
          {recents.length > 0 ? (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              <span
                style={{
                  fontSize: 12,
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
                <Chip
                  key={path}
                  active={path === workingDir}
                  onClick={() => setWorkingDir(path)}
                  className="max-w-[220px] truncate font-mono"
                >
                  {path}
                </Chip>
              ))}
            </div>
          ) : null}
        </div>
      )}
    </SectionCard>
  );
}
