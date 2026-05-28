import { motion } from 'motion/react';

import { Metric, Press, RevealItem } from '../components/motion';
import { spring } from '../lib/motion';
import { relativeTime } from '../lib/relativeTime';
import {
  useKnowledgePromotions,
  useKnowledgeStats,
} from '../hooks/useOfficeData';
import { fetchJSON } from '../api/client';
import { useMutation, useQueryClient } from '@tanstack/react-query';

// KnowledgeScreen — stats tiles + promoted library entries with a
// one-click revoke. The library powers PageIndex MCP for the agents,
// so this is where the operator audits what the team is learning.
export function KnowledgeScreen() {
  const stats = useKnowledgeStats();
  const promotions = useKnowledgePromotions();
  const qc = useQueryClient();

  const revoke = useMutation({
    mutationFn: (id: string) =>
      fetchJSON('/api/knowledge/revoke', { method: 'POST', body: { id } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['knowledge', 'stats'] });
      qc.invalidateQueries({ queryKey: ['knowledge', 'promotions'] });
    },
  });

  const list = promotions.data?.promotions ?? [];

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <header>
        <h1 style={{ margin: 0, fontSize: 24, fontWeight: 600, color: 'var(--text-primary)' }}>
          Knowledge
        </h1>
        <p style={{ margin: '4px 0 0', color: 'var(--text-muted)', fontSize: 13 }}>
          The library that fuels PageIndex MCP. Promotions are entries the team's auto-
          learner has surfaced as worth keeping.
        </p>
      </header>

      <section
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
      </section>

      <section
        style={{
          padding: 14,
          borderRadius: 'var(--radius-card)',
          background: 'var(--bg-surface)',
          border: '1px solid var(--border-subtle)',
        }}
      >
        <header
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'baseline',
            marginBottom: 10,
          }}
        >
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>
            Promotions
          </h2>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            <Metric value={list.length} size={11} /> entries
          </span>
        </header>
        {list.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: 24 }}>
            No promotions yet. They appear here when the auto-learner picks up a pattern.
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {list.map((p, i) => (
              <RevealItem key={p.id} index={Math.min(i, 12)}>
                <motion.div
                  whileHover={{ borderColor: 'var(--border-strong)' }}
                  transition={spring.smooth}
                  style={{
                    padding: '10px 12px',
                    background: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    opacity: p.revoked ? 0.5 : 1,
                  }}
                >
                  <span
                    style={{
                      fontSize: 11,
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
                    <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                      <Metric value={p.reuseCount} size={11} /> reuses
                    </span>
                  ) : null}
                  {p.promotedAt ? (
                    <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
                      {relativeTime(p.promotedAt)}
                    </span>
                  ) : null}
                  {p.revoked ? (
                    <span style={{ fontSize: 11, color: 'var(--status-error)' }}>revoked</span>
                  ) : (
                    <Press
                      pressScale={0.94}
                      onClick={() => {
                        if (window.confirm(`Revoke "${p.title}"?`)) {
                          revoke.mutate(p.id);
                        }
                      }}
                    >
                      <span
                        style={{
                          padding: '4px 10px',
                          background: 'transparent',
                          border: '1px solid var(--border-default)',
                          borderRadius: 'var(--radius-sm)',
                          color: 'var(--text-muted)',
                          fontSize: 11,
                          fontWeight: 600,
                        }}
                      >
                        Revoke
                      </span>
                    </Press>
                  )}
                </motion.div>
              </RevealItem>
            ))}
          </div>
        )}
      </section>
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
    <div
      style={{
        padding: 14,
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-md)',
        display: 'flex',
        flexDirection: 'column',
        gap: 4,
      }}
    >
      <span
        style={{
          fontSize: 11,
          color: 'var(--text-muted)',
          letterSpacing: 0.4,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </span>
      <Metric value={value} size={24} tone={tone} />
    </div>
  );
}
