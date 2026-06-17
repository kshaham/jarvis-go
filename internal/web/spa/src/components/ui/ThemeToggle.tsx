import { Contrast, Droplet, Monitor, Moon, Sun } from 'lucide-react';
import { AnimatePresence, m, useReducedMotion } from 'motion/react';
import type { CSSProperties } from 'react';

import { Press } from '@/components/motion/Press';
import { useTheme, type ThemePref } from '@/hooks/useTheme';

const toggleButtonStyle: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  width: 32,
  height: 32,
  borderRadius: 'var(--radius-md)',
  border: '1px solid var(--lg-rim)',
  background: 'var(--lg-fill)',
  boxShadow: 'var(--lg-shadow-rest)',
  WebkitBackdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
  backdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
  color: 'var(--text-secondary)',
  cursor: 'pointer',
};

const NEXT_LABEL: Record<ThemePref, string> = {
  dark: 'Theme: dark — switch to light',
  light: 'Theme: light — switch to blue',
  blue: 'Theme: blue — switch to graphite',
  graphite: 'Theme: graphite — switch to system',
  system: 'Theme: system — switch to dark',
};

const ICON: Record<ThemePref, typeof Moon> = {
  dark: Moon,
  light: Sun,
  blue: Droplet,
  graphite: Contrast,
  system: Monitor,
};

// ThemeToggle — header button that cycles dark → light → system, with a
// quick rotate/scale crossfade between the moon/sun/monitor glyphs.
export function ThemeToggle() {
  const { theme, cycle } = useTheme();
  const reduce = useReducedMotion();
  const Icon = ICON[theme];
  return (
    <Press
      pressScale={0.9}
      onClick={cycle}
      aria-label={NEXT_LABEL[theme]}
      title={NEXT_LABEL[theme]}
      style={toggleButtonStyle}
    >
      <AnimatePresence mode="wait" initial={false}>
        <m.span
          key={theme}
          initial={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.6, rotate: -30 }}
          animate={reduce ? { opacity: 1 } : { opacity: 1, scale: 1, rotate: 0 }}
          exit={reduce ? { opacity: 0 } : { opacity: 0, scale: 0.6, rotate: 30 }}
          transition={{ duration: 0.18 }}
          style={{ display: 'inline-flex' }}
        >
          <Icon size={15} />
        </m.span>
      </AnimatePresence>
    </Press>
  );
}
