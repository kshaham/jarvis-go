import React from 'react';
import { AnimatePresence, m, useReducedMotion } from 'motion/react';
import { NavLink, useLocation, useNavigate, useOutlet } from 'react-router-dom';
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import {
  Activity,
  ArrowLeftRight,
  BarChart3,
  BookOpen,
  CalendarDays,
  FileStack,
  FileText,
  FlaskConical,
  Images,
  ListTodo,
  Lock,
  Megaphone,
  MessageSquare,
  Network,
  Presentation,
  Send,
  Server,
  Settings,
  Sparkles,
  Users,
  UsersRound,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import { TeamHeader } from './TeamHeader';
import { CommandBar } from './motion/CommandBar';
import { Metric } from './motion/Metric';
import { AuroraBackground } from './ui/AuroraBackground';
import { Button } from './ui/Button';
import { ConnectionBanner } from './ui/ConnectionBanner';
import { TopProgress } from './ui/TopProgress';
import { QuickAddTaskModal } from './QuickAddTaskModal';
import { ShortcutsModal } from './ShortcutsModal';
import { spring } from '../lib/motion';
import { usePeers } from '../hooks/usePeers';
import { useToastBridge } from '../hooks/useToastBridge';
import { useTheme } from '../hooks/useTheme';

const navLinkBaseStyle: React.CSSProperties = {
  textDecoration: 'none',
  display: 'flex',
  alignItems: 'center',
  gap: 10,
  padding: '8px 10px',
  borderRadius: 'var(--radius-md)',
  border: '1px solid transparent',
  fontSize: 14,
  fontWeight: 600,
  position: 'relative',
  // Kill the browser's default (blue) focus outline; .focus-ring supplies a
  // themed keyboard-focus ring via :focus-visible instead.
  outline: 'none',
};

const navActivePillStyle: React.CSSProperties = {
  position: 'absolute',
  inset: 0,
  borderRadius: 'var(--radius-md)',
  background: 'var(--lg-fill-accent)',
  border: '1px solid rgb(var(--accent-rgb) / 0.35)',
  // No --glow-violet-sm bloom — it read as a stray light-green halo behind
  // the active item. Clean accent pill: fill + inset accent bar + specular.
  boxShadow: 'inset 2px 0 0 var(--accent), var(--lg-specular-convex)',
  zIndex: 0,
};

// 24x24 icon badge. A Lucide icon (geometric SVG) sits inside, centered by
// the flex box and its own viewBox — deterministic in every browser, unlike
// the old text-glyph baseline metrics.
const railGlyphStyle: React.CSSProperties = {
  width: 24,
  height: 24,
  boxSizing: 'border-box',
  borderRadius: 6,
  boxShadow: 'inset 0 1px 0 rgba(120,240,180,0.12)',
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  flexShrink: 0,
};

const railButtonStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  padding: '8px 10px',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-secondary)',
  fontSize: 12,
  fontWeight: 600,
  cursor: 'pointer',
};

const navBadgeStyle: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  minWidth: 22,
  height: 18,
  padding: '0 6px',
  borderRadius: 999,
  background: 'var(--status-error)',
  color: '#fff',
  fontSize: 12,
  fontWeight: 700,
};

const classicLinkStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 6,
  padding: '6px 10px',
  borderRadius: 'var(--radius-md)',
  color: 'var(--text-muted)',
  fontSize: 12,
  textDecoration: 'none',
  marginTop: 4,
};

