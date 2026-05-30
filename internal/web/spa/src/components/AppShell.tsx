import { AnimatePresence, motion, useReducedMotion } from 'motion/react';
import { NavLink, useLocation, useNavigate, useOutlet } from 'react-router-dom';
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';

import { TeamHeader } from './TeamHeader';
import { CommandBar, Metric } from './motion';
import { AuroraBackground, Button, ConnectionBanner, TopProgress } from './ui';
import { QuickAddTaskModal } from './QuickAddTaskModal';
import { ShortcutsModal } from './ShortcutsModal';
import { spring } from '../lib/motion';
import { usePeers } from '../hooks/usePeers';
import { useToastBridge } from '../hooks/useToastBridge';
import { useTheme } from '../hooks/useTheme';

// Per-route document titles (pathname is basename-relative).
const ROUTE_TITLES: Record<string, string> = {
  '/': 'Agents',
  '/tasks': 'Tasks',
  '/standup': 'Standup',
  '/chat': 'Chat',
  '/activity': 'Activity',
  '/whiteboard': 'Whiteboard',
  '/knowledge': 'Knowledge',
  '/reports': 'Reports',
  '/insights': 'Insights',
  '/metrics': 'Metrics',
  '/meetings': 'Meetings',
  '/network': 'Network',
  '/handoffs': 'Handoffs',
  '/dev-instances': 'Dev instances',
  '/locks': 'File locks',
  '/settings': 'Settings',
  '/spawn': 'Spawn',
  '/teams': 'Teams',
  '/relay': 'Relay',
  '/peers/new': 'Add peer',
};

type NavItem = {
  to: string;
  label: string;
  glyph: string;
  // Optional badge count (e.g. unread relay messages).
  badge?: number;
};

