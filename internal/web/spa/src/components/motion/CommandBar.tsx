import { Command, useCommandState } from 'cmdk';
import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { Modal } from './Modal';
import { Metric } from './Metric';
import { useOfficeStatus } from '../../hooks/useOfficeStatus';
import { usePeers } from '../../hooks/usePeers';

type Action = {
  id: string;
  label: string;
  hint?: string;
  glyph: string;
  // Router path (no /office-v2 prefix — the basename handles it).
  path: string;
};

type Props = {
  open: boolean;
  onClose: () => void;
  // Client-side actions surfaced as a "Commands" group (wired by AppShell).
  actions?: { onNewTask: () => void; onToggleTheme: () => void };
};

// CommandBar — the office's ⌘K palette, backed by cmdk for fuzzy
// filtering + keyboard nav, rendered inside the glass Modal so it
// inherits the scale-in/out. Navigation goes through react-router
// (useNavigate) instead of raw pushState. Same action set as before:
// routed pages, live agents, and peer relay jumps.
export function CommandBar({ open, onClose, actions }: Props) {
  const { data } = useOfficeStatus();
  const peers = usePeers();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');

  useEffect(() => {
    if (!open) setQuery('');
  }, [open]);

  const routes = useMemo<Action[]>(
    () => [
      { id: 'go:agents', label: 'Agents', hint: 'Live team roster', glyph: 'A', path: '/' },
      { id: 'go:tasks', label: 'Tasks', hint: 'Kanban board', glyph: 'K', path: '/tasks' },
      { id: 'go:standup', label: 'Standup', hint: 'Transcript + composer', glyph: 'S', path: '/standup' },
      { id: 'go:chat', label: 'Chat', hint: 'Agent messages', glyph: 'C', path: '/chat' },
      { id: 'go:activity', label: 'Activity', hint: 'Timeline', glyph: 'L', path: '/activity' },
      { id: 'go:whiteboard', label: 'Whiteboard', hint: 'Shared canvas', glyph: '▤', path: '/whiteboard' },
      { id: 'go:knowledge', label: 'Knowledge', hint: 'Promotions + stats', glyph: '◇', path: '/knowledge' },
      { id: 'go:reports', label: 'Reports', hint: 'Saved reports', glyph: '⊟', path: '/reports' },
      { id: 'go:insights', label: 'Insights', hint: 'KPI dashboard', glyph: 'I', path: '/insights' },
      { id: 'go:metrics', label: 'Metrics', hint: 'Cumulative counters', glyph: 'M', path: '/metrics' },
      { id: 'go:meetings', label: 'Meetings', hint: 'Past standups', glyph: '∷', path: '/meetings' },
      { id: 'go:network', label: 'Network', hint: 'Agent graph', glyph: '⌬', path: '/network' },
      { id: 'go:handoffs', label: 'Handoffs', hint: 'Pending handoffs', glyph: 'H', path: '/handoffs' },
      { id: 'go:dev', label: 'Dev instances', hint: 'Running dev servers', glyph: 'D', path: '/dev-instances' },
      { id: 'go:locks', label: 'File locks', hint: 'Active locks', glyph: '⊞', path: '/locks' },
      { id: 'go:settings', label: 'Settings', hint: 'Team configuration', glyph: 'G', path: '/settings' },
      { id: 'go:spawn', label: 'Spawn team', hint: 'Open the spawn wizard', glyph: '+', path: '/spawn' },
      { id: 'go:teams', label: 'Teams', hint: 'Local + peer teams', glyph: 'T', path: '/teams' },
      { id: 'go:relay', label: 'Relay', hint: 'Inter-team messages', glyph: 'R', path: '/relay' },
      { id: 'go:addpeer', label: 'Add peer team', hint: 'Register a peer jarvis', glyph: '+', path: '/peers/new' },
    ],
    [],
  );

  const agents = useMemo<Action[]>(
    () =>
      (data?.members ?? []).map((m) => ({
        id: `agent:${m.sessionId}`,
        label: m.name,
        hint: m.role,
        glyph: (m.avatarEmoji || m.avatarInitial || m.name?.[0] || '?').toString().slice(0, 1),
        path: '/',
      })),
    [data?.members],
  );

  const peerJumps = useMemo<Action[]>(
    () =>
      (peers.data?.peers ?? []).map((p) => ({
        id: `peer:${p.peerId}`,
        label: 'Relay → ' + p.displayName,
        hint: p.unreadInbound ? `${p.unreadInbound} unread` : 'Open conversation',
        glyph: 'R',
        path: `/relay?peer=${encodeURIComponent(p.peerId)}`,
      })),
    [peers.data?.peers],
  );

  const commands = useMemo<{ id: string; label: string; hint?: string; glyph: string; run: () => void }[]>(
    () => {
      if (!actions) return [];
      return [
        {
          id: 'cmd:newtask',
          label: 'New task',
          hint: 'Quick-add a task (t)',
          glyph: '+',
          run: () => {
            onClose();
            requestAnimationFrame(() => actions.onNewTask());
          },
        },
        {
          id: 'cmd:theme',
          label: 'Toggle theme',
          hint: 'Cycle dark / light / system',
          glyph: '◐',
          run: () => {
            onClose();
            actions.onToggleTheme();
          },
        },
      ];
    },
    [actions, onClose],
  );

  function run(path: string) {
    onClose();
    // Defer so the modal can begin its exit before the route swaps.
    requestAnimationFrame(() => navigate(path));
  }

  return (
    <Modal
      open={open}
      onOpenChange={(next) => (next ? null : onClose())}
      size="md"
      title="Command palette"
      hideHeader
    >
      <Command label="Command palette" loop>
        <Command.Input
          value={query}
          onValueChange={setQuery}
          placeholder="Jump to agent or /command…"
          autoFocus
        />
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            padding: '0 14px 6px',
            fontSize: 11,
            color: 'var(--text-muted)',
          }}
        >
          <span>↑↓ navigate · ⏎ commit · esc close</span>
          <MatchCount />
        </div>
        <Command.List>
          <Command.Empty>No matches.</Command.Empty>
          {commands.length > 0 ? (
            <Command.Group heading="Commands">
              {commands.map((c) => (
                <CommandRow key={c.id} action={c} onSelect={c.run} />
              ))}
            </Command.Group>
          ) : null}
          <Command.Group heading="Navigate">
            {routes.map((a) => (
              <CommandRow key={a.id} action={a} onSelect={() => run(a.path)} />
            ))}
          </Command.Group>
          {agents.length > 0 ? (
            <Command.Group heading="Agents">
              {agents.map((a) => (
                <CommandRow key={a.id} action={a} onSelect={() => run(a.path)} />
              ))}
            </Command.Group>
          ) : null}
          {peerJumps.length > 0 ? (
            <Command.Group heading="Peers">
              {peerJumps.map((a) => (
                <CommandRow key={a.id} action={a} onSelect={() => run(a.path)} />
              ))}
            </Command.Group>
          ) : null}
        </Command.List>
      </Command>
    </Modal>
  );
}

