import NumberFlow, { type Format } from '@number-flow/react';
import { motion, useAnimationControls, useReducedMotion } from 'motion/react';
import { useEffect, useRef } from 'react';
import type { CSSProperties } from 'react';

type Tone = 'primary' | 'muted' | 'accent' | 'success' | 'warning' | 'danger';

type Props = {
  // The number to display. Animates digit-by-digit when this changes.
  value: number;
  // Intl.NumberFormatOptions (minus 'scientific'/'engineering' notation
  // which NumberFlow doesn't support). Use for currency, percent,
  // compact, etc.
  format?: Format;
  // Prepended text (e.g. "$").
  prefix?: string;
  // Appended text (e.g. "ms", "tokens").
  suffix?: string;
  // Token-driven font size. Default 15 (label).
  size?: 11 | 12 | 13 | 15 | 17 | 20 | 24 | 32 | 48;
  // Token-driven color. Default 'primary' (var(--text-primary)).
  tone?: Tone;
  // When true, flash a brief glow + scale pulse whenever the value
  // changes (draws the eye to live updates). Opt-in; off by default so
  // existing call sites are unaffected. No-op under reduced motion.
  pulseOnChange?: boolean;
  className?: string;
  style?: CSSProperties;
};

const TONE_VAR: Record<Tone, string> = {
  primary: 'var(--text-primary)',
  muted: 'var(--text-muted)',
  accent: 'var(--accent)',
  success: 'var(--status-success)',
  warning: 'var(--status-warn)',
  danger: 'var(--status-error)',
};

// Metric — the canonical "this is a number that can change" component.
// Wraps NumberFlow with the office theme — tabular numbers, the right
// font size from the type scale, and tone-aware color. Use anywhere a
// number is shown that ever updates (counts, costs, throughput, uptime).
export function Metric({
  value,
  format,
  prefix,
  suffix,
  size = 15,
  tone = 'primary',
  pulseOnChange = false,
  className,
  style,
}: Props) {
  const reduce = useReducedMotion();
  const controls = useAnimationControls();
  const prev = useRef(value);

  useEffect(() => {
    if (pulseOnChange && !reduce && prev.current !== value) {
      controls.start({
        scale: [1, 1.08, 1],
        textShadow: [
          '0 0 0px rgba(124,92,255,0)',
          '0 0 10px rgba(124,92,255,0.7)',
          '0 0 0px rgba(124,92,255,0)',
        ],
        transition: { duration: 0.5, ease: 'easeOut' },
      });
    }
    prev.current = value;
  }, [value, pulseOnChange, reduce, controls]);

  const numberFlow = (
    <NumberFlow
      value={value}
      format={format}
      prefix={prefix}
      suffix={suffix}
      className={className}
      style={{
        fontSize: size,
        color: TONE_VAR[tone],
        fontVariantNumeric: 'tabular-nums',
        fontFeatureSettings: '"tnum" 1',
        ...style,
      }}
    />
  );

  if (!pulseOnChange) return numberFlow;

  return (
    <motion.span animate={controls} style={{ display: 'inline-block' }}>
      {numberFlow}
    </motion.span>
  );
}
