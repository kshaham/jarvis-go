import type { TeamTemplate, TeamTemplateMember } from '../../api/types';
import type { Tier } from './TierBadge';

// Pick the avatar that "represents" the template: prefer a strategic
// member (the lead), fall back to the first member. Used by the
// template card, the chip, and anywhere else that needs a single
// face for the team.
export function leadOf(tpl: TeamTemplate): TeamTemplateMember | undefined {
  return (
    tpl.members.find((m) => (m.tier as Tier | undefined) === 'strategic') ??
    tpl.members[0]
  );
}
