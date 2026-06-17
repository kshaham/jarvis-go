import { useEffect, useMemo } from 'react';
import type { CSSProperties, KeyboardEvent, ReactNode } from 'react';
import { m, useReducedMotion } from 'motion/react';
import type { TeamTemplate, TeamTemplateMember } from '../../api/types';
import { cn } from '@/lib/cn';
import { spring } from '@/lib/motion';
import { RevealItem } from '../motion/RevealItem';
import { SectionCard } from './formPrimitives';
import { Avatar } from './Avatar';
import { MemberEditor } from './MemberEditor';
import { isMemberEdited, memberFromTemplate } from './memberEditor.utils';
import type { MemberEdit } from './memberEditor.utils';
import type { Tier } from './TierBadge';

const emptyEditorStyle: CSSProperties = {
  padding: 16,
  background: 'var(--bg-surface)',
  border: '1px dashed var(--border-strong)',
  borderRadius: 'var(--radius-card-flat)',
  color: 'var(--text-muted)',
  fontSize: 12,
  textAlign: 'center' as const,
};

// Compact roster as a vertical LIST of rows (B2 mockup), not a card grid.
const rosterListStyle: CSSProperties = {
  listStyle: 'none',
  margin: 0,
  padding: 2,
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
};

const memberRowBaseStyle: CSSProperties = {
  appearance: 'none',
  font: 'inherit',
  color: 'inherit',
  textAlign: 'left',
  width: '100%',
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  padding: '6px 8px',
  cursor: 'pointer',
  borderRadius: 'var(--radius-md)',
};