// Live count of currently-matching commands (drives the "N matches"
// flourish via the animated Metric).
function MatchCount() {
  const count = useCommandState((state) => state.filtered.count);
  return (
    <span style={{ display: 'inline-flex', alignItems: 'baseline', gap: 4 }}>
      <Metric value={count} size={11} tone="muted" />
      matches
    </span>
  );
}

function CommandRow({
  action,
  onSelect,
}: {
  action: { label: string; hint?: string; glyph: string };
  onSelect: () => void;
}) {
  return (
    <Command.Item value={`${action.label} ${action.hint ?? ''}`} onSelect={onSelect}>
      <span
        style={{
          width: 26,
          height: 26,
          borderRadius: 6,
          background: 'var(--bg-pressed)',
          border: '1px solid var(--border-default)',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--text-primary)',
          fontWeight: 700,
          fontSize: 12,
          flexShrink: 0,
        }}
      >
        {action.glyph}
      </span>
      <span style={{ flex: 1, minWidth: 0 }}>
        <span
          className="cmdk-item-label"
          style={{
            display: 'block',
            fontSize: 14,
            fontWeight: 600,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {action.label}
        </span>
        {action.hint ? (
          <span
            style={{
              display: 'block',
              color: 'var(--text-muted)',
              fontSize: 11,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {action.hint}
          </span>
        ) : null}
      </span>
    </Command.Item>
  );
}
