import { useMemo } from 'react';
import { useAutoAnimate } from '@formkit/auto-animate/react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Library } from 'lucide-react';

import { Metric } from '../components/motion/Metric';
import { Button } from '../components/ui/Button';
import { EmptyState } from '../components/ui/EmptyState';
import { GlowCard } from '../components/ui/GlowCard';
import { Section } from '../components/ui/Section';
import { Skeleton } from '../components/ui/Skeleton';
import { relativeTime } from '../lib/relativeTime';
import {
  useKnowledgePromotions,
  useKnowledgeStats,
} from '../hooks/useOfficeData';
import { fetchJSON } from '../api/client';

// KnowledgeScreen — stats tiles + promoted library entries with a
// one-click revoke. The library powers PageIndex MCP for the agents,
// so this is where the operator audits what the team is learning.
export function KnowledgeScreen() {
  const stats = useKnowledgeStats();
  const promotions = useKnowledgePromotions();
  const qc = useQueryClient();
  // AutoAnimate owns enter/exit/reorder as promotions are revoked or
  // surface live (respects reduced motion).
  const [listRef] = useAutoAnimate<HTMLDivElement>();

  const revoke = useMutation({
    mutationFn: (id: string) =>
      fetchJSON('/api/knowledge/revoke', { method: 'POST', body: { id } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['knowledge', 'stats'] });
      qc.invalidateQueries({ queryKey: ['knowledge', 'promotions'] });
      toast.success('Promotion revoked');
    },
    onError: () => toast.error('Failed to revoke promotion'),
  });

  const list = promotions.data?.promotions ?? [];

  const promotionsTrailing = useMemo(
    () => (
      <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
        <Metric value={list.length} size={12} pulseOnChange /> entries
      </span>
    ),
    [list.length],
  );

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Knowledge
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          The library that fuels PageIndex MCP. Promotions are entries the team&apos;s auto-
          learner has surfaced as worth keeping.
        </p>
      </header>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))',
          gap: 10,
        }}
      >
        <StatTile label="Experiences" value={stats.data?.experiences ?? 0} />
        <StatTile label="Workflows" value={stats.data?.workflows ?? 0} />
        <StatTile label="Standups" value={stats.data?.standups ?? 0} />
        <StatTile label="Promoted" value={stats.data?.promoted ?? 0} tone="accent" />
        <StatTile label="Revoked" value={stats.data?.revoked ?? 0} tone="muted" />
      </div>

      <Section
        title="Promotions"
        trailing={promotionsTrailing}
      >
        {promotions.isLoading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {Array.from({ length: 4 }).map((_, i) => (
              <PromotionSkeleton key={i} />
            ))}
          </div>
        ) : list.length === 0 ? (
          <EmptyState
            icon={<Library size={22} />}
            title="No promotions yet"
            description="Entries appear here when the auto-learner picks up a pattern worth keeping."
          />
        ) : (
          <div ref={listRef} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {list.map((p) => (
              <GlowCard
                key={p.id}
                interactive={!p.revoked}
                className="flex items-center gap-3 px-3 py-2.5"
                style={{ opacity: p.revoked ? 0.5 : 1 }}
              >
                <span
                  style={{
                    fontSize: 12,
                    color: 'var(--accent)',
                    fontFamily: 'var(--font-mono)',
                    minWidth: 90,
                  }}
                >
                  {p.kind}
                </span>
                <span style={{ flex: 1, fontSize: 13, color: 'var(--text-primary)' }}>
                  {p.title}
                </span>
                {typeof p.reuseCount === 'number' ? (
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                    <Metric value={p.reuseCount} size={12} /> reuses
                  </span>
                ) : null}
                {p.promotedAt ? (
                  <span style={{ fontSize: 12, color: 'var(--text-faint)' }}>
                    {relativeTime(p.promotedAt)}
                  </span>
                ) : null}
                {p.revoked ? (
                  <span style={{ fontSize: 12, color: 'var(--status-error)' }}>revoked</span>
                ) : (
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={revoke.isPending}
                    onClick={() => {
                      if (window.confirm(`Revoke "${p.title}"?`)) {
                        revoke.mutate(p.id);
                      }
                    }}
                  >
                    Revoke
                  </Button>
                )}
              </GlowCard>
            ))}
          </div>
        )}
      </Section>
    </section>
  );
}

function StatTile({
  label,
  value,
  tone = 'primary',
}: {
  label: string;
  value: number;
  tone?: 'primary' | 'muted' | 'accent';
}) {
  return (
    <GlowCard className="flex flex-col gap-1 p-3.5">
      <span
        style={{
          fontSize: 12,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      {/* Stats pulse when the live count changes. */}
      <Metric value={value} size={24} tone={tone} pulseOnChange />
    </GlowCard>
  );
}

function PromotionSkeleton() {
  return (
    <div className="surface-card flex items-center gap-3 px-3 py-2.5">
      <Skeleton className="h-3 w-20" />
      <Skeleton className="h-3 flex-1" />
      <Skeleton className="h-2.5 w-12" />
    </div>
  );
}
