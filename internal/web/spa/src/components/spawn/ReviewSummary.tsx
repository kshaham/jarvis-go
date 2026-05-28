import { useMemo } from 'react';
import type { TeamTemplate } from '../../api/types';
import { Press } from '../motion';
import { AvatarStack } from './Avatar';
import type { MemberEdit } from './MemberEditor';
import { TierCounts } from './TierBadge';
import type { Tier } from './TierBadge';

type Validity = {
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

function workdirLabel({
  newProject,
  workingDir,
  projectName,
}: {
  newProject: boolean;
  workingDir: string;
  projectName: string;
}): string {
  if (newProject) return `new → /app/workspace/${projectName || 'project-name'}`;
  return workingDir || '(no folder set)';
}

export function ReviewSummary({
  tpl,
  members,
  defaultProvider,
  defaultModel,
  newProject,
  workingDir,
  projectName,
  validity,
  pending,
  error,
  onSpawn,
}: {
  tpl: TeamTemplate;
  members: MemberEdit[];
  defaultProvider: string;
  defaultModel: string;
  newProject: boolean;
  workingDir: string;
  projectName: string;
  validity: Validity;
  pending: boolean;
  error: string | null;
  onSpawn: () => void;
}) {
  const providerLabel =
    defaultProvider === 'claude-subscription'
      ? 'Claude · Sub'
      : defaultProvider === 'claude-api'
        ? 'Claude · API'
        : defaultProvider === 'zai-glm'
          ? 'Z.AI · GLM'
          : defaultProvider;

  const baseById = useMemo(
    () => new Map(tpl.members.map((m, i) => [m.id ?? `${tpl.id}-${i}`, m] as const)),
    [tpl],
  );
  const stack = members.map((m) => {
    const base = baseById.get(m.id);
    return {
      id: m.id,
      name: m.name,
      emoji: base?.emoji,
      color: base?.color,
      tier: m.tier ?? (base?.tier as Tier | undefined),
    };
  });
  const tiers = members.map((m) => ({ tier: m.tier ?? (baseById.get(m.id)?.tier as Tier | undefined) }));
  const spawnDisabled = !validity.ok || pending;

  return (
    <div
      style={{
        position: 'sticky',
        bottom: 0,
        zIndex: 10,
        background: 'color-mix(in srgb, var(--bg-app) 92%, transparent)',
        backdropFilter: 'blur(8px)',
        WebkitBackdropFilter: 'blur(8px)',
        borderTop: '1px solid var(--border-default)',
        padding: 12,
        marginInline: -12,
        marginTop: 8,
      }}
    >
      {error ? (
        <div
          role="alert"
          style={{
            padding: 10,
            marginBottom: 10,
            background: 'rgba(255,90,95,0.10)',
            border: '1px solid var(--status-error)',
            borderRadius: 'var(--radius-md)',
            color: 'var(--status-error)',
            fontSize: 12,
          }}
        >
          {error}
        </div>
      ) : null}

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          flexWrap: 'wrap',
        }}
      >
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10 }}>
          <AvatarStack members={stack} max={6} size="sm" />
          <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
            {members.length} member{members.length === 1 ? '' : 's'}
          </span>
        </span>

        <TierCounts members={tiers} />

        <span style={{ display: 'inline-flex', gap: 8, flexWrap: 'wrap' }}>
          <Chip>{providerLabel}{defaultModel ? ` · ${defaultModel}` : ''}</Chip>
          <Chip mono>{workdirLabel({ newProject, workingDir, projectName })}</Chip>
        </span>

        <span style={{ flex: 1 }} />

        <Press
          pressScale={spawnDisabled ? 1 : 0.97}
          onClick={spawnDisabled ? undefined : onSpawn}
          disabled={spawnDisabled}
          aria-disabled={spawnDisabled}
          title={validity.ok ? '' : validity.reason}
          style={{
            padding: '10px 20px',
            background: validity.ok ? 'var(--accent)' : 'var(--bg-pressed)',
            color: validity.ok ? '#fff' : 'var(--text-muted)',
            border: validity.ok ? 'none' : '1px solid var(--border-default)',
            borderRadius: 'var(--radius-md)',
            fontSize: 14,
            fontWeight: 700,
            cursor: spawnDisabled ? 'not-allowed' : 'pointer',
            opacity: pending ? 0.7 : 1,
            transition: 'background 120ms ease, opacity 120ms ease',
          }}
        >
          {pending ? 'Spawning…' : 'Spawn team'}
        </Press>
      </div>

      {!validity.ok ? (
        <p
          style={{
            margin: '8px 0 0',
            fontSize: 11,
            color: 'var(--text-muted)',
          }}
        >
          {validity.reason}
        </p>
      ) : null}
    </div>
  );
}

function Chip({ children, mono }: { children: React.ReactNode; mono?: boolean }) {
  return (
    <span
      style={{
        padding: '4px 10px',
        background: 'var(--bg-elevated)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-pill)',
        color: 'var(--text-secondary)',
        fontSize: 11,
        fontWeight: 600,
        fontFamily: mono ? 'var(--font-mono)' : 'inherit',
        maxWidth: 360,
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        display: 'inline-block',
      }}
    >
      {children}
    </span>
  );
}
