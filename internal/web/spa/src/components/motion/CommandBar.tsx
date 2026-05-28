import { AnimatePresence, motion } from 'motion/react';
import { useEffect, useMemo, useState } from 'react';

import { spring, timing } from '../../lib/motion';
import { useOfficeStatus } from '../../hooks/useOfficeStatus';
import { usePeers } from '../../hooks/usePeers';
import { Metric } from './Metric';

type Action = {
  id: string;
  label: string;
  hint?: string;
  glyph: string;
  run: () => void;
};

type Props = {
  open: boolean;
  onClose: () => void;
};

// CommandBar — the office's ⌘K palette. Opens with a soft scale-in
// from 96% → 100%, blurs the rest of the page at 24px, and lets the
// operator jump to any agent or fire a slash-command in two
// keystrokes. Arrow keys move the selection; Enter commits; Escape
// closes. Same design language as the mobile CommandBar — when you
// see it on both surfaces it should feel like the same product.
export function CommandBar({ open, onClose }: Props) {
  const { data } = useOfficeStatus();
  const [query, setQuery] = useState('');
  const [highlight, setHighlight] = useState(0);

  useEffect(() => {
    if (!open) {
      setQuery('');
      setHighlight(0);
    }
  }, [open]);

  const peers = usePeers();

  const actions = useMemo<Action[]>(() => {
    const slash: Action[] = [
      // Routed pages (stay in /office-v2)
      { id: 'go:agents', label: 'Agents', hint: 'Live team roster', glyph: 'A', run: () => spaNav('/office-v2/') },
      { id: 'go:teams', label: 'Teams', hint: 'Local + peer teams', glyph: 'T', run: () => spaNav('/office-v2/teams') },
      { id: 'go:relay', label: 'Relay', hint: 'Inter-team messages', glyph: 'R', run: () => spaNav('/office-v2/relay') },
      { id: 'go:addpeer', label: 'Add peer team', hint: 'Register a new peer jarvis', glyph: '+', run: () => spaNav('/office-v2/peers/new') },
      // Slash commands now route entirely inside the SPA — the React
      // port covers every flow these targets used to invoke. Lifecycle
      // commands without a 1:1 page (pause / resume) drop the
      // operator on Settings where the controls live.
      { id: '/spawn', label: '/spawn', hint: 'Open the spawn wizard', glyph: '+', run: () => spaNav('/office-v2/spawn') },
      { id: '/standup', label: '/standup', hint: 'Standup transcript + composer', glyph: 'S', run: () => spaNav('/office-v2/standup') },
      { id: '/pause', label: '/pause', hint: 'Pause the team (Settings)', glyph: '⏸', run: () => spaNav('/office-v2/settings') },
      { id: '/resume', label: '/resume', hint: 'Resume the team (Settings)', glyph: '▶', run: () => spaNav('/office-v2/settings') },
      { id: '/whiteboard', label: '/whiteboard', hint: 'Shared note pad', glyph: '▤', run: () => spaNav('/office-v2/whiteboard') },
      { id: '/insights', label: '/insights', hint: 'KPI dashboard', glyph: 'I', run: () => spaNav('/office-v2/insights') },
      { id: '/metrics', label: '/metrics', hint: 'Cumulative counters', glyph: 'M', run: () => spaNav('/office-v2/metrics') },
      { id: '/meetings', label: '/meetings', hint: 'Past standups', glyph: '∷', run: () => spaNav('/office-v2/meetings') },
      { id: '/network', label: '/network', hint: 'Agent network graph', glyph: '⌬', run: () => spaNav('/office-v2/network') },
      { id: '/locks', label: '/locks', hint: 'File locks', glyph: '⊞', run: () => spaNav('/office-v2/locks') },
      { id: '/handoffs', label: '/handoffs', hint: 'Pending task handoffs', glyph: 'H', run: () => spaNav('/office-v2/handoffs') },
    ];
    const agents: Action[] = (data?.members ?? []).map((m) => ({
      id: `agent:${m.sessionId}`,
      label: m.name,
      hint: m.role,
      glyph: (m.avatarEmoji || m.avatarInitial || m.name?.[0] || '?').toString().slice(0, 1),
      run: () => spaNav('/office-v2/'),
    }));
    // One entry per registered peer — operator can leap directly into
    // a conversation by typing the peer's name + Enter.
    const peerJumps: Action[] = (peers.data?.peers ?? []).map((p) => ({
      id: `peer:${p.peerId}`,
      label: 'Relay → ' + p.displayName,
      hint: p.unreadInbound
        ? `${p.unreadInbound} unread`
        : 'Open inter-team conversation',
      glyph: 'R',
      run: () => spaNav(`/office-v2/relay?peer=${encodeURIComponent(p.peerId)}`),
    }));
    return [...slash, ...agents, ...peerJumps];
  }, [data?.members, peers.data?.peers]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return actions;
    return actions.filter((a) => `${a.label} ${a.hint ?? ''}`.toLowerCase().includes(q));
  }, [query, actions]);

  // Global ⌘K / Ctrl+K toggle + within-palette key handling.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (!open) return;
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      } else if (e.key === 'ArrowDown') {
        e.preventDefault();
        setHighlight((h) => Math.min(h + 1, filtered.length - 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setHighlight((h) => Math.max(0, h - 1));
      } else if (e.key === 'Enter') {
        e.preventDefault();
        const next = filtered[highlight];
        if (next) {
          onClose();
          requestAnimationFrame(() => next.run());
        }
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open, onClose, filtered, highlight]);

  // spaNav uses pushState + popstate so React Router intercepts the
  // change instead of doing a full page load. Falls back to a full
  // load if the History API is unavailable for some reason.
  function spaNav(url: string) {
    try {
      window.history.pushState({}, '', url);
      window.dispatchEvent(new PopStateEvent('popstate'));
    } catch {
      window.location.href = url;
    }
  }

  return (
    <AnimatePresence>
      {open ? (
        <motion.div
          key="backdrop"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={timing.short}
          onClick={onClose}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 50,
            background: 'rgba(6,8,12,0.55)',
            backdropFilter: 'blur(var(--elev-command-blur))',
            WebkitBackdropFilter: 'blur(var(--elev-command-blur))',
            display: 'grid',
            placeItems: 'center',
            padding: '10vh 16px 0',
          }}
        >
          <motion.div
            key="panel"
            initial={{ scale: 0.96, opacity: 0, y: -8 }}
            animate={{ scale: 1, opacity: 1, y: 0 }}
            exit={{ scale: 0.96, opacity: 0, y: -8 }}
            transition={spring.smooth}
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 'min(560px, 100%)',
              background: 'var(--bg-elevated)',
              border: '1px solid var(--border-strong)',
              borderRadius: 'var(--radius-xl)',
              boxShadow: 'var(--elev-command-shadow)',
              padding: 8,
              maxHeight: '70vh',
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            <input
              autoFocus
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setHighlight(0);
              }}
              placeholder="Jump to agent or /command…"
              style={{
                width: '100%',
                padding: '12px 14px',
                fontSize: 17,
                color: 'var(--text-primary)',
                background: 'transparent',
                border: 'none',
                outline: 'none',
                fontVariantNumeric: 'tabular-nums',
              }}
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
              <span style={{ display: 'inline-flex', alignItems: 'baseline', gap: 4 }}>
                <Metric value={filtered.length} size={11} tone="muted" />
                matches
              </span>
            </div>
            <div
              style={{
                overflowY: 'auto',
                borderTop: '1px solid var(--border-subtle)',
                paddingTop: 6,
              }}
            >
              {filtered.length === 0 ? (
                <div
                  style={{
                    padding: 24,
                    textAlign: 'center',
                    color: 'var(--text-faint)',
                    fontSize: 12,
                  }}
                >
                  No matches.
                </div>
              ) : null}
              {filtered.slice(0, 12).map((action, i) => {
                const active = i === highlight;
                return (
                  <motion.button
                    key={action.id}
                    onClick={() => {
                      onClose();
                      requestAnimationFrame(() => action.run());
                    }}
                    onMouseEnter={() => setHighlight(i)}
                    whileTap={{ scale: 0.99 }}
                    transition={spring.snappy}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 10,
                      width: '100%',
                      padding: '8px 10px',
                      borderRadius: 'var(--radius-md)',
                      background: active ? 'var(--accent-muted)' : 'transparent',
                      border: 'none',
                      cursor: 'pointer',
                      textAlign: 'left',
                    }}
                  >
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
                      }}
                    >
                      {action.glyph}
                    </span>
                    <span style={{ flex: 1, minWidth: 0 }}>
                      <span
                        style={{
                          display: 'block',
                          color: active ? 'var(--accent)' : 'var(--text-primary)',
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
                  </motion.button>
                );
              })}
            </div>
          </motion.div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
