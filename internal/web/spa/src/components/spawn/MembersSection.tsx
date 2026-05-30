import { useEffect, useMemo } from 'react';
import type { CSSProperties } from 'react';
import type { TeamTemplate } from '../../api/types';
import { RevealItem } from '../motion';
import { SectionCard } from './formPrimitives';
import { MemberEditor, isMemberEdited, memberFromTemplate } from './MemberEditor';
import type { MemberEdit } from './MemberEditor';
import { MemberTile } from './MemberTile';
import { TierBadge } from './TierBadge';
import type { Tier } from './TierBadge';

const emptyEditorStyle: CSSProperties = {
  padding: 16,
  background: 'var(--lg-fill)',
  border: '1px dashed var(--lg-rim-strong)',
  borderRadius: 'var(--radius-md)',
  WebkitBackdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-tile)) saturate(var(--lg-saturate))',
  color: 'var(--text-muted)',
  fontSize: 12,
  textAlign: 'center' as const,
};

export function MembersSection({
  tpl,
  members,
  setMembers,
  selectedMemberId,
  setSelectedMemberId,
  defaultProvider,
  defaultModel,
}: {
  tpl: TeamTemplate;
  members: MemberEdit[];
  setMembers: (m: MemberEdit[]) => void;
  selectedMemberId: string | null;
  setSelectedMemberId: (id: string | null) => void;
  defaultProvider: string;
  defaultModel: string;
}) {
  // Auto-select the first member when there is nothing selected yet
  // and the roster has loaded. Including selectedMemberId in deps means
  // the effect re-evaluates when the selection changes, but the early
  // return keeps it a no-op unless selection is actually null.
  useEffect(() => {
    if (members.length === 0) return;
    if (selectedMemberId) return;
    setSelectedMemberId(members[0].id);
  }, [members, selectedMemberId, setSelectedMemberId]);

  const baseById = useMemo(() => {
    const map = new Map<string, (typeof tpl.members)[number]>();
    tpl.members.forEach((m, i) => {
      const id = m.id ?? `${tpl.id}-${i}`;
      map.set(id, m);
    });
    return map;
  }, [tpl]);

  const selected = members.find((m) => m.id === selectedMemberId) ?? null;
  const selectedBase = selected ? baseById.get(selected.id) : undefined;

  const updateOne = (next: MemberEdit) => {
    setMembers(members.map((m) => (m.id === next.id ? next : m)));
  };

  const resetSelected = () => {
    if (!selected || !selectedBase) return;
    const fresh = memberFromTemplate(selectedBase, selected.id);
    updateOne(fresh);
  };

  return (
    <SectionCard
      title={`Members (${members.length})`}
      hint="Click a card to edit. Amber pill = overridden from the template."
      trailing={
        <span style={{ display: 'inline-flex', gap: 10, alignItems: 'center' }}>
          <TierBadge tier="strategic" variant="dot" />
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Strategic</span>
          <TierBadge tier="worker" variant="dot" />
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Worker</span>
          <TierBadge tier="support" variant="dot" />
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Support</span>
        </span>
      }
    >
      <div
        role="listbox"
        aria-label="Team roster"
        aria-activedescendant={selectedMemberId ? `member-tile-${selectedMemberId}` : undefined}
        tabIndex={0}
        onKeyDown={(e) => {
          if (!['ArrowRight', 'ArrowLeft', 'ArrowUp', 'ArrowDown'].includes(e.key)) return;
          e.preventDefault();
          const idx = members.findIndex((m) => m.id === selectedMemberId);
          if (idx < 0) return;
          const delta = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : -1;
          const next = members[(idx + delta + members.length) % members.length];
          if (next) setSelectedMemberId(next.id);
        }}
        className="focus-ring"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
          gap: 10,
          padding: 2,
          marginBottom: 12,
        }}
      >
        {members.map((m, i) => {
          const base = baseById.get(m.id);
          const edited = base ? isMemberEdited(m, base) : false;
          return (
            <RevealItem key={m.id} index={i} yOffset={6} style={{ minWidth: 0 }}>
              <MemberTile
                id={`member-tile-${m.id}`}
                name={m.name}
                role={m.role}
                emoji={base?.emoji}
                color={base?.color}
                tier={m.tier ?? (base?.tier as Tier | undefined)}
                selected={m.id === selectedMemberId}
                edited={edited}
                onClick={() => setSelectedMemberId(m.id)}
                style={{ width: '100%' }}
              />
            </RevealItem>
          );
        })}
      </div>

      {selected ? (
        <MemberEditor
          member={selected}
          baseMember={selectedBase}
          templateProvider={defaultProvider}
          templateModel={defaultModel}
          onChange={updateOne}
          onReset={resetSelected}
        />
      ) : (
        <div style={emptyEditorStyle}>
          Select a card above to edit that member.
        </div>
      )}
    </SectionCard>
  );
}
