import { useEffect, useMemo } from 'react';
import type { TeamTemplate } from '../../api/types';
import { SectionCard } from './formPrimitives';
import { MemberEditor, isMemberEdited, memberFromTemplate } from './MemberEditor';
import type { MemberEdit } from './MemberEditor';
import { MemberTile } from './MemberTile';
import { TierBadge } from './TierBadge';
import type { Tier } from './TierBadge';

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
  // Auto-select the first member when the section first appears, so
  // the editor isn't empty.
  useEffect(() => {
    if (!selectedMemberId && members.length > 0) {
      setSelectedMemberId(members[0].id);
    }
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
      hint="Click a tile to edit. Yellow dot means overridden."
      trailing={
        <span style={{ display: 'inline-flex', gap: 10, alignItems: 'center' }}>
          <TierBadge tier="strategic" variant="dot" />
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Strategic</span>
          <TierBadge tier="worker" variant="dot" />
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Worker</span>
          <TierBadge tier="support" variant="dot" />
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Support</span>
        </span>
      }
    >
      <div
        role="listbox"
        aria-label="Team roster"
        style={{
          display: 'flex',
          gap: 8,
          overflowX: 'auto',
          padding: 2,
          marginBottom: 6,
        }}
      >
        {members.map((m) => {
          const base = baseById.get(m.id);
          const edited = base ? isMemberEdited(m, base) : false;
          return (
            <MemberTile
              key={m.id}
              name={m.name}
              role={m.role}
              emoji={base?.emoji}
              color={base?.color}
              tier={m.tier ?? (base?.tier as Tier | undefined)}
              selected={m.id === selectedMemberId}
              edited={edited}
              onClick={() => setSelectedMemberId(m.id)}
            />
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
        <div
          style={{
            padding: 16,
            background: 'var(--bg-elevated)',
            border: '1px dashed var(--border-default)',
            borderRadius: 'var(--radius-md)',
            color: 'var(--text-muted)',
            fontSize: 12,
            textAlign: 'center',
          }}
        >
          Select a tile above to edit that member.
        </div>
      )}
    </SectionCard>
  );
}
