import { Select, type SelectOption } from '@/components/ui';
import type { SavedProject } from '@/api/types';

// ProjectPicker — the saved-projects dropdown at the top of the spawn
// page's working-directory section. Picking an entry populates the form
// (path for local, repo for github) via onPick; the trailing remove
// control deletes the saved project via onRemove. Renders nothing when
// there are no saved projects yet. Replaces the old localStorage
// "Recent" chips.
export function ProjectPicker({
  projects,
  onPick,
  onRemove,
}: {
  projects: SavedProject[];
  onPick: (p: SavedProject) => void;
  onRemove: (id: string) => void;
}) {
  if (projects.length === 0) return null;

  const options: SelectOption[] = projects.map((p) => ({
    value: p.id,
    label: p.name,
    sublabel: p.kind === 'github' ? p.repoUrl : p.path,
    tag: p.kind,
    onRemove: () => onRemove(p.id),
  }));

  return (
    <Select
      value={null}
      options={options}
      placeholder="Saved projects…"
      ariaLabel="Saved projects"
      onChange={(id) => {
        const p = projects.find((x) => x.id === id);
        if (p) onPick(p);
      }}
    />
  );
}
