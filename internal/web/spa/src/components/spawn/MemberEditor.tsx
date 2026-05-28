import type { TeamTemplate, TeamTemplateMember } from '../../api/types';
import { Avatar } from './Avatar';
import { ChipGroup, Field, inputStyle, SectionCard } from './formPrimitives';
import type { Tier } from './TierBadge';
import { TierBadge } from './TierBadge';

export type MemberEdit = {
  id: string;
  name: string;
  role: string;
  personality: string;
  systemPrompt: string;
  maxTurns?: number;
  provider?: string;
  model?: string;
  tier?: Tier;
};

export function memberFromTemplate(
  m: TeamTemplateMember,
  fallbackId: string,
): MemberEdit {
  return {
    id: m.id ?? fallbackId,
    name: m.name,
    role: m.role,
    personality: m.personality ?? '',
    systemPrompt: m.systemPrompt ?? '',
    maxTurns: m.maxTurns,
    provider: m.provider,
    model: m.model,
    tier: (m.tier as Tier | undefined) ?? undefined,
  };
}

export function isMemberEdited(edit: MemberEdit, base: TeamTemplateMember): boolean {
  return (
    edit.name !== base.name ||
    edit.role !== base.role ||
    edit.personality !== (base.personality ?? '') ||
    edit.systemPrompt !== (base.systemPrompt ?? '') ||
    edit.maxTurns !== base.maxTurns ||
    edit.provider !== base.provider ||
    edit.model !== base.model ||
    edit.tier !== (base.tier as Tier | undefined)
  );
}

const TIER_OPTIONS: { value: Tier | 'none'; label: string }[] = [
  { value: 'strategic', label: 'Strategic' },
  { value: 'worker', label: 'Worker' },
  { value: 'support', label: 'Support' },
  { value: 'none', label: 'Unset' },
];

export function MemberEditor({
  member,
  baseMember,
  templateProvider,
  templateModel,
  onChange,
  onReset,
}: {
  member: MemberEdit;
  baseMember: TeamTemplateMember | undefined;
  templateProvider: string;
  templateModel: string;
  onChange: (next: MemberEdit) => void;
  onReset: () => void;
}) {
  const edited = baseMember ? isMemberEdited(member, baseMember) : false;
  return (
    <SectionCard
      title="Editing member"
      hint="Overrides apply only to this team. The bundled template stays untouched."
      trailing={
        edited ? (
          <button
            type="button"
            onClick={onReset}
            style={{
              padding: '4px 10px',
              background: 'transparent',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-pill)',
              color: 'var(--text-secondary)',
              fontSize: 11,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            Reset to template
          </button>
        ) : null
      }
      style={{ background: 'var(--bg-elevated)' }}
    >
      <header style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <Avatar
          name={member.name}
          emoji={baseMember?.emoji}
          color={baseMember?.color}
          tier={member.tier}
          size="lg"
          ring
        />
        <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          <span style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)' }}>
            {member.name || 'Unnamed'}
          </span>
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {member.role || 'No role set'}
          </span>
        </div>
        {member.tier ? <TierBadge tier={member.tier} variant="pill" /> : null}
      </header>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
          gap: 10,
        }}
      >
        <Field label="Name">
          <input
            value={member.name}
            onChange={(e) => onChange({ ...member, name: e.target.value })}
            style={inputStyle}
          />
        </Field>
        <Field label="Role">
          <input
            value={member.role}
            onChange={(e) => onChange({ ...member, role: e.target.value })}
            style={inputStyle}
          />
        </Field>
      </div>

      <Field label="Personality" hint="A short prompt fragment for the agent's voice.">
        <textarea
          value={member.personality}
          onChange={(e) => onChange({ ...member, personality: e.target.value })}
          rows={2}
          style={{ ...inputStyle, resize: 'vertical', minHeight: 56 }}
        />
      </Field>

      <Field label="System prompt" hint="Custom instructions appended to the standard kickstart.">
        <textarea
          value={member.systemPrompt}
          onChange={(e) => onChange({ ...member, systemPrompt: e.target.value })}
          rows={4}
          style={{ ...inputStyle, resize: 'vertical', minHeight: 96, fontFamily: 'var(--font-mono)' }}
        />
      </Field>

      <Field label="Tier">
        <ChipGroup<Tier | 'none'>
          ariaLabel="Member tier"
          options={TIER_OPTIONS}
          value={member.tier ?? 'none'}
          onChange={(v) =>
            onChange({ ...member, tier: v === 'none' ? undefined : (v as Tier) })
          }
        />
      </Field>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))',
          gap: 10,
        }}
      >
        <Field label="Provider" hint={`Team default: ${templateProvider}`}>
          <input
            value={member.provider ?? ''}
            onChange={(e) => onChange({ ...member, provider: e.target.value || undefined })}
            placeholder={templateProvider}
            style={inputStyle}
          />
        </Field>
        <Field label="Model" hint={templateModel ? `Team default: ${templateModel}` : 'Provider default'}>
          <input
            value={member.model ?? ''}
            onChange={(e) => onChange({ ...member, model: e.target.value || undefined })}
            placeholder={templateModel || '(provider default)'}
            style={{ ...inputStyle, fontFamily: 'var(--font-mono)' }}
          />
        </Field>
        <Field label="Max turns" hint="Per-prompt cap. Leave blank for the global default.">
          <input
            type="number"
            value={member.maxTurns ?? ''}
            onChange={(e) =>
              onChange({
                ...member,
                maxTurns: e.target.value ? Number(e.target.value) : undefined,
              })
            }
            placeholder="(default)"
            style={inputStyle}
          />
        </Field>
      </div>
    </SectionCard>
  );
}

// Convenience re-export so callers don't have to import from
// MemberEditor.tsx separately to use the spawn-team mutation payload.
export type { TeamTemplate };
