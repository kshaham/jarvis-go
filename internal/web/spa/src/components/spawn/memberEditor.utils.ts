import type { TeamTemplateMember } from '../../api/types';
import type { Tier } from './TierBadge';

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
