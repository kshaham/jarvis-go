import type { CSSProperties } from 'react';
import { LayoutTemplate } from 'lucide-react';

import { Chip, EmptyState, Input, Skeleton } from '@/components/ui';
import { TemplateCard } from './TemplateCard';
import type { TeamTemplate } from '../../api/types';

const templateGridStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
  gap: 12,
};

// TemplateStep — the full-width template gallery (step 1). Filters live in a
// slim tile; the template cards spread across the full content width.
export function TemplateStep({
  query,
  setQuery,
  category,
  setCategory,
  categories,
  templates,
  selected,
  onSelect,
  loading,
}: {
  query: string;
  setQuery: (q: string) => void;
  category: string;
  setCategory: (c: string) => void;
  categories: string[];
  templates: TeamTemplate[];
  selected: string;
  onSelect: (id: string) => void;
  loading: boolean;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div className="spawn-gallery-filters">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search templates…"
          aria-label="Search templates"
          style={{ flex: '1 1 220px', width: 'auto' }}
        />
        <div
          role="radiogroup"
          aria-label="Template category"
          style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}
        >
          {['', ...categories].map((cat) => {
            const active = cat === category;
            return (
              <Chip key={cat || 'all'} active={active} onClick={() => setCategory(cat)}>
                {cat || 'All'}
              </Chip>
            );
          })}
        </div>
      </div>

      {loading ? (
        <div style={templateGridStyle}>
          {Array.from({ length: 8 }).map((_, i) => (
            <TemplateCardSkeleton key={i} />
          ))}
        </div>
      ) : templates.length === 0 ? (
        <EmptyState
          icon={<LayoutTemplate size={22} />}
          title="No templates match"
          description="Try clearing the search or switching back to the All category."
        />
      ) : (
        <div style={templateGridStyle}>
          {templates.map((t) => (
            <TemplateCard
              key={t.id}
              tpl={t}
              selected={t.id === selected}
              onSelect={() => onSelect(t.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// Loading placeholder for a single template card — mirrors the card's shape
// (avatar + title, two text lines, footer row) so the grid keeps its rhythm
// while templates fetch.
function TemplateCardSkeleton() {
  return (
    <div className="surface-card flex flex-col gap-2.5 p-3">
      <div className="flex items-center gap-2.5">
        <Skeleton className="size-10 shrink-0 rounded-md" />
        <Skeleton className="h-3.5 w-32" />
      </div>
      <Skeleton className="h-2.5 w-full" />
      <Skeleton className="h-2.5 w-3/4" />
      <div className="mt-1 flex items-center justify-between">
        <Skeleton className="h-4 w-20" />
        <Skeleton className="h-4 w-12 rounded-full" />
      </div>
    </div>
  );
}