// AppShell is the persistent chrome around every routed screen:
// TeamHeader on top, left-rail navigation, animated outlet body,
// CommandBar mounted globally. The router lives outside this
// component — AppShell is just the layout.
export function AppShell({ children }: { children?: ReactNode }) {
  const peers = usePeers();
  // Live office events → transient toasts. Mounted once, here.
  useToastBridge();
  const totalUnread = (peers.data?.peers ?? []).reduce(
    (sum, p) => sum + (p.unreadInbound ?? 0),
    0,
  );

  const navigate = useNavigate();
  const location = useLocation();
  const reduce = useReducedMotion();
  const { cycle } = useTheme();
  const [commandOpen, setCommandOpen] = useState(false);
  const [quickAddOpen, setQuickAddOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      const inField = target && /^(INPUT|TEXTAREA|SELECT)$/i.test(target.tagName);
      const isMeta = e.metaKey || e.ctrlKey;
      if (isMeta && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        setCommandOpen((open) => !open);
        return;
      }
      if (inField || isMeta) return;
      // `t` opens quick-add task; `?` opens the shortcuts cheat-sheet.
      if (e.key === 't' || e.key === 'T') {
        e.preventDefault();
        setQuickAddOpen(true);
      } else if (e.key === '?') {
        e.preventDefault();
        setShortcutsOpen(true);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  // Per-route document title + scroll-to-top on navigation.
  // Dep on `location` (not `.pathname`) — React Router guarantees a new
  // object reference on every route change, so the effect re-runs reliably.
  useEffect(() => {
    const label = ROUTE_TITLES[location.pathname];
    document.title = label ? `${label} · Jarvis Office` : 'Jarvis · Office';
    window.scrollTo({ top: 0 });
  }, [location]);

  // Three groups in the left rail. Office (the live team's tabs),
  // Operations (the bigger dashboards / panels), Multi-team (peer relay).
  const items: (NavItem & { divider?: boolean })[] = [
    // Office
    { to: '/', label: 'Agents', glyph: 'A' },
    { to: '/tasks', label: 'Tasks', glyph: 'K' },
    { to: '/standup', label: 'Standup', glyph: 'S' },
    { to: '/chat', label: 'Chat', glyph: 'C' },
    { to: '/activity', label: 'Activity', glyph: 'L' },
    { to: '/whiteboard', label: 'Whiteboard', glyph: '▤' },
    { to: '/knowledge', label: 'Knowledge', glyph: '◇' },
    { to: '/reports', label: 'Reports', glyph: '⊟' },
    // Operations
    { to: '/insights', label: 'Insights', glyph: 'I', divider: true },
    { to: '/metrics', label: 'Metrics', glyph: 'M' },
    { to: '/meetings', label: 'Meetings', glyph: '∷' },
    { to: '/network', label: 'Network', glyph: '⌬' },
    { to: '/handoffs', label: 'Handoffs', glyph: 'H' },
    { to: '/dev-instances', label: 'Dev instances', glyph: 'D' },
    { to: '/locks', label: 'File locks', glyph: '⊞' },
    { to: '/settings', label: 'Settings', glyph: 'G' },
    // Multi-team
    { to: '/teams', label: 'Teams', glyph: 'T', divider: true },
    { to: '/relay', label: 'Relay', glyph: 'R', badge: totalUnread || undefined },
  ];

  return (
    <div className="min-h-screen flex flex-col">
      <TopProgress />
      <AuroraBackground />
      <TeamHeader />
      <ConnectionBanner />
      <div className="flex-1 flex" style={{ minHeight: 0 }}>
        {/* Left rail navigation — sticky to the viewport so it stays
            visible as content scrolls. 220px wide, collapses to a
            tighter glyph-only rail under sm. */}
        <nav
          aria-label="Office sections"
          className="hidden sm:flex flex-col gap-1"
          style={{
            width: 220,
            padding: 16,
            borderRight: '1px solid var(--lg-rim)',
            background: 'var(--lg-fill)',
            WebkitBackdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
            backdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
          }}
        >
          {/* Top-of-rail spawn CTA — the action operators reach for
              most often. Routes to the SPA's spawn wizard so the
              classic UI isn't needed for new teams. */}
          <Button
            variant="primary"
            onClick={() => navigate('/spawn')}
            className="w-full justify-center"
            style={{ marginBottom: 10 }}
          >
            + Spawn team
          </Button>
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className="transition-colors hover:bg-[var(--bg-elevated)]"
              style={({ isActive }) => ({
                marginTop: item.divider ? 12 : 0,
                borderTop: item.divider ? '1px solid var(--border-subtle)' : 'none',
                paddingTop: item.divider ? 12 : undefined,
                textDecoration: 'none',
                color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)',
                // Active background is the sliding layoutId pill below
                // (or a static fill under reduced motion).
                background: isActive && reduce ? 'var(--lg-fill-accent)' : undefined,
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '8px 10px',
                borderRadius: 'var(--radius-md)',
                fontSize: 14,
                fontWeight: 600,
                position: 'relative',
              })}
            >
              {({ isActive }) => (
                <>
                  {isActive && !reduce ? (
                    <motion.span
                      layoutId="nav-active"
                      transition={spring.smooth}
                      style={{
                        position: 'absolute',
                        inset: 0,
                        borderRadius: 'var(--radius-md)',
                        background: 'var(--lg-fill-accent)',
                        border: '1px solid rgb(var(--accent-rgb) / 0.35)',
                        boxShadow:
                          'inset 2px 0 0 var(--accent), var(--lg-specular-convex), var(--glow-violet-sm)',
                        zIndex: 0,
                      }}
                    />
                  ) : null}
                  <span
                    style={{
                      position: 'relative',
                      zIndex: 1,
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 10,
                      flex: 1,
                      minWidth: 0,
                    }}
                  >
                    <span
                      style={{
                        width: 24,
                        height: 24,
                        borderRadius: 6,
                        border: '1px solid ' + (isActive ? 'rgb(var(--accent-rgb) / 0.6)' : 'var(--lg-rim)'),
                        background: isActive ? 'rgb(var(--accent-rgb) / 0.92)' : 'var(--lg-fill)',
                        color: isActive ? '#04150e' : 'var(--text-secondary)',
                        boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.14)',
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontSize: 11,
                        fontWeight: 700,
                        flexShrink: 0,
                      }}
                    >
                      {item.glyph}
                    </span>
                    <span style={{ flex: 1, minWidth: 0 }}>{item.label}</span>
                    {item.badge ? (
                      <span
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          minWidth: 22,
                          height: 18,
                          padding: '0 6px',
                          borderRadius: 999,
                          background: 'var(--status-error)',
                          color: '#fff',
                          fontSize: 11,
                          fontWeight: 700,
                        }}
                      >
                        <Metric value={item.badge} size={11} />
                      </span>
                    ) : null}
                  </span>
                </>
              )}
            </NavLink>
          ))}
          <div style={{ flex: 1 }} />
          <button
            onClick={() => setQuickAddOpen(true)}
            className="focus-ring lg-button"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '8px 10px',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-secondary)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                background: 'var(--bg-pressed)',
                padding: '1px 6px',
                borderRadius: 4,
                border: '1px solid var(--border-default)',
              }}
            >
              t
            </span>
            Quick task
          </button>
          <button
            onClick={() => setCommandOpen(true)}
            className="focus-ring lg-button"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '8px 10px',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-secondary)',
              fontSize: 12,
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                background: 'var(--bg-pressed)',
                padding: '1px 6px',
                borderRadius: 4,
                border: '1px solid var(--border-default)',
              }}
            >
              ⌘K
            </span>
            Command bar
          </button>
          {/* Classic UI escape hatch — operators with bookmarks or
              workflows that the SPA hasn't ported yet (kanban, full
              standup transcript editor, knowledge ledger) can still
              reach the vanilla shell. Phase 5: the canonical office
              URL is now /office (which redirects here); /office-classic
              keeps the old shell alive for fallback. */}
          <a
            href="/office-classic"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 10px',
              borderRadius: 'var(--radius-md)',
              color: 'var(--text-muted)',
              fontSize: 11,
              textDecoration: 'none',
              marginTop: 4,
            }}
          >
            <span style={{ opacity: 0.7 }}>↗</span>
            Classic UI
          </a>
        </nav>

        {/* Main content. The persistent shell (header/rail/footer/
            CommandBar) stays mounted; only the routed screen inside
            AnimatedOutlet transitions on navigation. */}
        <main
          className="flex flex-1 flex-col px-6 py-5"
          style={{
            maxWidth: 'var(--width-content, 1180px)',
            width: '100%',
            margin: '0 auto',
            minWidth: 0,
          }}
        >
          {children ?? <AnimatedOutlet />}
        </main>
      </div>
      <footer
        className="px-6 py-3 text-xs"
        style={{
          color: 'var(--text-muted)',
          borderTop: '1px solid var(--border-subtle)',
        }}
      >
        jarvis · /office-v2 (React beta) · press{' '}
        <kbd
          style={{
            fontFamily: 'var(--font-mono)',
            padding: '1px 6px',
            borderRadius: 4,
            background: 'var(--bg-pressed)',
            border: '1px solid var(--border-default)',
          }}
        >
          ⌘K
        </kbd>{' '}
        to jump anywhere ·{' '}
        <a href="/office-classic" style={{ color: 'var(--accent)' }} className="hover:underline">
          classic UI
        </a>
      </footer>
      <CommandBar
        open={commandOpen}
        onClose={() => setCommandOpen(false)}
        actions={{ onNewTask: () => setQuickAddOpen(true), onToggleTheme: cycle }}
      />
      <QuickAddTaskModal open={quickAddOpen} onClose={() => setQuickAddOpen(false)} />
      <ShortcutsModal open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />
    </div>
  );
}

// AnimatedOutlet — captures the current route's element (useOutlet) and
// keys the wrapper by pathname so AnimatePresence animates the outgoing
// screen out before the incoming one springs in. mode="wait" keeps only
// one screen mounted at a time (important for the heavy graph/canvas
// routes). React-19 stable — no experimental ViewTransition. Reduced
// motion collapses to an instant opacity swap. flex-1 + minHeight:0 so
// full-height screens (Network, Whiteboard) still resolve height:100%.
function AnimatedOutlet() {
  const location = useLocation();
  const element = useOutlet();
  const reduce = useReducedMotion();
  return (
    <AnimatePresence mode="wait" initial={false}>
      <motion.div
        key={location.pathname}
        initial={reduce ? { opacity: 0 } : { opacity: 0, y: 8 }}
        animate={reduce ? { opacity: 1 } : { opacity: 1, y: 0 }}
        exit={reduce ? { opacity: 0 } : { opacity: 0, y: -6 }}
        transition={reduce ? { duration: 0 } : spring.smooth}
        style={{ flex: 1, minHeight: 0, minWidth: 0 }}
      >
        {element}
      </motion.div>
    </AnimatePresence>
  );
}
