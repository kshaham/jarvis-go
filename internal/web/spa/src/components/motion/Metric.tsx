import NumberFlow, { type Format } from '@number-flow/react';
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
  className,
  style,
}: Props) {
  return (
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
}