const rowNameStyle: CSSProperties = {
  fontSize: 13,
  fontWeight: 600,
  color: 'var(--text-primary)',
  lineHeight: 1.25,
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

const rowRoleStyle: CSSProperties = {
  fontSize: 11,
  color: 'var(--text-secondary)',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};

// A small accent pill marking a member overridden from the template — more
// legible than a bare dot and token-colored so it tracks every theme.
const editedPillStyle: CSSProperties = {
  flexShrink: 0,
  display: 'inline-flex',
  alignItems: 'center',
  padding: '1px var(--space-3)',
  borderRadius: 'var(--radius-pill)',
  fontSize: 10,
  fontWeight: 600,
  letterSpacing: '0.02em',
  lineHeight: 1.5,
  color: 'var(--accent)',
  background: 'rgb(var(--accent-rgb) / 0.12)',
  boxShadow: 'inset 0 0 0 1px rgb(var(--accent-rgb) / 0.28)',
};

// MemberRow — one compact roster row: avatar plus name + role. Selected =
// accent fill + ring + elevation; a small "Edited" pill marks a member
// overridden from the template. A real <button> for keyboard nav, with a
// subtle hover-lift (reduced-motion gated).
function MemberRow({
  id,
  name,
  role,
  emoji,
  color,
  tier,
  selected,
  edited,
  onClick,
  onKeyDown,
}: {
  id?: string;
  name: string;
  role: string;
  emoji?: string;
  color?: string;
  tier?: Tier;
  selected?: boolean;
  edited?: boolean;
  onClick?: () => void;
  onKeyDown?: (e: KeyboardEvent<HTMLButtonElement>) => void;
}): ReactNode {
  const reduce = useReducedMotion();
  return (
    <m.button
      type="button"
      id={id}
      aria-pressed={!!selected}
      aria-label={`${name || 'Unnamed'}${role ? `, ${role}` : ''}`}
      onClick={onClick}
      onKeyDown={onKeyDown}
      className={cn('surface-card transition-shadow duration-150', selected ? '' : 'hover:shadow-lg-hover')}
      style={{
        ...memberRowBaseStyle,
        background: selected ? 'var(--lg-fill-accent)' : undefined,
        border: selected ? '1px solid rgb(var(--accent-rgb) / 0.6)' : undefined,
        boxShadow: selected ? 'var(--lg-shadow-rest), var(--glow-violet-sm)' : undefined,
      }}
      whileHover={reduce ? undefined : { y: -2 }}
      whileTap={reduce ? undefined : { y: 0, scale: 0.99 }}
      transition={spring.snappy}
    >
      <Avatar name={name} emoji={emoji} color={color} tier={tier} size="sm" ring />
      <span style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
        <span style={rowNameStyle}>{name || 'Unnamed'}</span>
        {role ? <span style={rowRoleStyle}>{role}</span> : null}
      </span>
      {edited ? (
        <span aria-label="Overridden from the template" title="Overridden from the template" style={editedPillStyle}>
          Edited
        </span>
      ) : null}
    </m.button>
  );
}

// baseById — map a template's members by their stable id so the roster and
// editor can look up the bundled emoji/color/tier and diff for edits.
function useBaseById(tpl: TeamTemplate): Map<string, TeamTemplateMember> {
  return useMemo(() => {
    const map = new Map<string, TeamTemplateMember>();
    tpl.members.forEach((m, i) => {
      const id = m.id ?? `${tpl.id}-${i}`;
      map.set(id, m);
    });
    return map;
  }, [tpl]);
}

// RosterTile — the team roster as a compact vertical list of selectable
// rows. Selection-only: it never mutates members (the editor tile does).
// Auto-selects the first member once the roster loads.
export function RosterTile({
  tpl,
  members,
  selectedMemberId,
  setSelectedMemberId,
}: {
  tpl: TeamTemplate;
  members: MemberEdit[];
  selectedMemberId: string | null;
  setSelectedMemberId: (id: string | null) => void;
}) {
  useEffect(() => {
    if (members.length === 0) return;
    if (selectedMemberId) return;
    setSelectedMemberId(members[0].id);
  }, [members, selectedMemberId, setSelectedMemberId]);

  const baseById = useBaseById(tpl);

  // Arrow-key navigation between rows. Attached per-row so the container
  // stays a non-interactive element.
  const handleRowKeyDown = (memberId: string) => (e: KeyboardEvent<HTMLButtonElement>) => {
    if (!['ArrowRight', 'ArrowLeft', 'ArrowUp', 'ArrowDown'].includes(e.key)) return;
    e.preventDefault();
    const idx = members.findIndex((m) => m.id === memberId);
    if (idx < 0) return;
    const delta = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : -1;
    const next = members[(idx + delta + members.length) % members.length];
    if (next) {
      setSelectedMemberId(next.id);
      (document.getElementById(`member-row-${next.id}`) as HTMLButtonElement | null)?.focus();
    }
  };

  return (
    <SectionCard
      title={`Roster (${members.length})`}
      hint="Click a row to edit. The Edited pill marks a member overridden from the template."
    >
      <div role="list" aria-label="Team roster" style={rosterListStyle}>
        {members.map((m, i) => {
          const base = baseById.get(m.id);
          const edited = base ? isMemberEdited(m, base) : false;
          return (
            <RevealItem key={m.id} index={i} yOffset={6} role="listitem" style={{ minWidth: 0 }}>
              <MemberRow
                id={`member-row-${m.id}`}
                name={m.name}
                role={m.role}
                emoji={base?.emoji}
                color={base?.color}
                tier={m.tier ?? (base?.tier as Tier | undefined)}
                selected={m.id === selectedMemberId}
                edited={edited}
                onClick={() => setSelectedMemberId(m.id)}
                onKeyDown={handleRowKeyDown(m.id)}
              />
            </RevealItem>
          );
        })}
      </div>
    </SectionCard>
  );
}

// EditMemberTile — the per-member override editor for the currently selected
// roster row. Mutates only the selected member; overrides are team-local.
export function EditMemberTile({
  tpl,
  members,
  setMembers,
  selectedMemberId,
  defaultProvider,
  defaultModel,
}: {
  tpl: TeamTemplate;
  members: MemberEdit[];
  setMembers: (m: MemberEdit[]) => void;
  selectedMemberId: string | null;
  defaultProvider: string;
  defaultModel: string;
}) {
  const baseById = useBaseById(tpl);
  const selected = members.find((m) => m.id === selectedMemberId) ?? null;
  const selectedBase = selected ? baseById.get(selected.id) : undefined;

  const updateOne = (next: MemberEdit) => {
    setMembers(members.map((m) => (m.id === next.id ? next : m)));
  };

  const resetSelected = () => {
    if (!selected || !selectedBase) return;
    updateOne(memberFromTemplate(selectedBase, selected.id));
  };

  if (!selected) {
    return <div style={emptyEditorStyle}>Select a row in the roster to edit that member.</div>;
  }

  return (
    <MemberEditor
      member={selected}
      baseMember={selectedBase}
      templateProvider={defaultProvider}
      templateModel={defaultModel}
      onChange={updateOne}
      onReset={resetSelected}
    />
  );
}
