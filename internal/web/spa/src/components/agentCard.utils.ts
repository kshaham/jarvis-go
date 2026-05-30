import type { OfficeMember } from '@/api/types';

const avatarBg: Record<string, string> = {
  blue: 'bg-blue-500/20 text-blue-300',
  green: 'bg-emerald-500/20 text-emerald-300',
  purple: 'bg-purple-500/20 text-purple-300',
  orange: 'bg-orange-500/20 text-orange-300',
  pink: 'bg-pink-500/20 text-pink-300',
  cyan: 'bg-cyan-500/20 text-cyan-300',
  yellow: 'bg-yellow-500/20 text-yellow-300',
  red: 'bg-red-500/20 text-red-300',
};

// Avatar tint classes for a member's color — shared so the
// MemberDetailModal header avatar matches the card avatar during the
// shared-element morph.
export function agentAvatarClass(color?: string): string {
  return avatarBg[color || 'blue'] || avatarBg.blue;
}

// The glyph shown in an agent avatar (emoji → initial → first letter).
export function agentAvatarGlyph(member: Pick<OfficeMember, 'avatarEmoji' | 'avatarInitial' | 'name'>): string {
  return (member.avatarEmoji || member.avatarInitial || member.name?.[0] || '?').toString();
}