// Per-route document titles (pathname is basename-relative).
const ROUTE_TITLES: Record<string, string> = {
  '/': 'Agents',
  '/tasks': 'Tasks',
  '/tests': 'Tests',
  '/mockups': 'Mockups',
  '/deliverables': 'Deliverables',
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
  icon: LucideIcon;
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
    { to: '/', label: 'Agents', icon: Users },
    { to: '/tasks', label: 'Tasks', icon: ListTodo },
    { to: '/tests', label: 'Tests', icon: FlaskConical },
    { to: '/mockups', label: 'Mockups', icon: Images },
    { to: '/deliverables', label: 'Deliverables', icon: FileStack },
    { to: '/standup', label: 'Standup', icon: Megaphone },
    { to: '/chat', label: 'Chat', icon: MessageSquare },
    { to: '/activity', label: 'Activity', icon: Activity },
    { to: '/whiteboard', label: 'Whiteboard', icon: Presentation },
    { to: '/knowledge', label: 'Knowledge', icon: BookOpen },
    { to: '/reports', label: 'Reports', icon: FileText },
    // Operations
    { to: '/insights', label: 'Insights', icon: Sparkles, divider: true },
    { to: '/metrics', label: 'Metrics', icon: BarChart3 },
    { to: '/meetings', label: 'Meetings', icon: CalendarDays },
    { to: '/network', label: 'Network', icon: Network },
    { to: '/handoffs', label: 'Handoffs', icon: ArrowLeftRight },
    { to: '/dev-instances', label: 'Dev instances', icon: Server },
    { to: '/locks', label: 'File locks', icon: Lock },
    { to: '/settings', label: 'Settings', icon: Settings },
    // Multi-team
    { to: '/teams', label: 'Teams', icon: UsersRound, divider: true },
    { to: '/relay', label: 'Relay', icon: Send, badge: totalUnread || undefined },
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
            background: 'var(--chrome-bg)',
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
          {items.map((item) => {
            const Icon = item.icon;
            return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              // Unselected rows are liquid-glass tiles (.nav-tile: outline +
              // faint fill, brighter on hover) so they separate from the rail;
              // the active row uses the accent pill below instead. focus-ring
              // gives keyboard focus the themed ring.
              className={({ isActive }) => 'focus-ring' + (isActive ? '' : ' nav-tile')}
              style={({ isActive }) => ({
                ...navLinkBaseStyle,
                // Extra gap before a group's first item (Office / Operations /
                // Multi-team) — the spacing separates groups now that each row
                // is its own tile.
                marginTop: item.divider ? 18 : 0,
                color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)',
                // Active background is the sliding layoutId pill below
                // (or a static fill under reduced motion).
                background: isActive && reduce ? 'var(--lg-fill-accent)' : undefined,
              })}
            >
              {({ isActive }) => (
                <>
                  {isActive && !reduce ? (
                    <m.span
                      layoutId="nav-active"
                      transition={spring.smooth}
                      style={navActivePillStyle}
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
                        ...railGlyphStyle,
                        border: '1px solid ' + (isActive ? 'rgb(var(--accent-rgb) / 0.6)' : 'var(--lg-rim)'),
                        background: isActive ? 'rgb(var(--accent-rgb) / 0.92)' : 'var(--lg-fill)',
                        color: isActive ? 'var(--accent-on)' : 'var(--text-secondary)',
                      }}
                    >
                      <Icon size={15} strokeWidth={2} aria-hidden />
                    </span>
                    <span style={{ flex: 1, minWidth: 0 }}>{item.label}</span>
                    {item.badge ? (
                      <span style={navBadgeStyle}>
                        <Metric value={item.badge} size={11} />
                      </span>
                    ) : null}
                  </span>
                </>
              )}
            </NavLink>
            );
          })}
          <div style={{ flex: 1 }} />
          <button
            type="button"
            onClick={() => setQuickAddOpen(true)}
            className="focus-ring lg-button"
            style={railButtonStyle}
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
            type="button"
            onClick={() => setCommandOpen(true)}
            className="focus-ring lg-button"
            style={railButtonStyle}
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
            style={classicLinkStyle}
          >
            <span style={{ opacity: 0.7 }}>↗</span>
            Classic UI
          </a>
        </nav>

        {/* Main content. The persistent shell (header/rail/footer/
            CommandBar) stays mounted; only the routed screen inside
            AnimatedOutlet transitions on navigation. */}
        <main
          className="flex flex-1 flex-col px-6 pb-6 pt-3"
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
      <m.div
        key={location.pathname}
        initial={reduce ? { opacity: 0 } : { opacity: 0, y: 8 }}
        animate={reduce ? { opacity: 1 } : { opacity: 1, y: 0 }}
        exit={reduce ? { opacity: 0 } : { opacity: 0, y: -6 }}
        transition={reduce ? { duration: 0 } : spring.smooth}
        style={{ flex: 1, minHeight: 0, minWidth: 0 }}
      >
        {element}
      </m.div>
    </AnimatePresence>
  );
}
