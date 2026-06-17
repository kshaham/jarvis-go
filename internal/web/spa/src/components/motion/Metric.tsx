import NumberFlow, { type Format } from '@number-flow/react';
import { m, useAnimationControls, useReducedMotion } from 'motion/react';
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

  // Pulse is triggered declaratively via NumberFlow's onAnimationsStart
  // callback rather than a useEffect guard, so there is no effect watching
  // a prop to decide whether to fire a side effect (no-event-handler).
  const handleAnimationsStart = pulseOnChange && !reduce
    ? () => {
        controls.start({
          scale: [1, 1.08, 1],
          textShadow: [
            '0 0 0px rgb(var(--accent-rgb) / 0)',
            '0 0 10px rgb(var(--accent-rgb) / 0.7)',
            '0 0 0px rgb(var(--accent-rgb) / 0)',
          ],
          transition: { duration: 0.5, ease: 'easeOut' },
        });
      }
    : undefined;

  const numberFlow = (
    <NumberFlow
      value={value}
      format={format}
      prefix={prefix}
      suffix={suffix}
      className={className}
      onAnimationsStart={handleAnimationsStart}
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
    <m.span animate={controls} style={{ display: 'inline-block' }}>
      {numberFlow}
    </m.span>
  );
}
